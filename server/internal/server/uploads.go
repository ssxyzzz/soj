package server

// 题面图片上传（v0.5a）：POST /api/uploads 多文件上传（Vditor 钩子），
// GET /uploads/:file 公开读取（题面公开、图片随公开；随机名不可枚举）。
// 权限：持有题面编辑或系统设置权限位者（能打开编辑器的人群），不新增权限位。
// 文件访问一律经 os.Root（内核级限制在图片目录内，路径穿越从语言层被拒绝）。
// 孤儿文件不做自动清理——编辑器删引用不删文件，磁盘靠每人 200MB 配额兜底。

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const (
	maxImageBytes      = 5 << 20   // 单图 5MB
	maxUserUploadBytes = 200 << 20 // 每人累计 200MB
	maxUploadRequest   = 32 << 20  // 单次请求总量闸门（多文件余量）
)

// imageExts 后缀白名单 → Content-Type（同时用于上传校验与静态读取）。
var imageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// uploadNameRE 读取侧的文件名格式全匹配：本系统上传的文件名一律是
// 「32 位 hex 随机名 + 白名单后缀」（见 UploadImages），任何不符合该形状
// 的请求（含路径穿越语法）在正则层就不可能表达。
var uploadNameRE = regexp.MustCompile(`^([0-9a-f]{32})\.(png|jpg|jpeg|gif|webp)$`)

// sniffImage 魔数校验：后缀可伪造，内容不能。
func sniffImage(head []byte, ext string) bool {
	switch ext {
	case ".png":
		return len(head) > 7 && head[0] == 0x89 && head[1] == 'P' && head[2] == 'N' && head[3] == 'G'
	case ".jpg", ".jpeg":
		return len(head) > 2 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF
	case ".gif":
		return len(head) > 5 && string(head[:3]) == "GIF"
	case ".webp":
		return len(head) > 11 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP"
	}
	return false
}

// UploadImages 多文件上传（Vditor 上传钩子）。响应采用 Vditor 约定格式
// {msg, errFiles, succMap}：succMap 的 key 是原始文件名，value 为可插入题面的 URL。
func (h *Handlers) UploadImages(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequest)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "上传失败：请求体过大或格式错误（单图上限 5MB）")
		return
	}
	files := form.File["file"]
	if len(files) == 0 {
		fail(c, http.StatusBadRequest, "未选择图片")
		return
	}
	if err := os.MkdirAll(h.cfg.UploadsDir, 0o755); err != nil {
		fail(c, http.StatusInternalServerError, "创建图片目录失败")
		return
	}
	root, err := os.OpenRoot(h.cfg.UploadsDir)
	if err != nil {
		fail(c, http.StatusInternalServerError, "打开图片目录失败")
		return
	}
	defer root.Close()
	used, err := h.repo.UploadsUsedBytes(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取配额失败")
		return
	}

	succ := map[string]string{}
	errFiles := make([]string, 0)
	for _, fh := range files {
		reject := func(reason string) { errFiles = append(errFiles, fh.Filename+"（"+reason+"）") }
		if fh.Size > maxImageBytes {
			reject("超过单图 5MB 上限")
			continue
		}
		ext := strings.ToLower(path.Ext(fh.Filename))
		if _, okExt := imageExts[ext]; !okExt {
			reject("仅支持 png/jpg/jpeg/gif/webp")
			continue
		}
		src, err := fh.Open()
		if err != nil {
			reject("读取失败")
			continue
		}
		data, err := io.ReadAll(src)
		src.Close()
		if err != nil || int64(len(data)) != fh.Size {
			reject("读取失败")
			continue
		}
		if !sniffImage(data, ext) {
			reject("文件内容不是图片")
			continue
		}
		if used+fh.Size > maxUserUploadBytes {
			reject("累计图片存储已满 200MB")
			continue
		}

		rnd := make([]byte, 16)
		if _, err := rand.Read(rnd); err != nil {
			reject("生成文件名失败")
			continue
		}
		name := hex.EncodeToString(rnd) + ext
		w, err := root.Create(name) // os.Root：name 含任何穿越成分都直接报错
		if err != nil {
			reject("保存失败")
			continue
		}
		if _, err := w.Write(data); err != nil {
			w.Close()
			reject("保存失败")
			continue
		}
		w.Close()
		if err := h.repo.InsertUpload(c.Request.Context(), repo.Upload{
			File: name, UID: uid, Size: int64(len(data)), CreatedAt: time.Now(),
		}); err != nil {
			root.Remove(name)
			reject("记录失败")
			continue
		}
		used += fh.Size
		succ[fh.Filename] = "/uploads/" + name
	}
	ok(c, gin.H{"msg": "", "errFiles": errFiles, "succMap": succ})
}

// ServeUpload 公开读取图片（随机名不可枚举；一年 immutable——文件名不可变，缓存绝对安全）。
// 读取走 os.DirFS + fs.ReadFile：fs.ValidPath 在此层拒绝绝对路径与任何穿越成分。
func (h *Handlers) ServeUpload(c *gin.Context) {
	name := c.Param("file")
	ext := strings.ToLower(filepath.Ext(name))
	ctype, okExt := imageExts[ext]
	// 后缀白名单前置过滤
	if !okExt {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	// 强校验：文件名必须精确匹配「32 位 hex + 白名单后缀」的生成格式（纵深防御）
	if !uploadNameRE.MatchString(name) {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	data, err := fs.ReadFile(os.DirFS(h.cfg.UploadsDir), name)
	if err != nil {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, ctype, data)
}
