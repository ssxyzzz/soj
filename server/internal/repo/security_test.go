package repo

// 登录失败锁定与安全审计日志的回归测试（v0.14 验收发现两处线上 bug 后补：
// ① 更新管道 $$NOW 不可用导致锁定写入必败；② SecurityLog._id 零值显式落库，
// 第二条起全部重复键）。复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRecordLoginFailureLockout(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	uid := int64(990001)
	if _, err := r.Users.InsertOne(ctx, bson.M{
		"_id": uid, "uname": "lock_test", "pwdHash": "x", "nickname": "锁定测试",
		"role": RoleDefault, "createdAt": time.Now(),
	}); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	for i := 1; i < 5; i++ { // 前 4 次：计数递增，不锁定
		count, locked, err := r.RecordLoginFailure(ctx, uid, 5, 15*time.Minute)
		if err != nil {
			t.Fatalf("第 %d 次 RecordLoginFailure: %v", i, err)
		}
		if locked {
			t.Fatalf("第 %d 次不应锁定", i)
		}
		if count != i {
			t.Fatalf("第 %d 次返回计数 %d，应等于失败次数", i, count)
		}
	}
	// 第 5 次：达到阈值 → 锁定，计数归零（提示文案据此切换为锁定提示）
	count, locked, err := r.RecordLoginFailure(ctx, uid, 5, 15*time.Minute)
	if err != nil {
		t.Fatalf("第 5 次 RecordLoginFailure: %v", err)
	}
	if !locked {
		t.Fatal("第 5 次应触发锁定")
	}
	if count != 0 {
		t.Fatalf("锁定后计数应归零，实际 %d", count)
	}
	u, err := r.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !u.LockedUntil.After(time.Now()) {
		t.Fatalf("lockedUntil 应在未来，实际 %v", u.LockedUntil)
	}
	if u.FailedCount != 0 {
		t.Fatalf("锁定后计数应归零，实际 %d", u.FailedCount)
	}
	// 清零恢复
	if err := r.ClearLoginFailures(ctx, uid); err != nil {
		t.Fatalf("ClearLoginFailures: %v", err)
	}
	if u, err = r.GetUserByID(ctx, uid); err != nil || u.LockedUntil.After(time.Now()) {
		t.Fatalf("清零后不应处于锁定: %v %v", u.LockedUntil, err)
	}
}

func TestAddSecurityLogUniqueIDs(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := r.AddSecurityLog(ctx, SecurityLog{Type: SecLoginFailure, UID: int64(i), Username: "u", IP: "1.2.3.4"}); err != nil {
			t.Fatalf("第 %d 条审计写入: %v（曾因 _id 全零重复键必败）", i+1, err)
		}
	}
	list, total, err := r.ListSecurityLogs(ctx, 1, 10, SecLoginFailure, "")
	if err != nil {
		t.Fatalf("ListSecurityLogs: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("期望 3 条，实际 total=%d len=%d", total, len(list))
	}
}
