package server

// 讨论区（v0.24）三大挂载面的 handler：
//   - 导航栏学习分享（global）：/api/discuss 列表与发帖；
//   - 题内思路讨论（problem）：/api/problems/:id/discuss，代码块按「本题历史最高分门槛」
//     读时裁剪（教师群体/豁免位/达标者可见），题内禁图，赛时整题锁定；
//   - 主题库比赛讨论（contest）：/api/contests/:id/discuss，赛时学生提问默认仅组织者
//     可见、组织者可升格公开；赛后自动转公开复盘。
// 通用话题操作（详情/回复/点赞/采纳/举报/管理）统一挂在 /api/discuss/:tid 下。
// 管理面（举报队列/禁言/全站广播）在本文末尾，路由仍归 /api/admin。
// 防水闸门：挂载式发帖 + 必选类型 + 限速 + 冷静期 + 禁言 + 举报折叠（敏感词/外链白名单后续批）。

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const (
	maxDiscussTopicBytes  = 64 << 10 // 主楼正文上限 64KB（Markdown）
	maxDiscussReplyBytes  = 8 << 10  // 回复上限 8KB
	maxDiscussTitleRunes  = 80
	maxDiscussReasonRunes = 300
	discussMentionLimit   = 10 // 单条内容最多触达的被 @ 用户数
	// 代码门槛占位：未达标视角的围栏代码块被整体替换为这行提示
	codeHidePlaceholderFmt = "\n\n> 🔒 代码块已隐藏：本题最高得分需 ≥ %d 分，达到后自动可见\n\n"
)

// mentionRE 被 @ 提及：@用户名（限用户名字符集）。
var mentionRE = regexp.MustCompile(`@([0-9A-Za-z_\-]{1,32})`)

