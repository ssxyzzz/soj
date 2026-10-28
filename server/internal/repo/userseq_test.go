package repo

// 注册取号起点（v0.24 小批）单测：SeedUserCounter 起点垫高与 CreateUserWithID 的
// 计数器推进（手动 ID 不被后续自增撞号）。本地无 Mongo 自动 skip（openTestRepo 同款）。

import (
	"context"
	"testing"
)

func TestSeedUserCounterAndManualID(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 全新库：起点守卫后首个自增号应为 100
	if err := r.SeedUserCounter(ctx); err != nil {
		t.Fatalf("SeedUserCounter: %v", err)
	}
	uid, err := r.CreateUser(ctx, "alice", "hash", "Alice", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if uid != DefaultUserSeqStart {
		t.Fatalf("首个注册号 = %d，期望 %d", uid, DefaultUserSeqStart)
	}

	// 手动指定 ID（跳号）：后续自增必须避开
	manual, err := r.CreateUserWithID(ctx, 500, "bob", "hash", "Bob", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUserWithID: %v", err)
	}
	if manual != 500 {
		t.Fatalf("手动 ID = %d，期望 500", manual)
	}
	next, err := r.CreateUser(ctx, "carol", "hash", "Carol", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser after manual: %v", err)
	}
	if next <= 500 {
		t.Fatalf("手动指定 500 后自增号 = %d，应 > 500", next)
	}

	// 存量部署计数器已超起点：守卫不回退
	if err := r.SeedUserCounter(ctx); err != nil {
		t.Fatalf("SeedUserCounter second: %v", err)
	}
	again, err := r.CreateUser(ctx, "dave", "hash", "Dave", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser again: %v", err)
	}
	if again != next+1 {
		t.Fatalf("重复守卫后自增号 = %d，期望 %d（不应回退）", again, next+1)
	}
}

// TestUserSeqStartRuntime 运行期调大起点即时生效：设置 500 后下一个自动号 ≥ 500；
// 调小起点不回退（从计数器继续）。
func TestUserSeqStartRuntime(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	start := 300
	if err := r.UpdateSystemSettings(ctx, &SystemSettings{RateLimitPerMinute: DefaultRateLimitPerMinute, UserSeqStart: &start}); err != nil {
		t.Fatalf("UpdateSystemSettings: %v", err)
	}
	uid, err := r.CreateUser(ctx, "erin", "hash", "Erin", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if uid < 300 {
		t.Fatalf("起点 300 下首个自动号 = %d，应 ≥ 300", uid)
	}

	// 调小起点：计数器已超过，不回退
	lower := 5
	if err := r.UpdateSystemSettings(ctx, &SystemSettings{RateLimitPerMinute: DefaultRateLimitPerMinute, UserSeqStart: &lower}); err != nil {
		t.Fatalf("UpdateSystemSettings lower: %v", err)
	}
	uid2, err := r.CreateUser(ctx, "frank", "hash", "Frank", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser second: %v", err)
	}
	if uid2 != uid+1 {
		t.Fatalf("调小起点后自动号 = %d，期望 %d（从计数器继续不回退）", uid2, uid+1)
	}
}
