package server

// 讨论区·话题 CRUD（v0.32 自 discuss.go 五拆，纯搬家行为零变化）：
//   - 三区挂载面的列表与发帖——导航栏 global / 题内 problem / 比赛 contest；
//   - 通用话题操作：详情（视角门槛裁剪）/ 作者编辑 / 软删除。
// 闸门与公共体在 discuss.go；回复互动在 discuss_replies.go。

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ---------- 导航栏讨论区（global） ----------

// ListGlobalDiscuss GET /api/discuss?category=&q=&page=
func (h *Handlers) ListGlobalDiscuss(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	resp := gin.H{"enabled": s.DiscussGlobalOpen() && !s.DiscussGlobalLocked}
	if !s.DiscussGlobalOpen() {
		resp["disabled_reason"] = "讨论区已关闭"
	} else if s.DiscussGlobalLocked {
		resp["disabled_reason"] = "讨论区临时锁定（考试期间），考试结束后恢复"
	}
	kind := strings.TrimSpace(c.Query("category"))
	q := strings.TrimSpace(c.Query("q"))
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListGlobalTopics(c.Request.Context(), kind, q, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取讨论列表失败")
		return
	}
	ids := make([]int64, 0, len(list))
	for _, t := range list {
		ids = append(ids, t.ID)
	}
	liked, _ := h.repo.LikedDiscussTargets(c.Request.Context(), uid, "topic", ids)
	users := h.discussAuthors(c.Request.Context(), authorUIDsTopics(list))
	items := make([]gin.H, 0, len(list))
	for _, t := range list {
		items = append(items, topicJSON(t, users, liked, true))
	}
	resp["list"] = items
	resp["total"] = total
	ok(c, resp)
}

