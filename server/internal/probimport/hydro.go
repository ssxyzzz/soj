// Hydro 系自建实例适配器：按 hydrooj 开源渲染结构解析（templates/problem_detail.html +
// lib/content.ts buildContent）。hydro.ac 本体被 Cerberus 反爬保护抓不到——本适配器面向
// 自建/友校实例（无反爬，老师典型场景）。
//
// URL 形态：{origin}/d/{domain}/p/{pid}、{origin}/d/{domain}/{pid}、{origin}/p/{pid}。
// 结构（hydrooj 渲染口径）：h1.section__title（题号+名称）；div.problem-content（题面
// 渲染 HTML，样例区块为 pre>code.language-input{n}/language-output{n}）；限制在
// span.problem__tag-item（icon-stopwatch=时间 ms，icon-comparison=内存 MiB）。
package probimport

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func parseHydro(ctx context.Context, f *Fetcher, rawURL string) (*RemoteProblem, error) {
	body, err := f.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := parsePage(body)
	if err != nil {
		return nil, err
	}
	if findFirst(doc, "problem-content") == nil && findFirst(doc, "section__title") == nil {
		return nil, fmt.Errorf("页面里没有找到题面区块（problem-content），该站点可能不是 Hydro 系、已改版或有反爬拦截")
	}

	out := &RemoteProblem{Source: "Hydro " + hostOf(rawURL)}

	// 标题：h1.section__title 文本剥题号前缀
	if h1 := findFirst(doc, "section__title"); h1 != nil {
		out.Title = stripPrefixID(textContent(h1))
	}
	if out.Title == "" {
		return nil, fmt.Errorf("未能解析出标题")
	}

	// 限制：problem__tag-item 文本（1000ms / 1000~2000ms / 256MiB）
	for _, item := range findAll(doc, "problem__tag-item") {
		t := textContent(item)
		switch {
		case strings.Contains(t, "ms"):
			if out.TimeMS == 0 {
				out.TimeMS = firstNumber(t)
			}
		case strings.Contains(strings.ToLower(t), "mib") || strings.Contains(strings.ToLower(t), "mb"):
			if out.MemMB == 0 {
				out.MemMB = firstNumber(t)
			}
		}
	}

	// 题面：div.problem-content 的渲染 HTML（数学文本 $…$ 经 finishMath 进契约；
	// 样例区块保留在正文里展示，同时单独提取进 Samples）
	pc := findFirst(doc, "problem-content")
	out.HTML = finishMath(innerHTML(pc))
	out.Samples = hydroSampleIO(pc)
	return out, nil
}

// hydroSampleIO 从样例区块提取输入/输出（code.language-input{n} / language-output{n} 配对）。
func hydroSampleIO(root *html.Node) []SampleIO {
	var in, out []string
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "code" {
			return
		}
		cls := attr(n, "class")
		text := strings.TrimRight(textContent(n), "\n")
		switch {
		case regexp.MustCompile(`language-input(\d+)`).MatchString(cls):
			in = appendAt(in, cls, text)
		case regexp.MustCompile(`language-output(\d+)`).MatchString(cls):
			out = appendAt(out, cls, text)
		}
	})
	var out2 []SampleIO
	for i := 0; i < len(in) || i < len(out); i++ {
		var s SampleIO
		if i < len(in) {
			s.Input = in[i]
		}
		if i < len(out) {
			s.Output = out[i]
		}
		out2 = append(out2, s)
	}
	return out2
}

// appendAt 按样例序号放到对应下标（hydrooj 序号从 1）。
func appendAt(list []string, class, text string) []string {
	m := regexp.MustCompile(`language-(input|output)(\d+)`).FindStringSubmatch(class)
	idx := 0
	if m != nil {
		n, _ := strconv.Atoi(m[2])
		idx = n - 1
	}
	for len(list) <= idx {
		list = append(list, "")
	}
	list[idx] = text
	return list
}

func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func findAll(n *html.Node, class string) []*html.Node {
	var out []*html.Node
	walk(n, func(cur *html.Node) {
		if cur.Type == html.ElementNode && hasClass(cur, class) {
			out = append(out, cur)
		}
	})
	return out
}

func firstNumber(t string) int {
	m := regexp.MustCompile(`\d+`).FindString(t)
	if m == "" {
		return 0
	}
	v, _ := strconv.Atoi(m)
	return v
}
