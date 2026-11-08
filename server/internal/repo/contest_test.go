package repo

// 比赛榜单 v0.82.6 单测：同总分逐题决胜链（末题 → 倒数第二题 → … → 最后得分提交
// 时间早者 → UID）。依赖本地 MongoDB（docker compose up -d mongo），不可达时整文件跳过。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestRoundLeaderboardTieBreak 场次两题 T1(101)/T2(102)：
// 丙 T1=50/T2=100、甲乙均 T1=100/T2=50、丁全 0 —— 三人总分同为 150，
// 末题 T2 丙 100 > 甲乙 50 丙居首；甲乙逐题全同比时间（甲最后得分更早居前）；丁无得分垫底。
func TestRoundLeaderboardTieBreak(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const roundID = int64(5001)
	t0 := time.Now().Add(-time.Hour)
	sc := func(pid int64, score int, mins int) bson.M {
		return bson.M{"contestId": roundID, "uid": int64(0), "pid": pid,
			"score": score, "status": StatusAccepted, "judgedAt": t0.Add(time.Duration(mins) * time.Minute)}
	}
	put := func(uid int64, uname string, pid int64, score int, mins int) {
		doc := sc(pid, score, mins)
		doc["uid"] = uid
		doc["uname"] = uname
		insert(t, r, doc)
	}
	// 甲(11)：T1@+10 出分 100、T2@+30 出分 50（最后得分 +30）
	put(11, "甲", 101, 100, 10)
	put(11, "甲", 102, 50, 30)
	// 乙(12)：与甲逐题同分，但最后得分 +60 更晚 → 排甲后
	put(12, "乙", 101, 100, 20)
	put(12, "乙", 102, 50, 60)
	// 丙(13)：末题 T2 满分 → 同总分中居首
	put(13, "丙", 101, 50, 15)
	put(13, "丙", 102, 100, 25)
	// 丁(14)：只交未得分 → 无得分排最后
	put(14, "丁", 101, 0, 5)

	rows, err := r.RoundLeaderboard(ctx, KindContest, roundID,
		map[int64]int{101: 100, 102: 100}, []int64{101, 102})
	if err != nil {
		t.Fatalf("RoundLeaderboard: %v", err)
	}
	wantOrder := []int64{13, 11, 12, 14}
	wantTotal := []int{150, 150, 150, 0}
	if len(rows) != len(wantOrder) {
		t.Fatalf("rows = %d, want %d", len(rows), len(wantOrder))
	}
	for i, row := range rows {
		if row.UID != wantOrder[i] || row.Total != wantTotal[i] {
			t.Errorf("row[%d] = (uid %d total %d), want (uid %d total %d)",
				i, row.UID, row.Total, wantOrder[i], wantTotal[i])
		}
	}
}
