package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/repo"
	"soj/internal/testdata"
)

const maxCodeSize = 64 << 10 // 64KB

// provider 惰性构造本地存储实现（DataDir 不变，可复用）。
func (h *Handlers) provider() testdata.Provider {
	if h.store == nil {
		h.store = testdata.NewLocalFS(h.cfg.DataDir)
	}
	return h.store
}

// CreateSubmission 校验并落库一条提交，判题线程池会自动领取评测。
func (h *Handlers) CreateSubmission(c *gin.Context) {
	uid, uname, role, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		ProblemID int64  `json:"problem_id" binding:"required"`
		Language  string `json:"language" binding:"required"`
		Code      string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if !lang.Exists(req.Language) {
		fail(c, http.StatusBadRequest, "不支持的语言")
		return
	}
	if len(req.Code) > maxCodeSize {
		fail(c, http.StatusBadRequest, "代码过长（上限 64KB）")
		return
	}

	ctx := c.Request.Context()
	p, err := h.repo.GetProblem(ctx, req.ProblemID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}

	// 题目隐藏 + 比赛锁题判定（见 DEVELOPMENT.md「题目」「比赛」节）
	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	lk := lockMap[p.ID]
	privileged := hasPerm(c, repo.PermProblemViewAll)
	// v0.7：主题库隐藏题任何非特权场景都不可见/不可提交（备题用）
	if p.Hidden && !privileged {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if lk != nil {
		what := "比赛"
		if lk.Kind == repo.KindExam {
			what = "考试"
		}
		if lk.Phase == "running" {
			fail(c, http.StatusBadRequest, "该题正在"+what+"中，请从"+what+"页面提交")
		} else {
			fail(c, http.StatusBadRequest, "该题已被"+what+"锁定，暂不可提交")
		}
		return
	}

	// 全局限速（练习与比赛通用，root 后台可调）；持有「提交不限速」权限位者豁免
	if !hasPerm(c, repo.PermSubmissionRateExempt) {
		s, err := h.repo.GetSystemSettings(ctx)
		if err == nil && !h.limiter.allow(uid, s.RateLimitPerMinute, time.Now()) {
			fail(c, http.StatusTooManyRequests,
				"提交过于频繁：每分钟最多 "+strconv.Itoa(s.RateLimitPerMinute)+" 次")
			return
		}
	}

	// 无测试数据的空壳题（v0.6.1 验收反馈）：与前端禁提交同一文案
	if len(p.Cases) == 0 {
		fail(c, http.StatusBadRequest, "本题暂无数据，无法评测。请联系管理员上传数据")
		return
	}

	id, err := h.repo.CreateSubmission(ctx, uid, uname, p, req.Language, req.Code, nil, nil)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建提交失败")
		return
	}
	log.Printf("[web] 用户 %s 提交 #%d (题目 %d, %s)", uname, id, req.ProblemID, req.Language)
	ok(c, gin.H{"id": id})
}

