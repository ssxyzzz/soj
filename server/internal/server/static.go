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
// （vditor/<semver>/ 版本化自托管资源与旧版本 302 回退随 v0.53 渲染收口批退役：
// 查看端已 html-only，前端不再有任何 /vditor 子资源。）
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
		if missingAssetNoFallback(p) {
			c.String(http.StatusNotFound, "404 asset not found: %s", p)
			return
		}
		serveFromFS(c, sub, "index.html")
	})
}

// missingAssetNoFallback：未命中也不回退 index.html 的路径——/assets/ 全部是 vite
// 哈希产物，同名必同内容、版本绑定。旧前端（SW 缓存/旧标签页）拿着旧 index 引用
// 已删 chunk 时，HTML 冒充 200 JS 会让 ES module 解析炸掉、故障变玄学（生产实测
// 踩坑：发版后画板打不开，刷新自愈）；明 404 才能让浏览器/SW 正常走更新流程
func missingAssetNoFallback(p string) bool {
	return strings.HasPrefix(p, "/assets/")
}

// vite 构建产物形如 assets/index-BwZDfCAb.js：文件名带 8 位构建哈希，同名必同内容。
// 哈希字母表为 base64url（含 - 与 _，实测约 12% 文件名含 -）；vite.config.ts 预缓存
// 清单与 web/public/sw.js 的 HASHED 判定必须保持同一口径，漏 - 即漏缓存。
var hashedAsset = regexp.MustCompile(`-[0-9A-Za-z_$-]{8}\.[0-9A-Za-z]+$`)

// vite 构建产物固定输出在 assets/ 且文件名带 8 位哈希（assets/index-BwZDfCAb.js），
// 同名必同内容，可缓存一年；其余文件（index.html、public/ 直拷件）不带哈希——
// 必须每次回源校验，否则发版后用户拿到旧缓存。
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
