package server

// 讨论区·版务与举报（v0.32 自 discuss.go 五拆，纯搬家行为零变化）：
// 话题管理动作（置顶/加精/锁定 + 组织者升格公开）/ 用户举报（N 人自动折叠）/
// 管理面举报队列与批量处置（dismiss|delete|warn|mute）/ 禁言管理。

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ModDiscussTopic POST /api/discuss/:tid/mod —— 管理动作（置顶/加精/锁定）+ 组织者升格公开。
func (h *Handlers) ModDiscussTopic(c *gin.Context) {
	_, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tid, okID := parseID(c, "tid")
	if !okID {
		return
	}
	t, err := h.repo.GetDiscussTopic(c.Request.Context(), tid)
	if respondRepoErr(c, err, "话题不存在", "读取话题失败") {
		return
	}
	var req struct {
		Pinned   *bool `json:"pinned"`
		Featured *bool `json:"featured"`
		Locked   *bool `json:"locked"`
		Private  *bool `json:"private"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	canManage := hasPerm(c, repo.PermDiscussManage)
	if req.Pinned != nil || req.Featured != nil || req.Locked != nil {
		if !canManage {
			fail(c, http.StatusForbidden, "需要「讨论管理」权限")
			return
		}
	}
	publish := req.Private != nil && !*req.Private && t.Private
	if publish {
		// 升格公开：比赛组织者（或讨论管理）；只升不降
		gate, okG := h.contestDiscussGate(c, t.ScopeID)
		if t.Scope != repo.DiscussScopeContest || !okG || (!gate.Organizer && !canManage) {
			fail(c, http.StatusForbidden, "只有比赛组织者可以公开提问")
			return
		}
	}
	if req.Pinned == nil && req.Featured == nil && req.Locked == nil && req.Private == nil {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.SetDiscussTopicFlags(c.Request.Context(), tid, req.Pinned, req.Featured, req.Locked, req.Private); err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	if publish {
		s, _ := h.repo.GetSystemSettings(c.Request.Context())
		h.notifyOne(c.Request.Context(), t.UID, repo.NotifyInput{
			Typ: repo.NotifyReply, Title: truncRunes(t.Title, 40),
			Body: "组织者已公开答复你的提问", Link: "/discuss/" + fmt.Sprint(tid), RefID: tid,
		}, s, repo.NotifyReply)
	}
	ok(c, gin.H{"updated": tid})
}

// ReportDiscussTarget POST /api/discuss/:tid/report —— 举报（N 人自动折叠）。
func (h *Handlers) ReportDiscussTarget(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tid, okID := parseID(c, "tid")
	if !okID {
		return
	}
	var req struct {
		TargetType string `json:"target_type"` // topic | reply
		TargetID   int64  `json:"target_id"`
		Reason     string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.TargetType != "topic" && req.TargetType != "reply" {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		fail(c, http.StatusBadRequest, "请填写举报理由")
		return
	}
	if utf8.RuneCountInString(reason) > maxDiscussReasonRunes {
		fail(c, http.StatusBadRequest, "举报理由过长")
		return
	}
	targetUID := int64(0)
	if req.TargetType == "topic" {
		if req.TargetID != tid {
			fail(c, http.StatusBadRequest, "参数错误")
			return
		}
		t, err := h.repo.GetDiscussTopic(c.Request.Context(), tid)
		if err != nil {
			fail(c, http.StatusNotFound, "话题不存在")
			return
		}
		targetUID = t.UID
	} else {
		rep, err := h.repo.GetDiscussReply(c.Request.Context(), req.TargetID)
		if err != nil || rep.TID != tid {
			fail(c, http.StatusNotFound, "回复不存在")
			return
		}
		targetUID = rep.UID
	}
	if targetUID == uid {
		fail(c, http.StatusBadRequest, "不能举报自己的内容")
		return
	}
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	count, folded, err := h.repo.AddDiscussReport(c.Request.Context(), req.TargetType, req.TargetID, uid, reason, s.DiscussReportFoldVal())
	if err != nil {
		fail(c, http.StatusInternalServerError, "举报失败")
		return
	}
	ok(c, gin.H{"count": count, "folded": folded})
}

// ---------- 管理面：举报队列 / 禁言 ----------

// AdminDiscussReports GET /api/admin/discuss/reports?status=&page=
func (h *Handlers) AdminDiscussReports(c *gin.Context) {
	status := c.Query("status")
	if status != "" && status != "open" && status != "resolved" {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListDiscussReports(c.Request.Context(), status, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取举报列表失败")
		return
	}
	uids := make([]int64, 0, len(list))
	for _, r := range list {
		uids = append(uids, r.UID)
	}
	users := h.discussAuthors(c.Request.Context(), uids)
	items := make([]gin.H, 0, len(list))
	for _, r := range list {
		item := gin.H{
			"id": r.ID, "target_type": r.TargetType, "target_id": r.TargetID,
			"reason": r.Reason, "status": r.Status, "at": r.At.Format(time.RFC3339),
			"action": r.Action,
		}
		if u, ok := users[r.UID]; ok {
			item["reporter"] = gin.H{"uid": u.ID, "nickname": u.Nickname, "username": u.Username}
		}
		// 目标快照（跳转 + 内容预览）：删除后仍可看到残留标题
		tid := int64(0)
		if r.TargetType == "topic" {
			if t, err := h.repo.GetDiscussTopic(c.Request.Context(), r.TargetID); err == nil {
				tid = t.ID
				item["snippet"] = truncRunes(t.Title, 60)
				item["target_uid"] = t.UID
			}
		} else if rep, err := h.repo.GetDiscussReply(c.Request.Context(), r.TargetID); err == nil {
			tid = rep.TID
			item["snippet"] = truncRunes(rep.Content, 60)
			item["target_uid"] = rep.UID
		}
		item["tid"] = tid
		items = append(items, item)
	}
	ok(c, gin.H{"list": items, "total": total})
}

// AdminResolveDiscussReports POST /api/admin/discuss/reports/resolve
// {ids, action: dismiss|delete|warn|mute, days?, message?}
func (h *Handlers) AdminResolveDiscussReports(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if !hasPerm(c, repo.PermDiscussManage) {
		fail(c, http.StatusForbidden, "需要「讨论管理」权限")
		return
	}
	var req struct {
		IDs     []int64 `json:"ids"`
		Action  string  `json:"action"`
		Days    int     `json:"days"`
		Message string  `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	switch req.Action {
	case "dismiss", "delete", "warn", "mute":
	default:
		fail(c, http.StatusBadRequest, "处置动作不合法")
		return
	}
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	ctx := c.Request.Context()
	list, _, err := h.repo.ListDiscussReports(ctx, "", 1, 1000)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取举报失败")
		return
	}
	byID := map[int64]*repo.DiscussReportItem{}
	for _, r := range list {
		byID[r.ID] = r
	}
	for _, id := range req.IDs {
		r, okR := byID[id]
		if !okR || r.Status != "open" {
			continue
		}
		targetUID := int64(0)
		tid := int64(0)
		switch r.TargetType {
		case "topic":
			if t, err := h.repo.GetDiscussTopic(ctx, r.TargetID); err == nil {
				targetUID, tid = t.UID, t.ID
			}
		case "reply":
			if rep, err := h.repo.GetDiscussReply(ctx, r.TargetID); err == nil {
				targetUID, tid = rep.UID, rep.TID
			}
		}
		link := "/discuss/" + fmt.Sprint(tid)
		switch req.Action {
		case "dismiss":
			_ = h.repo.UnfoldDiscussTarget(ctx, r.TargetType, r.TargetID)
		case "delete":
			if r.TargetType == "topic" {
				_ = h.repo.SoftDeleteDiscussTopic(ctx, r.TargetID, uid)
			} else {
				_ = h.repo.SoftDeleteDiscussReply(ctx, r.TargetID)
			}
			_ = h.repo.AddSecurityLog(ctx, repo.SecurityLog{
				Type: repo.SecDiscussDelete, UID: uid, Username: uname,
				Detail: fmt.Sprintf("举报处置删除 %s #%d（作者 uid=%d）", r.TargetType, r.TargetID, targetUID),
			})
			h.notifyOne(ctx, targetUID, repo.NotifyInput{
				Typ: repo.NotifyWarn, Title: "你发布的内容因举报被删除",
				Body: truncRunes(req.Message, 80), Link: link,
			}, s, "")
		case "warn":
			h.notifyOne(ctx, targetUID, repo.NotifyInput{
				Typ: repo.NotifyWarn, Title: "管理员警告",
				Body: truncRunes(req.Message, 80), Link: link,
			}, s, "")
		case "mute":
			days := req.Days
			reason := req.Message
			if reason == "" {
				reason = "举报处置"
			}
			if err := h.repo.AddDiscussMute(ctx, targetUID, days, reason, uid); err == nil {
				_ = h.repo.AddSecurityLog(ctx, repo.SecurityLog{
					Type: repo.SecDiscussMute, UID: uid, Username: uname,
					Detail: fmt.Sprintf("禁言 uid=%d（%s）", targetUID, muteDaysText(days)),
				})
				h.notifyOne(ctx, targetUID, repo.NotifyInput{
					Typ: repo.NotifyWarn, Title: "你已被禁言（" + muteDaysText(days) + "）",
					Body: reason, Link: link,
				}, s, "")
			}
		}
	}
	if err := h.repo.ResolveDiscussReports(ctx, req.IDs, req.Action, uid); err != nil {
		fail(c, http.StatusInternalServerError, "结案失败")
		return
	}
	ok(c, gin.H{"resolved": len(req.IDs)})
}

