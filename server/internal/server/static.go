package server

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
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
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, ct, data)
}
