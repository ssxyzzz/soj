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