// imageMDRE 题内禁图检查：Markdown 图片语法或内联 <img（防截图代码绕过分数门槛）。
var imageMDRE = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)|<img\b`)

// ---------- 小工具 ----------

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n]) + "…"
}

func validDiscussKind(scope, kind string) bool {
	set, ok := repo.DiscussValidKinds[scope]
	return ok && kind != "" && set[kind]
}

// stripDiscussCodeBlocks 把 Markdown 围栏代码块整体替换为门槛提示（未达标视角读时裁剪）。
// 只处理 ``` 围栏（缩进式代码块不入门槛语义）；未闭合围栏裁到文末。
func stripDiscussCodeBlocks(content string, threshold int) string {
	if threshold <= 0 || !strings.Contains(content, "```") {
		return content
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines)+8)
	inFence := false
	wrote := false
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			if !inFence {
				inFence = true
				wrote = false
				out = append(out, fmt.Sprintf(codeHidePlaceholderFmt, threshold))
				continue
			}
			inFence = false
			continue
		}
		if inFence {
			if !wrote {
				out = append(out, "```", fmt.Sprintf(codeHidePlaceholderFmt, threshold), "```")
				wrote = true
			}
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// notifyOne 站内通知 + 可选邮件转发（仅 1:1 互动类；发布/广播类扇出走 NotifyUsers）。
// 邮件通道未配 SMTP 时静默跳过；任何失败不阻塞主流程。
func (h *Handlers) notifyOne(ctx context.Context, uid int64, in repo.NotifyInput, s *repo.SystemSettings, mailType string) {
	if uid <= 0 {
		return
	}
	if err := h.repo.UpsertNotification(ctx, uid, in, s.NotifyRetentionDaysVal()); err != nil {
		return
	}
	if mailType == "" {
		return
	}
	u, err := h.repo.GetUserByID(ctx, uid)
	if err != nil || u.Email == "" || u.Prefs == nil || s.MailChannelVal() != repo.MailChannelSMTP {
		return
	}
	on := false
	for _, cat := range u.Prefs.NotifyEmail {
		if cat == mailType {
			on = true
			break
		}
	}
	if !on {
		return
	}
	// 邮件通道在 smtp 档才会真正外发（log 档 Provider 打日志，此处已按通道过滤）
	_ = s.Mailer().Send(u.Email, "【SOJ】"+in.Title, in.Body)
}

// discussAuthors 批量取作者公开信息。
func (h *Handlers) discussAuthors(ctx context.Context, uids []int64) map[int64]repo.User {
	users, err := h.repo.GetUsersByIDs(ctx, uids)
	out := make(map[int64]repo.User, len(users))
	if err != nil {
		return out
	}
	for id, u := range users {
		out[id] = *u
	}
	return out
}

func authorUIDsTopics(list []*repo.DiscussTopic) []int64 {
	ids := make([]int64, 0, len(list))
	seen := map[int64]bool{}
	for _, t := range list {
		if !seen[t.UID] {
			seen[t.UID] = true
			ids = append(ids, t.UID)
		}
	}
	return ids
}

// authorUIDsReplies 批量取回复作者 uid。
func authorUIDsReplies(list []*repo.DiscussReply) []int64 {
	ids := make([]int64, 0, len(list))
	seen := map[int64]bool{}
	for _, r := range list {
		if !seen[r.UID] {
			seen[r.UID] = true
			ids = append(ids, r.UID)
		}
	}
	return ids
}

// ---------- 访问闸门 ----------

// discussProblemGate 题内讨论闸门：设置开关 / 题目可见性（隐藏题、赛程隐藏期与详情同口径）/
// 赛中整题锁定 / 代码门槛。返回 nil 表示已写响应。
type discussProblemGate struct {
	Problem     *repo.Problem
	Threshold   int // 生效代码门槛（0 = 不隐藏）
	CanViewCode bool
	CanManage   bool // 题面编辑者（教师群体）
	Privileged  bool
}

func (h *Handlers) problemDiscussGate(c *gin.Context, pid int64) (*discussProblemGate, bool) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return nil, false
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return nil, false
	}
	if !s.DiscussProblemOpen() {
		fail(c, http.StatusForbidden, "题内讨论已关闭")
		return nil, false
	}
	p, err := h.repo.GetProblem(c.Request.Context(), pid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return nil, false
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return nil, false
	}
	privileged := hasPerm(c, repo.PermProblemViewAll)
	canManage := h.problemEditable(c, p)
	// 隐藏题：与题目详情同口径，无权限视角一律不可见（API 层堵 URL 直闯）
	if p.Hidden && !privileged && !canManage {
		fail(c, http.StatusNotFound, "题目不存在")
		return nil, false
	}
	lockMap, err := h.loadLockMap(c.Request.Context(), uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return nil, false
	}
	lk := lockMap[p.ID]
	isRoundOrganizer := lk != nil && (lk.Round.CreatedBy == uid || role == repo.RoleRoot)
	// 赛程隐藏期（赛前/赛后未发布）：题目本身看不见，讨论同口径 404
	if lk != nil && lk.Phase != "running" && !privileged && !canManage && !isRoundOrganizer {
		fail(c, http.StatusNotFound, "题目不存在")
		return nil, false
	}
	// 赛中：整题锁定（不能发也不能看）；特权/教师/本场组织者放行
	if lk != nil && lk.Phase == "running" && !privileged && !canManage && !isRoundOrganizer {
		fail(c, http.StatusForbidden, "该题处于比赛/考试中，讨论已锁定，赛后开放")
		return nil, false
	}
	// 代码门槛：每题覆盖 > 全局默认；0 = 不隐藏
	threshold := s.DiscussCodeThresholdVal()
	if p.DiscussCodeThreshold > 0 {
		threshold = p.DiscussCodeThreshold
	}
	canViewCode := true
	if threshold > 0 {
		canViewCode = privileged || canManage || hasPerm(c, repo.PermDiscussCodeBypass)
		if !canViewCode {
			if best, err := h.repo.UserBestStatuses(c.Request.Context(), uid, []int64{p.ID}); err == nil {
				canViewCode = best[p.ID].Score >= threshold
			}
		}
	}
	return &discussProblemGate{
		Problem: p, Threshold: threshold, CanViewCode: canViewCode,
		CanManage: canManage, Privileged: privileged,
	}, true
}

// discussContestGate 比赛讨论闸门。
type discussContestGate struct {
	Round     *repo.Contest
	Running   bool
	Ended     bool
	Organizer bool
}

