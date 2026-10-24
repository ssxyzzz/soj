package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/auth"
	"soj/internal/repo"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// 登录失败锁定（v0.14）：连续失败 5 次锁 15 分钟，到期自动解锁，不做人工解锁路径。
const (
	loginMaxFailures  = 5
	loginLockDuration = 15 * time.Minute
)

// 弱口令黑名单（v0.14）：仅拦截新设/修改，存量账号登录不受影响。
var weakPasswords = map[string]struct{}{
	"12345678": {}, "123456789": {}, "1234567890": {}, "11223344": {},
	"password": {}, "password1": {}, "password123": {}, "p@ssw0rd": {},
	"qwerty123": {}, "1qaz2wsx": {}, "abcd1234": {}, "abc12345": {},
	"a1234567": {}, "123123123": {}, "11111111": {}, "00000000": {},
	"66666666": {}, "88888888": {}, "888888888": {}, "iloveyou": {},
}

// passwordProblem 密码强度（v0.14）：8-64 位、不在弱口令黑名单。
// （原「不得与用户名相同或包含用户名」已按需求移除）
// 注册/管理员建号/重置/本人改密四处统一调用；通过返回空串。
func passwordProblem(pwd string) string {
	if len(pwd) < 8 || len(pwd) > 64 {
		return "密码长度需在 8-64 位之间"
	}
	if _, bad := weakPasswords[strings.ToLower(pwd)]; bad {
		return "密码过于简单（命中常见弱口令），请更换"
	}
	return ""
}

func userJSON(u *repo.User) gin.H {
	return gin.H{"id": u.ID, "username": u.Username, "nickname": u.Nickname, "role": u.Role, "email": u.Email}
}

// userWithPermsJSON 登录态响应用：附生效权限位（root 展开为全目录，前端据此显隐入口）。
func (h *Handlers) userWithPermsJSON(ctx context.Context, u *repo.User) gin.H {
	perms := []string{}
	if role, err := h.repo.GetRole(ctx, u.Role); err == nil {
		perms = repo.EffectivePerms(role)
	}
	j := userJSON(u)
	j["perms"] = perms
	return j
}

