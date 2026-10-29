package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/repo"
)

// classManageable 判断用户能否管理某班级（建班人、班级管理员或 root）。
func (h *Handlers) classManageable(c *gin.Context, cl *repo.Class) bool {
	uid, _, role, logged := currentUser(c)
	if !logged {
		return false
	}
	if role == repo.RoleRoot || uid == cl.OwnerID {
		return true
	}
	memberRole, err := h.repo.ClassMemberRole(c.Request.Context(), cl.ID, uid)
	return err == nil && memberRole == repo.ClassRoleAdmin
}

func (h *Handlers) CreateClass(c *gin.Context) {
	uid, uname, _, _ := currentUser(c)
	var req struct {
		Name    string `json:"name" binding:"required"`
		Intro   string `json:"intro"`
		Private *bool  `json:"private"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "班级名称不能为空")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) == 0 || len(req.Name) > 64 {
		fail(c, http.StatusBadRequest, "班级名称需为 1-64 字符")
		return
	}
	private := req.Private != nil && *req.Private
	cl, err := h.repo.CreateClass(c.Request.Context(), req.Name, strings.TrimSpace(req.Intro), private, uid, uname)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建班级失败")
		return
	}
	ok(c, gin.H{"id": cl.ID, "name": cl.Name, "invite_code": cl.InviteCode})
}

// ListClasses 班级列表（按视角过滤私密班）：
//   - root/admin 全部可见；
//   - 登录用户可见公开班 + 自己加入的私密班（带 joined 标记）；
//   - 游客仅公开班。
func (h *Handlers) ListClasses(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	cards, err := h.repo.ListClasses(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	privileged := hasPerm(c, repo.PermClassViewAll)
	mine := map[int64]bool{}
	if logged && !privileged {
		ids, err := h.repo.MyClassIDs(c.Request.Context(), uid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取班级失败")
			return
		}
		for _, id := range ids {
			mine[id] = true
		}
	}
	visible := make([]repo.ClassCard, 0, len(cards))
	for _, card := range cards {
		if card.Private && !privileged {
			if !logged || !mine[card.ID] {
				continue
			}
		}
		if logged {
			if privileged {
				ok2, _ := h.repo.IsMember(c.Request.Context(), card.ID, uid)
				card.Joined = ok2
			} else {
				card.Joined = mine[card.ID]
			}
		}
		visible = append(visible, card)
	}
	// 个人视角排序（2026-10-26 v0.22）：置顶优先（pins 序）→ 已排序的已入班班（order 序）
	// → 其余保持默认 id 序垫底；置顶/拖动仅限已入班（marks 只含成员班），非成员班不可操作。
	if logged {
		pins, order, err := h.repo.GetClassMarks(c.Request.Context(), uid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取班级失败")
			return
		}
		pinIdx := make(map[int64]int, len(pins))
		for i, id := range pins {
			pinIdx[id] = i
		}
		ordIdx := make(map[int64]int, len(order))
		for i, id := range order {
			ordIdx[id] = i
		}
		rank := func(card repo.ClassCard) [2]int {
			if i, ok := pinIdx[card.ID]; ok {
				return [2]int{0, i}
			}
			if i, ok := ordIdx[card.ID]; ok {
				return [2]int{1, i}
			}
			return [2]int{2, int(card.ID)} // 默认序 = 建班先后（_id 升序）
		}
		for i := range visible {
			visible[i].Pinned = func() bool { _, ok := pinIdx[visible[i].ID]; return ok }()
		}
		sort.SliceStable(visible, func(i, j int) bool {
			ra, rb := rank(visible[i]), rank(visible[j])
			if ra[0] != rb[0] {
				return ra[0] < rb[0]
			}
			return ra[1] < rb[1]
		})
	}
	ok(c, visible)
}

// SaveClassMarks 保存个人班级视图标记（2026-10-26 v0.22）：pins 置顶序 + order 已入班展示序。
// 两组 id 均过滤为该用户的已入班集合——非成员班不可置顶/拖动（页脚口径：仅限已入班人员）。
func (h *Handlers) SaveClassMarks(c *gin.Context) {
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Pins  []int64 `json:"pins"`
		Order []int64 `json:"order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ids, err := h.repo.MyClassIDs(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	allow := make(map[int64]bool, len(ids))
	for _, id := range ids {
		allow[id] = true
	}
	filter := func(in []int64) []int64 {
		seen := make(map[int64]bool, len(in))
		out := make([]int64, 0, len(in))
		for _, id := range in {
			if allow[id] && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		return out
	}
	if err := h.repo.SaveClassMarks(c.Request.Context(), uid, filter(req.Pins), filter(req.Order)); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"saved": true})
}

