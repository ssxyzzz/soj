// Package richtext：markdown → HTML 富文本转换（v0.6 编辑器换血，2026-10-19 定稿见 plan.md §2.2）。
//
// 存量内容全部由 vditor（浏览器版 Lute）方言渲染，服务端换血后用同引擎 Lute（Go 版）转换，
// 渲染结果与现状一致；本包同时服务存量「编辑即迁移」与 v0.6.1 站点导入（洛谷/Hydro/LOJ 的
// md 题面走同一管道）。goldmark 为 Lute 维护出问题时的备选（CommonMark 语义与 Lute 有细微差异）。
//
// 数学公式契约：HTML 中以 data-type="inline-math|block-math" + data-latex（TeX 源码原文）表示——
// 查看端 KaTeX 渲染、编辑端 Tiptap Mathematics 官方扩展（migrateMathStrings 同契约）还原为
// 可编辑节点，导入-显示-再编辑全程无损；转换不产生任何渲染死 HTML。
package richtext

import (
	"html"
	"regexp"
	"strings"

	lute "github.com/88250/lute"
)

var engine = newEngine()

func newEngine() *lute.Lute {
	l := lute.New()
	// 与 vditor 预览同一方言语义；数学走本包 convertMath 的契约转换（lute 自带
	// InlineMath/BlockMath 渲染形态不受控，显式关闭后 $…$ 以文本形式输出再统一处理）。
	o := l.ParseOptions
	o.GFMTable = true
	o.GFMTaskListItem = true
	o.GFMStrikethrough = true
	o.GFMAutoLink = true
	o.InlineMath = false
	// 代码高亮关掉：vditor 现状是浏览器端 highlight.js 高亮，HTML 里存裸代码——
	// chroma 的 class/span 会污染 Tiptap codeBlock 往返（编辑器 schema 只收纯文本）
	l.RenderOptions.CodeSyntaxHighlight = false
	return l
}

// MdToHTML 把 markdown 转成富文本 HTML（公式转 data-latex 契约，见包注释）。
func MdToHTML(md string) string {
	return ConvertMath(engine.MarkdownStr("soj", md))
}

// SampleIO 样例输入/输出文本（HTML 样例章节拼装用；与 repo.SampleIO 同构，避免反向依赖）。
type SampleIO struct{ Input, Output string }

const samplesHeadingHTML = "样例"

var (
	samplesH2Re = regexp.MustCompile(`<h2[^>]*>\s*样例\s*</h2>`)
	nextH1H2Re  = regexp.MustCompile(`<h[12][\s>]`)
)

// StripSamplesHTML 剥离 HTML 题面中的「样例」章节（h2 样例 起到下一个一/二级标题或文末），
// 语义与 repo.StripSamplesMd 对齐：测试数据整包替换后样例需按新数据重建。
func StripSamplesHTML(h string) string {
	loc := samplesH2Re.FindStringIndex(h)
	if loc == nil {
		return h
	}
	tail := nextH1H2Re.FindStringIndex(h[loc[1]:])
	if tail == nil {
		return strings.TrimRight(h[:loc[0]], "\n\t ")
	}
	// 样例章节后还有一/二级标题内容：与 md 版语义一致，保留其后的内容
	return strings.TrimRight(h[:loc[0]], "\n\t ") + "\n" + h[loc[1]+tail[0]:]
}

