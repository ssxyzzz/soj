package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/repo"
)

// ---------- 锁题评估 ----------

// contestLock 对当前视角生效的题目锁。锁的传播范围跟赛事层级走：
// 主题库比赛对 root 以外的所有人生效；班级考试只对本班成员生效。
type contestLock struct {
	Round *repo.Contest // 比赛与考试同构，Kind 区分
	Phase string        // scheduled | running | hidden（已结束未发布）
	Bank  bool          // 主题库比赛（隐藏题赛中临时解锁、全局指引）
	Kind  string        // contest | exam
	RefID int64         // 比赛/考试 ID（前端指引跳转用）
}

// loadLockMap 当前视角下各题目的锁。未发布赛事数量小，每次请求全量评估。
func (h *Handlers) loadLockMap(ctx context.Context, uid int64, role string) (map[int64]*contestLock, error) {
	contests, err := h.repo.ListUnpublishedContests(ctx)
	if err != nil {
		return nil, err
	}
	exams, err := h.repo.ListUnpublishedExams(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := map[int64]*contestLock{}

	apply := func(pid int64, lk *contestLock) {
		old, ok := out[pid]
		// 主题库比赛优先于班级考试
		if !ok || (lk.Bank && !old.Bank) {
			out[pid] = lk
		}
	}
	phaseOf := func(start, end time.Time) string {
		p := ""
		switch {
		case now.Before(start):
			p = "scheduled"
		case now.Before(end):
			p = "running"
		default:
			p = "hidden"
		}
		return p
	}

	for _, ct := range contests {
		phase := phaseOf(ct.Start, ct.End)
		if role == repo.RoleRoot {
			continue // 主题库比赛对 root 不锁
		}
		for _, cp := range ct.Problems {
			apply(cp.PID, &contestLock{Round: ct, Phase: phase, Bank: true, Kind: repo.KindContest, RefID: ct.ID})
		}
	}
	for _, e := range exams {
		if uid <= 0 {
			continue
		}
		if member, _ := h.repo.IsMember(ctx, e.ClassID, uid); !member {
			continue
		}
		phase := phaseOf(e.Start, e.End)
		for _, ep := range e.Problems {
			apply(ep.PID, &contestLock{Round: e, Phase: phase, Bank: false, Kind: repo.KindExam, RefID: e.ID})
		}
	}
	return out, nil
}

// ---------- 提交限速（default 角色，root 后台可调） ----------

type rateLimiter struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{hits: map[int64][]time.Time{}} }

