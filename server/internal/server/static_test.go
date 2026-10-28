package server

import (
	"testing"
	"testing/fstest"
)

func TestCacheControl(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"assets/index-BwZDfCAb.js", "public, max-age=31536000, immutable"},
		{"assets/ProblemDetailView-DeVkCRQr.css", "public, max-age=31536000, immutable"},
		{"index.html", "no-cache"},
		// vditor 版本化路径（v0.26.2）：semver 即内容锁 → immutable
		{"vditor/4.0.1/dist/index.min.js", "public, max-age=31536000, immutable"},
		{"vditor/4.0.1/dist/css/content-theme/light.css", "public, max-age=31536000, immutable"},
		{"vditor/4.0.1/dist/js/lute/lute.min.js", "public, max-age=31536000, immutable"},
		// 「-packages.」形似 8 位哈希，但无 semver 版本前缀的 vditor 路径不能 immutable
		{"vditor/4.0.1/dist/js/mathjax/input/tex/extensions/all-packages.js", "public, max-age=31536000, immutable"},
		{"vditor/dist/index.min.js", "no-cache"}, // 老的无版本路径（发版前的存量标签页）
		{"favicon.ico", "no-cache"},
	}
	for _, c := range cases {
		if got := cacheControl(c.name); got != c.want {
			t.Errorf("cacheControl(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestVditorVersionRedirect(t *testing.T) {
	sub := fstest.MapFS{
		"vditor/4.0.2/dist/js/lute/lute.min.js": &fstest.MapFile{Data: []byte("lute")},
	}
	cases := []struct {
		name string
		want string
	}{
		// 旧版本路径（当前构建已无 4.0.1）→ 302 到当前版本同名子路径
		{"vditor/4.0.1/dist/js/lute/lute.min.js", "/vditor/4.0.2/dist/js/lute/lute.min.js"},
		// 当前版本存在的路径：不重定向（走正常 serve）
		{"vditor/4.0.2/dist/js/lute/lute.min.js", ""},
		// 当前版本目录里缺失的文件：不得重定向回同版本（死循环，v0.26.2 验收期补刀）
		{"vditor/4.0.2/dist/js/not-exist.js", ""},
		// 非版本化/非 vditor/畸形路径：不处理
		{"vditor/dist/index.min.js", ""},
		{"assets/index-abc.js", ""},
		{"vditor/notver/dist/x.js", ""},
	}
	for _, c := range cases {
		if got := vditorVersionRedirect(sub, c.name); got != c.want {
			t.Errorf("vditorVersionRedirect(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
