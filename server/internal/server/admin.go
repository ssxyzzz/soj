package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/auth"
	"soj/internal/repo"
)

// ---- 管理端（root）：用户管理 ----

func (h *Handlers) AdminListUsers(c *gin.Context) {
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListUsers(c.Request.Context(), page, pageSize, c.Query("q"), c.Query("role"))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取用户失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, u := range list {
		items = append(items, userJSON(u))
	}
	ok(c, gin.H{"list": items, "total": total})
}

func (h *Handlers) AdminCreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Nickname string `json:"nickname"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if !usernameRE.MatchString(req.Username) {
		fail(c, http.StatusBadRequest, "用户名需为 3-32 位字母/数字/下划线")
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 64 {
		fail(c, http.StatusBadRequest, "密码长度需在 6-64 之间")
		return
	}
	role := req.Role
	if role == "" {
		role = repo.RoleDefault
	}
	if !repo.ValidRoles[role] {
		fail(c, http.StatusBadRequest, "角色非法")
		return
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}
	uid, err := h.repo.CreateUserWithPassword(c.Request.Context(), req.Username, req.Password, nickname, role)
	if err != nil {
		if isDupKey(err) {
			fail(c, http.StatusBadRequest, "用户名已存在")
			return
		}
		fail(c, http.StatusInternalServerError, "创建用户失败")
		return
	}
	ok(c, gin.H{"id": uid, "username": req.Username, "role": role})
}

func (h *Handlers) AdminUpdateUser(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
		Nickname *string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	set := bson.M{}
	if req.Role != nil {
		// 初始管理员（ID=1）的角色不可修改：防止误降权把自己锁在 root 之外
		if id == repo.InitialAdminID && *req.Role != repo.RoleRoot {
			fail(c, http.StatusBadRequest, "初始管理员（ID=1）的 root 角色受保护，不可修改")
			return
		}
		if !repo.ValidRoles[*req.Role] {
			fail(c, http.StatusBadRequest, "角色非法")
			return
		}
		set["role"] = *req.Role
	}
	if req.Nickname != nil && *req.Nickname != "" {
		set["nickname"] = *req.Nickname
	}
	if req.Password != nil {
		if len(*req.Password) < 6 || len(*req.Password) > 64 {
			fail(c, http.StatusBadRequest, "密码长度需在 6-64 之间")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		set["pwdHash"] = hash
	}
	if len(set) == 0 {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateUser(c.Request.Context(), id, set); err != nil {
		if repo.IsNotFound(err) {
			fail(c, http.StatusNotFound, "用户不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "更新失败")
		return
	}
	ok(c, gin.H{"updated": id})
}

// ---- 系统设置（root） ----

// GetSettings 读取系统设置（root）。
func (h *Handlers) GetSettings(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	ok(c, gin.H{"rate_limit_per_minute": s.RateLimitPerMinute, "home_notice": s.HomeNotice})
}

// GetNotice 首页公告（公开，首页免登录展示）。
func (h *Handlers) GetNotice(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取公告失败")
		return
	}
	ok(c, gin.H{"content": s.HomeNotice})
}

// UpdateSettings 更新系统设置。
func (h *Handlers) UpdateSettings(c *gin.Context) {
	var req struct {
		RateLimitPerMinute *int    `json:"rate_limit_per_minute"`
		HomeNotice         *string `json:"home_notice"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	if req.RateLimitPerMinute != nil {
		if *req.RateLimitPerMinute < 1 || *req.RateLimitPerMinute > 60 {
			fail(c, http.StatusBadRequest, "每分钟提交上限需在 1-60 之间")
			return
		}
		s.RateLimitPerMinute = *req.RateLimitPerMinute
	}
	if req.HomeNotice != nil {
		if len(*req.HomeNotice) > 64<<10 {
			fail(c, http.StatusBadRequest, "公告过长")
			return
		}
		s.HomeNotice = *req.HomeNotice
	}
	if err := h.repo.UpdateSystemSettings(c.Request.Context(), s); err != nil {
		fail(c, http.StatusInternalServerError, "更新失败")
		return
	}
	ok(c, gin.H{"rate_limit_per_minute": s.RateLimitPerMinute, "home_notice": s.HomeNotice})
}
