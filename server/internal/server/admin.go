package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

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
		j["phone"] = u.Phone // 绑定信息（v0.16）：管理端可见；phone 未启用 UI，仅落库展示
		// 资料（v0.34）：管理端编辑弹窗回填（grade 为显示名，空串 = 未设置）
		j["gender"] = u.Gender
		j["grade"] = u.GradeName()
		j["school"] = u.School
		j["region"] = u.Region
		items = append(items, j)
	}
	ok(c, gin.H{"list": items, "total": total})
}

// AdminUnbindEmail 解绑用户邮箱（root 硬编码，学生换邮箱场景；v0.16）。
func (h *Handlers) AdminUnbindEmail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	u, err := h.repo.GetUserByID(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if u.Email == "" {
		fail(c, http.StatusBadRequest, "该用户未绑定邮箱")
		return
	}
	if err := h.repo.UnbindEmail(c.Request.Context(), id); err != nil {
		fail(c, http.StatusInternalServerError, "解绑失败")
		return
	}
	_, uname, _, _ := currentUser(c)
	h.securityLog(c, repo.SecEmailUnbind, id, u.Username, "root "+uname+" 解绑邮箱 "+u.Email)
	ok(c, gin.H{"unbound": true})
}

func (h *Handlers) AdminCreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Nickname string `json:"nickname"`
		Role     string `json:"role"`
		ID       *int64 `json:"id"` // 手动指定用户 ID（v0.24 小批；缺省自动从 100 起取号）
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
	if req.ID != nil && (*req.ID < 1 || *req.ID > 1_000_000_000) {
		fail(c, http.StatusBadRequest, "用户 ID 需为正整数（上限 10 亿）")
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
	var (
		uid int64
		err error
	)
	if req.ID != nil {
		// 手动指定 ID：占用冲突在唯一索引层兜底（用户名/ID 两个唯一键都可能撞）
		uid, err = h.repo.CreateUserWithID(c.Request.Context(), *req.ID, req.Username, req.Password, nickname, role)
	} else {
		uid, err = h.repo.CreateUserWithPassword(c.Request.Context(), req.Username, req.Password, nickname, role)
	}
	if err != nil {
		if isDupKey(err) {
			// 两个唯一键都可能撞（uname / _id）：先分辨给准确提示
			if _, _, uerr := h.repo.GetUserByUsername(c.Request.Context(), req.Username); uerr == nil {
				fail(c, http.StatusConflict, "用户名已存在")
				return
			}
			if req.ID != nil {
				fail(c, http.StatusConflict, "该用户 ID 已被占用")
				return
			}
			fail(c, http.StatusConflict, "用户已存在")
			return
		}
		fail(c, http.StatusInternalServerError, "创建用户失败")
		return
	}
	// 首登改密提醒：无 ID 分支走 CreateUserWithPassword（注册/种子同源）不带标志，
	// 建号后补置位——初始密码由管理员发放，下次登录提醒改密（可跳过）。best-effort 不阻断建号。
	if err := h.repo.SetPwdRemind(c.Request.Context(), uid, true); err != nil {
		slog.Warn("首登改密提醒置位失败", "component", "admin", "uid", uid, "error", err)
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
		// 资料（v0.34）：管理端编辑不受自改开关限制——锁定后的唯一修改口 + 迁移补录入口
		Gender *string `json:"gender"`
		Grade  *string `json:"grade"`  // 显示名/数字（空串 = 清空）；见 repo.ParseGrade
		School *string `json:"school"`
		Region *string `json:"region"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var patch repo.UserPatch
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
		patch.Role = req.Role
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
		patch.Uname = req.Username // patch 内部映射到 bson 字段 uname（勿用 API 字段名）
	}
	if req.Nickname != nil {
		nickname := trimTo(*req.Nickname, maxNicknameRunes)
		if nickname == "" {
			fail(c, http.StatusBadRequest, "昵称不能为空")
			return
		}
		patch.Nickname = &nickname
	}
	// 资料字段（v0.34）：性别/学校/地区 trim 截断同自助口径；年级显示名/数字解析
	if req.Gender != nil {
		g := trimTo(*req.Gender, 8)
		patch.Gender = &g
	}
	if req.School != nil {
		s := trimTo(*req.School, 64)
		patch.School = &s
	}
	if req.Region != nil {
		rg := trimTo(*req.Region, 32)
		patch.Region = &rg
	}
	if req.Grade != nil {
		g := strings.TrimSpace(*req.Grade)
		if no := repo.ParseGrade(g); no > 0 || g == "" {
			patch.GradeNo = &no
		} else {
			fail(c, http.StatusBadRequest, "年级无法识别（支持：一年级…高三 / 初一…初三 / 已毕业 / 数字 1-12）")
			return
		}
	}
	pwdResetUname := "" // 重置密码的审计对象（最终用户名：本请求可能同时改名）
	oldRole := ""       // 角色变更审计对象：更新前的原角色（v0.59）
	if req.Username != nil {
		pwdResetUname = *req.Username
	}
	if req.Username != nil || req.Role != nil {
		if u, err := h.repo.GetUserByID(c.Request.Context(), id); err == nil {
			if req.Username == nil {
				pwdResetUname = u.Username
			}
			if req.Role != nil {
				oldRole = u.Role
			}
		}
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
		patch.PwdHash = &hash
	}
	if patch.Empty() {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateUser(c.Request.Context(), id, patch); err != nil {
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
		// 首登改密提醒：重置产生的新初始密码由管理员发放，下次登录提醒改密（可跳过）。best-effort。
		if err := h.repo.SetPwdRemind(c.Request.Context(), id, true); err != nil {
			slog.Warn("首登改密提醒置位失败", "component", "admin", "uid", id, "error", err)
		}
		h.securityLog(c, repo.SecPwdResetAdmin, id, pwdResetUname, "管理员重置密码，该账号全部登录态已下线")
		// v0.16：改密通知原邮箱（防盗号无感知；依赖 smtp 通道，log 模式跳过）
		if s, _ := h.repo.GetSystemSettings(c.Request.Context()); s != nil {
			if u, err := h.repo.GetUserByID(c.Request.Context(), id); err == nil && u.Email != "" {
				h.notifyEmailAsync(s, u.Email, "密码已被重置",
					map[string]string{"username": pwdResetUname, "action": "的登录密码已被管理员重置"})
			}
		}
	}
	if req.Role != nil {
		// v0.59：角色变更落安全审计（提权/降权可查，与改密/改 ID 同级）
		h.securityLog(c, repo.SecRoleChange, id, pwdResetUname, "角色变更："+oldRole+" → "+*req.Role)
	}
	if req.Role != nil || req.Username != nil || req.Password != nil {
		h.idents.flush() // 角色/用户名/密码版本变更即时生效（不等缓存 TTL）
	}
	ok(c, gin.H{"updated": id})
}

// AdminChangeUserID PATCH /api/admin/users/:id/change-id（v0.50 管理端改 ID）。
// uid 是全站身份主键：搬家在 repo 事务内原子完成，成功后失效身份缓存（被改者旧
// token 立即下线，原用户名密码重登即恢复）并落 uid_change 安全审计。
func (h *Handlers) AdminChangeUserID(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		NewID int64 `json:"newId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.NewID <= 0 {
		fail(c, http.StatusBadRequest, "新 ID 需为正整数")
		return
	}
	if req.NewID == id {
		fail(c, http.StatusBadRequest, "新 ID 与当前 ID 相同")
		return
	}
	stats, err := h.repo.ChangeUserID(c.Request.Context(), id, req.NewID)
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrChangeIDBadArgs):
			fail(c, http.StatusBadRequest, "ID 非法（须为不相等的正整数）")
		case errors.Is(err, repo.ErrChangeIDNotFound):
			fail(c, http.StatusNotFound, "用户不存在")
		case errors.Is(err, repo.ErrChangeIDBusy):
			fail(c, http.StatusBadRequest, "目标 ID 已被占用")
		case errors.Is(err, repo.ErrChangeIDRoot):
			fail(c, http.StatusForbidden, "root 账号 ID 受保护，不可修改")
		default:
			slog.Error("改用户 ID 失败（事务已回滚）", "component", "admin",
				"old", id, "new", req.NewID, "error", err)
			fail(c, http.StatusInternalServerError, "改 ID 失败，数据未变更（事务已整体回滚）")
		}
		return
	}
	total := int64(0)
	for _, n := range stats {
		total += n
	}
	h.idents.flush() // 旧 uid 的身份缓存与 token 立即失效
	if u, err := h.repo.GetUserByID(c.Request.Context(), req.NewID); err == nil {
		h.securityLog(c, repo.SecUIDChange, req.NewID, u.Username,
			fmt.Sprintf("用户 ID #%d 改为 #%d（跨集合迁移记录 %d 条）", id, req.NewID, total))
	}
	ok(c, gin.H{"stats": stats, "moved": total})
}

