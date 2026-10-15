package server

import (
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
	privileged := role == repo.RoleAdmin || role == repo.RoleRoot
	if p.Hidden && !privileged && !(lk != nil && lk.Phase == "running" && lk.Bank) {
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

	// default 角色全局限速（练习与比赛通用，root 后台可调）
	if role == repo.RoleDefault {
		s, err := h.repo.GetSystemSettings(ctx)
		if err == nil && !h.limiter.allow(uid, s.RateLimitPerMinute, time.Now()) {
			fail(c, http.StatusTooManyRequests,
				"提交过于频繁：每分钟最多 "+strconv.Itoa(s.RateLimitPerMinute)+" 次")
			return
		}
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
	// 隐藏期内的比赛提交只对 root 可见（组织者经比赛页查看）
	_, _, role, _ := currentUser(c)
	includeContest := role == repo.RoleRoot

	list, total, err := h.repo.ListSubmissions(c.Request.Context(), userID, problemID, page, pageSize, includeContest)
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

// GetSubmission 提交详情。可见性规则：
//   - 隐藏期内的比赛提交：仅 root 与比赛创建者可见；
//   - 比赛锁题期间，default 查看自己赛前提交不返回代码（防抄旧代码）；
//   - 代码本体仅本人 / admin / root 可见。
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
	privileged := role == repo.RoleAdmin || role == repo.RoleRoot
	masked := false

	if s.ContestID != 0 {
		ct, err := h.repo.GetContest(ctx, s.ContestID)
		if err == nil && !ct.Published && !(logged && (uid == ct.CreatedBy || role == repo.RoleRoot)) {
			fail(c, http.StatusNotFound, "该提交来自隐藏期内的比赛")
			return
		}
	}
	if s.ExamID != 0 {
		e, err := h.repo.GetExam(ctx, s.ExamID)
		if err == nil && !e.Published && !(logged && (uid == e.CreatedBy || role == repo.RoleRoot)) {
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

	withCode := logged && (uid == s.UID || privileged) && !masked
	ok(c, submissionJSON(s, withCode))
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
	}
	return h
}
