package repo

// 连击/坚持计数器单测（v0.45 跨日口径）：断更清零、WA 清零、Cancelled 不清、
// 同题重复 AC 不涨、跨日续击不清。依赖本地 MongoDB（openTestRepo 同款）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// gameSeedUser 建（或重置为指定计数器态）一个用户文档。
func gameSeedUser(t *testing.T, r *Repo, uid int64, set bson.M) {
	t.Helper()
	if set == nil {
		set = bson.M{}
	}
	set["uname"] = "u"
	if _, err := r.users.UpdateOne(context.Background(), bson.M{"_id": uid},
		bson.M{"$set": set}, options.UpdateOne().SetUpsert(true)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// gameSetLastAcDay 直改 lastAcDay（模拟跨日，不真等时钟）。
func gameSetLastAcDay(t *testing.T, r *Repo, uid int64, day string) {
	t.Helper()
	if _, err := r.users.UpdateOne(context.Background(), bson.M{"_id": uid},
		bson.M{"$set": bson.M{"lastAcDay": day}}); err != nil {
		t.Fatalf("set lastAcDay: %v", err)
	}
}

func TestTrackJudgeOutcomeCombo(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	uid, p1, p2 := int64(71), int64(101), int64(102)
	gameSeedUser(t, r, uid, nil)

	// 首次 AC：连击 1
	st, err := r.TrackJudgeOutcome(ctx, uid, p1, true, false)
	if err != nil || st.ComboCur != 1 {
		t.Fatalf("首次 AC combo = %d err=%v，期望 1", st.ComboCur, err)
	}
	// 异题续击：2
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p2, true, false); st.ComboCur != 2 {
		t.Fatalf("异题续击 combo = %d，期望 2", st.ComboCur)
	}
	// 交替异题正常累计；同题连续重交：不涨（防刷口径——只挡连续重交同一题）
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p1, true, false); st.ComboCur != 3 {
		t.Fatalf("交替异题 combo = %d，期望 3", st.ComboCur)
	}
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p1, true, false); st.ComboCur != 3 {
		t.Fatalf("同题连续重交 combo = %d，期望不涨仍 3", st.ComboCur)
	}
	// WA：清零
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p1, false, false); st.ComboCur != 0 {
		t.Fatalf("WA 后 combo = %d，期望 0", st.ComboCur)
	}
	// Cancelled：不清零（先攒 1 再取消）
	r.TrackJudgeOutcome(ctx, uid, p1, true, false)
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p2, false, true); st.ComboCur != 1 {
		t.Fatalf("Cancelled 不应清零，combo = %d 期望 1", st.ComboCur)
	}

	// 跨日续击（lastAcDay=昨天）：不清零继续累计（异题 +1）
	gameSetLastAcDay(t, r, uid, beijingDay(time.Now().AddDate(0, 0, -1)))
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p2, true, false); st.ComboCur != 2 {
		t.Fatalf("跨日续击 combo = %d，期望 2（昨日有 AC 不断）", st.ComboCur)
	}
	// 断更（lastAcDay=前天）：异题 AC 从 1 重新起算
	gameSetLastAcDay(t, r, uid, beijingDay(time.Now().AddDate(0, 0, -2)))
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p1, true, false); st.ComboCur != 1 {
		t.Fatalf("断更后续击 combo = %d，期望 1（重新起算）", st.ComboCur)
	}
	// 同日同题重交：不涨不清
	if st, _ = r.TrackJudgeOutcome(ctx, uid, p1, true, false); st.ComboCur != 1 {
		t.Fatalf("同日同题重交 combo = %d，期望 1（同题不涨不清）", st.ComboCur)
	}

	// 坚持榜口径回归：断更重置为 1、跨日 +1、同日不变
	gameSeedUser(t, r, uid, bson.M{"streakDays": 5, "streakBest": 5})
	gameSetLastAcDay(t, r, uid, beijingDay(time.Now().AddDate(0, 0, -2)))
	if st, _ := r.TrackJudgeOutcome(ctx, uid, p1, true, false); st.StreakDays != 1 {
		t.Fatalf("断更后 streak = %d，期望 1", st.StreakDays)
	}
	gameSeedUser(t, r, uid, bson.M{"streakDays": 5, "streakBest": 5})
	gameSetLastAcDay(t, r, uid, beijingDay(time.Now().AddDate(0, 0, -1)))
	if st, _ := r.TrackJudgeOutcome(ctx, uid, p2, true, false); st.StreakDays != 6 {
		t.Fatalf("跨日续更 streak = %d，期望 6", st.StreakDays)
	}
	if st, _ := r.TrackJudgeOutcome(ctx, uid, p1, true, false); st.StreakDays != 6 {
		t.Fatalf("同日再 AC streak = %d，期望不变 6", st.StreakDays)
	}
}
