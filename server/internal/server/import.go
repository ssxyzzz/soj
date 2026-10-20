package server

// 题目 URL 导入（v0.6.1，plan.md §2.3）：POST /api/import/preview 抓取解析为统一
// 中间格式（标题/题面 HTML/样例/限制/来源），外链图转存 /uploads，前端弹窗预览
// 确认后回填建题/编辑表单（testdata zip 仍由老师上传——导入器只导题面）。
//
// 权限：problem.create ∨ problem.edit.own ∨ problem.edit.all（能进建题/编辑页的人群）。
// 限频：每用户滑动窗口 10 次/分钟（导入是重操作：外网抓取 + 图片转存）。
// 安全：抓取经 probimport.Fetcher（IP 白名单/端口/逐跳重定向/限量，见包注释）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/probimport"
	"soj/internal/repo"
)

const importPerMinute = 10

// importLimiter 每用户滑动窗口限频（内存态，重启即清——限的是手速不是计费）。
var importLimiter = struct {
	sync.Mutex
	hits map[int64][]time.Time
}{hits: map[int64][]time.Time{}}

func importAllow(uid int64) bool {
	importLimiter.Lock()
	defer importLimiter.Unlock()
	now := time.Now()
	hits := importLimiter.hits[uid]
	keep := hits[:0]
	for _, t := range hits {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= importPerMinute {
		importLimiter.hits[uid] = keep
		return false
	}
	importLimiter.hits[uid] = append(keep, now)
	return true
}

// ImportPreview 抓取并解析题目，返回预览数据（确认后由前端回填表单，本端点不落库）。
func (h *Handlers) ImportPreview(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if !importAllow(uid) {
		fail(c, http.StatusTooManyRequests, "导入太频繁，请稍后再试（10 次/分钟）")
		return
	}
	var req struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请提供题目 URL")
		return
	}
	rp, err := probimport.Parse(c.Request.Context(), probimport.New(), strings.TrimSpace(req.URL))
	if err != nil {
		var us *probimport.Unsupported
		if errors.As(err, &us) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		fail(c, http.StatusBadGateway, "导入失败："+err.Error())
		return
	}

	// 外链图转存本站 /uploads（失败保留外链）
	var transferMsg string
	if strings.Contains(rp.HTML, "<img") {
		rp.HTML, _ = f2Transfer(c, h, uid, rp.HTML)
		transferMsg = "题面图片已尽量转存本站；失败的外链保留原地址"
	}

	ok(c, gin.H{
		"title":           rp.Title,
		"html":            rp.HTML,
		"samples":         rp.Samples,
		"time_limit_ms":   rp.TimeMS,
		"memory_limit_mb": rp.MemMB,
		"source":          rp.Source,
		"transfer":        transferMsg,
	})
}

// f2Transfer 用一次性 context 转存图片（抓取可能慢于请求生命周期则截断在 30s）。
func f2Transfer(c *gin.Context, h *Handlers, uid int64, html string) (string, []probimport.TransferResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return probimport.New().TransferImages(ctx, html, func(body []byte, contentType string) (string, error) {
		return h.saveImportedImage(ctx, uid, body, contentType)
	})
}

// saveImportedImage 把转存的图片按 v0.5.1 同一套口径落 /uploads 卷并记元数据
// （随机 hex 名 + os.Root + 魔数校验 + 后缀白名单）。
func (h *Handlers) saveImportedImage(ctx context.Context, uid int64, data []byte, contentType string) (string, error) {
	ext := ""
	for e, ct := range imageExts {
		if strings.HasPrefix(contentType, ct) {
			ext = e
			break
		}
	}
	if ext == "" {
		return "", errors.New("不支持的图片类型 " + contentType)
	}
	if !sniffImage(data, ext) {
		return "", errors.New("文件内容不是图片")
	}
	if int64(len(data)) > maxImageBytes {
		return "", errors.New("超过单图 5MB 上限")
	}
	root, err := os.OpenRoot(h.cfg.UploadsDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	name := hex.EncodeToString(rnd) + ext
	w, err := root.Create(name)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return "", err
	}
	w.Close()
	if err := h.repo.InsertUpload(ctx, repo.Upload{
		File: name, UID: uid, Size: int64(len(data)), CreatedAt: time.Now(),
	}); err != nil {
		_ = root.Remove(name)
		return "", err
	}
	return "/uploads/" + name, nil
}
