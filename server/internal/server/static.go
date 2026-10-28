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
		// vditor 旧版本路径（发版后的旧标签页）：302 到当前版本同名子路径
		if to := vditorVersionRedirect(sub, name); to != "" {
			c.Redirect(http.StatusFound, to)
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
// 例外（v0.26.2）：vditor/<semver>/ 版本化路径——版本号即内容锁（copy-vditor.mjs 从
// node_modules/vditor/package.json 读版本进目录名），升级 vditor 时版本变、路径变、
// 旧缓存自动失效；lute.min.js 3.6MB 靠它从「每次 no-cache 全量重下」变为磁盘缓存直读
//（并激活 V8 code cache）。semver 必须显式校验，否则无版本前缀的老路径会漏进来。
var semverDir = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func cacheControl(name string) string {
	if strings.HasPrefix(name, "assets/") && hashedAsset.MatchString(path.Base(name)) {
		return "public, max-age=31536000, immutable"
	}
	if parts := strings.SplitN(name, "/", 3); len(parts) == 3 && parts[0] == "vditor" && semverDir.MatchString(parts[1]) {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

// vditorVersionRedirect 发版后旧标签页仍引用旧版本路径（/vditor/4.0.1/…）：当前构建里
// 已无该目录。把请求 302 到当前版本的同名子路径——旧页面拿到新 lute 继续跑（与 v0.26.2
// 前的「无版本路径永远拿最新」行为一致）。返回空串表示无需/无法重定向。
func vditorVersionRedirect(sub fs.FS, name string) string {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) != 3 || parts[0] != "vditor" || !semverDir.MatchString(parts[1]) {
		return ""
	}
	if _, err := fs.Stat(sub, path.Clean(name)); err == nil {
		return "" // 当前版本就存在（正常路径），无需重定向
	}
	entries, err := fs.ReadDir(sub, "vditor")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() || !semverDir.MatchString(e.Name()) || e.Name() == parts[1] {
			continue // 同版本目录的缺失文件不能重定向回自身（死循环；v0.26.2 验收期补刀）
		}
		// 只有一个当前版本目录（copy 脚本清历史后单版本）；找到即重定向
		return "/vditor/" + e.Name() + "/" + parts[2]
	}
	return ""
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
