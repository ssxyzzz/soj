package server

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/model"
)

const maxCodeSize = 64 << 10 // 64KB

// CreateSubmission 校验并落库一条提交，然后投递到 RabbitMQ 交给判题 worker。
func (h *Handlers) CreateSubmission(c *gin.Context) {
	uid, uname, _, okAuth := currentUser(c)
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
	if _, err := model.GetProblem(ctx, req.ProblemID); model.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}

	id, err := model.CreateSubmission(ctx, uid, req.ProblemID, req.Language, req.Code)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建提交失败")
		return
	}
	if err := h.pub.PublishSubmission(ctx, id); err != nil {
		// 队列不可用时把提交置为 System Error，避免前端无限轮询 Pending
		_ = model.FinishSubmission(ctx, id, model.StatusSystemError, 0, 0, 0, "消息队列不可用，请稍后重试")
		fail(c, http.StatusInternalServerError, "提交失败：消息队列不可用")
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

	list, total, err := model.ListSubmissions(c.Request.Context(), userID, problemID, page, pageSize)
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

// GetSubmission 提交详情；代码仅本人或管理员可见。
func (h *Handlers) GetSubmission(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	s, err := model.GetSubmission(ctx, id)
	if model.IsNotFound(err) {
		fail(c, http.StatusNotFound, "提交不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}

	uid, _, role, logged := currentUser(c) // 挂了 AuthOptional：有登录态时本人/管理员可见代码
	withCode := false
	if logged && (uid == s.UserID || role == "admin") {
		if code, err := model.GetSubmissionCode(ctx, id); err == nil {
			s.Code = code
			withCode = true
		}
	}
	ok(c, submissionJSON(s, withCode))
}

func submissionJSON(s *model.Submission, withCode bool) gin.H {
	h := gin.H{
		"id":            s.ID,
		"user_id":       s.UserID,
		"username":      s.Username,
		"problem_id":    s.ProblemID,
		"problem_title": s.ProblemTitle,
		"language":      s.Language,
		"status":        s.Status,
		"time_ms":       s.TimeMS,
		"memory_kb":     s.MemoryKB,
		"score":         s.Score,
		"error_info":    s.ErrorInfo,
		"created_at":    s.CreatedAt,
	}
	if withCode {
		h["code"] = s.Code
	}
	return h
}
