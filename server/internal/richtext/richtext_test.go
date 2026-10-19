package richtext

import (
	"strings"
	"testing"
)

// 转换质量基线（plan.md §2.2 质量验收）：文字层无损（纯文本一致）、公式进 data-latex 契约、
// 代码内 $ 不被误转、货币守卫生效、样例章节可剥可建。

const corpus = `# 两数之和

给定 <b>n</b> 个整数，求最大值与 ` + "`long long`" + ` 上界。

- [x] 子任务 1（30 分）
- [ ] 子任务 2（70 分）

| 测试点 | n |
| --- | --- |
| 1 | 100 |

> 数据范围：1 ≤ n ≤ 10^6

` + "```cpp\n// 代码里的 $E=mc^2$ 不转换\nint x = 1;\n```" + `

~~删除线~~ 与 [链接](https://example.com)。
`

func TestMdToHTMLBasics(t *testing.T) {
	h := MdToHTML(corpus)
	for _, want := range []string{"<h1>两数之和</h1>", "<table>", "checked", "<del>删除线</del>", "<blockquote>"} {
		if !strings.Contains(h, want) {
			t.Fatalf("缺 %q\nHTML: %s", want, h)
		}
	}
}

// 守卫与契约：公式进节点、货币不误转、代码内不转换
func TestConvertMathContract(t *testing.T) {
	cases := []struct {
		name, in, want, absent string
	}{
		{"行内公式", "<p>质能方程 $E=mc^2$ 很有名</p>",
			`data-type="inline-math" data-latex="E=mc^2"`, "$E=mc^2$"},
		{"块级公式", "<p>积分：</p>$$\\int_0^1 x^2 dx = \\frac{1}{3}$$",
			`data-type="block-math" data-latex="\int_0^1 x^2 dx = \frac{1}{3}"`, ""},
		{"货币不误转", "<p>价格 $5 和 $10 之间</p>", "价格 $5 和 $10 之间", "inline-math"},
		{"纯数字不配对", "<p>编号 $100$ 见附录</p>", "$100$", "inline-math"},
		{"闭合后跟数字不配对", "<p>坐标 $5x$3 号</p>", "坐标 $5x$3 号", "inline-math"},
		{"TeX 特殊字符转义", "<p>$a&lt;b &amp; c&gt;1$</p>", `data-latex="a&lt;b &amp; c&gt;1"`, ""},
		{"代码内保留", "<pre><code>$E=mc^2$</code></pre>", "<pre><code>$E=mc^2$</code></pre>", "inline-math"},
		{"行内代码保留", "<p>写 <code>$x^2$</code> 表示平方</p>", "<code>$x^2$</code>", `data-latex`},
	}
	for _, c := range cases {
		got := ConvertMath(c.in)
		if !strings.Contains(got, c.want) || (c.absent != "" && strings.Contains(got, c.absent)) {
			t.Errorf("%s:\n 输入 %s\n 得到 %s", c.name, c.in, got)
		}
	}
}

// 端到端：md 里的公式经 Lute + ConvertMath 进契约；行内公式含 TeX 符号
func TestMdToHTMLMathEndToEnd(t *testing.T) {
	h := MdToHTML("欧拉公式 $e^{i\\pi}+1=0$ 很美。\n\n$$\\sum_{i=1}^n i$$\n")
	if !strings.Contains(h, `data-latex="e^{i\pi}+1=0"`) {
		t.Fatalf("行内公式丢失: %s", h)
	}
	if !strings.Contains(h, `data-type="block-math"`) {
		t.Fatalf("块级公式丢失: %s", h)
	}
}

// 文字层无损：无公式语料转换前后纯文本一致（质量验收口径）
func TestPlainTextLossless(t *testing.T) {
	mdOnly := "## 两数之和\n\n给定 n 个整数，**求最大值**。\n\n- 甲\n- 乙\n"
	h := MdToHTML(mdOnly)
	got := PlainText(h)
	for _, want := range []string{"两数之和", "给定 n 个整数，求最大值。", "甲", "乙"} {
		if !strings.Contains(got, want) {
			t.Fatalf("纯文本缺 %q；得到 %q", want, got)
		}
	}
}

func TestSamplesRoundtrip(t *testing.T) {
	base := MdToHTML("# 题\n\n正文。")
	samples := []SampleIO{{Input: "3 4\n", Output: "7\n"}, {Input: "1 1\n", Output: "2\n"}}
	with := AppendSamplesHTML(base, samples)
	if !strings.Contains(with, ">样例<") || !strings.Contains(with, "<pre><code>7\n</code></pre>") {
		t.Fatalf("样例章节缺失或结构不对: %s", with)
	}
	got := strings.TrimSpace(StripSamplesHTML(with))
	if got != strings.TrimSpace(base) {
		t.Fatalf("剥离后应回到原文:\n得到 %q\n期望 %q", got, strings.TrimSpace(base))
	}
	// 幂等：已含样例章节不重复追加
	if again := AppendSamplesHTML(with, samples); again != with {
		t.Fatalf("重复追加: %s", again)
	}
}
