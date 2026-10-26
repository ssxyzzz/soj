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

// BackupStatus 当前密钥模式与上次备份时间。
// v0.15.1 起分层各报各的最近一次（db 日备 / media 周备），面板按 25 小时红灯规则巡检。
func (h *Handlers) BackupStatus(c *gin.Context) {
	st := backup.Inspect(h.cfg.KeysDir, h.cfg.BackupDir)
	fmtTime := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.Format(time.RFC3339)
	}
	ok(c, gin.H{
		"mode":             st.Mode,
		"last_backup_at":   fmtTime(st.LastBackupAt),
		"last_backup_file": st.LastBackupFile,
		"last_db_at":       fmtTime(st.LastDBAt),
		"last_db_file":     st.LastDBFile,
		"last_media_at":    fmtTime(st.LastMediaAt),
		"last_media_file":  st.LastMediaFile,
	})
}

// BackupRun 同步执行一次加密备份：服务器留存一份 + 文件流下载（永远是密文）。
// body 可选 {"tier":"db|media|all"}（缺省 all；面板按钮 = 全量）。
func (h *Handlers) BackupRun(c *gin.Context) {
	var req struct {
		Tier string `json:"tier"`
	}
	_ = c.ShouldBindJSON(&req) // 空 body 合法（历史调用方不带参数）
	if req.Tier != "" && !backup.ValidTier(req.Tier) {
		fail(c, http.StatusBadRequest, "tier 需为 db / media / all")
		return
	}
	opts := h.backupOptions()
	opts.Tier = req.Tier
	path, err := backup.Run(c.Request.Context(), opts)
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
