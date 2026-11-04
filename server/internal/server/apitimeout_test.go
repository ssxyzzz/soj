package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 白名单匹配口径（v0.66 批②单测锁）：长路由命中、普通路由与相近路径不误命中。
func TestIsLongRoute(t *testing.T) {
	yes := []string{
		"/api/problems/123/run",
		"/api/problems/123/ai",
		"/api/problems/123/ai/hint/2",
		"/api/submissions/456/ai-debug",
		"/api/problems/export",
		"/api/problems/789/testdata/download",
		"/api/contests/5/export",
		"/api/exams/7/export",
		"/api/assignments/9/export",
		"/api/admin/backup/download",
		"/api/admin/users/import-commit",
	}
	for _, p := range yes {
		if !isLongRoute(p) {
			t.Errorf("应命中白名单：%s", p)
		}
	}
	no := []string{
		"/api/auth/login", "/api/problems", "/api/problems/123",
		"/api/problems/123/submissions", "/api/contests/5/leaderboard",
		"/api/submissions/456", "/api/render", "/api/admin/backup/run",
	}
	for _, p := range no {
		if isLongRoute(p) {
			t.Errorf("不应命中白名单：%s", p)
		}
	}
}

// 中间件行为（v0.66 批②单测锁）：普通路由挂上 deadline、白名单路由保持无 deadline。
func TestAPITimeoutAppliesDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(apiTimeoutWith(50 * time.Millisecond))
	hasDeadline := func(c *gin.Context) {
		_, ok := c.Request.Context().Deadline()
		c.JSON(http.StatusOK, gin.H{"deadline": ok})
	}
	r.GET("/api/thing", hasDeadline)
	r.GET("/api/problems/1/run", hasDeadline) // 白名单

	get := func(path string) bool {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Body.String() == `{"deadline":true}`
	}
	if !get("/api/thing") {
		t.Fatal("普通路由应有 deadline")
	}
	if get("/api/problems/1/run") {
		t.Fatal("白名单路由不应有 deadline")
	}
}
