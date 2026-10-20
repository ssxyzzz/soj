// Codeforces 适配器：服务端渲染 HTML，div.problem-statement 结构多年稳定。
// 数学已是排版 HTML（tex-span/sub/sup），原样保留；样例行是 pre 内逐行 div，需还原换行。
package probimport

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

func parseCodeforces(ctx context.Context, f *Fetcher, rawURL string) (*RemoteProblem, error) {
	body, err := f.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := parsePage(body)
	if err != nil {
		return nil, err
	}
	ps := findFirst(doc, "problem-statement")
	if ps == nil {
		return nil, fmt.Errorf("页面里没有找到题面区块（problem-statement），站点可能改版或需要登录")
	}

	out := &RemoteProblem{Source: "Codeforces " + cfProblemID(rawURL)}

	// 标题：第一个 div.title（"A. Watermelon"）
	if t := findFirst(ps, "title"); t != nil {
		out.Title = stripPrefixID(textContent(t))
	}
	// 限制
	if t := findFirst(ps, "time-limit"); t != nil {
		out.TimeMS = parseLimitText(textContent(t), "second")
	}
	if m := findFirst(ps, "memory-limit"); m != nil {
		out.MemMB = parseLimitText(textContent(m), "megabyte")
	}

	// 正文区块：题面直接子级的 div 段落（描述）、input-specification/output-specification、
	// note；样例在 sample-tests。逐块抽取并重组成统一结构。
	var desc, inSpec, outSpec, note strings.Builder
	for c := ps.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch {
		case hasClass(c, "header"):
			// 头部（题号/限制行）不进正文；标题在头部里
			if t := findFirst(c, "title"); t != nil && out.Title == "" {
				out.Title = stripPrefixID(textContent(t))
			}
		case hasClass(c, "input-specification"):
			inSpec.WriteString(stripSectionTitle(innerHTML(c)))
		case hasClass(c, "output-specification"):
			outSpec.WriteString(stripSectionTitle(innerHTML(c)))
		case hasClass(c, "note"):
			note.WriteString(stripSectionTitle(innerHTML(c)))
		case hasClass(c, "sample-tests"):
			// 样例走统一重组（下面），不在正文里保留 CF 原始结构
		case c.Data == "div":
			desc.WriteString(innerHTML(c))
		}
	}

	// 样例：sample-tests > sample-test > (.input + .output)，每组样例一个 .sample-test 容器；
	// 直接在整个区块内扫 .input/.output（pre 文本按出现顺序配对）
	if st := findFirst(ps, "sample-tests"); st != nil {
		inputs, outputs := cfSampleIO(st)
		for i := range inputs {
			in := ""
			if i < len(inputs) {
				in = inputs[i]
			}
			out2 := ""
			if i < len(outputs) {
				out2 = outputs[i]
			}
			out.Samples = append(out.Samples, SampleIO{Input: in, Output: out2})
		}
	}

	var b strings.Builder
	if inSpec.Len() > 0 || outSpec.Len() > 0 || len(out.Samples) > 0 || note.Len() > 0 {
		b.WriteString(`<h3>题目描述</h3>`)
		b.WriteString(desc.String())
	} else {
		b.WriteString(desc.String())
	}
	if inSpec.Len() > 0 {
		b.WriteString(`<h3>输入格式</h3>` + inSpec.String())
	}
	if outSpec.Len() > 0 {
		b.WriteString(`<h3>输出格式</h3>` + outSpec.String())
	}
	if len(out.Samples) > 0 {
		b.WriteString(`<h3>样例</h3>`)
		for i, s := range out.Samples {
			fmt.Fprintf(&b, `<h4>样例 %d</h4><p>输入：</p><pre><code>%s</code></pre><p>输出：</p><pre><code>%s</code></pre>`,
				i+1, htmlEscape(s.Input), htmlEscape(s.Output))
		}
	}
	if note.Len() > 0 {
		b.WriteString(`<h3>说明</h3>` + note.String())
	}
	out.HTML = finishMath(b.String())
	return out, nil
}

// cfSampleIO 在样例区块内收集 .input/.output 的 pre 文本（CF 结构：sample-test 容器内
// 成对出现；老页面直接是 .input/.output 平铺）。
func cfSampleIO(sampleTests *html.Node) (inputs, outputs []string) {
	walk(sampleTests, func(c *html.Node) {
		if c.Type != html.ElementNode || !(hasClass(c, "input") || hasClass(c, "output")) {
			return
		}
		pre := findFirstTag(c, "pre")
		if pre == nil {
			return
		}
		text := cfPreText(pre)
		if hasClass(c, "input") {
			inputs = append(inputs, text)
		} else {
			outputs = append(outputs, text)
		}
	})
	return inputs, outputs
}

// cfPreText CF 样例 pre 里每行是 div（class=test-example-line-*），空行是无内容 div。
func cfPreText(pre *html.Node) string {
	var lines []string
	hasDiv := false
	for c := pre.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "div" {
			hasDiv = true
			lines = append(lines, textContent(c))
		}
	}
	if hasDiv {
		return strings.Join(lines, "\n")
	}
	return strings.TrimRight(textContent(pre), "\n")
}

func stripSectionTitle(h string) string {
	// 去掉 CF 自带的 "Input"/"Output"/"Note" 区块标题（我们用中文小节标题）
	r := regexpSectionTitle.ReplaceAllString(h, "")
	return r
}

var regexpSectionTitle = regexp.MustCompile(`<div class="section-title">[^<]*</div>`)

func cfProblemID(rawURL string) string {
	// .../contest/1234/problem/A → "1234A"
	parts := strings.Split(strings.TrimRight(rawURL, "/"), "/")
	var contest, idx string
	for i, p := range parts {
		switch {
		case p == "contest" && i+1 < len(parts):
			contest = parts[i+1]
		case p == "problem" && i+1 < len(parts):
			idx = parts[i+1]
		}
	}
	return contest + strings.ToUpper(idx)
}

func idOrEmpty(n *html.Node) string { return attr(n, "id") }

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
