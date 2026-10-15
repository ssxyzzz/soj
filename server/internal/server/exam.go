package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/repo"
)

// ---------- 班级考试（规则与主题库比赛一致，仅作用域为班级） ----------

// CreateExam 发起考试（班级管理员）。
func (h *Handlers) CreateExam(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Title      string    `json:"title" binding:"required"`
		Intro      string    `json:"intro"`
		Start      time.Time `json:"start" binding:"required"`
		End        time.Time `json:"end" binding:"required"`
		HiddenDays int       `json:"hidden_days"`
		ProblemIDs []int64   `json:"problem_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		fail(c, http.StatusBadRequest, "考试名称不能为空")
		return
	}
	if !req.Start.Before(req.End) {
		fail(c, http.StatusBadRequest, "开始时间必须早于结束时间")
		return
	}
	if req.End.Sub(req.Start) > repo.MaxContestDuration {
		fail(c, http.StatusBadRequest, "考试时长最长 7 天")
		return
	}
	if req.HiddenDays < 0 || req.HiddenDays > int(repo.MaxHiddenDays) {
		fail(c, http.StatusBadRequest, "赛后隐藏期最长 7 天")
		return
	}

	uid, _, _, _ := currentUser(c)
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可发起考试")
		return
	}

	seen := map[int64]bool{}
	problems := make([]repo.ContestProblem, 0, len(req.ProblemIDs))
	for _, pid := range req.ProblemIDs {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if _, err := h.repo.GetProblem(ctx, pid); err != nil {
			fail(c, http.StatusNotFound, "题目不存在: "+strconv.FormatInt(pid, 10))
			return
		}
		problems = append(problems, repo.ContestProblem{PID: pid, Score: 100})
	}

	e := &repo.Exam{
		ClassID:   classID,
		Title:     req.Title,
		Intro:     req.Intro,
		Start:     req.Start,
		End:       req.End,
		Problems:  problems,
		CreatedBy: uid,
	}
	if req.HiddenDays > 0 {
		e.HiddenUntil = req.End.AddDate(0, 0, req.HiddenDays)
	}
	if err := h.repo.CreateExam(ctx, e); err != nil {
		fail(c, http.StatusInternalServerError, "创建考试失败")
		return
	}
	ok(c, gin.H{"id": e.ID})
}

// ListClassExams 班级考试列表；非本班成员返回空列表。
func (h *Handlers) ListClassExams(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, logged := currentUser(c)
	accessible := h.assignmentAccessible(c.Request.Context(), classID, uid, role, logged)
	if !accessible {
		ok(c, gin.H{"visible": false, "list": []gin.H{}})
		return
	}
	list, err := h.repo.ListClassExams(c.Request.Context(), classID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取考试失败")
		return
	}
	now := time.Now()
	items := make([]gin.H, 0, len(list))
	for _, e := range list {
		item := gin.H{
			"id":            e.ID,
			"title":         e.Title,
			"intro":         e.Intro,
			"status":        e.Status(now),
			"start":         e.Start,
			"end":           e.End,
			"problem_count": len(e.Problems),
			"is_organizer":  uid == e.CreatedBy,
		}
		if !e.HiddenUntil.IsZero() {
			item["hidden_until"] = e.HiddenUntil
		}
		items = append(items, item)
	}
	ok(c, gin.H{"visible": true, "list": items})
}

// ExamDetail 考试详情。
func (h *Handlers) ExamDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	ctx := c.Request.Context()
	e, err := h.repo.GetExam(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取考试失败")
		return
	}
	member, _ := h.repo.IsMember(ctx, e.ClassID, uid)
	isOrganizer := uid == e.CreatedBy || role == repo.RoleRoot
	if !member && !isOrganizer {
		fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
		return
	}
	now := time.Now()
	status := e.Status(now)

	resp := gin.H{
		"id":            e.ID,
		"title":         e.Title,
		"intro":         e.Intro,
		"class_id":      e.ClassID,
		"status":        status,
		"start":         e.Start,
		"end":           e.End,
		"problem_count": len(e.Problems),
		"participant":   member,
		"is_organizer":  isOrganizer,
		"deletable":     isOrganizer && status == "scheduled",
	}
	if !e.HiddenUntil.IsZero() {
		resp["hidden_until"] = e.HiddenUntil
	}
	// 题目列表：赛中对本班学生可见；赛前只给组织者看
	if isOrganizer || (status != "scheduled" && member) {
		problems := make([]gin.H, 0, len(e.Problems))
		for _, ep := range e.Problems {
			title := ""
			if p, err := h.repo.GetProblem(ctx, ep.PID); err == nil {
				title = p.Title
			}
			problems = append(problems, gin.H{"id": ep.PID, "title": title, "score": ep.Score})
		}
		resp["problems"] = problems
	}
	ok(c, resp)
}

// DeleteExam 删除未开始的考试。
func (h *Handlers) DeleteExam(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, _ := currentUser(c)
	ctx := c.Request.Context()
	e, err := h.repo.GetExam(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if !(uid == e.CreatedBy || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "只有考试创建者可以删除")
		return
	}
	if e.Status(time.Now()) != "scheduled" {
		fail(c, http.StatusBadRequest, "只能删除未开始的考试")
		return
	}
	if err := h.repo.DeleteExam(ctx, id); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": id})
}

// SubmitToExam 考试页提交入口（考时唯一接受该题提交的通道）。
func (h *Handlers) SubmitToExam(c *gin.Context) {
	uid, uname, role, _ := currentUser(c)
	examID, okID := parseID(c, "id")
	if !okID {
		return
	}
	pid, okPID := parseID(c, "pid")
	if !okPID {
		return
	}
	var req struct {
		Language string `json:"language" binding:"required"`
		Code     string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if len(req.Code) > maxCodeSize {
		fail(c, http.StatusBadRequest, "代码过长（上限 64KB）")
		return
	}

	ctx := c.Request.Context()
	e, err := h.repo.GetExam(ctx, examID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if member, _ := h.repo.IsMember(ctx, e.ClassID, uid); !member {
		fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
		return
	}
	if e.Status(time.Now()) != "running" {
		fail(c, http.StatusBadRequest, "考试未开始或已结束，不能提交")
		return
	}
	if _, inExam := e.ProblemScore(pid); !inExam {
		fail(c, http.StatusBadRequest, "该题不在本场考试中")
		return
	}
	if !lang.Exists(req.Language) {
		fail(c, http.StatusBadRequest, "不支持的语言")
		return
	}

	// default 角色限速（练习与考试通用）
	if role == repo.RoleDefault {
		s, err := h.repo.GetSystemSettings(ctx)
		if err == nil && !h.limiter.allow(uid, s.RateLimitPerMinute, time.Now()) {
			fail(c, http.StatusTooManyRequests,
				"提交过于频繁：每分钟最多 "+strconv.Itoa(s.RateLimitPerMinute)+" 次")
			return
		}
	}

	p, err := h.repo.GetProblem(ctx, pid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	sid, err := h.repo.CreateSubmission(ctx, uid, uname, p, req.Language, req.Code, nil, e)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建提交失败")
		return
	}
	ok(c, gin.H{"id": sid})
}

// ExamMySubmissions 我的考试提交：考时只显示提交次数（不出分），组织者/root 或发布后可见全部。
func (h *Handlers) ExamMySubmissions(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	examID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	e, err := h.repo.GetExam(ctx, examID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if member, _ := h.repo.IsMember(ctx, e.ClassID, uid); !member {
		fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
		return
	}
	revealed := e.Published || uid == e.CreatedBy || role == repo.RoleRoot

	subs, err := h.repo.RoundSubmissions(ctx, repo.KindExam, e.ID, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}
	items := make([]gin.H, 0, len(subs))
	for _, s := range subs {
		if revealed {
			items = append(items, submissionJSON(s, s.UID == uid))
			continue
		}
		items = append(items, gin.H{"id": s.ID, "pid": s.ProblemID, "created_at": s.CreatedAt})
	}
	ok(c, gin.H{"revealed": revealed, "list": items})
}

// ExamLeaderboard 考试榜单：考时与隐藏期内仅组织者/root 可见。
func (h *Handlers) ExamLeaderboard(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	examID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	e, err := h.repo.GetExam(ctx, examID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if member, _ := h.repo.IsMember(ctx, e.ClassID, uid); !member {
		fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
		return
	}
	isOrganizer := logged && (uid == e.CreatedBy || role == repo.RoleRoot)
	if !e.Published && !isOrganizer {
		fail(c, http.StatusForbidden, "榜单暂不可见")
		return
	}

	cacheKey := "exam-" + strconv.FormatInt(e.ID, 10)
	h.leaderMu.Lock()
	entry, cached := h.leaderCache[cacheKey]
	h.leaderMu.Unlock()
	if !cached || (!e.Published && time.Since(entry.at) > 30*time.Second) {
		rows, err := h.repo.ExamLeaderboard(ctx, e)
		if err != nil {
			fail(c, http.StatusInternalServerError, "计算榜单失败")
			return
		}
		items := make([]gin.H, 0, len(rows))
		for i, r := range rows {
			probs := make([]gin.H, 0, len(e.Problems))
			for _, ep := range e.Problems {
				cell := r.Problems[ep.PID]
				if cell == nil {
					probs = append(probs, gin.H{"pid": ep.PID, "score": 0, "tries": 0})
					continue
				}
				probs = append(probs, gin.H{"pid": ep.PID, "score": cell.Score, "tries": cell.Tries})
			}
			item := gin.H{
				"rank":     i + 1,
				"uid":      r.UID,
				"username": r.Username,
				"nickname": r.Nickname,
				"total":    r.Total,
				"problems": probs,
			}
			if r.HasScoring {
				item["last_scoring_at"] = r.LastScoring
			}
			items = append(items, item)
		}
		entry = leaderEntry{at: time.Now(), published: e.Published, rows: items}
		h.leaderMu.Lock()
		h.leaderCache[cacheKey] = entry
		h.leaderMu.Unlock()
	}
	ok(c, entry.rows)
}
