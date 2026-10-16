package server

import "testing"

func TestCacheControl(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"assets/index-BwZDfCAb.js", "public, max-age=31536000, immutable"},
		{"assets/ProblemDetailView-DeVkCRQr.css", "public, max-age=31536000, immutable"},
		{"index.html", "no-cache"},
		{"vditor/dist/index.min.js", "no-cache"},
		{"vditor/dist/css/content-theme/light.css", "no-cache"},
		// 「-packages.」形似 8 位哈希，但 public/ 直拷文件不带哈希，不能 immutable
		{"vditor/dist/js/mathjax/input/tex/extensions/all-packages.js", "no-cache"},
		{"assets/all-packages.js", "public, max-age=31536000, immutable"},
		{"favicon.ico", "no-cache"},
	}
	for _, c := range cases {
		if got := cacheControl(c.name); got != c.want {
			t.Errorf("cacheControl(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
