package judge

// progressReporter 单测（v0.98 逐点回写）：合批窗口（攒批不出窗不落库、出窗
// 即冲）、回写失败自停用（进度纯增益不影响判题）、total<=0 零值路径 no-op。
// store 走窄接口 progressStore 注入桩，不接真 Mongo。

import (
	"context"
	"sync"
	"testing"
	"time"

	"soj/internal/repo"
)

// pushRecorder 记录 PushCaseResults/SetCaseStage 调用序列的假 repo。
type pushRecorder struct {
	mu      sync.Mutex
	pushes  [][]*repo.CaseResult // 每次调用的批次
	stages  []string
	failAll bool // 模拟写库失败
}

func (p *pushRecorder) PushCaseResults(_ context.Context, sid int64, judgeName string, batch []*repo.CaseResult, done, total int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failAll {
		return repo.ErrLeaseLost
	}
	cp := make([]*repo.CaseResult, len(batch))
	copy(cp, batch)
	p.pushes = append(p.pushes, cp)
	_ = sid
	_ = judgeName
	_ = done
	_ = total
	return nil
}

func (p *pushRecorder) SetCaseStage(_ context.Context, sid int64, judgeName, stage string, total int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failAll {
		return repo.ErrLeaseLost
	}
	p.stages = append(p.stages, stage)
	_ = sid
	_ = judgeName
	_ = total
	return nil
}

// newTestReporter 直接装配 reporter（store 走窄接口 progressStore 注入桩）。
func newReporterWith(p progressStore, sub *repo.Submission, total int) *progressReporter {
	return &progressReporter{store: p, sub: sub, total: total}
}

func TestProgressReporterBatching(t *testing.T) {
	old := progressFlushInterval
	progressFlushInterval = 50 * time.Millisecond
	t.Cleanup(func() { progressFlushInterval = old })

	rec := &pushRecorder{}
	p := newReporterWith(rec, &repo.Submission{ID: 1, Judge: "w1"}, 10)
	p.stage("compile")
	if len(rec.stages) != 1 || rec.stages[0] != "compile" {
		t.Fatalf("编译期标记应立即落库: %v", rec.stages)
	}

	// 窗口内三个点攒一批：Add 不触发落库（首拍 lastAt 已被 stage 推进）
	for i := 1; i <= 3; i++ {
		p.Add(&repo.CaseResult{Index: i, Status: "AC"})
	}
	if len(rec.pushes) != 0 {
		t.Fatalf("窗口内不应落库: %v", rec.pushes)
	}
	// 过窗后第 4 个点触发冲刷（前三点一批带走）
	time.Sleep(60 * time.Millisecond)
	p.Add(&repo.CaseResult{Index: 4, Status: "WA"})
	if len(rec.pushes) != 1 || len(rec.pushes[0]) != 4 {
		t.Fatalf("出窗应合并冲刷: pushes=%v", rec.pushes)
	}
	if p.done != 4 {
		t.Fatalf("进度计数应连续: %d", p.done)
	}
}

func TestProgressReporterFailureDisables(t *testing.T) {
	old := progressFlushInterval
	progressFlushInterval = 10 * time.Millisecond
	t.Cleanup(func() { progressFlushInterval = old })

	rec := &pushRecorder{failAll: true}
	p := newReporterWith(rec, &repo.Submission{ID: 2, Judge: "w1"}, 5)

	p.stage("compile") // 失败 → 停用
	if !p.dead {
		t.Fatal("回写失败应停用 reporter")
	}
	// 停用后 Add 全部 no-op（不再尝试落库、不再攒批）
	p.Add(&repo.CaseResult{Index: 1, Status: "AC"})
	time.Sleep(30 * time.Millisecond)
	p.Add(&repo.CaseResult{Index: 2, Status: "AC"})
	if len(rec.pushes) != 0 || len(p.pending) != 0 {
		t.Fatalf("停用后不应再尝试: %v %v", rec.pushes, p.pending)
	}
}

func TestNilReporterNoOp(t *testing.T) {
	// total<=0 返回 nil：Add/stage 全链 no-op 不 panic（自测/客观题路径）
	j := &Judger{}
	p := j.newProgressReporter(&repo.Submission{ID: 9}, 0)
	if p != nil {
		t.Fatal("total<=0 应返回 nil")
	}
	p.stage("compile")
	p.Add(&repo.CaseResult{Index: 1, Status: "AC"})
}
