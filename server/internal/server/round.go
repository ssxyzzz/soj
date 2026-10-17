package server

// 比赛/考试同构 handler（v0.5e ① 合并，2026-10-17 差异盘点定稿）。
//
// 差异清单（合并依据，全部差异收敛为四点）：
//  1. 存取集合与文案：kind/name（contests/exams 集合，「比赛」/「考试」）；
//  2. 参赛资格：比赛 = 全体登录用户；考试 = 本班成员（详情/提交/我的提交/榜单多一道成员校验）；
//  3. 创建权限：比赛 = contest.bank.create 权限位；考试 = 班级管理员（classManageable）；
//  4. 详情可见性：比赛挂公开路由（AuthOptional，游客可看）；考试需登录且成员/组织者。
//
// 行为统一（合并时修正的漂移）：提交限速原两处判断不一致（比赛 = rate_exempt 权限位，
// 考试 = role==default 旧判断），统一为 rate_exempt 权限位——自定义角色在考试中提交
// 不再被误限速。另删除 ContestMySubmissions 里 classId!=0 的迁移期遗留死代码。

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/repo"
)

type roundScope struct {
	Kind string // repo.KindContest | repo.KindExam
	Name string // 错误与提示文案
}

var (
	scopeContest = roundScope{Kind: repo.KindContest, Name: "比赛"}
	scopeExam    = roundScope{Kind: repo.KindExam, Name: "考试"}
)

// ---------- 共享创建 ----------

// roundCreateReq 创建请求（比赛/考试字段一致，FileIO 见 v0.5b-3）。
type roundCreateReq struct {
	Title      string    `json:"title" binding:"required"`
	Intro      string    `json:"intro"`
	Start      time.Time `json:"start" binding:"required"`
	End        time.Time `json:"end" binding:"required"`
	HiddenDays int       `json:"hidden_days"`
	ProblemIDs []int64   `json:"problem_ids" binding:"required"`
	FileIO     bool      `json:"file_io"`
}

// validateRoundCreate 校验创建参数；ok=false 时已写响应。
func (h *Handlers) validateRoundCreate(c *gin.Context, s roundScope, req *roundCreateReq) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return false
	}
	req.Title = strings.TrimSpace(req.Title)
	switch {
	case req.Title == "":
		fail(c, http.StatusBadRequest, s.Name+"名称不能为空")
		return false
	case !req.Start.Before(req.End):
		fail(c, http.StatusBadRequest, "开始时间必须早于结束时间")
		return false
	case req.End.Sub(req.Start) > repo.MaxContestDuration:
		fail(c, http.StatusBadRequest, s.Name+"时长最长 7 天")
		return false
	case req.HiddenDays < 0 || req.HiddenDays > int(repo.MaxHiddenDays):
		fail(c, http.StatusBadRequest, "赛后隐藏期最长 7 天")
		return false
	case len(req.Intro) > maxStatementSize:
		fail(c, http.StatusBadRequest, s.Name+"说明过长（上限 256KB）")
		return false
	}
	return true
}

// collectRoundProblems 题目去重与存在性校验；ok=false 时已写响应。
func (h *Handlers) collectRoundProblems(c *gin.Context, s roundScope, ids []int64) ([]repo.ContestProblem, bool) {
	ctx := c.Request.Context()
	seen := map[int64]bool{}
	problems := make([]repo.ContestProblem, 0, len(ids))
	for _, pid := range ids {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if _, err := h.repo.GetProblem(ctx, pid); err != nil {
			fail(c, http.StatusNotFound, "题目不存在: "+strconv.FormatInt(pid, 10))
			return nil, false
		}
		problems = append(problems, repo.ContestProblem{PID: pid, Score: 100})
	}
	return problems, true
}

// ---------- 薄包装：创建（权限语义不同，各自校验后走共享流程） ----------

// CreateContest 创建主题库比赛（contest.bank.create 权限位；班级赛事走考试接口）。
func (h *Handlers) CreateContest(c *gin.Context) {
	if !hasPerm(c, repo.PermContestBankCreate) {
		fail(c, http.StatusForbidden, "需要「创建主题库比赛」权限")
		return
	}
	h.createRound(c, scopeContest, 0)
}

// CreateExam 发起考试（班级管理员）。
func (h *Handlers) CreateExam(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可发起考试")
		return
	}
	h.createRound(c, scopeExam, classID)
}

