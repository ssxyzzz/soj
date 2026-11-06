// 下载直链（v0.64 前端卡停根治批·病根③收口）：下载类接口不再经前端 blob 中转
// （旧通道 timeout:0 无超时、连接 stall 即按钮永挂、整包驻内存），改为「登录态下
// 先签发 5 分钟短签直链，浏览器原生下载器接管（进度/断点/取消/超时天然自带）」。
//
// 机制（拍板 2026-11-04：HMAC 短时签名 5min、密钥独立派生与 JWT 用途隔离）：
//
//	签发 POST /api/download-link {path}（AuthRequired，path 须匹配下载白名单）
//	  → {url} = /api<path>?_duid=<签发者>&_dexp=<Unix秒>&_dsig=<HMAC-SHA256 hex>
//	验证 下载端点挂 dlAuth：带 Authorization 头照旧登录态；无头则验签——签名绑
//	  「路径+原始query（剔除签名参数）+过期时刻+签发者uid」，恒时比较；通过后
//	  loadIdent 回放签发者权威身份（降权即时生效，与 AuthRequired 同语义），
//	  再走同一道权限位检查。handler 零改动：每周限额记账、组织者校验等内部
//	  逻辑按签发者身份照常执行。
package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
)

// dlTTL 直链有效期：够浏览器发起并续传一次下载，泄露窗口短。
const dlTTL = 5 * time.Minute

// dlPathRes 可直链化的下载端点白名单（服务端绝对路径形状，含 /api 前缀——与验证侧
// c.Request.URL.Path 同一形态；query 不限、参与签名）。新增下载端点须同步登记——
// 白名单防「任意路径签名」演变成越权通道。
var dlPathRes = []*regexp.Regexp{
	regexp.MustCompile(`^/api/problems/[1-9][0-9]*/testdata/download$`),
	regexp.MustCompile(`^/api/problems/export$`), // query ids= 多题打包
	regexp.MustCompile(`^/api/(contests|exams)/[1-9][0-9]*/export$`),
	regexp.MustCompile(`^/api/assignments/[1-9][0-9]*/export$`),
	regexp.MustCompile(`^/api/problems/[1-9][0-9]*/objective/questions\.yaml$`),
	regexp.MustCompile(`^/api/admin/users/import-template$`),
}

// resolveDownloadSecret 下载签名密钥：显式 env > 密钥卷 download_sign_secret >
// 自动生成 32 字节随机数持久化（与 resolveJWTSecret 同机制、不同文件、互不混用）。
func resolveDownloadSecret(cfg *config.Config) string {
	if s := cfg.DownloadSecret; s != "" {
		return s
	}
	path := filepath.Join(cfg.KeysDir, "download_sign_secret")
	if b, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) < 32 {
			fatalWeb("下载签名密钥卷文件内容不足 32 字符（疑似损坏），请删除该文件后重启以自动重新生成", "path", path)
		}
		return s
	} else if !os.IsNotExist(err) {
		fatalWeb("下载签名密钥卷文件不可读，请检查 SOJ_KEYS_DIR 挂载权限，或显式设置 SOJ_DOWNLOAD_SECRET", "path", path, "error", err)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fatalWeb("生成下载签名密钥失败", "error", err)
	}
	s := hex.EncodeToString(b)
	if err := os.MkdirAll(cfg.KeysDir, 0o700); err != nil {
		fatalWeb("密钥卷目录不可写，无法持久化自动生成的下载签名密钥", "path", cfg.KeysDir, "error", err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		fatalWeb("下载签名密钥卷文件不可写，请修复挂载权限，或显式设置 SOJ_DOWNLOAD_SECRET", "path", path, "error", err)
	}
	slog.Info("未设置 SOJ_DOWNLOAD_SECRET，已自动生成下载签名密钥并持久化（多实例部署请显式设置该环境变量）", "component", "web", "path", path)
	return s
}

// dlSignBase 签名基准串：路径 + 原始 query（剔除 _duid/_dexp/_dsig 三参数，保持其余
// 参数原顺序原编码）——签发与验证两侧按同一规则构造，避免 re-encode 漂移。
func dlSignBase(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}
	keep := make([]string, 0, 4)
	for _, kv := range strings.Split(rawQuery, "&") {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if k == "_duid" || k == "_dexp" || k == "_dsig" {
			continue
		}
		keep = append(keep, kv)
	}
	if len(keep) == 0 {
		return path
	}
	return path + "?" + strings.Join(keep, "&")
}

