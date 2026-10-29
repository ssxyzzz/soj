package server

// 讨论区（v0.24）共享基座与访问闸门：
//   - 小工具：内容截断 / 代码门槛裁剪 / 通知助记 / 作者批取；
//   - 三闸门：题内（设置开关 / 隐藏题与赛程同口径 / 赛中整题锁定 / 代码分数门槛）、
//     比赛（赛程状态 + 组织者判定）、禁言与限速检查；
//   - 发帖公共体与校验。
// 三大挂载面（global / problem / contest）的列表与发帖、话题 CRUD 在 discuss_topics.go；
// 回复互动（回复/删除/采纳/点赞）在 discuss_replies.go；版务与举报（含禁言）在
// discuss_mod.go；全站广播在 discuss_broadcast.go——v0.32 巨型文件治理五拆，
// 包内私有共享不新设抽象。
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