func (h *Handlers) createRound(c *gin.Context, s roundScope, classID int64) {
	uid, _, _, _ := currentUser(c)
	var req roundCreateReq
	if !h.validateRoundCreate(c, s, &req) {
		return
	}
	problems, valid := h.collectRoundProblems(c, s, req.ProblemIDs)
	if !valid {
		return
	}
	ct := &repo.Contest{
		ClassID:   classID,
		Title:     req.Title,
		Intro:     req.Intro,
		Start:     req.Start,
		End:       req.End,
		Problems:  problems,
		CreatedBy: uid,
		FileIO:    req.FileIO,
	}
	if req.HiddenDays > 0 {
		ct.HiddenUntil = req.End.AddDate(0, 0, req.HiddenDays)
	}
	if err := h.repo.CreateRound(c.Request.Context(), s.Kind, ct); err != nil {
		fail(c, http.StatusInternalServerError, "创建"+s.Name+"失败")
		return
	}
	ok(c, gin.H{"id": ct.ID})
}

// ---------- 共享详情 ----------

// roundDetailJSON 组装详情响应与题目列表（参赛资格判定由调用方给出 participant）。
func (h *Handlers) roundDetailJSON(c *gin.Context, s roundScope, ct *repo.Contest, participant bool) {
	ctx := c.Request.Context()
	uid, _, role, logged := currentUser(c)
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	now := time.Now()
	status := ct.Status(now)

	resp := gin.H{
		"id":            ct.ID,
		"title":         ct.Title,
		"intro":         ct.Intro,
		"class_id":      ct.ClassID,
		"status":        status,
		"start":         ct.Start,
		"end":           ct.End,
		"problem_count": len(ct.Problems),
		"participant":   participant,
		"is_organizer":  isOrganizer,
		"deletable":     isOrganizer && status == "scheduled",
		"file_io":       ct.FileIO, // 本场启用文件 IO（v0.5b-3）
	}
	if !ct.HiddenUntil.IsZero() {
		resp["hidden_until"] = ct.HiddenUntil
	}
	// 题目列表：赛中对参赛者可见；赛前只给组织者看
	if isOrganizer || (status != "scheduled" && participant) {
		problems := make([]gin.H, 0, len(ct.Problems))
		for _, cp := range ct.Problems {
			title := ""
			if p, err := h.repo.GetProblem(ctx, cp.PID); err == nil {
				title = p.Title
			}
			problems = append(problems, gin.H{"id": cp.PID, "title": title, "score": cp.Score})
		}
		resp["problems"] = problems
	}
	ok(c, resp)
}

// ContestDetail 比赛详情（公开路由，游客可看；参赛者 = 全体登录用户）。
func (h *Handlers) ContestDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ct, err := h.repo.GetRound(c.Request.Context(), repo.KindContest, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return
	}
	_, _, _, logged := currentUser(c)
	h.roundDetailJSON(c, scopeContest, ct, logged)
}

// ExamDetail 考试详情（需登录；仅本班成员与组织者可见）。
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
	e, err := h.repo.GetRound(c.Request.Context(), repo.KindExam, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取考试失败")
		return
	}
	member, _ := h.repo.IsMember(c.Request.Context(), e.ClassID, uid)
	isOrganizer := uid == e.CreatedBy || role == repo.RoleRoot
	if !member && !isOrganizer {
		fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
		return
	}
	h.roundDetailJSON(c, scopeExam, e, member)
}

// ---------- 共享删除 / 说明 / 场景开关（组织者语义一致，仅 kind 与文案不同） ----------

func (h *Handlers) roundOrganizerOnly(c *gin.Context, s roundScope) (*repo.Contest, bool) {
	id, okID := parseID(c, "id")
	if !okID {
		return nil, false
	}
	uid, _, role, _ := currentUser(c)
	ct, err := h.repo.GetRound(c.Request.Context(), s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return nil, false
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return nil, false
	}
	if !(uid == ct.CreatedBy || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "只有"+s.Name+"创建者可以操作")
		return nil, false
	}
	return ct, true
}

func (h *Handlers) roundDelete(c *gin.Context, s roundScope) {
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	if ct.Status(time.Now()) != "scheduled" {
		fail(c, http.StatusBadRequest, "只能删除未开始的"+s.Name)
		return
	}
	if err := h.repo.DeleteRound(c.Request.Context(), s.Kind, ct.ID); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": ct.ID})
}

