package repo

// 自动题号起点单测：SeedProblemCounter 起点垫高、CreateProblemShell 首题 = 起点、
// UpsertProblem 指定题号的计数器推进、运行期调大/调小起点的只进不退。
// 本地无 Mongo 自动 skip（openTestRepo 同款）。

import (
	"context"
	"testing"
)

func TestProblemSeqStartAndShell(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 全新库：起点守卫后首题应为 1001
	if err := r.SeedProblemCounter(ctx); err != nil {
		t.Fatalf("SeedProblemCounter: %v", err)
	}
	p := &Problem{Title: "起点题", TimeLimitMS: 1000, MemoryLimitKB: 262144}
	if err := r.CreateProblemShell(ctx, p); err != nil {
		t.Fatalf("CreateProblemShell: %v", err)
	}
	if p.ID != DefaultProblemSeqStart {
		t.Fatalf("首题题号 = %d，期望 %d", p.ID, DefaultProblemSeqStart)
	}

	// 指定题号导入（跳号 1500）：后续自增必须避开
	manual := &Problem{ID: 1500, Title: "指定题", TimeLimitMS: 1000, MemoryLimitKB: 262144}
	if err := r.UpsertProblem(ctx, manual); err != nil {
		t.Fatalf("UpsertProblem: %v", err)
	}
	next := &Problem{Title: "后续题", TimeLimitMS: 1000, MemoryLimitKB: 262144}
	if err := r.CreateProblemShell(ctx, next); err != nil {
		t.Fatalf("CreateProblemShell after manual: %v", err)
	}
	if next.ID <= 1500 {
		t.Fatalf("指定 1500 后自动题号 = %d，应 > 1500", next.ID)
	}

	// 存量部署计数器已超起点：守卫不回退
	if err := r.SeedProblemCounter(ctx); err != nil {
		t.Fatalf("SeedProblemCounter second: %v", err)
	}
	again := &Problem{Title: "再来一题", TimeLimitMS: 1000, MemoryLimitKB: 262144}
	if err := r.CreateProblemShell(ctx, again); err != nil {
		t.Fatalf("CreateProblemShell again: %v", err)
	}
	if again.ID != next.ID+1 {
		t.Fatalf("重复守卫后题号 = %d，期望 %d（不应回退）", again.ID, next.ID+1)
	}
}

// TestProblemSeqStartRuntime 运行期调大起点即时生效：设置 2000 后下一个自动号 ≥ 2000；
// 调小起点不回退（从计数器继续）。
func TestProblemSeqStartRuntime(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	start := 2000
	if err := r.UpdateSystemSettings(ctx, &SystemSettings{RateLimitPerMinute: DefaultRateLimitPerMinute, ProblemSeqStart: &start}); err != nil {
		t.Fatalf("UpdateSystemSettings: %v", err)
	}
	p := &Problem{Title: "起点 2000 题", TimeLimitMS: 1000, MemoryLimitKB: 262144}
	if err := r.CreateProblemShell(ctx, p); err != nil {
		t.Fatalf("CreateProblemShell: %v", err)
	}
	if p.ID < 2000 {
		t.Fatalf("起点 2000 下自动题号 = %d，应 ≥ 2000", p.ID)
	}

	// 调小起点：计数器已超过，不回退
	lower := 5
	if err := r.UpdateSystemSettings(ctx, &SystemSettings{RateLimitPerMinute: DefaultRateLimitPerMinute, ProblemSeqStart: &lower}); err != nil {
		t.Fatalf("UpdateSystemSettings lower: %v", err)
	}
	p2 := &Problem{Title: "调小后题", TimeLimitMS: 1000, MemoryLimitKB: 262144}
	if err := r.CreateProblemShell(ctx, p2); err != nil {
		t.Fatalf("CreateProblemShell lower: %v", err)
	}
	if p2.ID != p.ID+1 {
		t.Fatalf("调小起点后自动题号 = %d，期望 %d（从计数器继续不回退）", p2.ID, p.ID+1)
	}
}
