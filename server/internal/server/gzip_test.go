package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	r.GET("/json", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": strings.Repeat("x", 4096)}) })
	r.GET("/small", func(c *gin.Context) { c.String(http.StatusOK, "hi") })
	r.GET("/bin", func(c *gin.Context) { c.Data(http.StatusOK, "image/png", bytes.Repeat([]byte{0}, 1024)) })
	return r
}

func TestGzipCompressesText(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	newTestEngine().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("body 不是合法 gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !strings.Contains(string(body), strings.Repeat("x", 4096)) {
		t.Fatal("解压后内容与原文不符")
	}
}

func TestGzipSkipsSmallResponses(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/small", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	newTestEngine().ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("小响应不应压缩, Content-Encoding = %q", got)
	}
	if w.Body.String() != "hi" {
		t.Fatalf("body = %q, want hi", w.Body.String())
	}
}

func TestGzipSkipsBinaryTypes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bin", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	newTestEngine().ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("二进制类型不应压缩, Content-Encoding = %q", got)
	}
}

func TestGzipPassthroughWithoutAcceptEncoding(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	w := httptest.NewRecorder()
	newTestEngine().ServeHTTP(w, req)

	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("客户端不支持时应透传, Content-Encoding = %q", got)
	}
	if !strings.Contains(w.Body.String(), strings.Repeat("x", 4096)) {
		t.Fatal("透传 body 应为原文")
	}
}