func muteDaysText(days int) string {
	if days <= 0 {
		return "永久"
	}
	return fmt.Sprintf("%d 天", days)
}

// AdminDiscussMutes GET /api/admin/discuss/mutes
func (h *Handlers) AdminDiscussMutes(c *gin.Context) {
	list, err := h.repo.ListActiveDiscussMutes(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取禁言列表失败")
		return
	}
	uids := make([]int64, 0, len(list))
	for _, m := range list {
		uids = append(uids, m.UID, m.ByUID)
	}
	users := h.discussAuthors(c.Request.Context(), uids)
	items := make([]gin.H, 0, len(list))
	for _, m := range list {
		item := gin.H{
			"id": m.ID, "uid": m.UID, "until": m.Until.Format(time.RFC3339),
			"permanent": m.Permanent, "reason": m.Reason, "at": m.At.Format(time.RFC3339),
		}
		if u, ok := users[m.UID]; ok {
			item["nickname"] = u.Nickname
			item["username"] = u.Username
		}
		if u, ok := users[m.ByUID]; ok {
			item["by"] = u.Nickname
		}
		items = append(items, item)
	}
	ok(c, gin.H{"list": items})
}

// AdminCreateDiscussMute POST /api/admin/discuss/mutes {uid, days(0=永久), reason}
func (h *Handlers) AdminCreateDiscussMute(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if !hasPerm(c, repo.PermDiscussManage) {
		fail(c, http.StatusForbidden, "需要「讨论管理」权限")
		return
	}
	var req struct {
		UID    int64  `json:"uid"`
		Days   int    `json:"days"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UID <= 0 {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Days < 0 || req.Days > 3650 {
		fail(c, http.StatusBadRequest, "禁言天数需在 0–3650（0 = 永久）")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(reason) > maxDiscussReasonRunes {
		fail(c, http.StatusBadRequest, "理由过长")
		return
	}
	target, err := h.repo.GetUserByID(c.Request.Context(), req.UID)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if err := h.repo.AddDiscussMute(c.Request.Context(), req.UID, req.Days, reason, uid); err != nil {
		fail(c, http.StatusInternalServerError, "禁言失败")
		return
	}
	_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
		Type: repo.SecDiscussMute, UID: uid, Username: uname,
		Detail: fmt.Sprintf("禁言 %s（uid=%d，%s）%s", target.Username, target.ID, muteDaysText(req.Days), reason),
	})
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	h.notifyOne(c.Request.Context(), req.UID, repo.NotifyInput{
		Typ: repo.NotifyWarn, Title: "你已被禁言（" + muteDaysText(req.Days) + "）",
		Body: reason,
	}, s, "")
	ok(c, gin.H{"muted": req.UID})
}

// AdminLiftDiscussMute DELETE /api/admin/discuss/mutes/:id
func (h *Handlers) AdminLiftDiscussMute(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if !hasPerm(c, repo.PermDiscussManage) {
		fail(c, http.StatusForbidden, "需要「讨论管理」权限")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	if err := h.repo.LiftDiscussMute(c.Request.Context(), id); err != nil {
		fail(c, http.StatusInternalServerError, "解除失败")
		return
	}
	_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
		Type: repo.SecDiscussMute, UID: uid, Username: uname,
		Detail: fmt.Sprintf("解除禁言记录 #%d", id),
	})
	ok(c, gin.H{"lifted": id})
}