func (h *Handlers) ListSubmissions(c *gin.Context) {
	page, pageSize := queryPage(c)
	var userID, problemID int64
	if v, err := strconv.ParseInt(c.Query("user_id"), 10, 64); err == nil {
		userID = v
	}
	if v, err := strconv.ParseInt(c.Query("problem_id"), 10, 64); err == nil {
		problemID = v
	}
	// 隐藏期赛事提交的可见性已并入「查看全部提交」权限位（2026-10-16 定稿），
	// 路由 RequirePerm 把门后这里不再过滤
	list, total, err := h.repo.ListSubmissions(c.Request.Context(), userID, problemID, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交记录失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, s := range list {
		items = append(items, submissionJSON(s, false))
	}
	ok(c, gin.H{"list": items, "total": total})
}

// GetSubmission 提交详情。可见性规则（隐藏期可见性 2026-10-16 并入 submission.view.all）：
//   - 隐藏期内的比赛/考试提交：创建者（组织者）与持「查看全部提交」权限位者可见；
//   - 赛事锁题期间，赛前提交对 default 不回代码（防抄，含本人），优先级最高；
//   - 代码：本人或持「查看全部提交」权限位者恒可；其他人拿到该题满分后解锁查看。
func (h *Handlers) GetSubmission(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	s, err := h.repo.GetSubmission(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "提交不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}

	uid, _, role, logged := currentUser(c) // 挂了 AuthOptional
	privileged := hasPerm(c, repo.PermSubmissionViewAll)
	masked := false

	if s.ContestID != 0 {
		ct, err := h.repo.GetRound(ctx, repo.KindContest, s.ContestID)
		if err == nil && !ct.Published && !(logged && (uid == ct.CreatedBy || privileged)) {
			fail(c, http.StatusNotFound, "该提交来自隐藏期内的比赛")
			return
		}
	}
	if s.ExamID != 0 {
		e, err := h.repo.GetRound(ctx, repo.KindExam, s.ExamID)
		if err == nil && !e.Published && !(logged && (uid == e.CreatedBy || privileged)) {
			fail(c, http.StatusNotFound, "该提交来自隐藏期内的考试")
			return
		}
	}
	if logged && !privileged {
		lockMap, err := h.loadLockMap(ctx, uid, role)
		if err == nil {
			if lk := lockMap[s.ProblemID]; lk != nil && s.CreatedAt.Before(lk.Round.Start) {
				masked = true // 赛前旧提交：锁定期内不回代码
			}
		}
	}

	withCode := false
	if logged && !masked {
		switch {
		case uid == s.UID || privileged:
			withCode = true
		case s.Published:
			if solved, serr := h.repo.HasSolved(ctx, uid, s.ProblemID); serr == nil && solved {
				withCode = true // 该题满分解锁：可看他人代码
			}
		}
	}
	resp := submissionJSON(s, withCode)
	if masked {
		resp["masked"] = true
	}
	ok(c, resp)
}

// ListProblemSubmissions 该题所有人的提交（含代码）。
// 门槛（2026-10-15 定稿）：admin/root 天然可看；其他人拿到该题满分后解锁。
func (h *Handlers) ListProblemSubmissions(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, _, logged := currentUser(c)
	privileged := hasPerm(c, repo.PermSubmissionViewAll)
	solved := false
	if logged {
		if privileged {
			solved = true
		} else {
			solved, _ = h.repo.HasSolved(c.Request.Context(), uid, id)
		}
	}
	if !solved {
		fail(c, http.StatusForbidden, "该题拿到满分后可查看他人的代码")
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListPidSubmissions(c.Request.Context(), id, 0, page, pageSize, c.Query("sort"), c.Query("lang"))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, s := range list {
		items = append(items, submissionJSON(s, true))
	}
	ok(c, gin.H{"list": items, "total": total})
}

// MyProblemSubmissions 我在该题的提交记录（自己的结果对自己恒可见）。
func (h *Handlers) MyProblemSubmissions(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListPidSubmissions(c.Request.Context(), id, uid, page, pageSize, "", "")
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, s := range list {
		items = append(items, submissionJSON(s, true))
	}
	ok(c, gin.H{"list": items, "total": total})
}

func submissionJSON(s *repo.Submission, withCode bool) gin.H {
	h := gin.H{
		"id":            s.ID,
		"user_id":       s.UID,
		"username":      s.Username,
		"problem_id":    s.ProblemID,
		"problem_title": s.ProblemTitle,
		"language":      s.Language,
		"status":        s.Status,
		"time_ms":       s.TimeMS,
		"memory_kb":     s.MemKB,
		"score":         s.Score,
		"error_info":    s.ErrorInfo,
		"created_at":    s.CreatedAt,
		"contest_id":    s.ContestID,
		"exam_id":       s.ExamID,
	}
	if s.ContestTitle != "" {
		h["contest_title"] = s.ContestTitle
	}
	if s.ExamTitle != "" {
		h["exam_title"] = s.ExamTitle
	}
	if withCode {
		h["code"] = s.Code
		h["detail"] = s.Detail // 测试点明细与代码同门槛（本人/总表/满分解锁）
	}
	return h
}

// RejudgeSubmission 重测一条提交（教师级权限）：重置 Pending 交还判题线程池自动重评。
func (h *Handlers) RejudgeSubmission(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "无效的提交 ID")
		return
	}
	err = h.repo.RejudgeSubmission(c.Request.Context(), id)
	if errors.Is(err, repo.ErrJudging) {
		fail(c, http.StatusConflict, "该提交正在评测中，稍后再试")
		return
	}
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "提交不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "重测失败")
		return
	}
	ok(c, gin.H{"id": id})
}