// ---- 系统设置（root） ----

// GetSettings 读取系统设置（root）。公告/备份说明为 Markdown（v0.8 起单一事实源）。
func (h *Handlers) GetSettings(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	comboStep, comboPct, comboCap := s.JadeComboCfg()
	ok(c, gin.H{
		"rate_limit_per_minute":     s.RateLimitPerMinute,
		"home_notice":               s.HomeNotice,
		"data_download_weekly_limit": s.DownloadWeeklyLimit(),
		"autocomplete_min_level":    s.MinAutocompleteLevel(),
		"allow_register":            s.RegistrationOpen(),
		"icp_number":                s.IcpNumber,
		"mps_number":                s.MpsNumber,
		"batch_import_problem_mb":   s.BatchImportProblemMBVal(),
		"batch_import_zip_mb":       s.BatchImportZipMBVal(),
		// 邮箱通道（v0.16）
		"mail_channel":           s.MailChannelVal(),
		"smtp_host":              s.SMTP.Host,
		"smtp_port":              s.SMTP.Port,
		"smtp_username":          s.SMTP.Username,
		"smtp_password":          s.SMTP.Password,
		"smtp_from":              s.SMTP.From,
		"register_verify_mode":   s.RegisterVerifyModeVal(),
		"password_reset_enabled": s.PasswordResetOpen(),
		// 讨论区与通知（v0.24）
		"discuss_global_enabled":     s.DiscussGlobalOpen(),
		"discuss_problem_enabled":    s.DiscussProblemOpen(),
		"discuss_contest_enabled":    s.DiscussContestOpen(),
		"discuss_global_locked":      s.DiscussGlobalLocked,
		"discuss_topic_daily_limit":  s.DiscussTopicDailyLimitVal(),
		"discuss_reply_interval_sec": s.DiscussReplyIntervalVal(),
		"discuss_newbie_days":        s.DiscussNewbieDaysVal(),
		"discuss_report_fold":        int(s.DiscussReportFoldVal()),
		"notify_retention_days":      s.NotifyRetentionDaysVal(),
		"notify_broadcast_per_day":   s.NotifyBroadcastPerDayVal(),
		"user_seq_start":             s.UserSeqStartVal(),
		"problem_seq_start":          s.ProblemSeqStartVal(),
		"currency_name":           s.CurrencyNameVal(),
		"jade_award_rate_pct":     s.JadeAwardRatePctVal(),
		"jade_discount":           normalizeJadeDiscountLocal(s.JadeDiscount),
		"jade_combo_step":         comboStep,
		"jade_combo_pct":          comboPct,
		"jade_combo_cap_pct":      comboCap,
		"jade_revenge_hours":      s.JadeRevengeHoursVal(),
		// 资料自改开关（v0.34）：五字段逐个可锁
		"profile_edit_nickname": s.OpenNickname(),
		"profile_edit_gender":   s.OpenGender(),
		"profile_edit_grade":    s.OpenGrade(),
		"profile_edit_school":   s.OpenSchool(),
		"profile_edit_region":   s.OpenRegion(),
	})
}

