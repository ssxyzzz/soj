package repo

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/event"
)

// 慢操作观测（v0.66 批①）：单条 Mongo 命令成功返回且耗时超阈值时打 Warn
// （命令名 + 库名 + 耗时），把「用户投诉才知道慢」变成「日志直接点名」——与
// server 层慢请求日志配对，一个从 HTTP 面点名、一个从数据面点名，慢在哪层一目了然。
// 只观测不改超时策略：op 超时保持默认（见 Connect 注释，防误伤长聚合/建索引）。
// 失败命令不在此记（错误随请求 5xx 暴露，避免双重噪音），观测以成功命令为主。
const slowOpWarnThreshold = 200 * time.Millisecond

// slowOpMonitor 挂在 options.Client().SetMonitor 上的命令级探针。每条命令都会
// 回调（含 getMore/hello 等高频小命令），回调内只做一次时长比较，开销可忽略。
func slowOpMonitor() *event.CommandMonitor {
	return &event.CommandMonitor{
		Succeeded: func(_ context.Context, ev *event.CommandSucceededEvent) {
			if ev.Duration >= slowOpWarnThreshold {
				slog.Warn("慢 Mongo 命令", "component", "repo",
					"cmd", ev.CommandName, "db", ev.DatabaseName,
					"ms", ev.Duration.Milliseconds())
			}
		},
	}
}
