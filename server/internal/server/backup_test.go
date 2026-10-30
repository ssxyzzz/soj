package server

// BackupDownload 体积硬禁单测（v0.36）：超限归档 403 且文案附 docker cp 取包命令，
// 阈值内照常 200 放行——2026-10-30 事故（219MB 在线下载独占公网管道 5m20s）的门禁。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
)

func newBackupDownloadEnv(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	dir := t.TempDir()
	h := &Handlers{cfg: &config.Config{BackupDir: dir, BackupDownloadLimit: 32 << 20}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/admin/backup/download", h.BackupDownload)
	return r, dir
}

func TestBackupDownloadSizeGate(t *testing.T) {
	r, dir := newBackupDownloadEnv(t)

	// 阈值内（db 档量级）：照常放行
	small := filepath.Join(dir, "soj-backup-db-20261030-120000.v1.tar.age")
	if err := os.WriteFile(small, []byte(strings.Repeat("x", 1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/backup/download?file=soj-backup-db-20261030-120000.v1.tar.age", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("小归档期望 200，得到 %d", w.Code)
	}

	// 超限（media 档量级）：403，文案含取包命令与实际体积
	big := filepath.Join(dir, "soj-backup-media-20261030-184846.v1.tar.age")
	if err := os.WriteFile(big, make([]byte, 33<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/backup/download?file=soj-backup-media-20261030-184846.v1.tar.age", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("超限归档期望 403，得到 %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"docker cp", "soj-backup-media-20261030-184846.v1.tar.age", "MB"} {
		if !strings.Contains(body, want) {
			t.Errorf("403 文案应含 %q，实际: %s", want, body)
		}
	}
}

func TestBackupDownloadBadName(t *testing.T) {
	r, _ := newBackupDownloadEnv(t)
	for _, name := range []string{"../../etc/passwd", "evil.tar.age", "notes.txt"} {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/backup/download?file="+name, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("非法文件名 %q 期望 400，得到 %d", name, w.Code)
		}
	}
}
