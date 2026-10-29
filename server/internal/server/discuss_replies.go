package server

// 讨论区·回复互动（v0.32 自 discuss.go 五拆，纯搬家行为零变化）：
// 回复（两层楼中楼 + @提及通知）/ 软删除 / 楼主采纳（+经验）/ 点赞。
// 闸门与公共体在 discuss.go；话题 CRUD 在 discuss_topics.go。

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// CreateDiscussReply POST /api/discuss/:tid/replies —— 回复（两层楼中楼）。
func (h *Handlers) CreateDiscussReply(c *gin.Context) {
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
	if t.Deleted {
		fail(c, http.StatusForbidden, "话题已删除，不能回复")
		return
	}
	if t.Locked {
		fail(c, http.StatusForbidden, "话题已锁定，不能回复")
		return
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	isTeacher := false
	switch t.Scope {
	case repo.DiscussScopeProblem:
		gate, okG := h.problemDiscussGate(c, t.ScopeID)
		if !okG {
			return
		}
		isTeacher = gate.CanManage
	case repo.DiscussScopeContest:
		gate, okG := h.contestDiscussGate(c, t.ScopeID)
		if !okG {
			return
		}
		// 赛时：私密帖只有作者与组织者可进；公开帖学生不能回（防互相启发），赛后放开
		if t.Private && t.UID != uid && !gate.Organizer {
			fail(c, http.StatusNotFound, "话题不存在")
			return
		}
		if gate.Running && !gate.Organizer && t.UID != uid {
			fail(c, http.StatusForbidden, "比赛进行中，不能回复他人话题；如有疑问请单独发提问")
			return
		}
	case repo.DiscussScopeGlobal:
		if !s.DiscussGlobalOpen() {
			fail(c, http.StatusForbidden, "讨论区已关闭")
			return
		}
		if s.DiscussGlobalLocked {
			fail(c, http.StatusForbidden, "讨论区临时锁定（考试期间），考试结束后恢复")
			return
		}
	}
	if !h.discussMuteCheck(c, uid) {
		return
	}
	if msg := h.discussRateCheck(c, uid, false, s); msg != "" {
		fail(c, http.StatusTooManyRequests, msg)
		return
	}
	var req struct {
		Content string `json:"content"`
		ReplyTo int64  `json:"reply_to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		fail(c, http.StatusBadRequest, "回复内容不能为空")
		return
	}
	if len(content) > maxDiscussReplyBytes {
		fail(c, http.StatusBadRequest, "回复过长（上限 8KB）")
		return
	}
	if t.Scope == repo.DiscussScopeProblem && imageMDRE.MatchString(content) {
		fail(c, http.StatusBadRequest, "题内讨论不支持图片（代码请用文本代码块）")
		return
	}
	// 楼中楼归一：嵌套回复统一挂到顶层楼（两层结构）
	replyTo := req.ReplyTo
	if replyTo > 0 {
		parent, err := h.repo.GetDiscussReply(c.Request.Context(), replyTo)
		if repo.IsNotFound(err) || parent.TID != tid {
			fail(c, http.StatusBadRequest, "被回复的楼层不存在")
			return
		}
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取回复失败")
			return
		}
		if parent.ReplyTo > 0 {
			replyTo = parent.ReplyTo
		}
	}
	rep := &repo.DiscussReply{
		TID: tid, UID: uid, Content: content, ReplyTo: replyTo,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := h.repo.InsertDiscussReply(c.Request.Context(), rep); err != nil {
		fail(c, http.StatusInternalServerError, "回复失败")
		return
	}
	if err := h.repo.IncDiscussTopicReplies(c.Request.Context(), tid); err != nil {
		// 计数失败不阻塞（列表以实际回复为准，下次操作自愈）
		_ = err
	}
	// 教师回应标记（题内求助帖「已官方解答」）
	if t.Scope == repo.DiscussScopeProblem && isTeacher {
		_ = h.repo.MarkDiscussOfficial(c.Request.Context(), tid)
	}

	// 通知：话题作者 + 被回复楼层作者 + 被 @ 用户（同源未读自动聚合）
	replier, _ := h.repo.GetUserByID(c.Request.Context(), uid)
	replierName := fmt.Sprint(uid)
	if replier != nil {
		replierName = replier.Nickname
	}
	title := truncRunes(t.Title, 40)
	link := "/discuss/" + fmt.Sprint(tid)
	notified := map[int64]bool{uid: true}
	h.notifyOne(c.Request.Context(), t.UID, repo.NotifyInput{
		Typ: repo.NotifyReply, Title: title,
		Body: replierName + " 回复了你的话题", Link: link, RefID: tid,
	}, s, repo.NotifyReply)
	notified[t.UID] = true
	if replyTo > 0 {
		if parent, err := h.repo.GetDiscussReply(c.Request.Context(), replyTo); err == nil && !notified[parent.UID] {
			h.notifyOne(c.Request.Context(), parent.UID, repo.NotifyInput{
				Typ: repo.NotifyReply, Title: title,
				Body: replierName + " 回复了你的回复", Link: link, RefID: tid,
			}, s, repo.NotifyReply)
			notified[parent.UID] = true
		}
	}
	// 被 @ 提及：解析内容中的 @用户名（去重、限量、跳过已通知者）
	seen := map[string]bool{}
	mentions := mentionRE.FindAllStringSubmatch(content, discussMentionLimit*2)
	count := 0
	for _, m := range mentions {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if count >= discussMentionLimit {
			break
		}
		if target, _, err := h.repo.GetUserByUsername(c.Request.Context(), name); err == nil && !notified[target.ID] {
			h.notifyOne(c.Request.Context(), target.ID, repo.NotifyInput{
				Typ: repo.NotifyMention, Title: title,
				Body: replierName + " 在讨论中提到了你", Link: link, RefID: tid,
			}, s, repo.NotifyMention)
			notified[target.ID] = true
			count++
		}
	}

	ok(c, gin.H{"id": rep.ID})
}

// DeleteDiscussReply DELETE /api/discuss/:tid/replies/:rid —— 作者本人 ∨ discussion.manage。
func (h *Handlers) DeleteDiscussReply(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tid, okID := parseID(c, "tid")
	rid, okRID := parseID(c, "rid")
	if !okID || !okRID {
		return
	}
	rep, err := h.repo.GetDiscussReply(c.Request.Context(), rid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "回复不存在")
		return
	}
	if err != nil || rep.TID != tid {
		fail(c, http.StatusNotFound, "回复不存在")
		return
	}
	byManager := rep.UID != uid && hasPerm(c, repo.PermDiscussManage)
	if rep.UID != uid && !byManager {
		fail(c, http.StatusForbidden, "只能删除自己的回复")
		return
	}
	if err := h.repo.SoftDeleteDiscussReply(c.Request.Context(), rid); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	if byManager {
		_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
			Type: repo.SecDiscussDelete, UID: uid, Username: uname,
			Detail: fmt.Sprintf("删除讨论回复 #%d（话题 #%d，作者 uid=%d）", rid, tid, rep.UID),
		})
		s, _ := h.repo.GetSystemSettings(c.Request.Context())
		h.notifyOne(c.Request.Context(), rep.UID, repo.NotifyInput{
			Typ: repo.NotifyWarn, Title: "你的回复已被管理员删除",
			Body: truncRunes(rep.Content, 40), Link: "/discuss/" + fmt.Sprint(tid),
		}, s, "")
	}
	ok(c, gin.H{"deleted": rid})
}

// AcceptDiscussReply POST /api/discuss/:tid/replies/:rid/accept —— 题内：楼主采纳（+经验）。
func (h *Handlers) AcceptDiscussReply(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tid, okID := parseID(c, "tid")
	rid, okRID := parseID(c, "rid")
	if !okID || !okRID {
		return
	}
	t, err := h.repo.GetDiscussTopic(c.Request.Context(), tid)
	if respondRepoErr(c, err, "话题不存在", "读取话题失败") {
		return
	}
	if t.UID != uid {
		fail(c, http.StatusForbidden, "只有楼主可以采纳回答")
		return
	}
	if t.Scope != repo.DiscussScopeProblem {
		fail(c, http.StatusBadRequest, "仅题内讨论支持采纳")
		return
	}
	rep, err := h.repo.GetDiscussReply(c.Request.Context(), rid)
	if repo.IsNotFound(err) || (err == nil && rep.TID != tid) {
		fail(c, http.StatusNotFound, "回复不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取回复失败")
		return
	}
	if rep.Deleted {
		fail(c, http.StatusBadRequest, "不能采纳已删除的回复")
		return
	}
	// 先清旧采纳标记（一帖一条，后选覆盖）
	if t.AcceptedReplyID > 0 && t.AcceptedReplyID != rid {
		_ = h.repo.SetDiscussReplyAccepted(c.Request.Context(), t.AcceptedReplyID, false)
	}
	if err := h.repo.AcceptDiscussReply(c.Request.Context(), tid, rid); err != nil {
		fail(c, http.StatusInternalServerError, "采纳失败")
		return
	}
	if err := h.repo.SetDiscussReplyAccepted(c.Request.Context(), rid, true); err != nil {
		fail(c, http.StatusInternalServerError, "采纳失败")
		return
	}
	// 激励：被采纳 + 经验（后台可配，走 xpAdjust 流水天然留痕）
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	if xp := s.DiscussAcceptXpVal(); xp > 0 && rep.UID != uid {
		if err := h.repo.AddXpAdjust(c.Request.Context(), rep.UID, xp,
			fmt.Sprintf("讨论回复被采纳（话题 #%d）", tid), uid); err == nil {
			h.notifyOne(c.Request.Context(), rep.UID, repo.NotifyInput{
				Typ: repo.NotifyAccept, Title: truncRunes(t.Title, 40),
				Body: fmt.Sprintf("你的回复被采纳为答案（+%d 经验）", xp), Link: "/discuss/" + fmt.Sprint(tid), RefID: tid,
			}, s, repo.NotifyAccept)
		}
	}
	ok(c, gin.H{"accepted": rid})
}

// LikeDiscussTopic POST /api/discuss/:tid/like
func (h *Handlers) LikeDiscussTopic(c *gin.Context) {
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
	liked, count, err := h.repo.ToggleDiscussLike(c.Request.Context(), "topic", tid, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	// 新点赞才通知（取消点赞不打扰）
	if liked && t.UID != uid {
		s, _ := h.repo.GetSystemSettings(c.Request.Context())
		liker, _ := h.repo.GetUserByID(c.Request.Context(), uid)
		name := fmt.Sprint(uid)
		if liker != nil {
			name = liker.Nickname
		}
		h.notifyOne(c.Request.Context(), t.UID, repo.NotifyInput{
			Typ: repo.NotifyLike, Title: truncRunes(t.Title, 40),
			Body: name + " 赞了你的话题", Link: "/discuss/" + fmt.Sprint(tid), RefID: tid,
		}, s, repo.NotifyLike)
	}
	ok(c, gin.H{"liked": liked, "like_count": count})
}

// LikeDiscussReply POST /api/discuss/:tid/replies/:rid/like
func (h *Handlers) LikeDiscussReply(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	rid, okID := parseID(c, "rid")
	if !okID {
		return
	}
	rep, err := h.repo.GetDiscussReply(c.Request.Context(), rid)
	if respondRepoErr(c, err, "回复不存在", "读取回复失败") {
		return
	}
	liked, count, err := h.repo.ToggleDiscussLike(c.Request.Context(), "reply", rid, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	if liked && rep.UID != uid {
		s, _ := h.repo.GetSystemSettings(c.Request.Context())
		liker, _ := h.repo.GetUserByID(c.Request.Context(), uid)
		name := fmt.Sprint(uid)
		if liker != nil {
			name = liker.Nickname
		}
		h.notifyOne(c.Request.Context(), rep.UID, repo.NotifyInput{
			Typ: repo.NotifyLike, Title: truncRunes(rep.Content, 40),
			Body: name + " 赞了你的回复", Link: "/discuss/" + fmt.Sprint(rep.TID), RefID: rid,
		}, s, repo.NotifyLike)
	}
	ok(c, gin.H{"liked": liked, "like_count": count})
}
