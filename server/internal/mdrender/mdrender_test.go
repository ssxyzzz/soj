package mdrender

// 渲染管线语义单测（无外部依赖，纯函数）：输出结构、XSS 消毒、公式定界符保留、
// 代码块 language class、并发安全（-race 覆盖）。

import (
	"strings"
	"sync"
	"testing"
)

func TestRenderBasics(t *testing.T) {
	out := Render("# 标题\n\n行内 `code` 与 **加粗**\n\n```cpp\nint main(){}\n```\n")
	for _, want := range []string{
		"<h1>", "标题", "</h1>",
		"<code>code</code>",
		"<strong>加粗</strong>",
		`<code class="language-cpp">`, // 服务端不产内联色，标准 class 交前端 hljs
		"int main(){}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺少 %q:\n%s", want, out)
		}
	}
	if Render("") != "" {
		t.Error("空串应进出空串")
	}
}

func TestRenderSanitize(t *testing.T) {
	// script 标签、js: 协议链接：写入即消毒（毒内容不落 html 字段）
	out := Render("<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n<img src=x onerror=alert(1)>")
	if strings.Contains(strings.ToLower(out), "<script") {
		t.Errorf("script 未被消毒: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "javascript:") {
		t.Errorf("javascript: 协议未被消毒: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "onerror") {
		t.Errorf("onerror 属性未被消毒: %s", out)
	}
}

func TestRenderMathKept(t *testing.T) {
	// 公式转 language-math 节点（lute 原生行为，与 Vditor 前端内部做法同源）：
	// 前端 KaTeX 对 .language-math 节点做 katex.render（比扫 $ 定界符更精确，
	// 不会误伤正文里的 $ 符号）
	out := Render("$a+b$ 与 $$\\frac{1}{n}$$")
	if !strings.Contains(out, `<span class="language-math">a+b</span>`) {
		t.Errorf("行内公式节点缺失: %s", out)
	}
	if !strings.Contains(out, `<div class="language-math">`) {
		t.Errorf("块级公式节点缺失: %s", out)
	}
}

func TestRenderTableGFM(t *testing.T) {
	out := Render("| a | b |\n| --- | --- |\n| 1 | 2 |\n")
	if !strings.Contains(out, "<table>") || !strings.Contains(out, "<td>1</td>") {
		t.Errorf("GFM 表格未渲染: %s", out)
	}
}

func TestRenderConcurrent(t *testing.T) {
	// 引擎共享实例串行化：并发调用不得 panic/串包（-race 下验证）
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			md := strings.Repeat("x", i) + " **b**"
			out := Render(md)
			if !strings.Contains(out, "<strong>b</strong>") {
				t.Error("并发渲染结果异常")
			}
		}(i)
	}
	wg.Wait()
}