// normalizeJadeDiscountLocal GET 回显归一（与 repo.normalizeJadeDiscount 同规则：夹取+单调）。
func normalizeJadeDiscountLocal(d [6]int) [6]int {
	def := [6]int{95, 90, 85, 80, 75, 70}
	for i := range d {
		if d[i] < 10 || d[i] > 100 {
			d[i] = def[i]
		}
	}
	for i := 4; i >= 0; i-- {
		if d[i] < d[i+1] {
			d[i] = d[i+1]
		}
	}
	return d
}

// GetSiteInfo 站点公开信息（v0.13 收编原 /auth/register-open）：注册开关 + ICP 备案号，
// 登录页页签显隐与页脚悬挂共用；后续公开站点事实（v0.16 注册验证方式等）并入此处。
func (h *Handlers) GetSiteInfo(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, gin.H{
		"register_open": s.RegistrationOpen(),
		"icp_number":    s.IcpNumber,
		"mps_number":    s.MpsNumber,
		// 注册验证方式与找回开关（v0.16）：登录页注册表单出邮箱验证码、忘记密码入口显隐
		"register_verify_mode":   s.RegisterVerifyModeVal(),
		"password_reset_enabled": s.PasswordResetOpen(),
		// 讨论区三区总开关（公开 UI 显隐用）：关掉的区，导航项/入口卡片直接隐藏
		"discuss_global_enabled":  s.DiscussGlobalOpen(),
		"discuss_problem_enabled": s.DiscussProblemOpen(),
		// 实物兑换开关（v0.33-②，公开）：导航入口显隐
		"jade_shop_enabled": s.JadeShopOpen(),
		"discuss_contest_enabled": s.DiscussContestOpen(),
	})
}

