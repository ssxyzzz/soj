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
// repo 层据此触发存量渲染缓存全量重刷，见 migrate_render.go）。
const LuteVersion = "1.7.8"

func engine() *lute.Lute {
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
	}
	return eng
}

// Render md → 消毒后的 HTML 片段（不含 <html> 骨架，v-html 直用）。
// 空串进空串出。超限/病态内容由写入口的体积上限把关（本层不做长度策略）。
func Render(md string) string {
	if md == "" {
		return ""
	}
	return engine().MarkdownStr("", md)
}