// AppendSamplesHTML 把样例以「样例」章节追加到 HTML 题面末尾（经同一转换器生成，渲染与
// md 版逐字一致）。已含样例章节或无样例时原样返回。
func AppendSamplesHTML(h string, samples []SampleIO) string {
	if len(samples) == 0 || samplesH2Re.MatchString(h) {
		return h
	}
	var b strings.Builder
	b.WriteString("## 样例")
	for i, s := range samples {
		b.WriteString("\n\n### 样例 ")
		b.WriteString(itoa(i + 1))
		b.WriteString("\n\n输入：\n\n```\n")
		b.WriteString(strings.TrimRight(s.Input, "\n"))
		b.WriteString("\n```\n\n输出：\n\n```\n")
		b.WriteString(strings.TrimRight(s.Output, "\n"))
		b.WriteString("\n```")
	}
	section := MdToHTML(b.String())
	if strings.TrimSpace(h) == "" {
		return section
	}
	return strings.TrimRight(h, "\n\t ") + "\n" + section
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

var (
	allDigitsRe  = regexp.MustCompile(`^\s*\d+\s*$`)
	attrEscaper  = html.EscapeString
	tagNameRe    = regexp.MustCompile(`^<\s*/?\s*([a-zA-Z][a-zA-Z0-9-]*)`)
	voidTagRe    = regexp.MustCompile(`^<\s*(br|hr|img|input|meta|link|source|col)\b`)
	selfCloseRe  = regexp.MustCompile(`/\s*>\s*$`)
	commentBegin = "<!--"
)

// ConvertMath 把 HTML 文本节点里的 $…$ / $$…$$ 转成 data-latex 契约节点。
// <pre>/<code> 内与标签内部不处理；守卫与官方 migrateMathRegex
// （/\$(?!\d+\$)(.+?)\$(?!\d)/g）语义对齐：纯数字内容（$100$）与闭合 $ 后紧跟数字
// （"$5 和 $10"）不视为公式——Go RE2 不支持负向前瞻，守卫在代码内实现。
func ConvertMath(h string) string {
	var b strings.Builder
	i := 0
	// codePre 栈深度：<pre> 可嵌套？HTML 里不会；<code> 出现在行内与 pre 内。计数即可。
	codeDepth := 0
	for i < len(h) {
		c := h[i]
		if c != '<' {
			if codeDepth == 0 {
				// 文本节点：处理到下一个 '<' 为止
				nxt := strings.IndexByte(h[i:], '<')
				var seg string
				if nxt < 0 {
					seg = h[i:]
					i = len(h)
				} else {
					seg = h[i : i+nxt]
					i += nxt
				}
				b.WriteString(mathifyText(seg))
			} else {
				b.WriteByte(c)
				i++
			}
			continue
		}
		// 标签 / 注释 / CDATA：原样搬移
		if strings.HasPrefix(h[i:], commentBegin) {
			end := strings.Index(h[i+4:], "-->")
			if end < 0 {
				b.WriteString(h[i:])
				break
			}
			b.WriteString(h[i : i+4+end+3])
			i += 4 + end + 3
			continue
		}
		// lute 默认（InlineMath 关闭时仍生效）把 $$…$$ 渲染为 <div class="language-math">TeX</div>，
		// 统一改写为块级公式契约节点
		if strings.HasPrefix(h[i:], `<div class="language-math">`) {
			end := strings.Index(h[i:], "</div>")
			if end >= 0 {
				tex := html.UnescapeString(h[i+len(`<div class="language-math">`) : i+end])
				b.WriteString(`<div data-type="block-math" data-latex="`)
				b.WriteString(attrEscaper(tex))
				b.WriteString(`"></div>`)
				i += end + len("</div>")
				continue
			}
		}
		gt := strings.IndexByte(h[i:], '>')
		if gt < 0 {
			b.WriteString(h[i:])
			break
		}
		tag := h[i : i+gt+1]
		b.WriteString(tag)
		i += gt + 1
		m := tagNameRe.FindStringSubmatch(strings.ToLower(tag))
		if m == nil {
			continue
		}
		name := m[1]
		switch name {
		case "pre", "code":
			if strings.HasPrefix(m[0], "</") {
				if codeDepth > 0 {
					codeDepth--
				}
			} else if !voidTagRe.MatchString(strings.ToLower(tag)) && !selfCloseRe.MatchString(tag) {
				codeDepth++
			}
		}
	}
	return b.String()
}

func mathifyText(seg string) string {
	if !strings.Contains(seg, "$") {
		return seg
	}
	var b strings.Builder
	i := 0
	n := len(seg)
	for i < n {
		if seg[i] != '$' {
			b.WriteByte(seg[i])
			i++
			continue
		}
		// 块级公式 $$…$$（可跨行）
		if i+1 < n && seg[i+1] == '$' {
			if end := strings.Index(seg[i+2:], "$$"); end >= 0 {
				latex := seg[i+2 : i+2+end]
				if strings.TrimSpace(latex) != "" {
					b.WriteString(`<div data-type="block-math" data-latex="`)
					b.WriteString(attrEscaper(latex))
					b.WriteString(`"></div>`)
					i = i + 2 + end + 2
					continue
				}
			}
			b.WriteString("$$")
			i += 2
			continue
		}
		// 行内公式 $…$（单行）
		if end := strings.IndexByte(seg[i+1:], '$'); end >= 0 {
			closeAbs := i + 1 + end
			content := seg[i+1 : closeAbs]
			afterDigit := closeAbs+1 < n && isDigit(seg[closeAbs+1])
			if content != "" && !strings.ContainsRune(content, '\n') &&
				!afterDigit && !allDigitsRe.MatchString(content) {
				// 文本节点里是实体转义后的形态（&lt; 等），先还原 TeX 源码再按属性转义
				b.WriteString(`<span data-type="inline-math" data-latex="`)
				b.WriteString(attrEscaper(html.UnescapeString(content)))
				b.WriteString(`"></span>`)
				i = closeAbs + 1
				continue
			}
			// 守卫失败：只消费开头的 $（"$5 和 $10" 两处都不配对）
			b.WriteByte('$')
			i++
			continue
		}
		b.WriteByte('$')
		i++
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// PlainText 提取 HTML 纯文本（标签剥离 + 实体还原），供转换质量校验（转换前后纯文本一致）。
func PlainText(h string) string {
	var b strings.Builder
	i := 0
	for i < len(h) {
		if h[i] == '<' {
			gt := strings.IndexByte(h[i:], '>')
			if gt < 0 {
				break
			}
			i += gt + 1
			continue
		}
		nxt := strings.IndexByte(h[i:], '<')
		var seg string
		if nxt < 0 {
			seg = h[i:]
			i = len(h)
		} else {
			seg = h[i : i+nxt]
			i += nxt
		}
		b.WriteString(seg)
	}
	return html.UnescapeString(b.String())
}
