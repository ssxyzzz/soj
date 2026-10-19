package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/auth"
	"soj/internal/repo"
	"soj/internal/richtext"
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
		j := userJSON(u)
		j["real_name"] = u.RealName // 真实姓名：后台用户列表对 admin.user.manage 持有者可见（导出场景另见 user.realname.view）
		items = append(items, j)
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
	if _, err := h.repo.GetRole(c.Request.Context(), role); repo.IsNotFound(err) {
		fail(c, http.StatusBadRequest, "角色不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取角色失败")
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
		RealName *string `json:"real_name"` // 真实姓名：仅管理员可改（本人只读，见 /me/profile）
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
		if _, err := h.repo.GetRole(c.Request.Context(), *req.Role); repo.IsNotFound(err) {
			fail(c, http.StatusBadRequest, "角色不存在")
			return
		} else if err != nil {
			fail(c, http.StatusInternalServerError, "读取角色失败")
			return
		}
		set["role"] = *req.Role
	}
	if req.Nickname != nil && *req.Nickname != "" {
		set["nickname"] = *req.Nickname
	}
	if req.RealName != nil {
		set["realName"] = trimTo(*req.RealName, 32) // 允许显式清空
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
	if req.Role != nil {
		h.idents.flush() // 角色变更即时生效（不等缓存 TTL）
	}
	ok(c, gin.H{"updated": id})
}

// ---- 系统设置（root） ----

// GetSettings 读取系统设置（root）。公告/备份说明返回 md（回退源）+ html（现役）双字段。
func (h *Handlers) GetSettings(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	ok(c, gin.H{
		"rate_limit_per_minute": s.RateLimitPerMinute,
		"home_notice":           s.HomeNotice,
		"home_notice_html":      noticeHTML(s),
		"backup_guide":          s.BackupGuide,
		"backup_guide_html":     backupGuideHTML(s),
	})
}

// noticeHTML 公告富文本：contentHtml 优先，存量 md 读时计算（编辑即迁移）。
func noticeHTML(s *repo.SystemSettings) string {
	if s.HomeNoticeHTML != "" {
		return s.HomeNoticeHTML
	}
	if s.HomeNotice == "" {
		return ""
	}
	return richtext.MdToHTML(s.HomeNotice)
}

// backupGuideHTML 备份说明富文本：同上；空 = 前端内置默认。
func backupGuideHTML(s *repo.SystemSettings) string {
	if s.BackupGuideHTML != "" {
		return s.BackupGuideHTML
	}
	if s.BackupGuide == "" {
		return ""
	}
	return richtext.MdToHTML(s.BackupGuide)
}

// GetNotice 首页公告（公开，首页免登录展示）。
func (h *Handlers) GetNotice(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取公告失败")
		return
	}
	ok(c, gin.H{"content": s.HomeNotice, "content_html": noticeHTML(s)})
}

// UpdateSettings 更新系统设置（公告/备份说明 v0.6 起收富文本 HTML，写 _html 字段；
// 原 md 字段冻结为回退源）。
func (h *Handlers) UpdateSettings(c *gin.Context) {
	var req struct {
		RateLimitPerMinute *int    `json:"rate_limit_per_minute"`
		HomeNoticeHTML     *string `json:"home_notice_html"`
		BackupGuideHTML    *string `json:"backup_guide_html"`
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
	if req.HomeNoticeHTML != nil {
		if len(*req.HomeNoticeHTML) > 64<<10 {
			fail(c, http.StatusBadRequest, "公告过长")
			return
		}
		s.HomeNoticeHTML = *req.HomeNoticeHTML
	}
	if req.BackupGuideHTML != nil {
		if len(*req.BackupGuideHTML) > 16<<10 {
			fail(c, http.StatusBadRequest, "备份使用说明过长（上限 16KB）")
			return
		}
		s.BackupGuideHTML = *req.BackupGuideHTML
	}
	if err := h.repo.UpdateSystemSettings(c.Request.Context(), s); err != nil {
		fail(c, http.StatusInternalServerError, "更新失败")
		return
	}
	ok(c, gin.H{
		"rate_limit_per_minute": s.RateLimitPerMinute,
		"home_notice_html":      s.HomeNoticeHTML,
		"backup_guide_html":     s.BackupGuideHTML,
	})
}
