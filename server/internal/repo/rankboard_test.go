package repo

// 排行榜快照取数 + 「各难度 AC 题数」两步法（v0.100）：与旧实现同库对照等价、已删题
// 不计、隐藏期提交不计、按题去重、上限取前 N 人；依赖本地 MongoDB（openTestRepo 同款）。

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// seedRankProblem 落一道最小题目文档（只要 _id 与难度；doc 追加/覆盖字段）。
func seedRankProblem(t *testing.T, r *Repo, pid int64, doc bson.M) {
	t.Helper()
	if doc == nil {
		doc = bson.M{}
	}
	doc["_id"] = pid
	if _, err := r.problems.InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("seed problem %d: %v", pid, err)
	}
}

// legacySolvedDifficultyCounts 旧实现（单管道逐行 $lookup 题目表）原样留在测试里，作
// 两步法的对照口径：字段缺失按 D3，难度 0/越界落不进任何档。
func legacySolvedDifficultyCounts(t *testing.T, r *Repo, uids []int64) map[int64][10]int64 {
	t.Helper()
	ctx := context.Background()
	out := map[int64][10]int64{}
	if len(uids) == 0 {
		return out
	}
	sum := func(d int) bson.M {
		return bson.M{"$sum": bson.M{"$cond": bson.A{
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$p.difficulty", 3}}, d}}, 1, 0}}}
	}
	group := bson.M{"_id": "$_id.uid"}
	for d := 1; d <= 10; d++ {
		group[fmt.Sprintf("c%d", d)] = sum(d)
	}
	cursor, err := r.submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"uid": bson.M{"$in": uids}, "status": StatusAccepted, "published": true}},
		{"$group": bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}},
		{"$lookup": bson.M{
			"from":         "problems",
			"localField":   "_id.pid",
			"foreignField": "_id",
			"pipeline":     bson.A{bson.M{"$project": bson.M{"difficulty": 1}}},
			"as":           "p",
		}},
		{"$unwind": "$p"},
		{"$group": group},
	})
	if err != nil {
		t.Fatalf("legacy aggregate: %v", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID  int64 `bson:"_id"`
			C1  int64 `bson:"c1"`
			C2  int64 `bson:"c2"`
			C3  int64 `bson:"c3"`
			C4  int64 `bson:"c4"`
			C5  int64 `bson:"c5"`
			C6  int64 `bson:"c6"`
			C7  int64 `bson:"c7"`
			C8  int64 `bson:"c8"`
			C9  int64 `bson:"c9"`
			C10 int64 `bson:"c10"`
		}
		if err := cursor.Decode(&row); err != nil {
			t.Fatalf("legacy decode: %v", err)
		}
		out[row.ID] = [10]int64{row.C1, row.C2, row.C3, row.C4, row.C5, row.C6, row.C7, row.C8, row.C9, row.C10}
	}
	if err := cursor.Err(); err != nil {
		t.Fatalf("legacy cursor: %v", err)
	}
	return out
}

type rankSeedSub struct {
	uid, pid   int64
	status     string
	published  bool
}

func seedRankSubs(t *testing.T, r *Repo, subs []rankSeedSub) {
	t.Helper()
	for _, s := range subs {
		insert(t, r, bson.M{"uid": s.uid, "pid": s.pid, "status": s.status, "published": s.published})
	}
}

