package repo

// 用户公开主页（v0.17）聚合单测：首次 AC 去重与口径、统计分母排除作废件、
// 比赛历史只含 contest 且按开赛倒序。依赖本地 MongoDB，不可达时整文件跳过。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func ptrTime(t time.Time) *time.Time { return &t }

func TestUserFirstAccepted(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	// 基准截到毫秒：BSON 日期只有 ms 精度，否则 Equal 对不上纳秒尾数
	base := time.Now().Add(-48 * time.Hour).Truncate(time.Millisecond)

	// 两次 AC 同题：首次 AC 取较早一条（即便其提交号更大——按时间而非提交号）
	insert(t, r, bson.M{"_id": int64(11), "uid": int64(7), "pid": int64(1),
		"status": StatusAccepted, "score": 100, "published": true,
		"createdAt": base.Add(time.Hour), "judgedAt": ptrTime(base.Add(time.Hour))})
	insert(t, r, bson.M{"_id": int64(10), "uid": int64(7), "pid": int64(1),
		"status": StatusAccepted, "score": 100, "published": true,
		"createdAt": base.Add(2 * time.Hour), "judgedAt": ptrTime(base.Add(2 * time.Hour))})
	// 未发布（隐藏期内比赛提交）：不计
	insert(t, r, bson.M{"_id": int64(12), "uid": int64(7), "pid": int64(2),
		"status": StatusAccepted, "score": 100, "published": false,
		"createdAt": base, "judgedAt": ptrTime(base)})
	// 非 AC：不计
	insert(t, r, bson.M{"_id": int64(13), "uid": int64(7), "pid": int64(3),
		"status": StatusWrongAnswer, "score": 60, "published": true, "createdAt": base})
	// 隐藏题的 AC：下发（hidden=true），由 handler 按观察者权限剔除
	insert(t, r, bson.M{"_id": int64(14), "uid": int64(7), "pid": int64(4),
		"status": StatusAccepted, "score": 100, "published": true, "createdAt": base,
		"judgedAt": ptrTime(base)})
	// 题目已删除：AC 不计
	insert(t, r, bson.M{"_id": int64(15), "uid": int64(7), "pid": int64(5),
		"status": StatusAccepted, "score": 100, "published": true, "createdAt": base,
		"judgedAt": ptrTime(base)})
	// 他人提交：不串
	insert(t, r, bson.M{"_id": int64(16), "uid": int64(8), "pid": int64(6),
		"status": StatusAccepted, "score": 100, "published": true, "createdAt": base,
		"judgedAt": ptrTime(base)})

	for _, p := range []*Problem{
		{ID: 1, Title: "甲", Difficulty: 3},
		{ID: 4, Title: "丁", Difficulty: 7, Hidden: true},
	} {
		if _, err := r.problems.InsertOne(ctx, p); err != nil {
			t.Fatalf("插入题目: %v", err)
		}
	}

	list, err := r.UserFirstAccepted(ctx, 7)
	if err != nil {
		t.Fatalf("UserFirstAccepted: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应恰 2 条（去重 + 剔隐藏期/已删题）: %+v", list)
	}
	// 倒序（最新在前）：pid1（base+1h）在前，pid4（base）在后
	if list[0].PID != 1 || list[1].PID != 4 {
		t.Fatalf("排序错误: %+v", list)
	}
	if !list[1].Hidden || list[1].Difficulty != 7 {
		t.Fatalf("hidden/difficulty 下发错误: %+v", list[1])
	}
	if !list[0].FirstACAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("首次 AC 应取较早时刻: %+v", list[0].FirstACAt)
	}
}

func TestUserSubmissionStats(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)

	// AC×2（其中 1 条隐藏期）、WA×1、Cancelled×1（作废件，出分母）
	insert(t, r, bson.M{"_id": int64(21), "uid": int64(9), "pid": int64(1),
		"status": StatusAccepted, "score": 100, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(22), "uid": int64(9), "pid": int64(2),
		"status": StatusAccepted, "score": 100, "published": false, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(23), "uid": int64(9), "pid": int64(3),
		"status": StatusWrongAnswer, "score": 40, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(24), "uid": int64(9), "pid": int64(4),
		"status": StatusCancelled, "score": 0, "published": true, "createdAt": base})

	total, accepted, err := r.UserSubmissionStats(ctx, 9)
	if err != nil {
		t.Fatalf("UserSubmissionStats: %v", err)
	}
	if total != 2 { // AC + WA；隐藏期 AC 与 Cancelled 都不算
		t.Fatalf("total = %d, want 2", total)
	}
	if accepted != 1 { // 隐藏期内 AC 不计公开 AC
		t.Fatalf("accepted = %d, want 1", accepted)
	}
}

func TestUserContests(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 两场 contest + 一场 exam（examId 不串 contestId）；未提交的比赛不出
	old := &Contest{Title: "老赛", Start: time.Now().Add(-72 * time.Hour), End: time.Now().Add(-48 * time.Hour), Published: true}
	recent := &Contest{Title: "新赛", Start: time.Now().Add(-24 * time.Hour), End: time.Now().Add(-time.Hour), Published: true}
	for _, ct := range []*Contest{old, recent} {
		if err := r.CreateRound(ctx, KindContest, ct); err != nil {
			t.Fatalf("CreateRound: %v", err)
		}
	}
	insert(t, r, bson.M{"_id": int64(41), "uid": int64(11), "pid": int64(1), "contestId": old.ID,
		"status": StatusAccepted, "score": 100, "published": true, "createdAt": time.Now()})
	insert(t, r, bson.M{"_id": int64(42), "uid": int64(11), "pid": int64(1), "contestId": recent.ID,
		"status": StatusWrongAnswer, "score": 0, "published": true, "createdAt": time.Now()})
	insert(t, r, bson.M{"_id": int64(43), "uid": int64(11), "pid": int64(2), "examId": int64(33),
		"status": StatusAccepted, "score": 100, "published": true, "createdAt": time.Now()})

	rounds, err := r.UserContests(ctx, 11)
	if err != nil {
		t.Fatalf("UserContests: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("应恰 2 场比赛: %+v", rounds)
	}
	if rounds[0].ID != recent.ID || rounds[1].ID != old.ID {
		t.Fatalf("应按开赛时间倒序: %d, %d", rounds[0].ID, rounds[1].ID)
	}
}
