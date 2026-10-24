package server

// 邮箱验证码与自助找回（v0.16）：发码（注册/找回/绑定三用途）、验证码注册、
// 找回密码重置、绑定/换绑邮箱。配套通知邮件（改密/换绑向原邮箱告知）。
// 通道 = SMTP 或 log（log 不外发打日志，人工转告）；短信只留 provider 接口。

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/auth"
	"soj/internal/mailer"
	"soj/internal/repo"
)

// 发送频控（五层，plan §2.8 + 2026-10-23 拍板数值）。
const (
	codeCooldown     = 60 * time.Second // 同目标 60s 冷却
	codeTargetPer1h  = 5                // 同目标 1 小时上限
	codeTargetPer1d  = 10               // 同目标 24 小时上限
	codeIPPerDay     = 30               // 同 IP 每日上限（全部用途合计）
	codeGlobalPerDay = 300              // 全局每日上限
)

var emailRE = regexp.MustCompile(`^[^@\s]{1,64}@[^@\s]{1,255}\.[^@\s]{2,}$`)

// NormalizeEmail 邮箱归一：trim + 转小写（绑定/查重/发码统一口径）。
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// codeHMAC 验证码哈希密钥：JWT secret 域分隔派生（sha256("soj:verifycode:"+secret)），
// 与 token 签名隔离——换 JWT secret 旧验证码一并失效（5 分钟有效期下无感）。
func (h *Handlers) codeHMAC(code string) string {
	sum := sha256.Sum256([]byte("soj:verifycode:" + h.cfg.JWTSecret))
	mac := hmac.New(sha256.New, sum[:])
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

// genCode 6 位数字验证码（crypto/rand）。
func genCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000" // 系统熵源不可用属灾难，宁可给固定值也不 panic
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// mailProvider 当前通道 provider（每次发信现读 settings，后台改配置即时生效）。
func (h *Handlers) mailProvider(c *gin.Context) (mailer.Provider, *repo.SystemSettings, error) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		return nil, nil, err
	}
	return s.Mailer(), s, nil
}

// codeRateCheck 五层频控；拒绝时返回对用户友好的文案（不给精确额度，防探测）。
func codeRateCheck(q *repo.VerifyCodeRateQuery) string {
	if since := time.Since(q.LastAt); since < codeCooldown {
		wait := int((codeCooldown-since+time.Minute-1) / time.Minute)
		return fmt.Sprintf("发送过于频繁，请约 %d 分钟后再试", wait)
	}
	if q.Target1h >= codeTargetPer1h {
		return "该邮箱发送次数已达本小时上限，请稍后再试"
	}
	if q.Target1d >= codeTargetPer1d {
		return "该邮箱今日发送次数已达上限，请明天再试"
	}
	if q.IP1d >= codeIPPerDay {
		return "当前网络今日发送次数已达上限，请明天再试"
	}
	if q.Global1d >= codeGlobalPerDay {
		return "站点今日验证码发送量已达上限，请联系管理员"
	}
	return ""
}

// sendVerifyCode 组装并发送验证码邮件；成功后落库。
func (h *Handlers) sendVerifyCode(c *gin.Context, s *repo.SystemSettings, provider mailer.Provider,
	purpose, target, username, tpl string) error {
	code := genCode()
	vars := map[string]string{
		"code":     code,
		"username": username,
		"site":     "SOJ",
		"action":   "",
	}
	subject := "【SOJ】邮箱验证码"
	if purpose == repo.VerifyPurposeReset {
		subject = "【SOJ】找回密码验证码"
	}
	body := mailer.Render(tpl, vars)
	if err := provider.Send(target, subject, body); err != nil {
		return err
	}
	now := time.Now()
	return h.repo.InsertVerifyCode(c.Request.Context(), &repo.VerifyCode{
		Purpose: purpose, Target: target, CodeHash: h.codeHMAC(code),
		IP: c.ClientIP(), CreatedAt: now,
		ExpiresAt: now.Add(repo.VerifyCodeTTL), PurgeAt: now.Add(repo.VerifyCodePurgeTTL),
	})
}

