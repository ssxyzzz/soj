// 题目导入适配器（v0.6.1，plan.md §2.3）：各站点解析为统一中间格式，站点解析与
// 存储格式解耦；每站配夹具单测，坏一个不伤其他。
//
// 一期实测结论（2026-10-19 抓取验证）：
//   - Codeforces：服务端渲染，DOM 结构多年稳定 → 完整适配（真实页夹具）。
//   - Hydro 系自建实例：hydrooj 开源渲染结构（problem-content / language-input{n} /
//     section__title / problem__tag-item）→ 完整适配；hydro.ac 本体被 Cerberus 反爬
//     拦截（返回 JS 挑战页），服务端抓不到——面向自建/友校实例。
//   - 洛谷：题面已登录墙（游客 302 无内容），不做代登录 → 剪贴板路线。
//   - LOJ：meta/样例/限制接口可取，但题面正文（markdown sections）的取数参数未能
//     定位 → 暂缓，走剪贴板路线。
package probimport

import (
	"context"
	"fmt"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"soj/internal/richtext"
)

// SampleIO 样例输入/输出文本（与 repo/richtext 同构，避免反向依赖）。
type SampleIO struct{ Input, Output string }

// RemoteProblem 统一中间格式：站点解析与存储格式解耦的唯一出口。
type RemoteProblem struct {
	Source  string     // 来源标注（如 "Codeforces 1A"、"Hydro example.com P1"）
	Title   string     // 纯标题（剥站点前缀）
	HTML    string     // 题面（含样例展示；数学已是 data-latex 契约或排版 HTML）
	Samples []SampleIO // 单独提取的样例对（表单展示与后续直接建测试数据用）
	TimeMS  int        // 时间限制 ms（0 = 站点未给）
	MemMB   int        // 内存限制 MB（0 = 站点未给）
}

// Unsupported 站点明确不支持（引导剪贴板路线）。
type Unsupported struct{ Host, Reason string }

func (e *Unsupported) Error() string {
	return fmt.Sprintf("暂不支持服务端导入 %s（%s），请用「粘贴导入」", e.Host, e.Reason)
}

// Parse 按 host 分发到适配器。
func Parse(ctx context.Context, f *Fetcher, rawURL string) (*RemoteProblem, error) {
	host := hostOf(rawURL)
	switch {
	case strings.Contains(host, "codeforces.com"):
		return parseCodeforces(ctx, f, rawURL)
	case strings.Contains(host, "luogu.com.cn"):
		return nil, &Unsupported{host, "题面已登录墙，无法匿名抓取"}
	case strings.Contains(host, "loj.ac"):
		return nil, &Unsupported{host, "题面正文接口暂未适配"}
	default:
		// 其余站点按 Hydro 系自建实例模式尝试（/d/{domain}/p/{pid} 或 /p/{pid}）
		return parseHydro(ctx, f, rawURL)
	}
}

func hostOf(rawURL string) string {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// ---- 共用 DOM 辅助（x/net/html） ----

// findFirst 深度优先找第一个匹配 class 的元素。
func findFirst(n *html.Node, class string) *html.Node {
	var walk func(*html.Node) *html.Node
	walk = func(cur *html.Node) *html.Node {
		if cur.Type == html.ElementNode && hasClass(cur, class) {
			return cur
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if hit := walk(c); hit != nil {
				return hit
			}
		}
		return nil
	}
	return walk(n)
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" && strings.Contains(" "+a.Val+" ", " "+class+" ") {
			return true
		}
	}
	return false
}

// innerHTML 序列化节点子树（x/net/html.Render，正确转义）。
func innerHTML(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&b, c)
	}
	return b.String()
}

// textContent 递归取纯文本。
func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}

// findFirstTag 深度优先找第一个指定标签名的元素（如 pre）。
func findFirstTag(n *html.Node, tag string) *html.Node {
	var walk func(*html.Node) *html.Node
	walk = func(cur *html.Node) *html.Node {
		if cur.Type == html.ElementNode && cur.Data == tag {
			return cur
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if hit := walk(c); hit != nil {
				return hit
			}
		}
		return nil
	}
	return walk(n)
}

// attr 取元素属性值。
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// parsePage 解析整页 HTML（容错：站点偶尔带 BOM/前导噪声）。
func parsePage(body []byte) (*html.Node, error) {
	doc, err := html.Parse(strings.NewReader(strings.TrimSpace(string(body))))
	if err != nil {
		return nil, fmt.Errorf("页面解析失败: %w", err)
	}
	return doc, nil
}

// finishMath 把题面 HTML 的文本节点 $…$ 统一转 data-latex 契约（Hydro 的 md 渲染
// 会把行内公式留成文本；CF 的 tex-span 已排版不受影响）。
func finishMath(h string) string { return richtext.ConvertMath(h) }

// stripPrefixID 剥标题里的题号前缀（"A. Watermelon" → "Watermelon"；"P1001 A+B Problem" → 剥 P1001）。
func stripPrefixID(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "."); i > 0 && i <= 4 && isUpperAlpha(s[:i]) {
		return strings.TrimSpace(s[i+1:])
	}
	if m := regexp.MustCompile(`^[A-Za-z]{0,2}\d{1,6}[-·–.\s]+`).FindString(s); m != "" {
		return strings.TrimSpace(s[len(m):])
	}
	return s
}

func isUpperAlpha(s string) bool {
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return s != ""
}

// parseLimitText 从限制文本提取数值（"1 second"→1000ms；"256 megabytes"→256；"1000ms"→1000；"1000~2000ms"→1000 取下界）。
func parseLimitText(text string, unit string) int {
	t := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(t, unit) {
		nums := regexp.MustCompile(`\d+(?:\.\d+)?`).FindAllString(t, -1)
		if len(nums) > 0 {
			v, _ := strconv.ParseFloat(nums[0], 64)
			switch unit {
			case "second":
				return int(v * 1000)
			default:
				return int(v)
			}
		}
	}
	// 单位词形变化（seconds/megabytes）宽松匹配
	nums := regexp.MustCompile(`\d+(?:\.\d+)?`).FindAllString(t, -1)
	if len(nums) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(nums[0], 64)
	switch {
	case strings.Contains(t, "second"):
		return int(v * 1000)
	case strings.Contains(t, "megabyte") || strings.Contains(t, "mib"):
		return int(v)
	case strings.Contains(t, "ms"):
		return int(v)
	}
	return 0
}
