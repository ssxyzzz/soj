package server

// 角色管理（硬编码 root，不设权限位、不可下放——管权限的权力永不下放）。

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

var roleIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

func (h *Handlers) AdminListRoles(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.repo.ListRoles(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取角色失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, r := range list {
		n, _ := h.repo.CountUsersWithRole(ctx, r.ID)
		items = append(items, gin.H{
			"id": r.ID, "name": r.Name, "perms": repo.EffectivePerms(r),
			"builtin": r.Builtin, "user_count": n,
		})
	}
	ok(c, gin.H{"list": items, "catalog": repo.PermCatalog})
}

func (h *Handlers) AdminCreateRole(c *gin.Context) {
	var req struct {
		ID    string   `json:"id" binding:"required"`
		Name  string   `json:"name" binding:"required"`
		Perms []string `json:"perms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if !roleIDRE.MatchString(req.ID) {
		fail(c, http.StatusBadRequest, "角色 ID 需为 2-32 位小写字母/数字/下划线，且以字母开头")
		return
	}
	if len(req.Name) > 32 {
		fail(c, http.StatusBadRequest, "角色名称需在 1-32 字符")
		return
	}
	if !repo.ValidPermSet(req.Perms) {
		fail(c, http.StatusBadRequest, "权限位非法（不可包含通配符）")
		return
	}
	if req.ID == repo.RoleDefault || req.ID == repo.RoleAdmin || req.ID == repo.RoleRoot {
		fail(c, http.StatusBadRequest, "角色 ID 与内置角色冲突")
		return
	}
	if _, err := h.repo.GetRole(c.Request.Context(), req.ID); err == nil {
		fail(c, http.StatusBadRequest, "角色 ID 已存在")
		return
	}
	if req.Perms == nil {
		req.Perms = []string{}
	}
	if err := h.repo.CreateRole(c.Request.Context(), repo.Role{
		ID: req.ID, Name: req.Name, Perms: req.Perms, Builtin: false,
	}); err != nil {
		fail(c, http.StatusInternalServerError, "创建角色失败")
		return
	}
	h.idents.flush()
	ok(c, gin.H{"id": req.ID})
}

func (h *Handlers) AdminUpdateRole(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name  *string   `json:"name"`
		Perms *[]string `json:"perms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if id == repo.RoleRoot {
		fail(c, http.StatusBadRequest, "root 角色不可修改")
		return
	}
	role, err := h.repo.GetRole(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取角色失败")
		return
	}
	name := role.Name
	if req.Name != nil {
		if len(*req.Name) == 0 || len(*req.Name) > 32 {
			fail(c, http.StatusBadRequest, "角色名称需在 1-32 字符")
			return
		}
		name = *req.Name
	}
	perms := role.Perms
	if req.Perms != nil {
		if !repo.ValidPermSet(*req.Perms) {
			fail(c, http.StatusBadRequest, "权限位非法（不可包含通配符）")
			return
		}
		perms = *req.Perms
	}
	if err := h.repo.UpdateRole(c.Request.Context(), id, name, perms); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	h.idents.flush()
	ok(c, gin.H{"id": id})
}

func (h *Handlers) AdminDeleteRole(c *gin.Context) {
	id := c.Param("id")
	role, err := h.repo.GetRole(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "角色不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取角色失败")
		return
	}
	if role.Builtin {
		fail(c, http.StatusBadRequest, "内置角色不可删除")
		return
	}
	n, err := h.repo.CountUsersWithRole(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取用户失败")
		return
	}
	if n > 0 {
		fail(c, http.StatusBadRequest, "仍有用户挂靠该角色，请先改派后再删除")
		return
	}
	if err := h.repo.DeleteRole(c.Request.Context(), id); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	h.idents.flush()
	ok(c, gin.H{"deleted": id})
}
