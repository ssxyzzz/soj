package server

// 讨论区·全站广播（v0.32 自 discuss.go 五拆，纯搬家行为零变化）：
// notify.broadcast 权限位门槛 + 每日上限 + 全员扇出（走 NotifyUsers，与 1:1 通知分流）。

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// AdminBroadcast POST /api/admin/broadcast {title, body} —— 全站公告广播（notify.broadcast）。
func (h *Handlers) AdminBroadcast(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if !hasPerm(c, repo.PermNotifyBroadcast) {
		fail(c, http.StatusForbidden, "需要「全站公告广播」权限")
		return
	}
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" || body == "" {
		fail(c, http.StatusBadRequest, "标题与内容不能为空")
		return
	}
	if utf8.RuneCountInString(title) > 60 || utf8.RuneCountInString(body) > 500 {
		fail(c, http.StatusBadRequest, "标题上限 60 字、内容上限 500 字")
		return
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	if limit := s.NotifyBroadcastPerDayVal(); limit > 0 {
		if n, err := h.repo.CountBroadcastsToday(c.Request.Context()); err == nil && n >= int64(limit) {
			fail(c, http.StatusTooManyRequests, fmt.Sprintf("今日广播已达上限（%d 条/天）", limit))
			return
		}
	}
	bid, err := h.repo.InsertBroadcastRecord(c.Request.Context(), uid, title, body)
	if err != nil {
		fail(c, http.StatusInternalServerError, "广播失败")
		return
	}
	uids, err := h.repo.AllUserIDs(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "广播失败")
		return
	}
	if err := h.repo.NotifyUsers(c.Request.Context(), uids, repo.NotifyInput{
		Typ: repo.NotifyBroadcast, Title: truncRunes(title, 60), Body: body, RefID: bid,
	}, s.NotifyRetentionDaysVal()); err != nil {
		fail(c, http.StatusInternalServerError, "广播失败")
		return
	}
	_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
		Type: repo.SecBroadcast, UID: uid, Username: uname,
		Detail: fmt.Sprintf("全站广播 #%d：%s（触达 %d 人）", bid, title, len(uids)),
	})
	ok(c, gin.H{"count": len(uids)})
}
