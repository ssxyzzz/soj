package server

// 备份面板（v0.4.9）：backup.run 备份与下载、backup.keys 密钥模式管理。
// 私钥传递硬规则：私钥仅出现在 POST /admin/backup/offline-key 的响应中一次，
// 响应 no-store 不可缓存；HTTP 与 CLI（--backup-run）共用 internal/backup 同一模块。
// v0.26 任务化：万题测试数据的打包在 GB 级，同步执行会把 HTTP 请求挂到代理超时——
// 改后台单任务（复用批量导入同款骨架：状态轮询 + 取消 + 互斥），完成后经
// GET /admin/backup/download 流式取包；取消覆盖 tar/加密阶段（mongodump 阶段体量小、
// cron 自动备份无取消场景，保持原样跑完该步后在下个阶段检测取消退出）。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/backup"
)

// backupJob 备份任务（全站单任务；job 在锁下创建/替换）。
type backupJob struct {
	mu        sync.Mutex
	state     string // running | done | failed | cancelled
	tier      string
	errMsg    string
	file      string // 成功产物文件名（服务器留存目录内，下载接口按名取）
	startedAt time.Time
	endedAt   time.Time
	cancel    context.CancelFunc
}

func (j *backupJob) snapshot() gin.H {
	j.mu.Lock()
	defer j.mu.Unlock()
	return gin.H{
		"state": j.state, "tier": j.tier, "error": j.errMsg, "file": j.file,
		"started_at": j.startedAt, "ended_at": j.endedAt,
	}
}

func (j *backupJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == "running"
}

// finish 终态落定（幂等：只有 running 可转终态）。
func (j *backupJob) finish(state, errMsg, file string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != "running" {
		return
	}
	j.state, j.errMsg, j.file, j.endedAt = state, errMsg, file, time.Now()
}

func (h *Handlers) backupOptions() backup.Options {
	return backup.Options{
		MongoURI:   h.cfg.MongoURI,
		DataDir:    h.cfg.DataDir,
		UploadsDir: h.cfg.UploadsDir,
		BackupDir:  h.cfg.BackupDir,
		KeysDir:    h.cfg.KeysDir,
	}
}

// BackupStatus 当前密钥模式、上次备份时间与备份任务状态。
// v0.15.1 起分层各报各的最近一次（db 日备 / media 周备），面板按 25 小时红灯规则巡检。
func (h *Handlers) BackupStatus(c *gin.Context) {
	st := backup.Inspect(h.cfg.KeysDir, h.cfg.BackupDir)
	fmtTime := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.Format(time.RFC3339)
	}
	h.backupMu.Lock()
	var job gin.H
	if h.backupTask != nil {
		job = h.backupTask.snapshot()
	}
	h.backupMu.Unlock()
	ok(c, gin.H{
		"mode":             st.Mode,
		"last_backup_at":   fmtTime(st.LastBackupAt),
		"last_backup_file": st.LastBackupFile,
		"last_db_at":       fmtTime(st.LastDBAt),
		"last_db_file":     st.LastDBFile,
		"last_media_at":    fmtTime(st.LastMediaAt),
		"last_media_file":  st.LastMediaFile,
		"job":              job,
	})
}

// BackupRun 启动一次后台加密备份：服务器留存一份，完成后经 /admin/backup/download 取包。
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
	tier := req.Tier
	if tier == "" {
		tier = string(backup.TierAll)
	}

	// 单任务排队（与批量导入同款：锁下创建/替换）
	h.backupMu.Lock()
	if h.backupTask != nil && h.backupTask.running() {
		h.backupMu.Unlock()
		fail(c, http.StatusConflict, "已有备份任务进行中，请等待完成或先取消")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &backupJob{state: "running", tier: tier, startedAt: time.Now(), cancel: cancel}
	h.backupTask = job
	h.backupMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				job.finish("failed", fmt.Sprintf("备份任务异常: %v", r), "")
			}
		}()
		opts := h.backupOptions()
		opts.Tier = tier
		path, err := backup.Run(ctx, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil || strings.Contains(err.Error(), "已取消") {
				job.finish("cancelled", "已取消", "")
				return
			}
			job.finish("failed", err.Error(), "")
			return
		}
		job.finish("done", "", filepath.Base(path))
	}()
	ok(c, gin.H{"started": true, "tier": tier})
}

// BackupCancel 取消进行中的备份任务（tar/加密阶段即时中断；mongodump 阶段等该步
// 跑完后在下个阶段退出）。
func (h *Handlers) BackupCancel(c *gin.Context) {
	h.backupMu.Lock()
	job := h.backupTask
	h.backupMu.Unlock()
	if job != nil && job.running() {
		job.cancel()
	}
	ok(c, gin.H{"cancel_requested": true})
}

// BackupDownload 下载服务器留存的备份归档（v0.26 任务化取包通道）：file 只接受
// 备份目录内 soj-backup- 前缀的归档文件名（basename 校验防目录穿越）。
func (h *Handlers) BackupDownload(c *gin.Context) {
	file := filepath.Base(c.Query("file"))
	if !strings.HasPrefix(file, backup.ArchivePrefix()) || !strings.HasSuffix(file, ".tar.age") {
		fail(c, http.StatusBadRequest, "非法备份文件名")
		return
	}
	path := filepath.Join(h.cfg.BackupDir, file)
	if _, err := os.Stat(path); err != nil {
		fail(c, http.StatusNotFound, "备份文件不存在")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.FileAttachment(path, file)
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
