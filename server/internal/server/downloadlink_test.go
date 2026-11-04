package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
)

// 下载直链（v0.64）口径锁：签名基准串构造、白名单形状、签发/验签全链路
//（过期/篡改/跨路径/缺参一律 401；权限回放路径依赖 ident 缓存，走集成栈不在此重复）。

func TestDlSignBase(t *testing.T) {
	cases := []struct {
		name string
		path string
		raw  string
		want string
	}{
		{"无 query", "/api/problems/export", "", "/api/problems/export"},
		{"原 query 保留顺序", "/api/problems/export", "ids=1,2&x=3", "/api/problems/export?ids=1,2&x=3"},
		{"剔除签名参数", "/api/problems/export", "ids=1,2&_dsig=abc&_dexp=9&_duid=7", "/api/problems/export?ids=1,2"},
		{"只剩签名参数视同无 query", "/api/problems/export", "_duid=7&_dexp=9&_dsig=abc", "/api/problems/export"},
		{"无值参数保留", "/api/problems/export", "a=&_duid=1", "/api/problems/export?a="},
	}
	for _, tc := range cases {
		if got := dlSignBase(tc.path, tc.raw); got != tc.want {
			t.Errorf("%s: dlSignBase(%q,%q) = %q, want %q", tc.name, tc.path, tc.raw, got, tc.want)
		}
	}
}

func TestDlPathAllowlist(t *testing.T) {
	allowed := []string{
		"/api/problems/12/testdata/download",
		"/api/problems/export",
		"/api/contests/3/export",
		"/api/exams/3/export",
		"/api/assignments/9/export",
		"/api/problems/45/objective/questions.yaml",
		"/api/admin/users/import-template",
	}
	for _, p := range allowed {
		hit := false
		for _, re := range dlPathRes {
			if re.MatchString(p) {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("白名单应放行 %s", p)
		}
	}
	denied := []string{
		"/api/problems/0/testdata/download",     // 非正整数 id
		"/api/problems/-1/testdata/download",    // 负 id
		"/api/problems/12/testdata/download/..", // 穿越
		"/api/auth/login",                       // 非下载端点（防任意路径签名）
		"/api/admin/users",                      // 相邻管理端点
		"/api/submissions/5",                    // 数据端点
		"/api/problems/12/testdata",             // 上传端点
		"/problems/12/testdata/download",        // 缺 /api 前缀（白名单是服务端绝对形态）
	}
	for _, p := range denied {
		for _, re := range dlPathRes {
			if re.MatchString(p) {
				t.Errorf("白名单不得放行 %s", p)
			}
		}
	}
}

func TestDlSignBinding(t *testing.T) {
	h := &Handlers{cfg: &config.Config{DownloadSecret: "unit-download-secret-0123456789abcdef"}}
	b1 := h.dlSign("/api/problems/export?ids=1", 1000, 7)
	if b2 := h.dlSign("/api/problems/export?ids=1", 1000, 7); string(b1) != string(b2) {
		t.Error("同参签名应确定")
	}
	if b3 := h.dlSign("/api/problems/export?ids=2", 1000, 7); string(b1) == string(b3) {
		t.Error("签名须绑定 query（换 ids 应换签名）")
	}
	if b4 := h.dlSign("/api/problems/export?ids=1", 1001, 7); string(b1) == string(b4) {
		t.Error("签名须绑定过期时刻")
	}
	if b5 := h.dlSign("/api/problems/export?ids=1", 1000, 8); string(b1) == string(b5) {
		t.Error("签名须绑定签发者 uid")
	}
}

func TestVerifyDownloadLinkRejects(t *testing.T) {
	h := &Handlers{cfg: &config.Config{DownloadSecret: "unit-download-secret-0123456789abcdef"}}
	gin.SetMode(gin.TestMode)

	mk := func(query string) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/problems/export?"+query, nil)
		return c
	}

	// 缺参 / 坏 uid / 坏 exp / 坏签名格式：全部 401 且不得放行
	for name, q := range map[string]string{
		"缺签名参数":  "ids=1",
		"坏uid":   "ids=1&_duid=x&_dexp=9999999999&_dsig=00",
		"坏exp":   "ids=1&_duid=7&_dexp=x&_dsig=00",
		"已过期":    "ids=1&_duid=7&_dexp=1&_dsig=00",
		"非hex签名": "ids=1&_duid=7&_dexp=9999999999&_dsig=zzzz",
	} {
		c := mk(q)
		if h.verifyDownloadLink(c) {
			t.Errorf("%s: 不应验签通过", name)
		}
		if !c.IsAborted() || c.Writer.Status() != 401 {
			t.Errorf("%s: 应 401，got %d", name, c.Writer.Status())
		}
	}

	// 伪造签名（密钥不对）：401
	c := mk("ids=1&_duid=7&_dexp=9999999999&_dsig=" + strings.Repeat("ab", 32))
	if h.verifyDownloadLink(c) {
		t.Error("伪签名不应通过")
	}
}
