package server

// 滑动续期（v0.82.7）：临期 token（剩余 < RenewWindow）过 AuthRequired 链时响应头
// 下发新票、新票寿命完整（窗口外不再触发）；新鲜/窗口外 token 不触发；改密后的
// 临期旧票（pver 不匹配）既 401 也不续期。手签指定寿命的票绕开 GenerateToken 的
// 固定 TTL，是本测试的关键道具；只挂 AuthRequired + 空 handler，隔离中间件行为。

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"soj/internal/auth"
	"soj/internal/repo"
)

// signTokenWithTTL 手工签指定寿命的 token（测试道具：GenerateToken 固定 7 天）。
func signTokenWithTTL(uid int64, uname string, pwdVer int, secret string, ttl time.Duration) string {
	claims := jwt.MapClaims{
		"uid": uid, "uname": uname, "pver": pwdVer,
		"iat": time.Now().Unix(), "exp": time.Now().Add(ttl).Unix(),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		panic(err)
	}
	return tok
}

func renewEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/ping", AuthRequired(h), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})
	return r
}

func pingWith(t *testing.T, r *gin.Engine, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthSlidingRenew(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := renewEngine(h)
	f.addUser(h, 1, "u1", repo.RoleDefault, 0)

	// ① 新鲜 token（GenerateToken = 完整 7 天）：不触发续期
	fresh := f.addUser(h, 1, "u1", repo.RoleDefault, 0)
	if w := pingWith(t, r, fresh); w.Code != http.StatusOK || w.Header().Get("X-Renewed-Token") != "" {
		t.Fatalf("新鲜 token 不应触发续期：code=%d header=%q", w.Code, w.Header().Get("X-Renewed-Token"))
	}

	// ② 窗口外（剩余 3 天 + 1h > RenewWindow）：不触发
	mid := signTokenWithTTL(1, "u1", 0, testJWTSecret, auth.RenewWindow+time.Hour)
	if w := pingWith(t, r, mid); w.Code != http.StatusOK || w.Header().Get("X-Renewed-Token") != "" {
		t.Fatalf("窗口外 token 不应触发续期：code=%d header=%q", w.Code, w.Header().Get("X-Renewed-Token"))
	}

	// ③ 临期 token（剩余 3 天 - 1h）：下发新票
	near := signTokenWithTTL(1, "u1", 0, testJWTSecret, auth.RenewWindow-time.Hour)
	w := pingWith(t, r, near)
	if w.Code != http.StatusOK {
		t.Fatalf("临期 token 应正常过鉴权：code=%d", w.Code)
	}
	renewed := w.Header().Get("X-Renewed-Token")
	if renewed == "" || renewed == near {
		t.Fatalf("临期 token 应下发新签续期票，got %q", renewed)
	}
	// 新票寿命完整（≈ TokenTTL），且可解析回同一身份
	uid, uname, pwdVer, exp, err := auth.ParseToken(renewed, testJWTSecret)
	if err != nil || uid != 1 || uname != "u1" || pwdVer != 0 {
		t.Fatalf("续期票身份不符：uid=%d uname=%q pver=%d err=%v", uid, uname, pwdVer, err)
	}
	if remain := time.Until(exp); remain < auth.TokenTTL-time.Minute {
		t.Fatalf("续期票寿命应接近完整 TokenTTL，remain=%v", remain)
	}
	// 续期票自身在窗口外：再请求不再续期（链式滑动闭环，无续期风暴）
	if w := pingWith(t, r, renewed); w.Code != http.StatusOK || w.Header().Get("X-Renewed-Token") != "" {
		t.Fatalf("续期票在窗口外不应再触发续期：code=%d header=%q", w.Code, w.Header().Get("X-Renewed-Token"))
	}

	// ④ 改密后的临期旧票（pver 落后）：401 且不续期（续期不越过全端下线语义）。
	// identCache 有 60s TTL，先按 ChangePassword 同款精准失效再改库值
	h.idents.invalidateUID(1)
	f.users[1].PwdVer = 1
	stale := signTokenWithTTL(1, "u1", 0, testJWTSecret, auth.RenewWindow-time.Hour)
	if w := pingWith(t, r, stale); w.Code != http.StatusUnauthorized || w.Header().Get("X-Renewed-Token") != "" {
		t.Fatalf("pver 不匹配应 401 且不续期：code=%d header=%q", w.Code, w.Header().Get("X-Renewed-Token"))
	}
}
