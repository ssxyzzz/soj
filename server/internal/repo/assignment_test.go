package repo

// 作业成绩表 v0.82.6 单测：同总分并列跳号名次（行序仍 总分 → 通过题数 → UID）。
// 依赖本地 MongoDB（docker compose up -d mongo），不可达时整文件跳过。

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestAssignmentProgressRankTie 三题 T1-T3：21/22/23 总分同 200（并列跳号同 rank 1），
// 24 总分 100 → 跳号 rank 4，25 未做 → rank 5；组内按通过题数降序（23 一题通过排 21/22 后）。
func TestAssignmentProgressRankTie(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	a := &Assignment{ID: 9001, ClassID: 1, Title: "并列跳号", Problems: []int64{201, 202, 203}}
	sc := func(uid int64, pid int64, score int) bson.M {
		return bson.M{"uid": uid, "uname": "u", "pid": pid, "score": score,
			"status": StatusAccepted, "published": true}
	}
	put := func(uid int64, cells ...bson.M) {
		for _, c := range cells {
			insert(t, r, c)
		}
	}
	put(21, sc(21, 201, 100), sc(21, 202, 100))                 // 200 / 两题通过
	put(22, sc(22, 201, 100), sc(22, 202, 100), sc(22, 203, 0)) // 200 / 两题通过
	put(23, sc(23, 201, 100), sc(23, 202, 50), sc(23, 203, 50)) // 200 / 一题通过 → 组内排后
	put(24, sc(24, 201, 50), sc(24, 202, 50))                   // 100
	put(25, sc(25, 201, 0))                                     // 0

	rows, err := r.AssignmentProgress(ctx, a, []int64{21, 22, 23, 24, 25})
	if err != nil {
		t.Fatalf("AssignmentProgress: %v", err)
	}
	wantUID := []int64{21, 22, 23, 24, 25}
	wantRank := []int{1, 1, 1, 4, 5}
	wantTotal := []int{200, 200, 200, 100, 0}
	if len(rows) != len(wantUID) {
		t.Fatalf("rows = %d, want %d", len(rows), len(wantUID))
	}
	for i, row := range rows {
		if row.UID != wantUID[i] || row.Rank != wantRank[i] || row.Total != wantTotal[i] {
			t.Errorf("row[%d] = (uid %d rank %d total %d), want (uid %d rank %d total %d)",
				i, row.UID, row.Rank, row.Total, wantUID[i], wantRank[i], wantTotal[i])
		}
	}
}
