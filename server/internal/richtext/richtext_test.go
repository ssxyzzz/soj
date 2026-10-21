package richtext

import (
	"strings"
	"testing"
)

// 转换质量基线（v0.8 回迁管道）：HTML 结构转 GFM md、公式契约经占位符归一为 $ 文本。

// v0.6–v0.7 时代编辑器（TipTap schema）产出的典型 HTML：标题/加粗/任务列表/表格/
// 引用/代码块/删除线/链接 + 两种公式契约节点。
const corpusHTML = `<h1>两数之和</h1><p>给定 <strong>n</strong> 个整数，求最大值与 <code>long long</code> 上界。质能方程 <span data-type="inline-math" data-latex="E=mc^2"></span> 很有名。</p><ul><li><p>子任务 1（30 分）</p></li><li><p>子任务 2（70 分）</p></li></ul><table><tbody><tr><th>测试点</th><th>n</th></tr><tr><td>1</td><td>100</td></tr></tbody></table><blockquote><p>数据范围：1 ≤ n ≤ 10^6</p></blockquote><pre><code class="language-cpp">// 注释
int x = 1;</code></pre><p><del>删除线</del> 与 <a href="https://example.com">链接</a>。</p><p>积分：</p><div data-type="block-math" data-latex="\int_0^1 x^2 dx = \frac{1}{3}"></div><p>完。</p>`

func TestHTMLToMdBasics(t *testing.T) {
	md := HTMLToMd(corpusHTML)
	for _, want := range []string{
		"# 两数之和", "**n**", "`long long`", "$E=mc^2$",
		"| 测试点", "| 100 |", // 列宽填充不敏感：lute 会按列宽 pad 空格
		"> 数据范围", "```cpp", "int x = 1;",
		"~~删除线~~", "[链接](https://example.com)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("缺 %q\nmd: %s", want, md)
		}
	}
}

// 公式契约 → $ 文本：行内原地、块级自成段落（$$ 换行形态）；TeX 特殊字符（< & >）经属性转义往返后还原。
// 注意裸 $ 文本不在此列——h2m 会正确转义成 \$（字面美元），契约节点才是公式的存储形态。
func TestHTMLToMdMathContract(t *testing.T) {
	md := HTMLToMd(`<p>比较 <span data-type="inline-math" data-latex="a&lt;b &amp; c&gt;1"></span> 与常量。</p><div data-type="block-math" data-latex="\sum_{i=1}^n i"></div>`)
	if !strings.Contains(md, "$a<b & c>1$") {
		t.Errorf("行内公式特殊字符未还原: %s", md)
	}
	// 块级公式必须自成一段（前后空行、$$ 换行形态），否则会被当行内公式
	if !strings.Contains(md, "\n\n$$\n\\sum_{i=1}^n i\n$$") {
		t.Errorf("块级公式未独立成段: %q", md)
	}
}
