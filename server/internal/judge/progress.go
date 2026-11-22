package judge

// 逐点进度回写器（v0.98 全站推送 P3）：判题循环每完成一个测试点投递，经
// 200ms 窗口合批 $push 进 submissions.detail（vj4 是 16ms 直推，我们更保守——
// workers=2 下写放大上限可控，动热点须压测比对基线）。
//
// 护栏（绝不反向影响判题主流程）：任一回写失败（含租约丢失 ErrLeaseLost）只记
// 日志并停用本份提交的回写——进度显示是纯增益，判题结果一字不差照常回写；
// 终态无需冲刷尾部批次：FinishSubmission 对 detail 整写覆盖，未冲刷的点自然
// 随终态一次到位（caseDone 瞬态字段同步清除）。

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"soj/internal/repo"
)

// progressFlushInterval 合批窗口：窗口内的点攒一批一次 $push（点速快于窗口时
// 写放大上限 = 每份提交每窗口一次）；单测可注入毫秒级。
var progressFlushInterval = 200 * time.Millisecond

// progressCtxTimeout 回写用独立短超时：不挂判题 ctx——取消/租约丢失路径上进度
// 回写不应再抢判题循环的时间片（终态回写由主路径负责）。
const progressCtxTimeout = 3 * time.Second

// progressStore 逐点回写的最小仓储面（*repo.Repo 隐式满足；单测注入桩）。
type progressStore interface {
	PushCaseResults(ctx context.Context, sid int64, judgeName string, batch []*repo.CaseResult, done, total int) error
	SetCaseStage(ctx context.Context, sid int64, judgeName, stage string, total int) error
}

// progressReporter 每份提交一个（evaluate 入口创建），非 goroutine 安全——
// 判题循环单线程调用（互斥只防 future 误用，代价可忽略）。
type progressReporter struct {
	store progressStore
	sub   *repo.Submission
	total int

	mu      sync.Mutex
	pending []*repo.CaseResult
	done    int
	lastAt  time.Time
	dead    bool // 回写失败后停用（租约丢失/写库异常）
}

func (j *Judger) newProgressReporter(sub *repo.Submission, total int) *progressReporter {
	if total <= 0 {
		return nil // 无测试点路径（客观题已在 evaluate 前分流）不出信号
	}
	return &progressReporter{store: j.Repo, sub: sub, total: total}
}

// stage 编译期阶段标记（前端「编译中…」）；进入逐点回写后由 PushCaseResults
// 覆写为 run。失败停用同 Add。
func (p *progressReporter) stage(stage string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), progressCtxTimeout)
	defer cancel()
	if err := p.store.SetCaseStage(ctx, p.sub.ID, p.sub.Judge, stage, p.total); err != nil {
		p.disable(err)
		return
	}
	p.lastAt = time.Now() // stage 也是一次回写：推进合批窗口，编译后首点不立即冲刷
}

// Add 投递一个已完成的测试点（evaluate 循环在 st.detail 追加后调用）；
// 距上次冲刷超过窗口即落库，否则攒批。
func (p *progressReporter) Add(cr *repo.CaseResult) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return
	}
	p.pending = append(p.pending, cr)
	p.done++
	if time.Since(p.lastAt) < progressFlushInterval {
		return
	}
	p.flushLocked()
}

// flushLocked 冲刷攒批（调用方持锁）。
func (p *progressReporter) flushLocked() {
	if len(p.pending) == 0 || p.dead {
		return
	}
	batch := p.pending
	p.pending = nil
	p.lastAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), progressCtxTimeout)
	defer cancel()
	if err := p.store.PushCaseResults(ctx, p.sub.ID, p.sub.Judge, batch, p.done, p.total); err != nil {
		p.disable(err)
	}
}

// disable 停用本份提交的进度回写：进度是纯增益，任何失败都不值得重试或影响判题。
func (p *progressReporter) disable(err error) {
	p.dead = true
	p.pending = nil
	slog.Info("逐点进度回写停用（不影响判题）", "component", "judge",
		"sid", p.sub.ID, "error", err)
}

// caseTotalOf 题目测试点总数（子任务模式求和，单点模式直取）。
func caseTotalOf(problem *repo.Problem) int {
	if len(problem.Subtasks) > 0 {
		n := 0
		for _, st := range problem.Subtasks {
			n += len(st.Cases)
		}
		return n
	}
	return len(problem.Cases)
}
