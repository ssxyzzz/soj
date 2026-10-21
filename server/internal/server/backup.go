package server

// 备份面板（v0.4.9）：backup.run 备份与下载、backup.keys 密钥模式管理。
// 私钥传递硬规则：私钥仅出现在 POST /admin/backup/offline-key 的响应中一次，
// 响应 no-store 不可缓存；HTTP 与 CLI（--backup-run）共用 internal/backup 同一模块。

import (
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/backup"
)

func (h *Handlers) backupOptions() backup.Options {
	return backup.Options{
		MongoURI:   h.cfg.MongoURI,
		DataDir:    h.cfg.DataDir,
		UploadsDir: h.cfg.UploadsDir,
		BackupDir:  h.cfg.BackupDir,
		KeysDir:    h.cfg.KeysDir,
	}
}

// BackupStatus 当前密钥模式、上次备份时间与使用说明（guide：空 = 前端用内置默认）。
func (h *Handlers) BackupStatus(c *gin.Context) {
	st := backup.Inspect(h.cfg.KeysDir, h.cfg.BackupDir)
	var lastAt any
	if st.LastBackupAt.IsZero() {
		lastAt = nil
	} else {
		lastAt = st.LastBackupAt.Format(time.RFC3339)
	}
	guide := ""
	if s, err := h.repo.GetSystemSettings(c.Request.Context()); err == nil {
		guide = s.BackupGuide // Markdown（v0.8 起单一事实源）；空 = 前端用内置默认
	}
	ok(c, gin.H{
		"mode":             st.Mode,
		"last_backup_at":   lastAt,
		"last_backup_file": st.LastBackupFile,
		"guide":            guide,
	})
}

// BackupRun 同步执行一次加密备份：服务器留存一份 + 文件流下载（永远是密文）。
func (h *Handlers) BackupRun(c *gin.Context) {
	path, err := backup.Run(c.Request.Context(), h.backupOptions())
	if errors.Is(err, backup.ErrBusy) {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "备份失败："+err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	c.FileAttachment(path, filepath.Base(path))
}

// BackupOfflineKey 升级 Tier 1（离线私钥）：响应含一次性私钥，此后服务器只存公钥。
func (h *Handlers) BackupOfflineKey(c *gin.Context) {
	private, err := backup.UpgradeTier1(h.cfg.KeysDir)
	if err != nil {
		fail(c, http.StatusInternalServerError, "升级失败："+err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	ok(c, gin.H{"private_key": private})
}
