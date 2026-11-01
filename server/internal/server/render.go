package server

// Markdown 预览渲染端点（v0.52 编辑器内芯换血）：编辑器分屏预览的唯一渲染源。
// 复用 mdrender.Render——与写入时渲染同一把 lute、同一套消毒口径，「编辑器里
// 看到的 = 发布后看到的」从结构上成立（$1 \le N$ 数字开头公式等口径差异绝迹，
// 客户端不再有第二渲染引擎）。渲染是 CPU 短临界区（写路径一直在用），前端
// 300ms 防抖后的零星调用无压力；独立限频窗不与提交限速共享预算，超限 429
// 由前端预览区降级提示，编辑与保存完全不受阻（编辑器本地自治）。

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/mdrender"
)

// renderMaxBytes 与全部写入口的 256KB 上限对齐（防病态内容拖垮渲染临界区）；
// renderPerMinute 按持续输入每秒一渲染也够用，超了只会预览降级不影响编辑。
const (
	renderMaxBytes  = 256 << 10
	renderPerMinute = 60
)

// RenderMarkdown POST /api/render {md} → {html}。登录态即可（预览不落库、
// 消毒在渲染层强制，无越面风险）。
func (h *Handlers) RenderMarkdown(c *gin.Context) {
	uid, _, _, _ := currentUser(c)
	var body struct {
		Md string `json:"md"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if len(body.Md) > renderMaxBytes {
		fail(c, http.StatusBadRequest, "内容过长（上限 256KB）")
		return
	}
	if !h.renderLimit.allow(uid, renderPerMinute, time.Now()) {
		fail(c, http.StatusTooManyRequests, "预览渲染过于频繁，请稍候")
		return
	}
	ok(c, gin.H{"html": mdrender.Render(body.Md)})
}
