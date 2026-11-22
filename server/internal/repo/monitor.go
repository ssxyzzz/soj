package repo

import (
	"context"
	"log/slog"
	"sync"
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
// 回调（含 getMore/hello 等高频小命令），回调内只做命令名与时长比较，开销可忽略
// （BenchmarkSlowOp*）。
//
// 等待型 getMore（v0.98.1）：Change Stream / tailable awaitData 游标的 getMore 是
// 「故意挂着等新数据」，耗时 = 等待而非工作。按常规阈值判，变更流每秒一次的空闲等待
// 会条条报成慢命令（v0.98 上线后生产 web 日志 77% 是它：真慢命令被淹没，日志轮转
// 留存的时间窗被压掉大半）。口径：请求自带 maxTimeMS 即等待型，申报预算内返回不算慢；
// 超出预算达常规阈值才告警（带 awaitMS 标出预算，一眼可辨是服务端超预算返回）。
// 预算只在 Started 事件的请求体里，按 RequestID 带到 Succeeded。驱动只在显式设置
// MaxAwaitTime 时才发这个字段——等待型游标必须显式设置（见 subwatch.go），否则探针
// 无从识别，照常规阈值判。
func slowOpMonitor() *event.CommandMonitor {
	// 只存带 maxTimeMS 的 getMore（每秒量级），普通命令不进这张表。
	var awaits sync.Map // RequestID → 等待预算 time.Duration
	return &event.CommandMonitor{
		Started: func(_ context.Context, ev *event.CommandStartedEvent) {
			if ev.CommandName != "getMore" {
				return
			}
			if v, err := ev.Command.LookupErr("maxTimeMS"); err == nil {
				if ms, ok := v.Int64OK(); ok && ms > 0 {
					awaits.Store(ev.RequestID, time.Duration(ms)*time.Millisecond)
				}
			}
		},
		Succeeded: func(_ context.Context, ev *event.CommandSucceededEvent) {
			var budget time.Duration
			if ev.CommandName == "getMore" {
				if b, ok := awaits.LoadAndDelete(ev.RequestID); ok {
					budget = b.(time.Duration)
					if ev.Duration < budget+slowOpWarnThreshold {
						return // 申报预算内返回：等待而非工作
					}
				}
			}
			if ev.Duration < slowOpWarnThreshold {
				return
			}
			attrs := []any{"component", "repo", "cmd", ev.CommandName, "db", ev.DatabaseName,
				"ms", ev.Duration.Milliseconds()}
			if budget > 0 {
				attrs = append(attrs, "awaitMS", budget.Milliseconds())
			}
			slog.Warn("慢 Mongo 命令", attrs...)
		},
		Failed: func(_ context.Context, ev *event.CommandFailedEvent) {
			if ev.CommandName == "getMore" {
				awaits.Delete(ev.RequestID) // 失败的等待不配对 Succeeded：清掉防残留
			}
		},
	}
}