func (h *Handlers) MyClasses(c *gin.Context) {
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	cards, err := h.repo.MyClasses(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	ok(c, cards)
}

// GetClass 班级详情：私密班仅本班成员（与 root/admin）可见；成员列表与邀请码仅班级管理员可见；
// 作业列表单独走 /classes/:id/assignments（仅本班成员可见）。
func (h *Handlers) GetClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, id)
	if respondRepoErr(c, err, "班级不存在", "读取班级失败") {
		return
	}

	uid, _, _, logged := currentUser(c)
	isMember := false
	if logged {
		isMember, _ = h.repo.IsMember(ctx, id, uid)
	}
	privileged := hasPerm(c, repo.PermClassViewAll)
	// 私密班对非成员隐藏存在本身
	if cl.Private && !privileged && !isMember {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	manage := h.classManageable(c, cl)

	resp := gin.H{
		"id":         cl.ID,
		"name":       cl.Name,
		"intro":      cl.Intro, // 班级公告（Markdown，v0.8 起单一事实源）
		"intro_html": cl.IntroHtml, // 渲染缓存（v0.30）
		"owner_id":   cl.OwnerID,
		"private":    cl.Private,
		"created_at": cl.CreatedAt,
		"joined":     isMember,
		"manageable": manage,
	}
	if manage {
		resp["invite_code"] = cl.InviteCode
		members, err := h.repo.ListMembers(ctx, id)
		if err == nil {
			list := make([]gin.H, 0, len(members))
			for _, m := range members {
				memberRole := m.Role
				if memberRole == "" {
					memberRole = repo.ClassRoleMember
				}
				list = append(list, gin.H{
					"uid": m.UID, "username": m.Uname, "role": memberRole, "joined_at": m.JoinedAt,
					"is_owner": m.UID == cl.OwnerID,
				})
			}
			resp["members"] = list
		}
	}
	// 成员（非建班人）的可退判定：是成员 + 持 class.leave 位
	if logged && isMember && uid != cl.OwnerID {
		resp["leaveable"] = hasPerm(c, repo.PermClassLeave)
	}
	ok(c, resp)
}

func (h *Handlers) JoinClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, uname, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请输入邀请码")
		return
	}
	err := h.repo.JoinClass(c.Request.Context(), id, uid, uname, strings.ToLower(strings.TrimSpace(req.Code)))
	if errors.Is(err, repo.ErrInviteCode) {
		fail(c, http.StatusBadRequest, "邀请码错误")
		return
	}
	if respondRepoErr(c, err, "班级不存在", "加入班级失败") {
		return
	}
	ok(c, gin.H{"joined": true})
}

func (h *Handlers) LeaveClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, uname, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	// v0.7.2：退出班级改为权限位管控（default 角色默认没有，由老师移出或管理员授予）
	if !hasPerm(c, repo.PermClassLeave) {
		fail(c, http.StatusForbidden, "没有退出班级的权限，请联系老师将你移出")
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if respondRepoErr(c, err, "班级不存在", "读取班级失败") {
		return
	}
	// 建班人不可退班（2026-10-26 用户拍板：只能转让或解散，删除「没人了才可退」例外）
	if cl.OwnerID == uid {
		fail(c, http.StatusBadRequest, "建班人不能退出班级，请在编辑班级中转让或解散")
		return
	}
	err = h.repo.LeaveClass(c.Request.Context(), id, uid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusBadRequest, "你不是该班级的成员")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "退出班级失败")
		return
	}
	slog.Info("用户退出班级", "component", "web", "username", uname, "class_id", id)
	ok(c, gin.H{"left": true})
}

// UpdateClass 编辑班级（v0.7.3）：名称 / 公告，均可选；classManageable 门槛
// （建班人/班级管理员/root，同管理卡其它按钮）。
func (h *Handlers) UpdateClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Intro   *string `json:"intro"`
		Private *bool   `json:"private"` // v0.7.4：随编辑班级弹窗一并保存；切可见性权限更严（建班人 ∨ root）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, id)
	if respondRepoErr(c, err, "班级不存在", "读取班级失败") {
		return
	}
	if !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可编辑班级")
		return
	}
	uid, _, role, logged := currentUser(c)
	set := bson.M{}
	if req.Name != nil {
		name := trimTo(*req.Name, 64)
		if name == "" {
			fail(c, http.StatusBadRequest, "班级名称不能为空")
			return
		}
		set["name"] = name
	}
	if req.Intro != nil {
		if len(*req.Intro) > maxStatementSize {
			fail(c, http.StatusBadRequest, "班级公告过长（上限 256KB）")
			return
		}
		// TrimSpace 归一（同 lists.go 题单简介口径）：Markdown 编辑器清空后会留 "\n"
		// 这类空白串，原样入库会让前端的「暂无简介」兜底失效
		set["intro"] = strings.TrimSpace(*req.Intro)
	}
	if req.Private != nil {
		if !logged || !(uid == cl.OwnerID || role == repo.RoleRoot) {
			fail(c, http.StatusForbidden, "仅建班人或 root 可切换班级可见性")
			return
		}
		set["private"] = *req.Private
	}
	if len(set) == 0 {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateClass(ctx, id, set); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": id})
}

