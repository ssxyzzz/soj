package server

// 判题可观测（v0.32 轻量口径）：只读统计端点挂管理面（root 门槛，与种子/备份
// 同为部署级运维视角）。滚动窗口来自 judge 包进程级单例（同进程部署 = 全量口径）；
// Pending 深度实时查库；判题进程独立部署时 Web 进程窗口为空，看判题进程日志。
// 多实例部署各进程各自为政——单副本边界，见 plan 工程备忘。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"soj/internal/judge"
)

// AdminJudgeStats GET /admin/judge/stats：近 1min/5min 判题数、5min 窗口平均/最大
// 判题时长、当前 Pending 深度。
func (h *Handlers) AdminJudgeStats(c *gin.Context) {
	pending, err := h.repo.PendingDepth(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取队列深度失败")
		return
	}
	s := judge.SnapshotStats()
	ok(c, gin.H{
		"judged_1m": s.Judged1m,
		"judged_5m": s.Judged5m,
		"avg_ms_5m": s.AvgMS,
		"max_ms_5m": s.MaxMS,
		"pending":   pending,
	})
}
