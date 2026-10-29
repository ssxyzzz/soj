package judge

// 判题可观测（v0.32 轻量口径，不上 Prometheus）：进程内滚动窗口统计
// 近 1min/5min 判题完成数与判题时长（evaluate 墙钟，不含排队等待），
// 快照经管理端点只读暴露；Pending 深度告警由 Pool 的巡检 goroutine 承担
// （每分钟查一次队列深度，超阈值 Warn 一行——沙箱故障/积压从「学生报障才发现」
// 变为主动可见）。单副本边界：进程内状态，多实例部署需收敛为共享存储（工程备忘口径）。

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const metricsWindow = 5 * time.Minute

// PendingWarnDepth Pending 深度告警阈值（每分钟巡检，超过即 Warn 一行）。
const PendingWarnDepth = 20

type judgeDone struct {
	at  time.Time
	dur time.Duration
}

type metrics struct {
	mu   sync.Mutex
	done []judgeDone
}

var gMetrics metrics // 进程级单例：Pool 各 worker 并发记录

// recordJudge 记一次判题完成（dur = evaluate 起止墙钟）。
func recordJudge(dur time.Duration) {
	now := time.Now()
	gMetrics.mu.Lock()
	gMetrics.done = append(gMetrics.done, judgeDone{now, dur})
	cut := now.Add(-metricsWindow)
	i := 0
	for ; i < len(gMetrics.done) && gMetrics.done[i].at.Before(cut); i++ {
	}
	if i > 0 {
		gMetrics.done = gMetrics.done[i:]
	}
	gMetrics.mu.Unlock()
}

// JudgeStatsSnapshot 滚动窗口快照：近 1min/5min 完成数，5min 窗口平均/最大耗时（毫秒）。
type JudgeStatsSnapshot struct {
	Judged1m int     `json:"judged_1m"`
	Judged5m int     `json:"judged_5m"`
	AvgMS    float64 `json:"avg_ms_5m"`
	MaxMS    int64   `json:"max_ms_5m"`
}

// SnapshotStats 当前窗口快照（管理端点读取；无并发记录时全零）。
func SnapshotStats() JudgeStatsSnapshot {
	now := time.Now()
	m1, m5 := now.Add(-time.Minute), now.Add(-metricsWindow)
	var s JudgeStatsSnapshot
	var total time.Duration
	gMetrics.mu.Lock()
	for _, d := range gMetrics.done {
		if d.at.Before(m5) {
			continue
		}
		s.Judged5m++
		total += d.dur
		if ms := d.dur.Milliseconds(); ms > s.MaxMS {
			s.MaxMS = ms
		}
		if !d.at.Before(m1) {
			s.Judged1m++
		}
	}
	gMetrics.mu.Unlock()
	if s.Judged5m > 0 {
		s.AvgMS = float64(total.Microseconds()) / float64(s.Judged5m) / 1000
	}
	return s
}

// pendingWatch Pending 深度巡检（Pool 启动，进程内单例）：每分钟一次，
// 超过 PendingWarnDepth 告警一行；读库失败也只 Warn（巡检自身不能成为噪声源）。
func (j *Judger) pendingWatch(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := j.Repo.PendingDepth(ctx)
			if err != nil {
				slog.Warn("Pending 深度巡检失败", "component", "judge", "error", err)
				continue
			}
			if n > PendingWarnDepth {
				slog.Warn("判题积压：Pending 深度超阈值", "component", "judge", "pending", n, "threshold", PendingWarnDepth)
			}
		}
	}
}
