// Package richtext：HTML → markdown 转换（v0.8 编辑器推倒重来，2026-10-20 定稿）。
//
// v0.6–v0.7 富文本时代内容以 HTML 存储；v0.8 回归 markdown 单一事实源后，本包服务
// 存量回迁：repo 启动迁移（contentMdV1）把 v0.6–v0.7 时代的 HTML 字段一次性转回 md
// （html 覆盖 md——「编辑即迁移」语义下 html 是编辑后的新内容）。
//
// 数学公式契约：HTML 中以 data-type="inline-math|block-math" + data-latex（TeX 源码，
// 属性转义形态）表示——HTMLToMd 用占位符中转归一为 $…$/$$…$$，Vditor 原生编辑渲染。
package richtext

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	lute "github.com/88250/lute"
)

var engine = newEngine()

func newEngine() *lute.Lute {
	l := lute.New()
	// 与 vditor 同方言语义：表格/任务列表/删除线/自动链接在 DOM→AST 映射与 md 输出两端都要开
	o := l.ParseOptions
	o.GFMTable = true
	o.GFMTaskListItem = true
	o.GFMStrikethrough = true
	o.GFMAutoLink = true
	return l
}

// HTMLToMd 把 HTML 转成 markdown（存量回迁用）。契约公式节点先抽成占位符再走
// lute HTML2Markdown，最后回填为 $ 文本——不能直接注入 $…$ 文本，h2m 会把正文里的 $ 转义成
// \$（防误判公式定界符）；也不能改写成 h2m 内部的 data-content/data-tex 属性形态，那依赖
// 引擎实现细节。占位符是纯字母数字，md 与 DOM 双向惰性，h2m 原样透传。
func HTMLToMd(h string) string {
	h, maths := contractToPlaceholders(h)
	md, err := engine.HTML2Markdown(h)
	if err != nil {
		return h // h2m 内部是宽松 DOM 解析，err 实际不可达；兜底原样返回保数据
	}
	return strings.TrimSpace(restoreMath(md, maths))
}

var (
	inlineMathRe = regexp.MustCompile(`<span\b[^>]*\bdata-type="inline-math"[^>]*></span>`)
	blockMathRe  = regexp.MustCompile(`<div\b[^>]*\bdata-type="block-math"[^>]*></div>`)
	latexAttrRe  = regexp.MustCompile(`\bdata-latex="([^"]*)"`)
	mathTokenFmt = "sojmathplaceholder%d" // 纯字母数字：不经 md 转义、不被 DOM 吞
)

type mathPlaceholder struct {
	latex string
	block bool
}

// contractToPlaceholders 把契约公式节点整体替换为占位符文本：行内原地；块级包进 <p>——
// 裸文本级的占位符与相邻 <p> 会被 h2m 挤进同一段，$$…$$ 必须自成一段才不被误判。
func contractToPlaceholders(h string) (string, []mathPlaceholder) {
	var maths []mathPlaceholder
	register := func(tag string, block bool) string {
		latex, ok := latexOf(tag)
		if !ok {
			return tag
		}
		maths = append(maths, mathPlaceholder{latex: latex, block: block})
		tok := fmt.Sprintf(mathTokenFmt, len(maths)-1)
		if block {
			return "<p>" + tok + "</p>"
		}
		return tok
	}
	h = blockMathRe.ReplaceAllStringFunc(h, func(m string) string { return register(m, true) })
	h = inlineMathRe.ReplaceAllStringFunc(h, func(m string) string { return register(m, false) })
	return h, maths
}

// restoreMath 把占位符回填为公式文本：块级 $$…$$ 自成一段，行内 $…$ 原地。
// 两端先补 \n 再整体 Trim，覆盖占位符顶格在 md 首尾的边界形态。
func restoreMath(md string, maths []mathPlaceholder) string {
	md = "\n" + md + "\n"
	for i, m := range maths {
		tok := fmt.Sprintf(mathTokenFmt, i)
		repl := "$" + m.latex + "$"
		if m.block {
			repl = "$$\n" + m.latex + "\n$$"
		}
		// 常规形态：占位符独占一行（块级 div 落在段落之间被解析成独立段落），保留原段界
		md = strings.ReplaceAll(md, "\n"+tok+"\n", "\n"+repl+"\n")
		md = strings.ReplaceAll(md, tok, repl) // 兜底：行内与意外形态
	}
	return md
}

// latexOf 从契约节点标签里取 TeX 源码：属性形态是转义后的，先还原。
func latexOf(tag string) (string, bool) {
	m := latexAttrRe.FindStringSubmatch(tag)
	if m == nil {
		return "", false
	}
	return html.UnescapeString(m[1]), true
}