// CreateGlobalDiscuss POST /api/discuss
func (h *Handlers) CreateGlobalDiscuss(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	if !s.DiscussGlobalOpen() {
		fail(c, http.StatusForbidden, "讨论区已关闭")
		return
	}
	if s.DiscussGlobalLocked {
		fail(c, http.StatusForbidden, "讨论区临时锁定（考试期间），考试结束后恢复")
		return
	}
	if !h.discussMuteCheck(c, uid) {
		return
	}
	if msg := h.discussRateCheck(c, uid, true, s); msg != "" {
		fail(c, http.StatusTooManyRequests, msg)
		return
	}
	// 新号冷静期：仅拦导航栏主楼（题内求助不受限，别把提问的新生挡在外面）
	if hours := s.DiscussNewbieHoursVal(); hours > 0 {
		if u, err := h.repo.GetUserByID(c.Request.Context(), uid); err == nil &&
			time.Since(u.CreatedAt) < time.Duration(hours)*time.Hour {
			fail(c, http.StatusForbidden, fmt.Sprintf("注册未满 %d 小时，暂不能在讨论区发帖（可在题目下提问）", hours))
			return
		}
	}
	var b discussTopicBody
	if err := c.ShouldBindJSON(&b); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if msg := validateTopicBody(repo.DiscussScopeGlobal, &b); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	now := time.Now()
	t := &repo.DiscussTopic{
		Scope: repo.DiscussScopeGlobal, ScopeID: 0, UID: uid,
		Kind: b.Kind, Title: strings.TrimSpace(b.Title), Content: b.Content,
		RelatedPids: b.RelatedPids, ListID: b.ListID, TagID: b.TagID,
		CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	if t.RelatedPids == nil {
		t.RelatedPids = []int64{}
	}
	if err := h.repo.InsertDiscussTopic(c.Request.Context(), t); err != nil {
		fail(c, http.StatusInternalServerError, "发布失败")
		return
	}
	ok(c, gin.H{"id": t.ID})
}

// ---------- 题内讨论（problem） ----------

// ListProblemDiscuss GET /api/problems/:id/discuss?page=
func (h *Handlers) ListProblemDiscuss(c *gin.Context) {
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	gate, okG := h.problemDiscussGate(c, pid)
	if !okG {
		return
	}
	uid, _, _, _ := currentUser(c)
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListScopeTopics(c.Request.Context(),
		repo.DiscussScopeProblem, pid, uid, false, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取讨论列表失败")
		return
	}
	ids := make([]int64, 0, len(list))
	for _, t := range list {
		ids = append(ids, t.ID)
	}
	liked, _ := h.repo.LikedDiscussTargets(c.Request.Context(), uid, "topic", ids)
	users := h.discussAuthors(c.Request.Context(), authorUIDsTopics(list))
	items := make([]gin.H, 0, len(list))
	for _, t := range list {
		items = append(items, topicJSON(t, users, liked, true))
	}
	ok(c, gin.H{
		"list": items, "total": total,
		"code_threshold": gate.Threshold,
		"can_view_code":  gate.CanViewCode,
		"locked":         false,
	})
}

// CreateProblemDiscuss POST /api/problems/:id/discuss
func (h *Handlers) CreateProblemDiscuss(c *gin.Context) {
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	if _, okG := h.problemDiscussGate(c, pid); !okG {
		return
	}
	uid, _, _, _ := currentUser(c)
	if !h.discussMuteCheck(c, uid) {
		return
	}
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	if msg := h.discussRateCheck(c, uid, true, s); msg != "" {
		fail(c, http.StatusTooManyRequests, msg)
		return
	}
	var b discussTopicBody
	if err := c.ShouldBindJSON(&b); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if msg := validateTopicBody(repo.DiscussScopeProblem, &b); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	now := time.Now()
	t := &repo.DiscussTopic{
		Scope: repo.DiscussScopeProblem, ScopeID: pid, UID: uid,
		Kind: b.Kind, Title: strings.TrimSpace(b.Title), Content: b.Content,
		CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	if err := h.repo.InsertDiscussTopic(c.Request.Context(), t); err != nil {
		fail(c, http.StatusInternalServerError, "发布失败")
		return
	}
	ok(c, gin.H{"id": t.ID})
}

// ---------- 比赛讨论（contest） ----------

// ListContestDiscuss GET /api/contests/:id/discuss?page=
func (h *Handlers) ListContestDiscuss(c *gin.Context) {
	cid, okID := parseID(c, "id")
	if !okID {
		return
	}
	gate, okG := h.contestDiscussGate(c, cid)
	if !okG {
		return
	}
	uid, _, _, _ := currentUser(c)
	page, pageSize := queryPage(c)
	// 组织者看全部（含赛时私密提问）；普通视角看公开帖 + 自己的私密帖；赛后全量公开
	list, total, err := h.repo.ListScopeTopics(c.Request.Context(),
		repo.DiscussScopeContest, cid, uid, gate.Organizer || gate.Ended, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取讨论列表失败")
		return
	}
	ids := make([]int64, 0, len(list))
	for _, t := range list {
		ids = append(ids, t.ID)
	}
	liked, _ := h.repo.LikedDiscussTargets(c.Request.Context(), uid, "topic", ids)
	users := h.discussAuthors(c.Request.Context(), authorUIDsTopics(list))
	items := make([]gin.H, 0, len(list))
	for _, t := range list {
		items = append(items, topicJSON(t, users, liked, true))
	}
	ok(c, gin.H{
		"list": items, "total": total,
		"running": gate.Running, "ended": gate.Ended, "is_organizer": gate.Organizer,
		"title": gate.Round.Title,
	})
}

// CreateContestDiscuss POST /api/contests/:id/discuss
func (h *Handlers) CreateContestDiscuss(c *gin.Context) {
	cid, okID := parseID(c, "id")
	if !okID {
		return
	}
	gate, okG := h.contestDiscussGate(c, cid)
	if !okG {
		return
	}
	uid, _, _, _ := currentUser(c)
	if !h.discussMuteCheck(c, uid) {
		return
	}
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	if msg := h.discussRateCheck(c, uid, true, s); msg != "" {
		fail(c, http.StatusTooManyRequests, msg)
		return
	}
	var b discussTopicBody
	if err := c.ShouldBindJSON(&b); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if msg := validateTopicBody(repo.DiscussScopeContest, &b); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	// 赛时形态：学生提问强制私密（仅组织者可见）；组织者公告公开。赛前/赛后一律公开。
	private := false
	if gate.Running && !gate.Organizer {
		private = true
	}
	now := time.Now()
	t := &repo.DiscussTopic{
		Scope: repo.DiscussScopeContest, ScopeID: cid, UID: uid,
		Kind: b.Kind, Title: strings.TrimSpace(b.Title), Content: b.Content,
		Private: private, CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	if err := h.repo.InsertDiscussTopic(c.Request.Context(), t); err != nil {
		fail(c, http.StatusInternalServerError, "发布失败")
		return
	}
	ok(c, gin.H{"id": t.ID, "private": private})
}

// ---------- 通用话题操作 ----------

// replyJSON 单条回复下发视图。
func replyJSON(rep *repo.DiscussReply, users map[int64]repo.User, liked map[int64]bool, threshold int, canViewCode bool) gin.H {
	content := rep.Content
	if !canViewCode {
		content = stripDiscussCodeBlocks(content, threshold)
	}
	item := gin.H{
		"id":         rep.ID,
		"tid":        rep.TID,
		"uid":        rep.UID,
		"content":    content,
		"reply_to":   rep.ReplyTo,
		"like_count": rep.LikeCount,
		"accepted":   rep.Accepted,
		"folded":     rep.Folded,
		"created_at": rep.CreatedAt.Format(time.RFC3339),
		"liked":      liked[rep.ID],
	}
	if rep.Edited {
		item["edited"] = true
	}
	if rep.Deleted {
		item["deleted"] = true
		item["content"] = ""
	}
	if u, ok := users[rep.UID]; ok {
		item["nickname"] = u.Nickname
		item["username"] = u.Username
	}
	return item
}

// topicJSON 主题帖下发视图。
func topicJSON(t *repo.DiscussTopic, users map[int64]repo.User, liked map[int64]bool, withAuthor bool) gin.H {
	item := gin.H{
		"id":                t.ID,
		"scope":             t.Scope,
		"scope_id":          t.ScopeID,
		"uid":               t.UID,
		"kind":              t.Kind,
		"title":             t.Title,
		"reply_count":       t.ReplyCount,
		"like_count":        t.LikeCount,
		"pinned":            t.Pinned,
		"featured":          t.Featured,
		"locked":            t.Locked,
		"folded":            t.Folded,
		"official":          t.Official,
		"accepted_reply_id": t.AcceptedReplyID,
		"created_at":        t.CreatedAt.Format(time.RFC3339),
		"last_active_at":    t.LastActiveAt.Format(time.RFC3339),
		"liked":             liked[t.ID],
	}
	if t.Private {
		item["private"] = true
	}
	if !t.PublicAt.IsZero() {
		item["public_at"] = t.PublicAt.Format(time.RFC3339)
	}
	if t.Edited {
		item["edited"] = true
	}
	if t.Deleted {
		item["deleted"] = true
	}
	if withAuthor {
		if u, ok := users[t.UID]; ok {
			item["nickname"] = u.Nickname
			item["username"] = u.Username
		}
	}
	if len(t.RelatedPids) > 0 {
		item["related_pids"] = t.RelatedPids
	}
	if t.ListID > 0 {
		item["list_id"] = t.ListID
	}
	if t.TagID > 0 {
		item["tag_id"] = t.TagID
	}
	return item
}

// GetDiscussTopic GET /api/discuss/:tid —— 详情 + 回复（按视角做门槛裁剪）。
func (h *Handlers) GetDiscussTopic(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
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
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}

	var threshold int
	canViewCode := true
	isOrganizer := false
	switch t.Scope {
	case repo.DiscussScopeProblem:
		gate, okG := h.problemDiscussGate(c, t.ScopeID)
		if !okG {
			return
		}
		threshold, canViewCode = gate.Threshold, gate.CanViewCode
	case repo.DiscussScopeContest:
		gate, okG := h.contestDiscussGate(c, t.ScopeID)
		if !okG {
			return
		}
		isOrganizer = gate.Organizer
		// 赛时私密提问：仅作者与组织者可见（404 不泄露存在性）；赛后全量公开
		if t.Private && !gate.Ended && t.UID != uid && !gate.Organizer {
			fail(c, http.StatusNotFound, "话题不存在")
			return
		}
	case repo.DiscussScopeGlobal:
		// 关闭 = 全不可读；临时锁定只封发帖入口，历史帖保留可读（通知深链不 404）
		if !s.DiscussGlobalOpen() {
			fail(c, http.StatusForbidden, "讨论区已关闭")
			return
		}
	}

	// 主楼分页（v0.26）：顶层回复按页取，楼中楼随本页主楼批量带出——前端收到的仍是
	// 扁平 replies（分组逻辑不变），总量/页码单发供分页器使用
	replyPage := atoiDefault(c.Query("page"), 1)
	tops, replyTotal, err := h.repo.ListDiscussReplyPage(c.Request.Context(), tid, replyPage)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取回复失败")
		return
	}
	topIDs := make([]int64, 0, len(tops))
	for _, r := range tops {
		topIDs = append(topIDs, r.ID)
	}
	children, err := h.repo.ListDiscussReplyChildren(c.Request.Context(), tid, topIDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取回复失败")
		return
	}
	replies := append(tops, children...)
	uids := authorUIDsReplies(replies)
	uids = append(uids, t.UID)
	users := h.discussAuthors(c.Request.Context(), uids)
	topicLiked, _ := h.repo.LikedDiscussTargets(c.Request.Context(), uid, "topic", []int64{tid})
	replyIDs := make([]int64, 0, len(replies))
	for _, r := range replies {
		replyIDs = append(replyIDs, r.ID)
	}
	replyLiked, _ := h.repo.LikedDiscussTargets(c.Request.Context(), uid, "reply", replyIDs)

	tj := topicJSON(t, users, topicLiked, true)
	if t.Deleted {
		tj["content"] = "" // 软删除：正文抹掉，保留楼层（回复照常下发）
		tj["title"] = "该帖已删除"
	} else if !canViewCode {
		tj["content"] = stripDiscussCodeBlocks(t.Content, threshold)
	} else {
		tj["content"] = t.Content
	}
	canManage := hasPerm(c, repo.PermDiscussManage)
	tj["can_edit"] = t.UID == uid && !t.Deleted
	tj["can_manage"] = canManage
	tj["can_accept"] = t.UID == uid && t.Scope == repo.DiscussScopeProblem && !t.Deleted
	tj["is_organizer"] = isOrganizer

	items := make([]gin.H, 0, len(replies))
	for _, r := range replies {
		items = append(items, replyJSON(r, users, replyLiked, threshold, canViewCode))
	}
	resp := gin.H{
		"topic":       tj,
		"replies":     items,
		"reply_total": replyTotal, // 顶层楼总数（分页口径，不含楼中楼）
		"reply_page":  replyPage,
		"reply_pages": (replyTotal + int64(repo.DiscussReplyPageSize) - 1) / int64(repo.DiscussReplyPageSize),
	}
	if t.Scope == repo.DiscussScopeProblem {
		resp["gate"] = gin.H{"code_threshold": threshold, "can_view_code": canViewCode}
	}
	ok(c, resp)
}

// UpdateDiscussTopic PATCH /api/discuss/:tid —— 作者编辑（保「已编辑」痕迹）。
func (h *Handlers) UpdateDiscussTopic(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
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
	if t.UID != uid {
		fail(c, http.StatusForbidden, "只能编辑自己发布的话题")
		return
	}
	if t.Deleted {
		fail(c, http.StatusForbidden, "话题已删除")
		return
	}
	var b discussTopicBody
	if err := c.ShouldBindJSON(&b); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if msg := validateTopicBody(t.Scope, &b); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	if err := h.repo.UpdateDiscussTopicContent(c.Request.Context(), tid,
		strings.TrimSpace(b.Title), b.Content, b.Kind, b.RelatedPids, b.ListID, b.TagID); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": tid})
}

// DeleteDiscussTopic DELETE /api/discuss/:tid —— 作者本人 ∨ discussion.manage（软删除保楼层）。
func (h *Handlers) DeleteDiscussTopic(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
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
	byManager := t.UID != uid && hasPerm(c, repo.PermDiscussManage)
	if t.UID != uid && !byManager {
		fail(c, http.StatusForbidden, "只能删除自己发布的话题")
		return
	}
	if err := h.repo.SoftDeleteDiscussTopic(c.Request.Context(), tid, uid); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	if byManager {
		_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
			Type: repo.SecDiscussDelete, UID: uid, Username: uname,
			Detail: fmt.Sprintf("删除讨论话题 #%d（作者 uid=%d）", tid, t.UID),
		})
		s, _ := h.repo.GetSystemSettings(c.Request.Context())
		h.notifyOne(c.Request.Context(), t.UID, repo.NotifyInput{
			Typ: repo.NotifyWarn, Title: "你的话题已被管理员删除",
			Body: truncRunes(t.Title, 40), Link: "/discuss/" + fmt.Sprint(tid),
		}, s, "")
	}
	ok(c, gin.H{"deleted": tid})
}