func (h *Handlers) contestDiscussGate(c *gin.Context, cid int64) (*discussContestGate, bool) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return nil, false
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return nil, false
	}
	if !s.DiscussContestOpen() {
		fail(c, http.StatusForbidden, "比赛讨论已关闭")
		return nil, false
	}
	round, err := h.repo.GetRound(c.Request.Context(), repo.KindContest, cid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return nil, false
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return nil, false
	}
	st := round.Status(time.Now())
	return &discussContestGate{
		Round:     round,
		Running:   st == "running",
		Ended:     st == "ended",
		Organizer: round.CreatedBy == uid || role == repo.RoleRoot || hasPerm(c, repo.PermDiscussManage),
	}, true
}

// discussMuteCheck 禁言检查（命中即 403）。
func (h *Handlers) discussMuteCheck(c *gin.Context, uid int64) bool {
	mute, err := h.repo.ActiveDiscussMute(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取禁言状态失败")
		return false
	}
	if mute != nil {
		msg := "你已被禁言"
		if mute.Reason != "" {
			msg += "（" + mute.Reason + "）"
		}
		fail(c, http.StatusForbidden, msg)
		return false
	}
	return true
}

// discussRateCheck 发帖/回复限速；返回错误消息（空 = 放行）。
func (h *Handlers) discussRateCheck(c *gin.Context, uid int64, topic bool, s *repo.SystemSettings) string {
	if hasPerm(c, repo.PermDiscussRateExempt) {
		return ""
	}
	if topic {
		limit := s.DiscussTopicDailyLimitVal()
		if limit > 0 {
			if n, err := h.repo.CountUserDiscussTopicsToday(c.Request.Context(), uid); err == nil && n >= int64(limit) {
				return fmt.Sprintf("今日发帖已达上限（%d 篇/天），明天再来", limit)
			}
		}
		return ""
	}
	interval := s.DiscussReplyIntervalVal()
	if interval > 0 {
		if last, err := h.repo.LastUserReplyAt(c.Request.Context(), uid); err == nil && !last.IsZero() {
			if wait := interval - int(time.Since(last).Seconds()); wait > 0 {
				return fmt.Sprintf("回复太快了，%d 秒后再试", wait)
			}
		}
	}
	return ""
}

// ---------- 发帖公共体 ----------

type discussTopicBody struct {
	Title       string  `json:"title"`
	Kind        string  `json:"kind"`
	Content     string  `json:"content"`
	RelatedPids []int64 `json:"related_pids"` // global：相关题目（多选）
	ListID      int64   `json:"list_id"`      // global：关联题单
	TagID       int64   `json:"tag_id"`       // global：关联知识树节点
	Private     *bool   `json:"private"`      // contest：赛时私密提问
}

// validateTopicBody 校验发帖体；非法返回错误消息。
// 关联实体（题目/题单/知识点）不校验存在性：前端按 id 渲染、缺失自然降级为纯文本，
// 与「讨论随实体删改不阻塞」的宽容口径一致。
func validateTopicBody(scope string, b *discussTopicBody) string {
	title := strings.TrimSpace(b.Title)
	if title == "" {
		return "标题不能为空"
	}
	if utf8.RuneCountInString(title) > maxDiscussTitleRunes {
		return fmt.Sprintf("标题过长（上限 %d 字）", maxDiscussTitleRunes)
	}
	if !validDiscussKind(scope, b.Kind) {
		return "请选择正确的帖子类型"
	}
	if strings.TrimSpace(b.Content) == "" {
		return "内容不能为空"
	}
	if len(b.Content) > maxDiscussTopicBytes {
		return "内容过长（上限 64KB）"
	}
	if scope == repo.DiscussScopeProblem && imageMDRE.MatchString(b.Content) {
		return "题内讨论不支持图片（代码请用文本代码块， screenshots 会被分数门槛拦截）"
	}
	if len(b.RelatedPids) > 10 {
		return "相关题目最多关联 10 道"
	}
	return ""
}

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

// ---------- 管理面：举报队列 / 禁言 / 全站广播 ----------

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
