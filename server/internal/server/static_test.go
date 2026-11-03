package server

import (
	"testing"
)

func TestCacheControl(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"assets/index-BwZDfCAb.js", "public, max-age=31536000, immutable"},
		{"assets/ProblemDetailView-DeVkCRQr.css", "public, max-age=31536000, immutable"},
		// 哈希含 - / _（base64url 字母表，约 12% 文件命中）也必须 immutable：
		// 曾因字符类漏 - 导致这些 chunk 不吃长缓存、也不进 SW 预缓存清单
		{"assets/DiffView-NIMh-nfR.js", "public, max-age=31536000, immutable"},
		{"assets/TagFilter-By7PiP-_.js", "public, max-age=31536000, immutable"},
		{"index.html", "no-cache"},
		// vditor/<semver>/ 版本化路径随 v0.53 渲染收口退役：前端已无 /vditor 子资源，
		// 该前缀回落普通 no-cache（历史标签页的旧请求 404 后前端自然刷新到新版）
		{"vditor/4.0.1/dist/js/lute/lute.min.js", "no-cache"},
		{"favicon.ico", "no-cache"},
		// v0.46 SW 预缓存批的两个稳定名文件必须 no-cache：sw.js 字节变化是浏览器对
		// SW 做 update 检查的唯一触发信号、asset-manifest.json 是换版预热清单——
		// 被中间层/浏览器缓存即换版失明
		{"sw.js", "no-cache"},
		{"asset-manifest.json", "no-cache"},
	}
	for _, c := range cases {
		if got := cacheControl(c.name); got != c.want {
			t.Errorf("cacheControl(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// 发版后旧前端（SW 缓存/旧标签页）会引用已删除的旧哈希 chunk，这些请求必须明 404、
// 不得回退 index.html（HTML 冒充 200 JS → ES module 解析炸 → 故障玄学化，生产实测踩坑）
func TestMissingAssetNoFallback(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/assets/BoardView-OLDHASH.js", true},
		{"/assets/vendor-core-DQ9VZijj.js", true},
		{"/assets/", true},
		{"/draw", false},
		{"/login", false},
		{"/sw.js", false},
		{"/favicon.ico", false},
		{"/api/missing", false}, // /api 在 NoRoute 前置分支 JSON 404，不走回退判定
	}
	for _, c := range cases {
		if got := missingAssetNoFallback(c.path); got != c.want {
			t.Errorf("missingAssetNoFallback(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
