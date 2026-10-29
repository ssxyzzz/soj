package repo

// 题解每人每题一篇（2026-10-26）单测：应用层检查、历史重复清理、唯一索引兜底并发。
// 依赖本地 MongoDB（docker compose up -d mongo），不可达时整文件跳过。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestSolutionOnePerProblem(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	pid, uid, otherUID := int64(9001), int64(42), int64(43)
	now := time.Now()
	insert := func(uid int64, content string, at time.Time) {
		t.Helper()
		if err := r.InsertSolution(ctx, &Solution{PID: pid, UID: uid, Content: content, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatalf("插入题解: %v", err)
		}
	}

	// ① 应用层检查：未发布 = nil
	mine, err := r.GetUserSolution(ctx, pid, uid)
	if err != nil {
		t.Fatalf("GetUserSolution: %v", err)
	}
	if mine != nil {
		t.Fatalf("初始不应有题解: %+v", mine)
	}

	// ② 造历史重复（无索引阶段同题同人两篇）+ 他人一篇不受影响
	insert(uid, "旧版第一篇", now)
	insert(uid, "旧版第二篇", now.Add(time.Minute))
	insert(otherUID, "他人题解", now)
	mine, err = r.GetUserSolution(ctx, pid, uid)
	if err != nil || mine == nil || mine.Content != "旧版第一篇" {
		t.Fatalf("应取到最早一篇: %v %+v", err, mine)
	}
	other, err := r.GetUserSolution(ctx, pid, otherUID)
	if err != nil || other == nil {
		t.Fatalf("他人题解应存在: %v", err)
	}

	// ③ 历史重复清理：保留最早一篇，删 1 篇；他人不受影响
	removed, err := r.EnsureSolutionUnique(ctx)
	if err != nil {
		t.Fatalf("EnsureSolutionUnique: %v", err)
	}
	if removed != 1 {
		t.Fatalf("应清理 1 篇重复，实际 %d", removed)
	}
	total, err := r.solutions.CountDocuments(ctx, bson.M{"pid": pid})
	if err != nil || total != 2 {
		t.Fatalf("清理后应剩 2 篇（自己 1 + 他人 1）: %v %d", err, total)
	}
	mine, _ = r.GetUserSolution(ctx, pid, uid)
	if mine == nil || mine.Content != "旧版第一篇" {
		t.Fatalf("应保留最早一篇: %+v", mine)
	}

	// ④ 唯一索引兜底：绕过应用层直接插重复 → 拒绝
	err = r.InsertSolution(ctx, &Solution{PID: pid, UID: uid, Content: "并发插入", CreatedAt: now, UpdatedAt: now})
	if err == nil || !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("重复插入应被唯一索引拒绝: %v", err)
	}
}
