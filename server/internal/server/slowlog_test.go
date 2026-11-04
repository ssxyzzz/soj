package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 慢请求观测阈值行为：慢 handler 打 Warn 带路由模板，快 handler 静默
// （v0.66 批①单测锁口径；阈值注入避免真等 1 秒）。
func TestSlowLogWarnsOverThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	r := gin.New()
	r.Use(slowLogWith(20 * time.Millisecond))
	r.GET("/api/thing/:id", func(c *gin.Context) {
		time.Sleep(30 * time.Millisecond)
		c.Status(http.StatusOK)
	})
	r.GET("/api/fast", func(c *gin.Context) { c.Status(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/thing/7", nil))
	out := buf.String()
	if !strings.Contains(out, "慢 HTTP 请求") || !strings.Contains(out, "/api/thing/:id") {
		t.Fatalf("慢请求应告警并带路由模板：%s", out)
	}

	buf.Reset()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/fast", nil))
	if buf.Len() != 0 {
		t.Fatalf("快请求不应告警：%s", buf.String())
	}
}