func (h *Handlers) roundSetIntro(c *gin.Context, s roundScope) {
	var req struct {
		Intro string `json:"intro" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, s.Name+"说明不能为空")
		return
	}
	if len(req.Intro) > maxStatementSize {
		fail(c, http.StatusBadRequest, s.Name+"说明过长（上限 256KB）")
		return
	}
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	if err := h.repo.SetRoundIntro(c.Request.Context(), s.Kind, ct.ID, req.Intro); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"id": ct.ID})
}

func (h *Handlers) roundSetFileIO(c *gin.Context, s roundScope) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	if err := h.repo.SetRoundFileIO(c.Request.Context(), s.Kind, ct.ID, *req.Enabled); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"file_io": *req.Enabled})
}

func (h *Handlers) DeleteContest(c *gin.Context) { h.roundDelete(c, scopeContest) }
func (h *Handlers) DeleteExam(c *gin.Context)    { h.roundDelete(c, scopeExam) }

// SetContestIntro 编辑比赛说明（Markdown，创建者/root）。
func (h *Handlers) SetContestIntro(c *gin.Context) { h.roundSetIntro(c, scopeContest) }

// SetExamIntro 编辑考试说明（Markdown，组织者/root）。
func (h *Handlers) SetExamIntro(c *gin.Context) { h.roundSetIntro(c, scopeExam) }

// SetContestFileIO 设置比赛「本场启用文件 IO」场景开关（创建者/root，中途可改：
// 改动只影响之后的提交，此前提交的结果不回滚——需要时用后续的重测功能）。
func (h *Handlers) SetContestFileIO(c *gin.Context) { h.roundSetFileIO(c, scopeContest) }

// SetExamFileIO 设置考试「本场启用文件 IO」场景开关（语义同比赛）。
func (h *Handlers) SetExamFileIO(c *gin.Context) { h.roundSetFileIO(c, scopeExam) }

// ---------- 共享提交 ----------

func (h *Handlers) roundSubmit(c *gin.Context, s roundScope) {
	uid, uname, _, _ := currentUser(c)
	id, okID := parseID(c, "id")
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
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if s.Kind == repo.KindExam {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member {
			fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
			return
		}
	}
	if ct.Status(time.Now()) != "running" {
		fail(c, http.StatusBadRequest, s.Name+"未开始或已结束，不能提交")
		return
	}
	if _, inRound := ct.ProblemScore(pid); !inRound {
		fail(c, http.StatusBadRequest, "该题不在本场"+s.Name+"中")
		return
	}
	if !lang.Exists(req.Language) {
		fail(c, http.StatusBadRequest, "不支持的语言")
		return
	}

	// 全局限速；持有「提交不限速」权限位者豁免
	if !hasPerm(c, repo.PermSubmissionRateExempt) {
		if st, err := h.repo.GetSystemSettings(ctx); err == nil && !h.limiter.allow(uid, st.RateLimitPerMinute, time.Now()) {
			fail(c, http.StatusTooManyRequests,
				"提交过于频繁：每分钟最多 "+strconv.Itoa(st.RateLimitPerMinute)+" 次")
			return
		}
	}

	p, err := h.repo.GetProblem(ctx, pid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	var ctArg, eArg *repo.Contest
	if s.Kind == repo.KindExam {
		eArg = ct
	} else {
		ctArg = ct
	}
	sid, err := h.repo.CreateSubmission(ctx, uid, uname, p, req.Language, req.Code, ctArg, eArg)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建提交失败")
		return
	}
	ok(c, gin.H{"id": sid})
}

// SubmitToContest 比赛页提交入口（赛时唯一接受该题提交的通道）。
func (h *Handlers) SubmitToContest(c *gin.Context) { h.roundSubmit(c, scopeContest) }

// SubmitToExam 考试页提交入口（考时唯一接受该题提交的通道）。
func (h *Handlers) SubmitToExam(c *gin.Context) { h.roundSubmit(c, scopeExam) }

// ---------- 共享「我的提交」 ----------

func (h *Handlers) roundMySubmissions(c *gin.Context, s roundScope) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if s.Kind == repo.KindExam {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member {
			fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
			return
		}
	}
	revealed := ct.Published || uid == ct.CreatedBy || role == repo.RoleRoot

	subs, err := h.repo.RoundSubmissions(ctx, s.Kind, ct.ID, uid)
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
		// 赛时：只显示提交记录，不出分
		items = append(items, gin.H{"id": s.ID, "pid": s.ProblemID, "created_at": s.CreatedAt})
	}
	ok(c, gin.H{"revealed": revealed, "list": items})
}

// ContestMySubmissions 我的比赛提交：赛时只显示提交次数（不出分），隐藏期结束或组织者可见全部。
func (h *Handlers) ContestMySubmissions(c *gin.Context) { h.roundMySubmissions(c, scopeContest) }

// ExamMySubmissions 我的考试提交：考时只显示提交次数（不出分），组织者/root 或发布后可见全部。
func (h *Handlers) ExamMySubmissions(c *gin.Context) { h.roundMySubmissions(c, scopeExam) }

// ---------- 共享「用户提交明细」（榜单单元格点看） ----------

// roundUserSubmissions 查看指定用户在某比赛/考试某题的全部提交。
// 门槛：组织者本人 ∨「查看用户提交明细」权限位（root 恒过）。
func (h *Handlers) roundUserSubmissions(c *gin.Context, s roundScope) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	pid, okPID := parseID(c, "pid")
	if !okPID {
		return
	}
	targetUID := int64(atoiDefault(c.Query("uid"), 0))
	if targetUID <= 0 {
		fail(c, http.StatusBadRequest, "缺少 uid 参数")
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if uid != ct.CreatedBy && !hasPerm(c, repo.PermSubmissionViewUser) {
		fail(c, http.StatusForbidden, "需要「查看用户提交明细」权限或为该"+s.Name+"组织者")
		return
	}
	subs, err := h.repo.RoundUserProblemSubmissions(ctx, s.Kind, ct.ID, targetUID, pid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}
	items := make([]gin.H, 0, len(subs))
	for _, s := range subs {
		items = append(items, submissionJSON(s, true))
	}
	ok(c, gin.H{"list": items})
}

// ContestUserSubmissions 比赛「用户提交明细」。
func (h *Handlers) ContestUserSubmissions(c *gin.Context) { h.roundUserSubmissions(c, scopeContest) }

// ExamUserSubmissions 考试「用户提交明细」。
func (h *Handlers) ExamUserSubmissions(c *gin.Context) { h.roundUserSubmissions(c, scopeExam) }

// ---------- 共享榜单 ----------

func (h *Handlers) roundLeaderboard(c *gin.Context, s roundScope) {
	uid, _, role, logged := currentUser(c)
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if s.Kind == repo.KindExam {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member {
			fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
			return
		}
	}
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	if !ct.Published && !isOrganizer {
		fail(c, http.StatusForbidden, "榜单暂不可见")
		return
	}

	cacheKey := s.Kind + "-" + strconv.FormatInt(ct.ID, 10)
	h.leaderMu.Lock()
	entry, cached := h.leaderCache[cacheKey]
	h.leaderMu.Unlock()
	if !cached || (!ct.Published && time.Since(entry.at) > 30*time.Second) {
		rows, err := h.repo.RoundLeaderboard(ctx, s.Kind, ct.ID)
		if err != nil {
			fail(c, http.StatusInternalServerError, "计算榜单失败")
			return
		}
		items := make([]gin.H, 0, len(rows))
		for i, r := range rows {
			probs := make([]gin.H, 0, len(ct.Problems))
			for _, cp := range ct.Problems {
				cell := r.Problems[cp.PID]
				if cell == nil {
					probs = append(probs, gin.H{"pid": cp.PID, "score": 0, "tries": 0})
					continue
				}
				probs = append(probs, gin.H{"pid": cp.PID, "score": cell.Score, "tries": cell.Tries})
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
		entry = leaderEntry{at: time.Now(), published: ct.Published, rows: items}
		h.leaderMu.Lock()
		h.leaderCache[cacheKey] = entry
		h.leaderMu.Unlock()
	}
	ok(c, entry.rows)
}

// ContestLeaderboard 比赛榜单：赛时与隐藏期内仅组织者/root 可见。
func (h *Handlers) ContestLeaderboard(c *gin.Context) { h.roundLeaderboard(c, scopeContest) }

// ExamLeaderboard 考试榜单：考时与隐藏期内仅组织者/root 可见。
func (h *Handlers) ExamLeaderboard(c *gin.Context) { h.roundLeaderboard(c, scopeExam) }

// ---------- 考试列表（班级维度） ----------

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
