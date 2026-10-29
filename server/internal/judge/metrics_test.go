package judge

import (
	"testing"
	"time"
)

// 滚动窗口统计（判题可观测）的窗口归组与裁剪口径。
func TestSnapshotStatsWindow(t *testing.T) {
	gMetrics = metrics{} // 包级单例，逐例重置
	now := time.Now()
	gMetrics.done = []judgeDone{
		{at: now.Add(-6 * time.Minute), dur: 9 * time.Second},          // 窗口外：不计
		{at: now.Add(-3 * time.Minute), dur: 2 * time.Second},          // 5min 内
		{at: now.Add(-90 * time.Second), dur: 1500 * time.Millisecond}, // 1min 外、5min 内
		{at: now.Add(-10 * time.Second), dur: 700 * time.Millisecond},  // 1min 内
	}
	s := SnapshotStats()
	if s.Judged5m != 3 || s.Judged1m != 1 {
		t.Fatalf("窗口完成数 = %d/%d，期望 3/1", s.Judged5m, s.Judged1m)
	}
	// 平均 = (2000 + 1500 + 700)ms / 3 = 1400ms；最大 2000ms
	if s.AvgMS != 1400 {
		t.Fatalf("avg = %v，期望 1400", s.AvgMS)
	}
	if s.MaxMS != 2000 {
		t.Fatalf("max = %d，期望 2000", s.MaxMS)
	}
}

// recordJudge 并发记录后裁剪：窗口外旧条目被丢弃，不无限增长。
func TestRecordJudgePrunes(t *testing.T) {
	gMetrics = metrics{}
	gMetrics.done = []judgeDone{{at: time.Now().Add(-metricsWindow - time.Second), dur: time.Second}}
	recordJudge(50 * time.Millisecond)
	if len(gMetrics.done) != 1 {
		t.Fatalf("裁剪后残留 %d 条，期望 1（仅新记录）", len(gMetrics.done))
	}
	s := SnapshotStats()
	if s.Judged5m != 1 || s.Judged1m != 1 || s.MaxMS != 50 {
		t.Fatalf("快照 = %+v，期望单条 50ms", s)
	}
}
