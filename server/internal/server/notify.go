package server

// 站内通知中心（v0.24）handler：顶栏铃铛角标（轮询）、消息中心列表（未读/类型筛选）、
// 已读（单条/全部）、邮件转发偏好（仅 1:1 互动类，见 repo/notify.go）。

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ---------- 发布/开赛事件扇出（v0.24） ----------
// 作业→本班成员、考试→本班成员、比赛→全站（主题库比赛无报名机制，全站广播型）。
// 扇出走独立 goroutine + 独立超时 ctx：通知失败绝不阻塞创建主流程。

func (h *Handlers) notifyRoundCreated(ct *repo.Contest, kind string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := h.repo.GetSystemSettings(ctx)
	if err != nil {
		return
	}
	what, typ := "比赛", repo.NotifyContest
	var uids []int64
	if kind == repo.KindExam {
		what, typ = "考试", repo.NotifyExam
		if uids, err = h.repo.ClassMemberIDs(ctx, ct.ClassID); err != nil {
			return
		}
	} else if uids, err = h.repo.AllUserIDs(ctx); err != nil {
		return
	}
	_ = h.repo.NotifyUsers(ctx, uids, repo.NotifyInput{
		Typ:   typ,
		Title: what + "「" + truncRunes(ct.Title, 40) + "」已发布",
		Body:  "开始时间 " + ct.Start.Format("2006-01-02 15:04"),
		RefID: ct.ID,
	}, s.NotifyRetentionDaysVal())
}

func (h *Handlers) notifyAssignmentCreated(classID int64, title string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := h.repo.GetSystemSettings(ctx)
	if err != nil {
		return
	}
	uids, err := h.repo.ClassMemberIDs(ctx, classID)
	if err != nil {
		return
	}
	_ = h.repo.NotifyUsers(ctx, uids, repo.NotifyInput{
		Typ:   repo.NotifyAssignment,
		Title: "作业「" + truncRunes(title, 40) + "」已发布",
		Body:  "老师布置了新作业，快去完成",
		RefID: classID,
	}, s.NotifyRetentionDaysVal())
}

// notifyStartLoop 开赛/开考提醒扫描（v0.24）：每分钟扫一遍 startNotified 未标记、
// 将在 15 分钟内开始的场次（含重启后 1 小时内错过的补发），扇出后落标记防重发。
func (h *Handlers) notifyStartLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.scanRoundStarts(ctx)
		}
	}
}

func (h *Handlers) scanRoundStarts(ctx context.Context) {
	rounds, err := h.repo.ListRoundsStartingSoon(ctx, 15*time.Minute)
	if err != nil || len(rounds) == 0 {
		return
	}
	s, err := h.repo.GetSystemSettings(ctx)
	if err != nil {
		return
	}
	for _, ct := range rounds {
		kind, what, typ := repo.KindContest, "比赛", repo.NotifyContest
		var uids []int64
		var err error
		if ct.ClassID > 0 {
			kind, what, typ = repo.KindExam, "考试", repo.NotifyExam
			if uids, err = h.repo.ClassMemberIDs(ctx, ct.ClassID); err != nil {
				continue
			}
		} else if uids, err = h.repo.AllUserIDs(ctx); err != nil {
			continue
		}
		if err := h.repo.NotifyUsers(ctx, uids, repo.NotifyInput{
			Typ:   typ,
			Title: what + "「" + truncRunes(ct.Title, 40) + "」即将开始",
			Body:  "开始时间 " + ct.Start.Format("15:04"),
			RefID: ct.ID,
		}, s.NotifyRetentionDaysVal()); err != nil {
			continue
		}
		_ = h.repo.MarkRoundStartNotified(ctx, kind, ct.ID)
	}
}

// notificationJSON 通知下发视图。
func notificationJSON(n *repo.Notification) gin.H {
	item := gin.H{
		"id":         n.ID,
		"type":       n.Typ,
		"title":      n.Title,
		"body":       n.Body,
		"link":       n.Link,
		"agg_count":  n.AggCount,
		"read":       n.Read,
		"created_at": n.CreatedAt.Format(time.RFC3339),
	}
	if n.AggCount < 1 {
		item["agg_count"] = int64(1)
	}
	return item
}

// NotificationBadge GET /api/me/notifications/badge —— 顶栏未读角标（前端 60s 轮询）。
func (h *Handlers) NotificationBadge(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	n, err := h.repo.UnreadNotificationCount(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取通知失败")
		return
	}
	ok(c, gin.H{"unread": n})
}

// ListNotifications GET /api/me/notifications?filter=all|unread&type=&page=
func (h *Handlers) ListNotifications(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	unreadOnly := c.Query("filter") == "unread"
	typ := c.Query("type")
	if typ != "" {
		valid := false
		for _, t := range []string{
			repo.NotifyReply, repo.NotifyMention, repo.NotifyLike, repo.NotifyAccept,
			repo.NotifyAssignment, repo.NotifyExam, repo.NotifyContest,
			repo.NotifyBroadcast, repo.NotifyWarn,
		} {
			if t == typ {
				valid = true
				break
			}
		}
		if !valid {
			typ = ""
		}
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListNotifications(c.Request.Context(), uid, unreadOnly, typ, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取通知失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, n := range list {
		items = append(items, notificationJSON(n))
	}
	unread, _ := h.repo.UnreadNotificationCount(c.Request.Context(), uid)
	ok(c, gin.H{"list": items, "total": total, "unread": unread})
}

// ReadNotifications POST /api/me/notifications/read —— {ids: []} 指定条目；{} 或 {all:true} 全部。
func (h *Handlers) ReadNotifications(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ids := req.IDs
	if req.All {
		ids = nil
	}
	if err := h.repo.MarkNotificationsRead(c.Request.Context(), uid, ids); err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	unread, _ := h.repo.UnreadNotificationCount(c.Request.Context(), uid)
	ok(c, gin.H{"unread": unread})
}

// GetNotifyPrefs GET /api/me/notify-prefs —— 邮件转发偏好。
func (h *Handlers) GetNotifyPrefs(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	u, err := h.repo.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取偏好失败")
		return
	}
	on := map[string]bool{}
	if u.Prefs != nil {
		for _, cat := range u.Prefs.NotifyEmail {
			on[cat] = true
		}
	}
	cats := make([]gin.H, 0, len(repo.NotifyMailCategories))
	for _, cat := range repo.NotifyMailCategories {
		cats = append(cats, gin.H{"type": cat, "enabled": on[cat]})
	}
	ok(c, gin.H{"categories": cats, "has_email": u.Email != ""})
}

// UpdateNotifyPrefs PUT /api/me/notify-prefs {email: [types]}
func (h *Handlers) UpdateNotifyPrefs(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Email []string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	valid := map[string]bool{}
	for _, cat := range repo.NotifyMailCategories {
		valid[cat] = true
	}
	cats := make([]string, 0, len(req.Email))
	for _, cat := range req.Email {
		if valid[cat] {
			cats = append(cats, cat)
		}
	}
	if err := h.repo.UpdateNotifyEmailPrefs(c.Request.Context(), uid, cats); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"saved": true})
}
