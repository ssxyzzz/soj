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
		Name  string `json:"name" binding:"required"`
		Intro string `json:"intro"`
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
	cl, err := h.repo.CreateClass(c.Request.Context(), req.Name, strings.TrimSpace(req.Intro), uid, uname)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建班级失败")
		return
	}
	ok(c, gin.H{"id": cl.ID, "name": cl.Name, "invite_code": cl.InviteCode})
}

func (h *Handlers) ListClasses(c *gin.Context) {
	cards, err := h.repo.ListClasses(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	ok(c, cards)
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

// GetClass 班级详情：基础信息任何人可见；成员列表与邀请码仅班级管理员可见；
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
	manage := h.classManageable(c, cl)

	resp := gin.H{
		"id":         cl.ID,
		"name":       cl.Name,
		"intro":      cl.Intro,
		"owner_id":   cl.OwnerID,
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
