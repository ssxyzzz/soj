package server

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/webui"
)

// registerStatic 用 embed 的前端产物托管页面：
// 命中文件返回文件，其余路径回退 index.html（SPA 前端路由）。
func registerStatic(r *gin.Engine) {
	sub, err := webui.Dist()
	if err != nil {
		return // dist 缺失（未构建前端）时只提供 API
	}

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "接口不存在"})
			return
		}
		name := strings.TrimPrefix(path.Clean(p), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if f, err := sub.Open(name); err == nil {
			_ = f.Close()
			serveFromFS(c, sub, name)
			return
		}
		serveFromFS(c, sub, "index.html")
	})
}

// vite 构建产物形如 assets/index-BwZDfCAb.js：文件名带 8 位构建哈希，同名必同内容。
var hashedAsset = regexp.MustCompile(`-[0-9A-Za-z_$]{8}\.[0-9A-Za-z]+$`)

// vite 构建产物固定输出在 assets/ 且文件名带 8 位哈希（assets/index-BwZDfCAb.js），
// 同名必同内容，可缓存一年；public/ 直拷文件（/vditor 等）不带哈希——形似名字
// （如 mathjax 的 all-packages.js）不能当哈希对待——必须每次回源校验，否则发版后用户拿到旧缓存。
func cacheControl(name string) string {
	if strings.HasPrefix(name, "assets/") && hashedAsset.MatchString(path.Base(name)) {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

func serveFromFS(c *gin.Context, sub fs.FS, name string) {
	data, err := fs.ReadFile(sub, name)
	if err != nil {
		c.String(http.StatusNotFound, "front-end asset not found")
		return
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Cache-Control", cacheControl(name))
	c.Data(http.StatusOK, ct, data)
}
