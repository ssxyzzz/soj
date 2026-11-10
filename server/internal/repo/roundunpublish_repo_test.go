package repo

// UnpublishRound（v0.87 回炉）契约：场次与该场全部提交一并翻回未公开；幂等（重复执行
// 无害）。无本地 Mongo 自动 skip。

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUnpublishRound(t *testing.T) {
	r := openTestRepo(t)
	defer r.Close(t.Context())
	ctx := t.Context()

	ct := &Contest{
		ClassID: 0, Title: "回炉验证场", CreatedBy: 1,
		Start: time.Now().Add(-2 * time.Hour), End: time.Now().Add(-time.Hour),
		Problems: []ContestProblem{{PID: 100, Score: 100}},
	}
	if err := r.CreateRound(ctx, KindContest, ct); err != nil {
		t.Fatalf("建赛: %v", err)
	}
	// 场内提交两条（创建态 published=false）+ 模拟发布（场次与提交全翻 true）
	p := &Problem{ID: 100, Title: "题", Difficulty: 3}
	if _, err := r.CreateSubmission(ctx, 7, "stu", p, "cpp", "int main(){}", ct, nil, 0); err != nil {
		t.Fatalf("建提交: %v", err)
	}
	if _, err := r.CreateSubmission(ctx, 7, "stu", p, "cpp", "int main(){};", ct, nil, 0); err != nil {
		t.Fatalf("建提交2: %v", err)
	}
	if _, err := r.submissions.UpdateMany(ctx, bson.M{"contestId": ct.ID},
		bson.M{"$set": bson.M{"published": true}}); err != nil {
		t.Fatalf("模拟发布提交: %v", err)
	}
	if _, err := r.roundsCol(KindContest).UpdateOne(ctx, bson.M{"_id": ct.ID},
		bson.M{"$set": bson.M{"published": true}}); err != nil {
		t.Fatalf("模拟发布场次: %v", err)
	}

	// 回炉：场次与提交全部翻回
	if err := r.UnpublishRound(ctx, KindContest, ct.ID); err != nil {
		t.Fatalf("回炉: %v", err)
	}
	after, err := r.GetRound(ctx, KindContest, ct.ID)
	if err != nil || after.Published {
		t.Fatalf("回炉后场次应未发布: published=%v err=%v", after.Published, err)
	}
	subs, _, err := r.ListSubmissions(ctx, SubmissionFilter{PID: 100}, 1, 50)
	if err != nil {
		t.Fatalf("读提交: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("应读到 2 条场内提交，得到 %d", len(subs))
	}
	for _, s := range subs {
		if s.Published {
			t.Fatalf("回炉后提交 %d 仍为公开态", s.ID)
		}
	}

	// 幂等：再跑一遍无变化无报错
	if err := r.UnpublishRound(ctx, KindContest, ct.ID); err != nil {
		t.Fatalf("幂等回炉: %v", err)
	}
}
