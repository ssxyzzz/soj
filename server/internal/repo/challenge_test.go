package repo

// 挑战模式（v0.23）单测：生效时长推导与可挑战资格（纯函数）；
// 会话状态机与榜单聚合复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestEffChallengeSec(t *testing.T) {
	cases := []struct {
		name string
		p    Problem
		want int
	}{
		{"未配置按难度D1=5min", Problem{Difficulty: 1}, 300},
		{"未配置按难度D10=50min", Problem{Difficulty: 10}, 3000},
		{"未配置难度兜底D3=15min", Problem{}, 900},
		{"显式覆盖生效", Problem{Difficulty: 3, ChallengeTimeSec: 120}, 120},
		{"低于下限回落默认", Problem{Difficulty: 3, ChallengeTimeSec: 30}, 900},
		{"高于上限回落默认", Problem{Difficulty: 3, ChallengeTimeSec: 20000}, 900},
	}
	for _, c := range cases {
		if got := c.p.EffChallengeSec(); got != c.want {
			t.Errorf("%s: EffChallengeSec() = %d, 期望 %d", c.name, got, c.want)
		}
	}
}

func TestChallengeEligible(t *testing.T) {
	stdio := Problem{Type: "", FileIO: false}
	if !stdio.ChallengeEligible() {
		t.Error("传统 stdio 题应可挑战")
	}
	ineligible := []Problem{
		{Type: TypeObjective, Questions: []ObjectiveQuestion{{Stem: "1+1="}}},
		{Type: TypeFunction},
		{Type: TypeInteractive},
		{Type: "", FileIO: true},
	}
	for _, p := range ineligible {
		if p.ChallengeEligible() {
			t.Errorf("题型 %q FileIO=%v 不应可挑战", p.Type, p.FileIO)
		}
	}
}

func TestChallengeSessionFlow(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	uid, pid := int64(9101), int64(9102)
	p := &Problem{ID: pid, Title: "挑战测试题", Difficulty: 3}

	c1, err := r.StartChallenge(ctx, uid, p)
	if err != nil {
		t.Fatalf("StartChallenge: %v", err)
	}
	if c1.Status != ChallengeRunning || c1.DeadlineAt.Sub(c1.StartedAt) != 15*time.Minute {
		t.Fatalf("会话初始态/时长不符: %+v", c1)
	}
	// 重复开挑战：旧会话顶为 quit，新会话 running
	c2, err := r.StartChallenge(ctx, uid, p)
	if err != nil {
		t.Fatalf("StartChallenge #2: %v", err)
	}
	var old Challenge
	if err := r.challenges.FindOne(ctx, bson.M{"_id": c1.ID}).Decode(&old); err != nil {
		t.Fatalf("读旧会话: %v", err)
	}
	if old.Status != ChallengeQuit {
		t.Fatalf("旧会话应被顶为 quit，实际 %s", old.Status)
	}
	// 提交绑定 → AC 终判（createdAt 在 deadline 前）
	ok, err := r.ChallengeRunningOK(ctx, c2.ID, uid, pid)
	if err != nil || !ok {
		t.Fatalf("ChallengeRunningOK = %v, %v", ok, err)
	}
	sid := int64(9103)
	doc := bson.M{
		"_id": sid, "uid": uid, "uname": "challenger", "pid": pid, "ptitle": p.Title,
		"lang": "cpp", "code": "int main(){}", "status": StatusAccepted,
		"score": 100, "timeMs": 1, "memKb": 1024, "createdAt": time.Now(), "published": true,
	}
	if _, err := r.submissions.InsertOne(ctx, doc); err != nil {
		t.Fatalf("造提交: %v", err)
	}
	if _, err := r.AttachChallengeSubmission(ctx, c2.ID, sid, "cpp", 100, nil); err != nil {
		t.Fatalf("AttachChallengeSubmission: %v", err)
	}
	r.FinalizeChallengeOnAC(ctx, uid, pid, sid, 100)
	var fin Challenge
	if err := r.challenges.FindOne(ctx, bson.M{"_id": c2.ID}).Decode(&fin); err != nil {
		t.Fatalf("读终态: %v", err)
	}
	if fin.Status != ChallengeAC || fin.DurationMS < 0 || fin.CharsPerSec <= 0 {
		t.Fatalf("AC 终判不符: %+v", fin)
	}
	// 榜单：最短用时去重
	rows, err := r.ChallengeRank(ctx, pid, 50)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ChallengeRank = %v, %v", rows, err)
	}
	// 主动退出：开新会话再退（上一场已被 AC 终判为终态）
	if _, err := r.StartChallenge(ctx, uid, p); err != nil {
		t.Fatalf("StartChallenge #3: %v", err)
	}
	quit, err := r.QuitChallenge(ctx, uid, pid)
	if err != nil || !quit {
		t.Fatalf("QuitChallenge = %v, %v", quit, err)
	}
	if active, err := r.ActiveChallenge(ctx, uid, pid); err != nil || active != nil {
		t.Fatalf("退出后仍有活跃会话: %v, %v", active, err)
	}
}