// TransferClass 转让班级（建班人 ∨ root）：指定成员成为新建班人，原建班人降为班级管理员。
func (h *Handlers) TransferClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, uname, role, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if respondRepoErr(c, err, "班级不存在", "读取班级失败") {
		return
	}
	if !(uid == cl.OwnerID || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "仅建班人可转让班级")
		return
	}
	var req struct {
		UID int64 `json:"uid" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UID <= 0 {
		fail(c, http.StatusBadRequest, "请选择受让人")
		return
	}
	if req.UID == uid {
		fail(c, http.StatusBadRequest, "不能转让给自己")
		return
	}
	if err := h.repo.TransferClass(c.Request.Context(), id, uid, req.UID); errors.Is(err, repo.ErrNotMember) {
		fail(c, http.StatusBadRequest, "对方不是班级成员，请先将其加入班级")
		return
	} else if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "转让失败")
		return
	}
	target, terr := h.repo.GetUserByID(c.Request.Context(), req.UID)
	tname := fmt.Sprintf("#%d", req.UID)
	if terr == nil {
		tname = target.Username
	}
	slog.Info("班级已转让", "component", "web", "class_id", id, "from", uname, "to", tname)
	ok(c, gin.H{"transferred": true})
}

// DeleteClass 解散班级（建班人 ∨ root）：级联删除班内全部考试、作业与成员关系。
func (h *Handlers) DeleteClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, uname, role, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if respondRepoErr(c, err, "班级不存在", "读取班级失败") {
		return
	}
	if !(uid == cl.OwnerID || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "仅建班人可解散班级")
		return
	}
	if err := h.repo.DeleteClass(c.Request.Context(), id); err != nil {
		if repo.IsNotFound(err) {
			fail(c, http.StatusNotFound, "班级不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "解散班级失败")
		return
	}
	slog.Info("班级已解散（考试/作业/成员关系一并清理）", "component", "web", "class_id", id, "username", uname)
	ok(c, gin.H{"deleted": true})
}

func (h *Handlers) RegenInvite(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可执行此操作")
		return
	}
	code, err := h.repo.RegenInviteCode(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "重置邀请码失败")
		return
	}
	ok(c, gin.H{"invite_code": code})
}

// SetClassPrivate 切换班级公开/私密（建班人/root）。
func (h *Handlers) SetClassPrivate(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if respondRepoErr(c, err, "班级不存在", "读取班级失败") {
		return
	}
	uid, _, role, logged := currentUser(c)
	if !logged || !(uid == cl.OwnerID || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "仅建班人或 root 可切换班级可见性")
		return
	}
	var req struct {
		Private *bool `json:"private" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Private == nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.repo.SetClassPrivate(c.Request.Context(), id, *req.Private); err != nil {
		fail(c, http.StatusInternalServerError, "切换失败")
		return
	}
	ok(c, gin.H{"private": *req.Private})
}

// JoinByCode 纯邀请码入班（班级列表里无需先知道班级 ID）。
func (h *Handlers) JoinByCode(c *gin.Context) {
	uid, uname, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请输入邀请码")
		return
	}
	code := strings.ToLower(strings.TrimSpace(req.Code))
	cl, err := h.repo.GetClassByInviteCode(c.Request.Context(), code)
	if repo.IsNotFound(err) {
		fail(c, http.StatusBadRequest, "邀请码错误")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "加入班级失败")
		return
	}
	if err := h.repo.JoinClass(c.Request.Context(), cl.ID, uid, uname, code); err != nil {
		fail(c, http.StatusInternalServerError, "加入班级失败")
		return
	}
	ok(c, gin.H{"joined": true, "class_id": cl.ID, "name": cl.Name})
}

// ImportMembers 按用户名批量导入（换行/逗号/空格分隔）。
func (h *Handlers) ImportMembers(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可执行此操作")
		return
	}
	var req struct {
		Usernames string `json:"usernames" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请填写用户名列表")
		return
	}
	fields := strings.FieldsFunc(req.Usernames, func(r rune) bool {
		return r == '\n' || r == ',' || r == ' ' || r == '\t' || r == '、' || r == '\r'
	})
	if len(fields) == 0 {
		fail(c, http.StatusBadRequest, "没有解析到用户名")
		return
	}
	added, err := h.repo.ImportMembers(c.Request.Context(), id, fields)
	if err != nil {
		fail(c, http.StatusInternalServerError, "导入失败")
		return
	}
	ok(c, gin.H{"added": added, "total": len(fields)})
}

// ---------- 班级多管理员 ----------

// SetMemberRole 设置班级成员角色（建班人、班级管理员或 root；owner 角色不可改）。
func (h *Handlers) SetMemberRole(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	targetUID, okUID := parseID(c, "uid")
	if !okUID {
		return
	}
	var req struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if req.Role != repo.ClassRoleAdmin && req.Role != repo.ClassRoleMember {
		fail(c, http.StatusBadRequest, "角色只能设为 admin 或 member")
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可以设置成员角色")
		return
	}
	if targetUID == cl.OwnerID {
		fail(c, http.StatusBadRequest, "不能修改建班人的角色")
		return
	}
	if err := h.repo.SetClassMemberRole(ctx, classID, targetUID, req.Role); err != nil {
		fail(c, http.StatusInternalServerError, "设置失败")
		return
	}
	ok(c, gin.H{"updated": targetUID, "role": req.Role})
}
