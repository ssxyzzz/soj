package repo

// 邮箱验证码（v0.16）repo 行为回归：最新一码有效 / 一次性消费 / 失败 5 次作废 /
// 频控计数（冷却与小时/日窗口）。复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestVerifyCodeLifecycle(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	mk := func(code string) *VerifyCode {
		now := time.Now()
		return &VerifyCode{
			Purpose: VerifyPurposeReset, Target: "a@test.com", CodeHash: "hash-" + code,
			IP: "1.2.3.4", CreatedAt: now,
			ExpiresAt: now.Add(VerifyCodeTTL), PurgeAt: now.Add(VerifyCodePurgeTTL),
		}
	}

	if err := r.InsertVerifyCode(ctx, mk("first")); err != nil {
		t.Fatalf("插入验证码: %v", err)
	}
	if err := r.InsertVerifyCode(ctx, mk("second")); err != nil {
		t.Fatalf("再插验证码: %v", err)
	}
	// 最新一码有效：旧码（first）已随重发作废
	vc, err := r.GetValidVerifyCode(ctx, VerifyPurposeReset, "a@test.com")
	if err != nil {
		t.Fatalf("取有效验证码: %v", err)
	}
	if vc.CodeHash != "hash-second" {
		t.Fatalf("应取最新一码，got %q", vc.CodeHash)
	}
	// 用途绑定：别的用途取不到
	if _, err := r.GetValidVerifyCode(ctx, VerifyPurposeRegister, "a@test.com"); !IsNotFound(err) {
		t.Fatalf("跨用途应取不到码，got err=%v", err)
	}
	// 一次性消费后取不到
	if err := r.ConsumeVerifyCode(ctx, vc.ID); err != nil {
		t.Fatalf("消费: %v", err)
	}
	if _, err := r.GetValidVerifyCode(ctx, VerifyPurposeReset, "a@test.com"); !IsNotFound(err) {
		t.Fatalf("消费后应取不到码，got err=%v", err)
	}

	// 失败 5 次作废
	if err := r.InsertVerifyCode(ctx, mk("fail")); err != nil {
		t.Fatal(err)
	}
	vc, _ = r.GetValidVerifyCode(ctx, VerifyPurposeReset, "a@test.com")
	attempts := vc.Attempts
	for i := 1; i <= VerifyCodeMaxFails; i++ {
		left, err := r.FailVerifyCode(ctx, vc.ID, attempts)
		if err != nil {
			t.Fatalf("失败计数 #%d: %v", i, err)
		}
		if want := VerifyCodeMaxFails - i; left != want {
			t.Fatalf("#%d 剩余次数应 %d，got %d", i, want, left)
		}
		attempts++
	}
	if _, err := r.GetValidVerifyCode(ctx, VerifyPurposeReset, "a@test.com"); !IsNotFound(err) {
		t.Fatalf("失败满 5 次应作废，got err=%v", err)
	}
}

func TestVerifyCodeRateQuery(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 直接写集合造历史：InsertVerifyCode 自带「新码作废同目标旧码」语义，
	// 频控历史需要多条并存
	mkAt := func(target, ip string, at time.Time) {
		id, err := r.nextSeq(ctx, "verifyCode")
		if err != nil {
			t.Fatalf("取号: %v", err)
		}
		if _, err := r.db.Collection("verifyCodes").InsertOne(ctx, bson.M{
			"_id": id, "purpose": VerifyPurposeReset, "target": target, "codeHash": "h", "ip": ip,
			"createdAt": at, "expiresAt": at.Add(VerifyCodeTTL), "purgeAt": at.Add(VerifyCodePurgeTTL),
		}); err != nil {
			t.Fatalf("插入: %v", err)
		}
	}
	now := time.Now()
	mkAt("rate@test.com", "9.9.9.9", now.Add(-2*time.Minute)) // 本小时第 1 条（也是最近一条）
	mkAt("rate@test.com", "9.9.9.9", now.Add(-2*time.Hour))   // 24h 内、1h 外
	mkAt("rate@test.com", "9.9.9.9", now.Add(-30*time.Hour))  // 24h 外
	mkAt("other@test.com", "9.9.9.9", now.Add(-time.Minute))  // 同 IP 其他目标

	q, err := r.QueryVerifyCodeRate(ctx, VerifyPurposeReset, "rate@test.com", "9.9.9.9")
	if err != nil {
		t.Fatalf("频控查询: %v", err)
	}
	if since := time.Since(q.LastAt); since < time.Minute || since > 3*time.Minute {
		t.Fatalf("LastAt 应为最近一条（约 2 分钟前），got %v", since)
	}
	if q.Target1h != 1 {
		t.Fatalf("目标 1h 计数应 1，got %d", q.Target1h)
	}
	if q.Target1d != 2 {
		t.Fatalf("目标 24h 计数应 2，got %d", q.Target1d)
	}
	if q.IP1d < 3 { // 同 IP 当日至少 3 条（不含 30h 前那条）
		t.Fatalf("IP 当日计数应 ≥3，got %d", q.IP1d)
	}
	if q.Global1d < q.IP1d {
		t.Fatalf("全局计数 %d 不应小于 IP 计数 %d", q.Global1d, q.IP1d)
	}
}