// EmailCode 发送验证码。purpose=register/reset 公开（防枚举口径见各分支），
// purpose=bind 需登录；换绑场景发往新邮箱前须先验证旧邮箱（密码或旧邮箱验证码二选一）。
func (h *Handlers) EmailCode(c *gin.Context) {
	var req struct {
		Purpose     string `json:"purpose" binding:"required"`
		Email       string `json:"email"`
		OldMode     string `json:"old_mode"` // 换绑验证旧邮箱：password | code
		OldPassword string `json:"old_password"`
		OldCode     string `json:"old_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	email := NormalizeEmail(req.Email)
	// reset 用途的 email 字段接受用户名或邮箱（找回入口单框输入，后端解析出绑定邮箱）；
	// register/bind 必须是合法邮箱
	if req.Purpose != repo.VerifyPurposeReset {
		if !emailRE.MatchString(email) || len(email) > 254 {
			fail(c, http.StatusBadRequest, "邮箱格式不正确")
			return
		}
	}
	valid := false
	for _, p := range repo.VerifyPurposes {
		if req.Purpose == p {
			valid = true
			break
		}
	}
	if !valid {
		fail(c, http.StatusBadRequest, "不支持的验证码用途")
		return
	}

	provider, s, err := h.mailProvider(c)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	var resetUsername string // 找回命中的用户名（模板 {{username}}）
	var bindUsername string  // 首次绑定的用户名

	ctx := c.Request.Context()
	switch req.Purpose {
	case repo.VerifyPurposeRegister:
		// 注册：注册开放 + 邮箱验证模式才可发；邮箱已被占用明确提示（小社区口径，不防枚举）
		if !s.RegistrationOpen() {
			fail(c, http.StatusForbidden, "注册已关闭，请联系管理员")
			return
		}
		if s.RegisterVerifyModeVal() != repo.RegisterVerifyEmail {
			fail(c, http.StatusBadRequest, "当前注册无需邮箱验证")
			return
		}
		if _, err := h.repo.GetUserByEmail(ctx, email); err == nil {
			fail(c, http.StatusBadRequest, "该邮箱已被注册")
			return
		} else if !repo.IsNotFound(err) {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
	case repo.VerifyPurposeReset:
		// 找回：须后台开启；账号（用户名或邮箱）不存在/未绑邮箱时静默按成功返回
		//（防枚举），不发信。命中用户时按其绑定邮箱投递，模板带用户名。
		if !s.PasswordResetOpen() {
			fail(c, http.StatusBadRequest, "未开启找回密码，请联系管理员")
			return
		}
		var u *repo.User
		var uerr error
		if strings.Contains(email, "@") {
			u, uerr = h.repo.GetUserByEmail(ctx, email)
		} else {
			u, _, uerr = h.repo.GetUserByUsername(ctx, strings.TrimSpace(req.Email))
		}
		if uerr != nil || u == nil || u.Email == "" {
			ok(c, gin.H{"sent": true, "message": "若该账号存在，验证码已发送"})
			return
		}
		email = u.Email
		resetUsername = u.Username
	case repo.VerifyPurposeBind:
		uid, bindUname, _, logged := currentUser(c)
		if !logged {
			fail(c, http.StatusUnauthorized, "未登录")
			return
		}
		u, err := h.repo.GetUserByID(ctx, uid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		if u.Email != "" {
			if email == u.Email {
				// 发往当前绑定邮箱 = 换绑流程的旧邮箱验证码
				break
			}
			// 发往新邮箱（换绑）：先验证旧邮箱——密码或旧邮箱验证码二选一
			switch req.OldMode {
			case "password":
				if !auth.CheckPassword(u.PasswordHash, req.OldPassword) {
					fail(c, http.StatusBadRequest, "密码不正确")
					return
				}
			case "code":
				if msg := h.verifyCode(c, repo.VerifyPurposeBind, u.Email, req.OldCode); msg != "" {
					fail(c, http.StatusBadRequest, msg)
					return
				}
			default:
				fail(c, http.StatusBadRequest, "换绑邮箱需先验证旧邮箱（密码或旧邮箱验证码）")
				return
			}
		} else {
			bindUsername = bindUname
		}
	}

	if q, err := h.repo.QueryVerifyCodeRate(ctx, req.Purpose, email, c.ClientIP()); err == nil {
		if msg := codeRateCheck(q); msg != "" {
			// reset 防枚举补口：频控拒绝若回 429 文案，攻击者对同一账号连发两次
			// 即可区分「存在（进冷却）」与「不存在（无记录不冷却）」——与发码
			// 成功统一同一文案（邮件本来就没发，真实用户等不到也不算被骗）。
			if req.Purpose == repo.VerifyPurposeReset {
				ok(c, gin.H{"sent": true, "message": "若该账号存在，验证码已发送"})
				return
			}
			fail(c, http.StatusTooManyRequests, msg)
			return
		}
	} else {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	tpl := s.TplRegisterVal()
	if req.Purpose == repo.VerifyPurposeReset {
		tpl = s.TplResetVal()
	}
	codeUsername := bindUsername
	if req.Purpose == repo.VerifyPurposeReset {
		codeUsername = resetUsername
	}
	if err := h.sendVerifyCode(c, s, provider, req.Purpose, email, codeUsername, tpl); err != nil {
		log.Printf("[email] 验证码发送失败 purpose=%s target=%s: %v", req.Purpose, email, err)
		fail(c, http.StatusInternalServerError, "邮件发送失败，请稍后再试或联系管理员")
		return
	}
	// reset 用途防枚举：发码成功与账号不存在/未绑定走同一文案——
	// 否则攻击者可借两种响应的差异探测「哪些用户名存在且绑了邮箱」。
	msg := "验证码已发送，请查收邮箱（5 分钟内有效）"
	if req.Purpose == repo.VerifyPurposeReset {
		msg = "若该账号存在，验证码已发送"
	}
	ok(c, gin.H{"sent": true, "message": msg})
}

// verifyCode 校验验证码（哈希比对 + 失败计数 + 一次性消费）。
// 返回空串 = 通过；否则返回用户可见的错误文案。成功时已删除文档。
func (h *Handlers) verifyCode(c *gin.Context, purpose, target, code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "请输入验证码"
	}
	vc, err := h.repo.GetValidVerifyCode(c.Request.Context(), purpose, target)
	if repo.IsNotFound(err) {
		return "验证码无效或已过期，请重新获取"
	}
	if err != nil {
		return "服务器内部错误"
	}
	if !hmac.Equal([]byte(h.codeHMAC(code)), []byte(vc.CodeHash)) {
		left, err := h.repo.FailVerifyCode(c.Request.Context(), vc.ID, vc.Attempts)
		if err != nil {
			log.Printf("[email] 验证码失败计数写入失败: %v", err)
		}
		if left > 0 {
			return fmt.Sprintf("验证码错误，还可尝试 %d 次", left)
		}
		return "验证码错误次数过多已作废，请重新获取"
	}
	if err := h.repo.ConsumeVerifyCode(c.Request.Context(), vc.ID); err != nil {
		log.Printf("[email] 验证码消费删除失败: %v", err)
	}
	return ""
}

// PasswordReset 找回密码自助重置：验证码 + 新密码；成功后 pwdVer+1 全端下线并跳登录。
// account 可为用户名或邮箱；防枚举：账号不存在/未绑邮箱与验证码错误返回同一文案。
func (h *Handlers) PasswordReset(c *gin.Context) {
	var req struct {
		Account  string `json:"account" binding:"required"`
		Code     string `json:"code" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	if !s.PasswordResetOpen() {
		fail(c, http.StatusBadRequest, "未开启找回密码，请联系管理员")
		return
	}
	codeErrMsg := "验证码错误或已过期，请重新获取"
	ctx := c.Request.Context()
	var u *repo.User
	account := strings.TrimSpace(req.Account)
	if strings.Contains(account, "@") {
		if u, err = h.repo.GetUserByEmail(ctx, NormalizeEmail(account)); err != nil {
			fail(c, http.StatusBadRequest, codeErrMsg)
			return
		}
	} else {
		if u, _, err = h.repo.GetUserByUsername(ctx, account); err != nil {
			fail(c, http.StatusBadRequest, codeErrMsg)
			return
		}
	}
	if u.Email == "" {
		fail(c, http.StatusBadRequest, codeErrMsg)
		return
	}
	if msg := h.verifyCode(c, repo.VerifyPurposeReset, u.Email, req.Code); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	if msg := passwordProblem(req.Password); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	if err := h.repo.UpdatePassword(ctx, u.ID, hash); err != nil {
		fail(c, http.StatusInternalServerError, "重置失败")
		return
	}
	_ = h.repo.ClearLoginFailures(ctx, u.ID)
	h.idents.flush()
	h.securityLog(c, repo.SecPwdReset, u.ID, u.Username, "找回密码自助重置，全部登录态已下线")
	h.notifyEmailAsync(s, u.Email, "密码已被重置",
		map[string]string{"username": u.Username, "action": "的登录密码已被通过邮箱验证码重置"})
	ok(c, gin.H{"reset": true})
}

// EmailBind 绑定/换绑邮箱（登录态）：验证新邮箱验证码后落库。
// 换绑（旧邮箱存在且不同）的旧邮箱验证已在发码时完成（密码或旧邮箱验证码）。
func (h *Handlers) EmailBind(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required"`
		Code  string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	uid, uname, _, _ := currentUser(c)
	email := NormalizeEmail(req.Email)
	if !emailRE.MatchString(email) || len(email) > 254 {
		fail(c, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	ctx := c.Request.Context()
	u, err := h.repo.GetUserByID(ctx, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	if u.Email != "" && email == u.Email {
		fail(c, http.StatusBadRequest, "新邮箱与当前绑定一致")
		return
	}
	if msg := h.verifyCode(c, repo.VerifyPurposeBind, email, req.Code); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	oldEmail := u.Email
	if err := h.repo.SetUserEmail(ctx, uid, email); err != nil {
		if isDupKey(err) {
			fail(c, http.StatusBadRequest, "该邮箱已被其他账号绑定")
			return
		}
		fail(c, http.StatusInternalServerError, "绑定失败")
		return
	}
	h.securityLog(c, repo.SecEmailBind, uid, uname,
		map[bool]string{true: "换绑邮箱 " + oldEmail + " → " + email, false: "绑定邮箱 " + email}[oldEmail != ""])
	s, _ := h.repo.GetSystemSettings(ctx)
	if s != nil && oldEmail != "" {
		// 换绑告知原邮箱，防盗号无感知（依赖通道开启；log 模式跳过——见 plan §2.8）
		h.notifyEmailAsync(s, oldEmail, "邮箱已被换绑",
			map[string]string{"username": uname, "action": "的绑定邮箱已被换绑为 " + email})
	}
	ok(c, gin.H{"bound": true, "email": email})
}

// notifyEmailAsync 账号安全通知（异步）：仅 smtp 通道真实发信（log 模式跳过，
// plan §2.8 口径）；发送失败只打日志。net/smtp 无 ctx 版本，超时依赖拨号超时。
func (h *Handlers) notifyEmailAsync(s *repo.SystemSettings, to, subject string, vars map[string]string) {
	if to == "" || s.MailChannelVal() != repo.MailChannelSMTP {
		return
	}
	body := mailer.Render(s.TplNotifyVal(), vars)
	provider := mailer.NewSMTP(s.SMTP)
	go func() {
		if err := provider.Send(to, subject, body); err != nil {
			log.Printf("[email] 通知邮件发送失败 to=%s subject=%q: %v", to, subject, err)
		}
	}()
}