func (h *Handlers) Register(c *gin.Context) {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	if !s.RegistrationOpen() {
		fail(c, http.StatusForbidden, "注册已关闭，请联系管理员")
		return
	}
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Nickname string `json:"nickname"`
		Email    string `json:"email"` // 注册验证方式 = email 时必填（带验证码）
		Code     string `json:"code"`
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
	// 邮箱验证注册（v0.16）：验证码先于建号校验；mode=none 时拒收邮箱字段
	//（未经验证的邮箱不落库，防伪绑定）
	email := ""
	if s.RegisterVerifyModeVal() == repo.RegisterVerifyEmail {
		email = NormalizeEmail(req.Email)
		if !emailRE.MatchString(email) || len(email) > 254 {
			fail(c, http.StatusBadRequest, "邮箱格式不正确")
			return
		}
		if _, err := h.repo.GetUserByEmail(c.Request.Context(), email); err == nil {
			fail(c, http.StatusBadRequest, "该邮箱已被注册")
			return
		} else if !repo.IsNotFound(err) {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		if msg := h.verifyCode(c, repo.VerifyPurposeRegister, email, req.Code); msg != "" {
			fail(c, http.StatusBadRequest, msg)
			return
		}
	} else if req.Email != "" {
		fail(c, http.StatusBadRequest, "当前注册无需邮箱，请留空")
		return
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}

	ctx := c.Request.Context()
	if _, _, err := h.repo.GetUserByUsername(ctx, req.Username); err == nil {
		fail(c, http.StatusBadRequest, "用户名已存在")
		return
	} else if !repo.IsNotFound(err) {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	var uid int64
	if email != "" {
		uid, err = h.repo.CreateUserWithEmail(ctx, req.Username, req.Password, nickname, repo.RoleDefault, email)
	} else {
		uid, err = h.repo.CreateUserWithPassword(ctx, req.Username, req.Password, nickname, repo.RoleDefault)
	}
	if err != nil {
		if isDupKey(err) {
			fail(c, http.StatusBadRequest, "用户名已存在")
			return
		}
		fail(c, http.StatusInternalServerError, "创建用户失败")
		return
	}
	token, err := auth.GenerateToken(uid, req.Username, 0, h.cfg.JWTSecret)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	u := &repo.User{ID: uid, Username: req.Username, Nickname: nickname, Role: repo.RoleDefault, Email: email}
	ok(c, gin.H{"token": token, "user": h.userWithPermsJSON(c.Request.Context(), u)})
}

// 注册开关等站点公开状态见 GetSiteInfo（admin.go，v0.13 收编 /auth/register-open）。

func (h *Handlers) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	ctx := c.Request.Context()
	u, hash, err := h.repo.GetUserByUsername(ctx, req.Username)
	if repo.IsNotFound(err) {
		auth.DummyCheck(req.Password) // v0.14：等代价 bcrypt，消除「用户名是否存在」的时间差
		h.securityLog(c, repo.SecLoginFailure, 0, req.Username, "账号不存在")
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	// 锁定检查在密码比对之前：锁定期内正确密码也拒绝，不给爆破继续试的口子（v0.14）
	if u.LockedUntil.After(time.Now()) {
		mins := int(math.Ceil(time.Until(u.LockedUntil).Minutes()))
		h.securityLog(c, repo.SecLoginLocked, u.ID, u.Username,
			fmt.Sprintf("锁定期内尝试登录，剩余约 %d 分钟", mins))
		fail(c, http.StatusLocked, fmt.Sprintf("连续失败次数过多，账号已锁定，请约 %d 分钟后再试", mins))
		return
	}
	if !auth.CheckPassword(hash, req.Password) {
		count, locked, ferr := h.repo.RecordLoginFailure(ctx, u.ID, loginMaxFailures, loginLockDuration)
		if ferr != nil {
			log.Printf("[security] 登录失败计数写入失败 uid=%d: %v", u.ID, ferr)
		}
		if locked {
			// 本次失败恰好达到阈值：直接给锁定文案（与锁定期内尝试的提示衔接）
			h.securityLog(c, repo.SecLoginLocked, u.ID, u.Username,
				fmt.Sprintf("连续 %d 次失败，锁定 %d 分钟", loginMaxFailures, int(loginLockDuration.Minutes())))
			fail(c, http.StatusUnauthorized,
				fmt.Sprintf("用户名或密码错误，已连续失败 %d 次，账号锁定 %d 分钟", loginMaxFailures, int(loginLockDuration.Minutes())))
			return
		}
		h.securityLog(c, repo.SecLoginFailure, u.ID, u.Username, fmt.Sprintf("密码错误（连续第 %d 次）", count))
		fail(c, http.StatusUnauthorized, fmt.Sprintf("用户名或密码错误（已连续失败 %d 次，还剩 %d 次机会）",
			count, loginMaxFailures-count))
		return
	}
	if err := h.repo.ClearLoginFailures(ctx, u.ID); err != nil {
		log.Printf("[security] 清理登录失败计数失败 uid=%d: %v", u.ID, err) // 不阻断登录
	}
	h.securityLog(c, repo.SecLoginSuccess, u.ID, u.Username, "")
	token, err := auth.GenerateToken(u.ID, u.Username, u.PwdVer, h.cfg.JWTSecret)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	h.idents.flush() // 登录时失效缓存，确保拿到最新角色/昵称
	ok(c, gin.H{"token": token, "user": h.userWithPermsJSON(c.Request.Context(), u)})
}

// ChangePassword 本人修改密码（v0.14）：验旧密码 → 强度校验 → 落库并 pwdVer+1，
// 该账号所有旧 token（含本请求的）立即失效，前端引导重新登录。
// 前端二次确认（新密码输两遍）在 UI 层；原密码错误返回 400 而非 401——
// 401 会触发前端拦截器自动登出跳转，原密码打错不该把人踢出去。
func (h *Handlers) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	uid, uname, _, _ := currentUser(c)
	if msg := passwordProblem(req.NewPassword); msg != "" {
		fail(c, http.StatusBadRequest, msg)
		return
	}
	ctx := c.Request.Context()
	u, err := h.repo.GetUserByID(ctx, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.OldPassword) {
		fail(c, http.StatusBadRequest, "原密码不正确")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	if err := h.repo.UpdatePassword(ctx, uid, hash); err != nil {
		fail(c, http.StatusInternalServerError, "修改失败")
		return
	}
	h.idents.flush() // 缓存里的 pwdVer 立即失效，旧 token 全端 401
	h.securityLog(c, repo.SecPwdChange, uid, uname, "本人修改密码，全部登录态已下线")
	if s, _ := h.repo.GetSystemSettings(ctx); s != nil && u.Email != "" {
		h.notifyEmailAsync(s, u.Email, "密码已被修改",
			map[string]string{"username": uname, "action": "的登录密码已在个人设置中修改"})
	}
	ok(c, gin.H{"changed": true})
}

func (h *Handlers) Me(c *gin.Context) {
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	u, err := h.repo.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	ok(c, h.userWithPermsJSON(c.Request.Context(), u))
}

// parseID 解析路径参数中的数字 ID。
func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "非法的 ID")
		return 0, false
	}
	return id, true
}

// isDupKey MongoDB 唯一索引冲突（并发注册同名用户的兜底）。
func isDupKey(err error) bool {
	var we mongo.WriteException
	if errors.As(err, &we) {
		return we.HasErrorCode(11000)
	}
	return false
}
