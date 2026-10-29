// Package mdrender 查看端 Markdown 写入时渲染管线（v0.30，Discourse 模式）：
// md 保存时经 Go 原生 lute 渲染成 HTML 并消毒，与 md 双存（html 是渲染缓存，
// md 永远是唯一事实源）；读取接口直发 html，查看页零引擎下载、零客户端渲染 pass。
//
// 引擎一致性：lute 即 Vditor 的渲染引擎本体（Vditor 的 lute.min.js 由本库转译），
// 服务端与编辑端预览同源；v0.26.2 起编辑端子资源走版本化缓存，本包渲染选项对齐
// MdViewInner 的 Vditor.preview 配置（sanitize 开、GFM 自动链接、脚注、emoji）。
//
// 公式与代码高亮留在客户端：公式由 lute 输出 language-math 节点（前端 KaTeX 节点级
// render，与 Vditor 内部做法同源），代码块输出 language-xxx class（前端 hljs 按需高亮）。
package mdrender

import (
	"sync"

	"github.com/88250/lute"
)

var (
	mu  sync.Mutex // lute 引擎非并发安全（内部共享 emoji 表等），渲染串行化
	eng *lute.Lute
)

// LuteVersion 引擎版本标记（与 go.mod 的 lute 依赖对应；升级依赖时同步改这里，
// repo 层据此触发存量渲染缓存全量重刷，见 migrate_render.go）。后缀标记渲染口径
// 变更：-mdig = 开启行内公式「$ 后允许数字」（2026-10-29：luogu 题面 $1$、
// $1 \leq N \leq 3\cdot 10^5$ 被 lute 默认的金额保护拒绝、整段显示原文；前端
// Vditor 预览的 math.inlineDigit:true 早已放开——两端口径对齐）。
const LuteVersion = "1.7.8-mdig"

// Render md → 消毒后的 HTML 片段（不含 <html> 骨架，v-html 直用）。
// 空串进空串出。超限/病态内容由写入口的体积上限把关（本层不做长度策略）。
// 渲染全程持锁：lute 引擎内部有共享态（emoji 表等），此前 MarkdownStr 跑在锁外，
// 并发安全只是碰巧（当前版本的 Markdown 路径恰好无写共享态）——升级 lute 即静默
// 数据竞争。渲染是 CPU 型短临界区，串行可接受（后端快赢批根修）。
func Render(md string) string {
	if md == "" {
		return ""
	}
	mu.Lock()
	defer mu.Unlock()
	if eng == nil {
		eng = lute.New()
		// 与前端 Vditor.preview（MdViewInner）口径对齐：
		// sanitize 开（XSS 消毒在写入时服务端强制）；GFM 自动链接/脚注/emoji 是
		// lute.New 默认开启项，无需显式设置
		eng.RenderOptions.Sanitize = true
		// 服务端只产结构不产色：关闭内联高亮样式，输出标准 language-xxx class
		// 交给前端 hljs（与现查看端一致）
		eng.RenderOptions.CodeSyntaxHighlight = false
		// 行内公式 $ 后允许数字（对齐前端 math.inlineDigit:true，MdViewInner 同款
		// 注释——lute 默认的金额保护会拒 $1$ / $0$ / $1 \leq N \leq …$ 这类 OJ
		// 题面数据范围的高频形态，html 里整段留原文）
		eng.SetInlineMathAllowDigitAfterOpenMarker(true)
	}
	return eng.MarkdownStr("", md)
}