// GetNotice 首页公告（公开，首页免登录展示）。
func (h *Handlers) GetNotice(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取公告失败")
		return
	}
	ok(c, gin.H{"content": s.HomeNotice, "html": s.HomeNoticeHtml})
}

// UpdateSettings 更新系统设置（公告收 Markdown）。
func (h *Handlers) UpdateSettings(c *gin.Context) {
	var req struct {
		RateLimitPerMinute     *int    `json:"rate_limit_per_minute"`
		HomeNotice             *string `json:"home_notice"`
		DataDownloadWeeklyLimit *int    `json:"data_download_weekly_limit"`
		AutocompleteMinLevel   *int    `json:"autocomplete_min_level"`
		AllowRegister          *bool   `json:"allow_register"`
		IcpNumber              *string `json:"icp_number"`
		MpsNumber              *string `json:"mps_number"`
		BatchImportProblemMB   *int    `json:"batch_import_problem_mb"` // 批量导入单题数据上限 MB（v0.15）
		BatchImportZipMB       *int    `json:"batch_import_zip_mb"`     // 批量导入整包 zip 上限 MB（v0.15）
		// 邮箱通道（v0.16）
		MailChannel          *string `json:"mail_channel"`
		SMTPHost             *string `json:"smtp_host"`
		SMTPPort             *int    `json:"smtp_port"`
		SMTPUsername         *string `json:"smtp_username"`
		SMTPPassword         *string `json:"smtp_password"`
		SMTPFrom             *string `json:"smtp_from"`
		RegisterVerifyMode   *string `json:"register_verify_mode"`
		PasswordResetEnabled *bool   `json:"password_reset_enabled"`
		// 讨论区与通知（v0.24）
		DiscussGlobalEnabled    *bool `json:"discuss_global_enabled"`
		DiscussProblemEnabled   *bool `json:"discuss_problem_enabled"`
		DiscussContestEnabled   *bool `json:"discuss_contest_enabled"`
		DiscussGlobalLocked     *bool `json:"discuss_global_locked"`
		DiscussTopicDailyLimit  *int  `json:"discuss_topic_daily_limit"`
		DiscussReplyIntervalSec *int  `json:"discuss_reply_interval_sec"`
		DiscussNewbieDays       *int  `json:"discuss_newbie_days"`
		DiscussReportFold       *int  `json:"discuss_report_fold"`
		NotifyRetentionDays     *int  `json:"notify_retention_days"`
		NotifyBroadcastPerDay   *int  `json:"notify_broadcast_per_day"`
		UserSeqStart            *int  `json:"user_seq_start"` // 注册取号起点（v0.24 小批）
		ProblemSeqStart         *int  `json:"problem_seq_start"` // 自动题号起点
		CurrencyName            *string `json:"currency_name"` // 货币名（v0.33，1–6 字，默认「玉」）
		JadeAwardRatePct        *int  `json:"jade_award_rate_pct"` // 比赛奖池获奖比例 %（v0.33）
		JadeComboStep           *int  `json:"jade_combo_step"`     // 连击加成步长（每满 N 连击一档，v0.45）
		JadeComboPct            *int  `json:"jade_combo_pct"`      // 每档加成 %（v0.45）
		JadeComboCapPct         *int  `json:"jade_combo_cap_pct"`  // 加成封顶 %（v0.45，100=峰值×2）
		JadeRevengeHours        *int  `json:"jade_revenge_hours"`  // 复仇窗口小时（v0.45）
		JadeDiscount            *[6]int `json:"jade_discount"`    // 玉折扣六档 Lv5–10 %（v0.40）
		JadeSolutionUnlockMult  *int  `json:"jade_solution_unlock_mult"` // 题解解锁价系数（v0.33-②）
		JadeBountyMin           *int  `json:"jade_bounty_min"` // 悬赏额下限（v0.33-②）
		JadeBountyMax           *int  `json:"jade_bounty_max"` // 悬赏额上限（v0.33-②）
		JadeShopEnabled         *bool `json:"jade_shop_enabled"` // 实物兑换开关（v0.33-②，root）
		// 资料自改开关（v0.34）：五字段逐个可锁（true = 允许本人改）
		ProfileEditNickname *bool `json:"profile_edit_nickname"`
		ProfileEditGender   *bool `json:"profile_edit_gender"`
		ProfileEditGrade    *bool `json:"profile_edit_grade"`
		ProfileEditSchool   *bool `json:"profile_edit_school"`
		ProfileEditRegion   *bool `json:"profile_edit_region"`
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
	if req.DataDownloadWeeklyLimit != nil {
		if *req.DataDownloadWeeklyLimit < 0 || *req.DataDownloadWeeklyLimit > 1000 {
			fail(c, http.StatusBadRequest, "每周可下载数据的题目数需在 0-1000 之间（0 为不限制）")
			return
		}
		s.DataDownloadWeeklyLimit = req.DataDownloadWeeklyLimit
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
	if req.MpsNumber != nil {
		mps := strings.TrimSpace(*req.MpsNumber)
		if len(mps) > 64 {
			fail(c, http.StatusBadRequest, "公安网备号过长（上限 64 字符）")
			return
		}
		s.MpsNumber = mps
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
	// ---- 邮箱通道（v0.16）----
	if req.MailChannel != nil {
		ch := *req.MailChannel
		if ch != repo.MailChannelLog && ch != repo.MailChannelSMTP {
			fail(c, http.StatusBadRequest, "邮件通道仅支持 log / smtp")
			return
		}
		s.MailChannel = ch
	}
	// SMTP 三项均可清空（log 通道不用、切回时清配置），仅超长拒绝。
	if req.SMTPHost != nil {
		v := strings.TrimSpace(*req.SMTPHost)
		if len(v) > 253 {
			fail(c, http.StatusBadRequest, "SMTP 主机过长（上限 253 字符）")
			return
		}
		s.SMTP.Host = v
	}
	if req.SMTPPort != nil {
		if *req.SMTPPort < 0 || *req.SMTPPort > 65535 {
			fail(c, http.StatusBadRequest, "SMTP 端口需在 0-65535 之间（0 = 未配置）")
			return
		}
		s.SMTP.Port = *req.SMTPPort
	}
	if req.SMTPUsername != nil {
		v := strings.TrimSpace(*req.SMTPUsername)
		if len(v) > 254 {
			fail(c, http.StatusBadRequest, "SMTP 账号过长（上限 254 字符）")
			return
		}
		s.SMTP.Username = v
	}
	if req.SMTPPassword != nil {
		if len(*req.SMTPPassword) > 128 {
			fail(c, http.StatusBadRequest, "SMTP 授权码过长（上限 128 字符）")
			return
		}
		s.SMTP.Password = strings.TrimSpace(*req.SMTPPassword)
	}
	if req.SMTPFrom != nil {
		v := strings.TrimSpace(*req.SMTPFrom)
		if len(v) > 254 {
			fail(c, http.StatusBadRequest, "发件人过长（上限 254 字符）")
			return
		}
		s.SMTP.From = v
	}
	if req.RegisterVerifyMode != nil {
		m := *req.RegisterVerifyMode
		if m != repo.RegisterVerifyNone && m != repo.RegisterVerifyEmail {
			fail(c, http.StatusBadRequest, "注册验证方式仅支持 none / email")
			return
		}
		s.RegisterVerifyMode = m
	}
	if req.PasswordResetEnabled != nil {
		s.PasswordResetEnabled = req.PasswordResetEnabled
	}
	// ---- 讨论区与通知（v0.24）----
	if req.DiscussGlobalEnabled != nil {
		s.DiscussGlobalEnabled = req.DiscussGlobalEnabled
	}
	if req.DiscussProblemEnabled != nil {
		s.DiscussProblemEnabled = req.DiscussProblemEnabled
	}
	if req.DiscussContestEnabled != nil {
		s.DiscussContestEnabled = req.DiscussContestEnabled
	}
	if req.DiscussGlobalLocked != nil {
		s.DiscussGlobalLocked = *req.DiscussGlobalLocked
	}
	if req.DiscussTopicDailyLimit != nil {
		if *req.DiscussTopicDailyLimit < 0 || *req.DiscussTopicDailyLimit > 100 {
			fail(c, http.StatusBadRequest, "每日发帖上限需在 0-100 之间（0 为不限制）")
			return
		}
		s.DiscussTopicDailyLimit = req.DiscussTopicDailyLimit
	}
	if req.DiscussReplyIntervalSec != nil {
		if *req.DiscussReplyIntervalSec < 0 || *req.DiscussReplyIntervalSec > 3600 {
			fail(c, http.StatusBadRequest, "回复间隔需在 0-3600 秒之间（0 为不限制）")
			return
		}
		s.DiscussReplyIntervalSec = req.DiscussReplyIntervalSec
	}
	if req.DiscussNewbieDays != nil {
		if *req.DiscussNewbieDays < 0 || *req.DiscussNewbieDays > 30 {
			fail(c, http.StatusBadRequest, "新号冷静期需在 0-30 天之间（0 为不限制）")
			return
		}
		s.DiscussNewbieDays = req.DiscussNewbieDays
	}
	if req.DiscussReportFold != nil {
		if *req.DiscussReportFold < 0 || *req.DiscussReportFold > 100 {
			fail(c, http.StatusBadRequest, "举报折叠阈值需在 0-100 之间（0 为不自动折叠）")
			return
		}
		s.DiscussReportFold = req.DiscussReportFold
	}
	if req.NotifyRetentionDays != nil {
		if *req.NotifyRetentionDays < 7 || *req.NotifyRetentionDays > 365 {
			fail(c, http.StatusBadRequest, "通知保留天数需在 7-365 之间")
			return
		}
		s.NotifyRetentionDays = req.NotifyRetentionDays
	}
	if req.NotifyBroadcastPerDay != nil {
		if *req.NotifyBroadcastPerDay < 0 || *req.NotifyBroadcastPerDay > 100 {
			fail(c, http.StatusBadRequest, "广播每日上限需在 0-100 之间（0 为不限制）")
			return
		}
		s.NotifyBroadcastPerDay = req.NotifyBroadcastPerDay
	}
	if req.UserSeqStart != nil {
		if *req.UserSeqStart < 1 || *req.UserSeqStart > 1_000_000_000 {
			fail(c, http.StatusBadRequest, "注册取号起点需为正整数（上限 10 亿）")
			return
		}
		s.UserSeqStart = req.UserSeqStart
	}
	if req.ProblemSeqStart != nil {
		if *req.ProblemSeqStart < 1 || *req.ProblemSeqStart > 1_000_000_000 {
			fail(c, http.StatusBadRequest, "自动题号起点需为正整数（上限 10 亿）")
			return
		}
		s.ProblemSeqStart = req.ProblemSeqStart
	}
	if req.CurrencyName != nil {
		name := strings.TrimSpace(*req.CurrencyName)
		if n := len([]rune(name)); n < 1 || n > 6 {
			fail(c, http.StatusBadRequest, "货币名需为 1–6 个字（如：玉 / 金币 / 钻石 / 奖励币）")
			return
		}
		s.CurrencyName = name
	}
	if req.JadeAwardRatePct != nil {
		if *req.JadeAwardRatePct < 1 || *req.JadeAwardRatePct > 100 {
			fail(c, http.StatusBadRequest, "奖池获奖比例需在 1–100 之间")
			return
		}
		s.JadeAwardRatePct = req.JadeAwardRatePct
	}
	if req.JadeDiscount != nil {
		if len(*req.JadeDiscount) != 6 {
			fail(c, http.StatusBadRequest, "玉折扣需为 6 档（Lv5–10）")
			return
		}
		s.JadeDiscount = *req.JadeDiscount
	}
	if req.JadeSolutionUnlockMult != nil {
		if *req.JadeSolutionUnlockMult < 1 || *req.JadeSolutionUnlockMult > 100 {
			fail(c, http.StatusBadRequest, "题解解锁价系数需在 1–100 之间")
			return
		}
		s.JadeSolutionUnlockMult = req.JadeSolutionUnlockMult
	}
	if req.JadeBountyMin != nil {
		if *req.JadeBountyMin < 1 || *req.JadeBountyMin > 100000 {
			fail(c, http.StatusBadRequest, "悬赏下限需在 1–100000 之间")
			return
		}
		s.JadeBountyMin = req.JadeBountyMin
	}
	if req.JadeBountyMax != nil {
		if *req.JadeBountyMax < 1 || *req.JadeBountyMax > 1000000 {
			fail(c, http.StatusBadRequest, "悬赏上限需在 1–1000000 之间")
			return
		}
		s.JadeBountyMax = req.JadeBountyMax
	}
	if s.JadeBountyMaxVal() < s.JadeBountyMinVal() {
		fail(c, http.StatusBadRequest, "悬赏上限不能小于下限")
		return
	}
	if req.JadeShopEnabled != nil {
		s.JadeShopEnabled = req.JadeShopEnabled
	}
	// 连击加成/复仇窗口（v0.45）：范围校验后落字段（normalize 兜底双保险）
	if req.JadeComboStep != nil {
		if *req.JadeComboStep < 1 || *req.JadeComboStep > 1000 {
			fail(c, http.StatusBadRequest, "连击步长需在 1–1000 之间")
			return
		}
		s.JadeComboStep = req.JadeComboStep
	}
	if req.JadeComboPct != nil {
		if *req.JadeComboPct < 1 || *req.JadeComboPct > 100 {
			fail(c, http.StatusBadRequest, "连击每档加成需在 1–100% 之间")
			return
		}
		s.JadeComboPct = req.JadeComboPct
	}
	if req.JadeComboCapPct != nil {
		if *req.JadeComboCapPct < 0 || *req.JadeComboCapPct > 900 {
			fail(c, http.StatusBadRequest, "连击加成封顶需在 0–900% 之间")
			return
		}
		s.JadeComboCapPct = req.JadeComboCapPct
	}
	if req.JadeRevengeHours != nil {
		if *req.JadeRevengeHours < 1 || *req.JadeRevengeHours > 24*30 {
			fail(c, http.StatusBadRequest, "复仇窗口需在 1–720 小时之间")
			return
		}
		s.JadeRevengeHours = req.JadeRevengeHours
	}
	// 资料自改开关（v0.34）：先归一到当前生效配置再覆盖出现项（未出现的保持原值）
	if req.ProfileEditNickname != nil || req.ProfileEditGender != nil || req.ProfileEditGrade != nil ||
		req.ProfileEditSchool != nil || req.ProfileEditRegion != nil {
		pe := s.ProfileEditCfg()
		if req.ProfileEditNickname != nil {
			pe.Nickname = *req.ProfileEditNickname
		}
		if req.ProfileEditGender != nil {
			pe.Gender = *req.ProfileEditGender
		}
		if req.ProfileEditGrade != nil {
			pe.Grade = *req.ProfileEditGrade
		}
		if req.ProfileEditSchool != nil {
			pe.School = *req.ProfileEditSchool
		}
		if req.ProfileEditRegion != nil {
			pe.Region = *req.ProfileEditRegion
		}
		s.ProfileEdit = &pe
	}
	// 与启动校验同一口径：smtp 通道开着但配置不全不让存——否则重启直接拒绝启动，
	// 管理员把自己锁在门外（运行期 Mailer() 会降级 log，但重启过不去）。
	if s.MailChannelVal() == repo.MailChannelSMTP && !s.SMTP.Ready() {
		fail(c, http.StatusBadRequest, "邮件通道为 smtp 但配置不完整（需主机/端口/发件人），请补全或先切回 log 通道")
		return
	}
	if err := h.repo.UpdateSystemSettings(c.Request.Context(), s); err != nil {
		fail(c, http.StatusInternalServerError, "更新失败")
		return
	}
	ok(c, gin.H{
		"rate_limit_per_minute":     s.RateLimitPerMinute,
		"home_notice":               s.HomeNotice,
		"data_download_weekly_limit": s.DownloadWeeklyLimit(),
		"autocomplete_min_level":    s.MinAutocompleteLevel(),
		"allow_register":            s.RegistrationOpen(),
		"icp_number":                s.IcpNumber,
		"mps_number":                s.MpsNumber,
		"batch_import_problem_mb":   s.BatchImportProblemMBVal(),
		"batch_import_zip_mb":       s.BatchImportZipMBVal(),
		// 邮箱通道（v0.16）
		"mail_channel":           s.MailChannelVal(),
		"smtp_host":              s.SMTP.Host,
		"smtp_port":              s.SMTP.Port,
		"smtp_username":          s.SMTP.Username,
		"smtp_password":          s.SMTP.Password,
		"smtp_from":              s.SMTP.From,
		"register_verify_mode":   s.RegisterVerifyModeVal(),
		"password_reset_enabled": s.PasswordResetOpen(),
		// 资料自改开关（v0.34）
		"profile_edit_nickname": s.OpenNickname(),
		"profile_edit_gender":   s.OpenGender(),
		"profile_edit_grade":    s.OpenGrade(),
		"profile_edit_school":   s.OpenSchool(),
		"profile_edit_region":   s.OpenRegion(),
	})
}

// AdminTodoCounts GET /api/admin/todo-counts —— 后台导航待办徽标（v0.35-①）：
// 待处理举报数 + 待发货订单数。字段级权限：讨论数需 discussion.manage、
// 订单数仅 root（与各自列表端点口径一致），未授权字段直接省略——前端徽标自然不渲染。
// 徽标属提示性数据，单项计数失败仅省略该字段，不让整个导航角标 500。
func (h *Handlers) AdminTodoCounts(c *gin.Context) {
	out := gin.H{}
	if hasPerm(c, repo.PermDiscussManage) {
		if n, err := h.repo.CountOpenDiscussReports(c.Request.Context()); err == nil {
			out["discuss_reports"] = n
		}
	}
	if _, _, role, logged := currentUser(c); logged && role == repo.RoleRoot {
		if n, err := h.repo.CountPendingShopOrders(c.Request.Context()); err == nil {
			out["shop_orders"] = n
		}
	}
	ok(c, out)
}
