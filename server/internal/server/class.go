package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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
	ok(c, visible)
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
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
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
		"intro":      cl.Intro,
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
	err := h.repo.JoinClass(c.Request.Context(), id, uid, uname, strings.ToUpper(strings.TrimSpace(req.Code)))
	if errors.Is(err, repo.ErrInviteCode) {
		fail(c, http.StatusBadRequest, "邀请码错误")
		return
	}
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "加入班级失败")
		return
	}
	ok(c, gin.H{"joined": true})
}

func (h *Handlers) LeaveClass(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	if cl.OwnerID == uid {
		fail(c, http.StatusBadRequest, "建班人不能退出自己的班级")
		return
	}
	if err := h.repo.LeaveClass(c.Request.Context(), id, uid); err != nil {
		fail(c, http.StatusInternalServerError, "退出班级失败")
		return
	}
	ok(c, gin.H{"left": true})
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
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
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
	code := strings.ToUpper(strings.TrimSpace(req.Code))
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
