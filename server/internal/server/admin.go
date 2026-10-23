package server

import (
	"net/http"
	"strings"
	"time"

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
		j := userJSON(u)
		if u.LockedUntil.After(time.Now()) {
			j["locked_until"] = u.LockedUntil // 锁定中（v0.14 登录失败锁定），前端徽章展示
		}
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
	if msg := passwordProblem(req.Password); msg != "" {
		fail(c, http.StatusBadRequest, msg)
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
		Username *string `json:"username"`
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
	if req.Username != nil {
		// 用户名是登录身份标识，独立权限位控制（默认仅 root）
		if !hasPerm(c, repo.PermUserUsernameEdit) {
			fail(c, http.StatusForbidden, "当前角色不允许修改用户名")
			return
		}
		if !usernameRE.MatchString(*req.Username) {
			fail(c, http.StatusBadRequest, "用户名需为 3-32 位字母/数字/下划线")
			return
		}
		set["uname"] = *req.Username // User 结构体的 bson 字段名是 uname（裸 $set 不走结构体，勿用 API 字段名）
	}
	if req.Nickname != nil {
		nickname := trimTo(*req.Nickname, 32)
		if nickname == "" {
			fail(c, http.StatusBadRequest, "昵称不能为空")
			return
		}
		set["nickname"] = nickname
	}
	pwdResetUname := "" // 重置密码的审计对象（最终用户名：本请求可能同时改名）
	if req.Username != nil {
		pwdResetUname = *req.Username
	} else if u, err := h.repo.GetUserByID(c.Request.Context(), id); err == nil {
		pwdResetUname = u.Username
	}
	if req.Password != nil {
		if msg := passwordProblem(*req.Password); msg != "" {
			fail(c, http.StatusBadRequest, msg)
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
		if isDupKey(err) {
			fail(c, http.StatusBadRequest, "用户名已存在")
			return
		}
		fail(c, http.StatusInternalServerError, "更新失败")
		return
	}
	if req.Username != nil {
		// 冗余 uname（提交记录/班级成员展示名）同步；失败如实上报——用户名本体已生效
		if err := h.repo.SyncUnameReferences(c.Request.Context(), id, *req.Username); err != nil {
			fail(c, http.StatusInternalServerError, "用户名已改，但提交记录/班级成员展示名同步失败，请查看日志")
			return
		}
	}
	if req.Password != nil {
		// v0.14：重置密码联动 pwdVer+1，该用户全部旧 token 立即失效（防捡到旧登录态继续用）
		if err := h.repo.BumpPwdVer(c.Request.Context(), id); err != nil {
			fail(c, http.StatusInternalServerError, "密码已重置，但登录态失效标记写入失败，请查看日志")
			return
		}
		h.securityLog(c, repo.SecPwdResetAdmin, id, pwdResetUname, "管理员重置密码，该账号全部登录态已下线")
	}
	if req.Role != nil || req.Username != nil || req.Password != nil {
		h.idents.flush() // 角色/用户名/密码版本变更即时生效（不等缓存 TTL）
	}
	ok(c, gin.H{"updated": id})
}

// ---- 系统设置（root） ----

// GetSettings 读取系统设置（root）。公告/备份说明为 Markdown（v0.8 起单一事实源）。
func (h *Handlers) GetSettings(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	ok(c, gin.H{
		"rate_limit_per_minute":     s.RateLimitPerMinute,
		"home_notice":               s.HomeNotice,
		"backup_guide":              s.BackupGuide,
		"data_download_daily_limit": s.DownloadDailyLimit(),
		"autocomplete_min_level":    s.MinAutocompleteLevel(),
		"allow_register":            s.RegistrationOpen(),
		"icp_number":                s.IcpNumber,
		"batch_import_problem_mb":   s.BatchImportProblemMBVal(),
		"batch_import_zip_mb":       s.BatchImportZipMBVal(),
	})
}

// GetSiteInfo 站点公开信息（v0.13 收编原 /auth/register-open）：注册开关 + ICP 备案号，
// 登录页页签显隐与页脚悬挂共用；后续公开站点事实（v0.16 注册验证方式等）并入此处。
func (h *Handlers) GetSiteInfo(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, gin.H{"register_open": s.RegistrationOpen(), "icp_number": s.IcpNumber})
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

// UpdateSettings 更新系统设置（公告/备份说明收 Markdown）。
func (h *Handlers) UpdateSettings(c *gin.Context) {
	var req struct {
		RateLimitPerMinute     *int    `json:"rate_limit_per_minute"`
		HomeNotice             *string `json:"home_notice"`
		BackupGuide            *string `json:"backup_guide"`
		DataDownloadDailyLimit *int    `json:"data_download_daily_limit"`
		AutocompleteMinLevel   *int    `json:"autocomplete_min_level"`
		AllowRegister          *bool   `json:"allow_register"`
		IcpNumber              *string `json:"icp_number"`
		BatchImportProblemMB   *int    `json:"batch_import_problem_mb"` // 批量导入单题数据上限 MB（v0.15）
		BatchImportZipMB       *int    `json:"batch_import_zip_mb"`     // 批量导入整包 zip 上限 MB（v0.15）
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
	if req.DataDownloadDailyLimit != nil {
		if *req.DataDownloadDailyLimit < 0 || *req.DataDownloadDailyLimit > 1000 {
			fail(c, http.StatusBadRequest, "每日可下载数据的题目数需在 0-1000 之间（0 为不限制）")
			return
		}
		s.DataDownloadDailyLimit = req.DataDownloadDailyLimit
	}
	if req.AutocompleteMinLevel != nil {
		if !repo.ValidDifficulty(*req.AutocompleteMinLevel) {
			fail(c, http.StatusBadRequest, "自动补全解锁等级需在 1-10 之间")
			return
		}
		s.AutocompleteMinLevel = req.AutocompleteMinLevel
	}
	if req.AllowRegister != nil {
		s.AllowRegister = req.AllowRegister
	}
	if req.IcpNumber != nil {
		icp := strings.TrimSpace(*req.IcpNumber)
		if len(icp) > 64 {
			fail(c, http.StatusBadRequest, "备案号过长（上限 64 字符）")
			return
		}
		s.IcpNumber = icp
	}
	if req.BackupGuide != nil {
		if len(*req.BackupGuide) > 16<<10 {
			fail(c, http.StatusBadRequest, "备份使用说明过长（上限 16KB）")
			return
		}
		s.BackupGuide = *req.BackupGuide
	}
	if req.BatchImportProblemMB != nil {
		if *req.BatchImportProblemMB < 1 || *req.BatchImportProblemMB > 4096 {
			fail(c, http.StatusBadRequest, "批量导入单题数据上限需在 1-4096MB 之间")
			return
		}
		s.BatchImportProblemMB = req.BatchImportProblemMB
	}
	if req.BatchImportZipMB != nil {
		if *req.BatchImportZipMB < 1 || *req.BatchImportZipMB > 8192 {
			fail(c, http.StatusBadRequest, "批量导入整包上限需在 1-8192MB 之间")
			return
		}
		s.BatchImportZipMB = req.BatchImportZipMB
	}
	if err := h.repo.UpdateSystemSettings(c.Request.Context(), s); err != nil {
		fail(c, http.StatusInternalServerError, "更新失败")
		return
	}
	ok(c, gin.H{
		"rate_limit_per_minute":     s.RateLimitPerMinute,
		"home_notice":               s.HomeNotice,
		"backup_guide":              s.BackupGuide,
		"data_download_daily_limit": s.DownloadDailyLimit(),
		"autocomplete_min_level":    s.MinAutocompleteLevel(),
		"allow_register":            s.RegistrationOpen(),
		"icp_number":                s.IcpNumber,
		"batch_import_problem_mb":   s.BatchImportProblemMBVal(),
		"batch_import_zip_mb":       s.BatchImportZipMBVal(),
	})
}
