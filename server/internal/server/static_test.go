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
	}
	for _, c := range cases {
		if got := cacheControl(c.name); got != c.want {
			t.Errorf("cacheControl(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