// 两步法口径：按题去重、已删题不计、隐藏期（未发布）提交不计、非 AC 不计、难度缺省
// 按 D3；有效 AC 为零的用户不入表；uids 子集只算子集；与旧实现同库逐用户逐档相等。
func TestSolvedDifficultyCountsTwoStep(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	seedRankProblem(t, r, 1, bson.M{"difficulty": 1})
	seedRankProblem(t, r, 2, bson.M{"difficulty": 2})
	seedRankProblem(t, r, 3, bson.M{"difficulty": 10})
	seedRankProblem(t, r, 4, bson.M{"title": "无难度字段"})
	// pid 999：题目已删除（无文档）
	seedRankSubs(t, r, []rankSeedSub{
		{11, 1, StatusAccepted, true},
		{11, 1, StatusAccepted, true}, // 同题重复 AC：只算一次
		{11, 2, StatusAccepted, true},
		{11, 999, StatusAccepted, true}, // 已删题
		{11, 3, StatusWrongAnswer, true},
		{11, 4, StatusAccepted, true}, // 难度缺省 → D3
		{12, 3, StatusAccepted, true},
		{12, 1, StatusAccepted, false}, // 隐藏期提交
		{12, 2, StatusCancelled, true},
		{13, 999, StatusAccepted, true}, // 只有已删题的 AC
	})
	uids := []int64{11, 12, 13, 14}
	got, err := r.SolvedDifficultyCounts(ctx, uids)
	if err != nil {
		t.Fatalf("SolvedDifficultyCounts: %v", err)
	}
	want := map[int64][10]int64{
		11: {1, 1, 1},
		12: {0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("两步法结果 = %v, want %v", got, want)
	}
	if legacy := legacySolvedDifficultyCounts(t, r, uids); !reflect.DeepEqual(got, legacy) {
		t.Fatalf("两步法与旧实现不等价：\n got=%v\nwant=%v", got, legacy)
	}

	sub, err := r.SolvedDifficultyCounts(ctx, []int64{12})
	if err != nil || !reflect.DeepEqual(sub, map[int64][10]int64{12: {0, 0, 0, 0, 0, 0, 0, 0, 0, 1}}) {
		t.Fatalf("子集 = %v err=%v", sub, err)
	}
	if empty, err := r.SolvedDifficultyCounts(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("空入参 = %v err=%v", empty, err)
	}
}

// 难度 0 / 越界（存量未设置）按 D3 计——与 EffDifficulty 同规则；旧管道会让它落不进任何档。
func TestSolvedDifficultyCountsZeroDifficulty(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	seedRankProblem(t, r, 1, bson.M{"difficulty": 0})
	seedRankProblem(t, r, 2, bson.M{"difficulty": 11})
	seedRankSubs(t, r, []rankSeedSub{
		{21, 1, StatusAccepted, true},
		{21, 2, StatusAccepted, true},
	})
	got, err := r.SolvedDifficultyCounts(ctx, []int64{21})
	if err != nil {
		t.Fatalf("SolvedDifficultyCounts: %v", err)
	}
	want := map[int64][10]int64{21: {0, 0, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("难度 0/越界 = %v, want %v（两题都按 D3）", got, want)
	}
}

// 合榜快照取数：解题数 = 题集 ∩ 现存题、各难度计数、连击/坚持/上次 AC 日随行、账号不存在
// 的行用户名为空、现存题为零的用户不出榜、行按 uid 升序；limit 取题集最大的前 N 人。
func TestRankBoard(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	seedRankProblem(t, r, 1, bson.M{"difficulty": 1})
	seedRankProblem(t, r, 2, bson.M{"difficulty": 5})
	seedRankProblem(t, r, 3, bson.M{"difficulty": 3})
	// pid 9：已删题（无文档）
	today := beijingDay(time.Now())
	gameSeedUser(t, r, 21, bson.M{"nickname": "n", "comboCur": 3, "comboBest": 7,
		"streakDays": 4, "streakBest": 9, "lastAcDay": today})
	gameSeedUser(t, r, 22, bson.M{"nickname": ""})
	// uid 23：有 AC 但账号文档不存在；uid 24：只 AC 过已删题
	seedRankSubs(t, r, []rankSeedSub{
		{21, 1, StatusAccepted, true},
		{21, 1, StatusAccepted, true}, // 重复 AC
		{21, 2, StatusAccepted, true},
		{21, 9, StatusAccepted, true}, // 已删题
		{21, 3, StatusWrongAnswer, true},
		{22, 3, StatusAccepted, true},
		{22, 1, StatusAccepted, false}, // 隐藏期
		{23, 2, StatusAccepted, true},
		{24, 9, StatusAccepted, true}, // 现存题为零 → 不出榜
	})
	rows, err := r.RankBoard(ctx, 0)
	if err != nil {
		t.Fatalf("RankBoard: %v", err)
	}
	want := []RankRow{
		{UID: 21, Solved: 2, Counts: [10]int64{1, 0, 0, 0, 1}, Username: "u", Nickname: "n",
			ComboCur: 3, ComboBest: 7, StreakDays: 4, StreakBest: 9, LastAcDay: today},
		{UID: 22, Solved: 1, Counts: [10]int64{0, 0, 1}, Username: "u"},
		{UID: 23, Solved: 1, Counts: [10]int64{0, 0, 0, 0, 1}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("RankBoard = %+v\nwant %+v", rows, want)
	}

	top, err := r.RankBoard(ctx, 2)
	if err != nil || len(top) != 2 || top[0].UID != 21 || top[1].UID != 22 {
		t.Fatalf("RankBoard(limit=2) = %+v err=%v, want uid [21 22]", top, err)
	}
}