// dlSign 计算签名：HMAC-SHA256(密钥, base + "\n" + exp + "\n" + uid)。
func (h *Handlers) dlSign(base string, exp int64, uid int64) []byte {
	mac := hmac.New(sha256.New, []byte(h.cfg.DownloadSecret))
	mac.Write([]byte(base))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(strconv.FormatInt(uid, 10)))
	return mac.Sum(nil)
}

// dlAuth 下载端点鉴权（v0.64）：带 Authorization 头 = 登录态照旧（身份 + 权限位）；
// 无头 = 验短签直链，通过则回放签发者权威身份再过同一道权限位。perms 为空表示
// 权限由 handler 内部自查（成绩导出按组织者）。
func (h *Handlers) dlAuth(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" {
			if !h.authIdent(c) {
				return
			}
		} else if !h.verifyDownloadLink(c) {
			return
		}
		if len(perms) > 0 && !permAnyCheck(c, perms...) {
			return
		}
	}
}

// verifyDownloadLink 验签并回放签发者身份（loadIdent 查库，降权/禁用即时生效）。
func (h *Handlers) verifyDownloadLink(c *gin.Context) bool {
	unauthorized := func(msg string) bool {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": msg})
		return false
	}
	uidQ, expQ, sigQ := c.Query("_duid"), c.Query("_dexp"), c.Query("_dsig")
	if uidQ == "" || expQ == "" || sigQ == "" {
		return unauthorized("下载链接缺少签名参数，请回页面重新下载")
	}
	uid, err := strconv.ParseInt(uidQ, 10, 64)
	if err != nil || uid <= 0 {
		return unauthorized("下载链接无效，请回页面重新下载")
	}
	exp, err := strconv.ParseInt(expQ, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return unauthorized("下载链接已过期（有效期 5 分钟），请回页面重新下载")
	}
	want, err := hex.DecodeString(sigQ)
	if err != nil {
		return unauthorized("下载链接无效，请回页面重新下载")
	}
	base := dlSignBase(c.Request.URL.Path, c.Request.URL.RawQuery)
	if !hmac.Equal(h.dlSign(base, exp, uid), want) {
		return unauthorized("下载链接校验失败，请回页面重新下载")
	}
	e, ok := h.loadIdent(c.Request.Context(), uid)
	if !ok {
		return unauthorized("签发该链接的账号状态异常，请重新登录后再下载")
	}
	c.Set("uid", uid)
	c.Set("uname", e.uname)
	c.Set("role", e.role)
	c.Set("perms", e.perms)
	return true
}

// IssueDownloadLink POST /api/download-link：登录态下为白名单内的下载路径签发
// 短时直链。签发本身不限权限位——下载时 dlAuth 按回放身份执行同一道权限检查，
// 无权用户签了也下不了。
func (h *Handlers) IssueDownloadLink(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		fail(c, http.StatusBadRequest, "请提供要下载的路径")
		return
	}
	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	full := "/api" + path // 前端传 /problems/...（相对 /api），统一到服务端绝对形状
	// 白名单按「路径部分」匹配（query 参与签名、不受白名单约束）
	purePath := full
	if i := strings.IndexByte(full, '?'); i >= 0 {
		purePath = full[:i]
	}
	allowed := false
	for _, re := range dlPathRes {
		if re.MatchString(purePath) {
			allowed = true
			break
		}
	}
	if !allowed {
		fail(c, http.StatusBadRequest, "该路径不支持直链下载")
		return
	}
	uid, _, _, _ := currentUser(c)
	exp := time.Now().Add(dlTTL).Unix()
	rawQuery := ""
	if i := strings.IndexByte(full, '?'); i >= 0 {
		rawQuery = full[i+1:]
	}
	base := dlSignBase(purePath, rawQuery)
	sig := hex.EncodeToString(h.dlSign(base, exp, uid))
	q := "_duid=" + strconv.FormatInt(uid, 10) + "&_dexp=" + strconv.FormatInt(exp, 10) + "&_dsig=" + sig
	url := purePath + "?" + q
	if rawQuery != "" { // 原 query 保留在前，签名参数在后
		url = purePath + "?" + rawQuery + "&" + q
	}
	ok(c, gin.H{"url": url, "expires_in": int(dlTTL.Seconds())})
}
