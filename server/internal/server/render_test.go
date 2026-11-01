package server

// /api/render 端点测试（v0.52 编辑器内芯换血）：鉴权 / 超长 / 限频 / 渲染口径。
// 口径用例是本端点存在的意义——数字开头行内公式（$1 \le N$，2026-10-29 曾因
// 服务端漏配 InlineMathAllowDigitAfterOpenMarker 与编辑端预览不一致）必须在
// 同一函数里给出版本，保证「编辑器里看到的 = 发布后看到的」。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

func renderEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", AuthRequired(h))
	api.POST("/render", h.RenderMarkdown)
	return r
}

func TestRenderMarkdownAuth(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := renderEngine(h)

	code, out := doJSON(r, "POST", "/api/render", "", `{"md":"hi"}`)
	if code != 401 {
		t.Fatalf("未登录应 401，得 %d %v", code, out)
	}

	tok := f.addUser(h, 1, "u1", repo.RoleDefault, 1)
	code, out = doJSON(r, "POST", "/api/render", tok, `{"md":"**加粗**"}`)
	if code != 200 {
		t.Fatalf("登录后应 200，得 %d %v", code, out)
	}
	data, _ := out["data"].(map[string]any)
	html, _ := data["html"].(string)
	if !strings.Contains(html, "<strong>加粗</strong>") {
		t.Fatalf("应渲染加粗，得 %q", html)
	}
}

func TestRenderMarkdownMathInlineDigit(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := renderEngine(h)
	tok := f.addUser(h, 1, "u1", repo.RoleDefault, 1)

	// $ 后允许数字（OJ 题面数据范围高频形态）：应出 language-math 节点而非原文
	code, out := doJSON(r, "POST", "/api/render", tok, `{"md":"$1 \\leq N \\leq 3$"}`)
	if code != 200 {
		t.Fatalf("应 200，得 %d %v", code, out)
	}
	data, _ := out["data"].(map[string]any)
	html, _ := data["html"].(string)
	if !strings.Contains(html, "language-math") {
		t.Fatalf("数字开头行内公式应渲染为公式节点，得 %q", html)
	}
	if !strings.Contains(html, `1 \leq N \leq 3`) {
		t.Fatalf("TeX 源应保留给前端 KaTeX，得 %q", html)
	}
}

func TestRenderMarkdownSizeAndSanitize(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := renderEngine(h)
	tok := f.addUser(h, 1, "u1", repo.RoleDefault, 1)

	// 超长 400（与写入口 256KB 同限）
	code, out := doJSON(r, "POST", "/api/render", tok, fmt.Sprintf(`{"md":"%s"}`, strings.Repeat("a", renderMaxBytes+1)))
	if code != 400 {
		t.Fatalf("超长应 400，得 %d %v", code, out)
	}

	// 消毒与写入口径一致：script 注入应被剥（mdrender 层 lute Sanitize 强制）
	code, out = doJSON(r, "POST", "/api/render", tok, `{"md":"<script>alert(1)</script>ok"}`)
	if code != 200 {
		t.Fatalf("应 200，得 %d %v", code, out)
	}
	data, _ := out["data"].(map[string]any)
	html, _ := data["html"].(string)
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Fatalf("script 应被消毒剥除，得 %q", html)
	}
}

func TestRenderMarkdownRateLimit(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := renderEngine(h)
	tok := f.addUser(h, 1, "u1", repo.RoleDefault, 1)

	for i := 0; i < renderPerMinute; i++ {
		code, _ := doJSON(r, "POST", "/api/render", tok, `{"md":""}`)
		if code != 200 {
			t.Fatalf("第 %d 次应 200，得 %d", i+1, code)
		}
	}
	code, out := doJSON(r, "POST", "/api/render", tok, `{"md":""}`)
	if code != 429 {
		t.Fatalf("超限应 429，得 %d %v", code, out)
	}

	// 他人窗口独立：不同 uid 不受影响
	tok2 := f.addUser(h, 2, "u2", repo.RoleDefault, 1)
	if code, _ := doJSON(r, "POST", "/api/render", tok2, `{"md":""}`); code != 200 {
		t.Fatalf("他人应 200，得 %d", code)
	}
}