func (l *rateLimiter) allow(uid int64, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	fresh := l.hits[uid][:0]
	for _, t := range l.hits[uid] {
		if now.Sub(t) < time.Minute {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= limit {
		l.hits[uid] = fresh
		return false
	}
	l.hits[uid] = append(fresh, now)
	return true
}

// ---------- 比赛 ----------

// CreateContest 创建主题库比赛（仅 root；班级赛事走考试接口）。
func (h *Handlers) CreateContest(c *gin.Context) {
	uid, _, role, _ := currentUser(c)
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
		fail(c, http.StatusBadRequest, "比赛名称不能为空")
		return
	}
	if !req.Start.Before(req.End) {
		fail(c, http.StatusBadRequest, "开始时间必须早于结束时间")
		return
	}
	if req.End.Sub(req.Start) > repo.MaxContestDuration {
		fail(c, http.StatusBadRequest, "比赛时长最长 7 天")
		return
	}
	if req.HiddenDays < 0 || req.HiddenDays > int(repo.MaxHiddenDays) {
		fail(c, http.StatusBadRequest, "赛后隐藏期最长 7 天")
		return
	}
	if role != repo.RoleRoot {
		fail(c, http.StatusForbidden, "仅 root 可创建主题库比赛")
		return
	}

	ctx := c.Request.Context()
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

	ct := &repo.Contest{
		Title:     req.Title,
		Intro:     req.Intro,
		Start:     req.Start,
		End:       req.End,
		Problems:  problems,
		CreatedBy: uid,
	}
	if req.HiddenDays > 0 {
		ct.HiddenUntil = req.End.AddDate(0, 0, req.HiddenDays)
	}
	if err := h.repo.CreateContest(ctx, ct); err != nil {
		fail(c, http.StatusInternalServerError, "创建比赛失败")
		return
	}
	ok(c, gin.H{"id": ct.ID})
}

// ListContests 主题库比赛列表（班级赛事见 /classes/:id/exams）。
func (h *Handlers) ListContests(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	list, err := h.repo.ListContests(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return
	}
	now := time.Now()
	items := make([]gin.H, 0, len(list))
	for _, ct := range list {
		item := gin.H{
			"id":            ct.ID,
			"title":         ct.Title,
			"status":        ct.Status(now),
			"start":         ct.Start,
			"end":           ct.End,
			"problem_count": len(ct.Problems),
			"is_organizer":  logged && uid == ct.CreatedBy,
		}
		if !ct.HiddenUntil.IsZero() {
			item["hidden_until"] = ct.HiddenUntil
		}
		items = append(items, item)
	}
	ok(c, items)
}

func (h *Handlers) ContestDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetContest(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return
	}

	uid, _, role, logged := currentUser(c)
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	participant := logged // 主题库比赛面向全体登录用户
	now := time.Now()
	status := ct.Status(now)

	resp := gin.H{
		"id":            ct.ID,
		"title":         ct.Title,
		"intro":         ct.Intro,
		"status":        status,
		"start":         ct.Start,
		"end":           ct.End,
		"problem_count": len(ct.Problems),
		"participant":   participant,
		"is_organizer":  isOrganizer,
		"deletable":     isOrganizer && status == "scheduled",
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

func (h *Handlers) DeleteContest(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, _ := currentUser(c)
	ctx := c.Request.Context()
	ct, err := h.repo.GetContest(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	if !(uid == ct.CreatedBy || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "只有比赛创建者可以删除")
		return
	}
	if ct.Status(time.Now()) != "scheduled" {
		fail(c, http.StatusBadRequest, "只能删除未开始的比赛")
		return
	}
	if err := h.repo.DeleteContest(ctx, id); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": id})
}

// SubmitToContest 比赛页提交入口（赛时唯一接受该题提交的通道）。
func (h *Handlers) SubmitToContest(c *gin.Context) {
	uid, uname, role, _ := currentUser(c)
	contestID, okID := parseID(c, "id")
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
	ct, err := h.repo.GetContest(ctx, contestID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	if ct.Status(time.Now()) != "running" {
		fail(c, http.StatusBadRequest, "比赛未开始或已结束，不能提交")
		return
	}
	if _, inContest := ct.ProblemScore(pid); !inContest {
		fail(c, http.StatusBadRequest, "该题不在本场比赛中")
		return
	}
	if !lang.Exists(req.Language) {
		fail(c, http.StatusBadRequest, "不支持的语言")
		return
	}

	// default 角色限速（练习与比赛通用）
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
	sid, err := h.repo.CreateSubmission(ctx, uid, uname, p, req.Language, req.Code, ct, nil)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建提交失败")
		return
	}
	ok(c, gin.H{"id": sid})
}

// ContestMySubmissions 我的比赛提交：赛时只显示提交次数（不出分），隐藏期结束或组织者可见全部。
func (h *Handlers) ContestMySubmissions(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	contestID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetContest(ctx, contestID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	if ct.ClassID != 0 {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member {
			fail(c, http.StatusForbidden, "该比赛仅限本班成员参赛")
			return
		}
	}
	revealed := ct.Published || uid == ct.CreatedBy || role == repo.RoleRoot

	subs, err := h.repo.RoundSubmissions(ctx, repo.KindContest, ct.ID, uid)
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

// ContestLeaderboard 比赛榜单：赛时与隐藏期内仅组织者/root 可见。
func (h *Handlers) ContestLeaderboard(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	contestID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetContest(ctx, contestID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	if !ct.Published && !isOrganizer {
		fail(c, http.StatusForbidden, "榜单暂不可见")
		return
	}

	h.leaderMu.Lock()
	entry, cached := h.leaderCache["contest-"+strconv.FormatInt(ct.ID, 10)]
	h.leaderMu.Unlock()
	if !cached || (!ct.Published && time.Since(entry.at) > 30*time.Second) {
		rows, err := h.repo.ContestLeaderboard(ctx, ct)
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
		h.leaderCache["contest-"+strconv.FormatInt(ct.ID, 10)] = entry
		h.leaderMu.Unlock()
	}
	ok(c, entry.rows)
}

// ---------- 班级多管理员 ----------

// SetMemberRole 设置班级成员角色（建班人、班级管理员或 root；owner 角色不可改）。
func (h *Handlers) SetMemberRole(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	targetUID, okUID := parseID(c, "uid")
	if !okUID {
		return
	}
	var req struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if req.Role != repo.ClassRoleAdmin && req.Role != repo.ClassRoleMember {
		fail(c, http.StatusBadRequest, "角色只能设为 admin 或 member")
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可以设置成员角色")
		return
	}
	if targetUID == cl.OwnerID {
		fail(c, http.StatusBadRequest, "不能修改建班人的角色")
		return
	}
	if err := h.repo.SetClassMemberRole(ctx, classID, targetUID, req.Role); err != nil {
		fail(c, http.StatusInternalServerError, "设置失败")
		return
	}
	ok(c, gin.H{"updated": targetUID, "role": req.Role})
}
