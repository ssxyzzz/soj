package repo

// 课堂 PK（v0.42）单测：发起互斥与取消、榜单去重排序、截止懒结算与冠军通知、
// 终判 raceMs 回写 + 前 3 名掉落补差与重测幂等。复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func seedClassMember(t *testing.T, r *Repo, classID, uid int64, uname string) {
	t.Helper()
	_, err := r.db.Collection("classmembers").InsertOne(context.Background(), bson.M{
		"classId": classID, "uid": uid, "uname": uname,
		"role": ClassRoleMember, "joinedAt": time.Now(),
	})
	if err != nil {
		t.Fatalf("seed classmember: %v", err)
	}
}

func TestClassroomRaceLifecycle(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	classID, hostUID, pid := int64(8801), int64(8802), int64(8803)
	seedClassMember(t, r, classID, hostUID, "host")

	rc, err := r.CreateClassroomRace(ctx, hostUID, "host", classID, pid, "PK 测试题", 120, 0, true)
	if err != nil {
		t.Fatalf("CreateClassroomRace: %v", err)
	}
	if rc.Status != RaceRunning || rc.Bonus != true {
		t.Fatalf("race 初始态不对: %+v", rc)
	}
	if delta := rc.DeadlineAt.Sub(rc.StartedAt); delta < 119*time.Second || delta > 121*time.Second {
		t.Fatalf("deadline 应为 startedAt+effSec，得 %v", delta)
	}
	// 同班互斥：第二场被拒
	if _, err := r.CreateClassroomRace(ctx, hostUID, "host", classID, pid, "另一场", 60, 0, false); err != ErrRaceActive {
		t.Fatalf("并发第二场应 ErrRaceActive，得 %v", err)
	}
	// 窗口反查（自动挂 raceId 的数据源）：成员 + 同题 + 窗口内
	got, err := r.ActiveRaceForUser(ctx, hostUID, pid)
	if err != nil || got == nil || got.ID != rc.ID {
		t.Fatalf("ActiveRaceForUser 应命中本场，得 %v / %v", got, err)
	}
	if _, err := r.ActiveRaceForUser(ctx, hostUID, 999999); err != nil || got != nil && got.PID == 999999 {
		// 异题无 race：只验证不 panic，nil 属正常
		_ = err
	}
	// 取消后窗口关闭、互斥解除
	if ok, err := r.CancelClassroomRace(ctx, classID, rc.ID); err != nil || !ok {
		t.Fatalf("CancelClassroomRace: %v / %v", ok, err)
	}
	if got, _ := r.ActiveRaceForUser(ctx, hostUID, pid); got != nil {
		t.Fatalf("取消后不应再命中 race")
	}
	if rc2, err := r.CreateClassroomRace(ctx, hostUID, "host", classID, pid+1, "第二场", 60, 0, false); err != nil {
		t.Fatalf("取消后应可再发起: %v", err)
	} else if _, err := r.CancelClassroomRace(ctx, classID, rc2.ID); err != nil {
		t.Fatalf("清场失败: %v", err)
	}
}

func TestClassroomRaceBoardAndSettle(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	classID, pid := int64(8811), int64(8812)
	for _, m := range []struct {
		uid int64
		un  string
	}{{8813, "甲"}, {8814, "乙"}, {8815, "丙"}} {
		seedClassMember(t, r, classID, m.uid, m.un)
	}
	rc, err := r.CreateClassroomRace(ctx, 8813, "甲", classID, pid, "榜单测试题", 60, 0, true)
	if err != nil {
		t.Fatalf("CreateClassroomRace: %v", err)
	}
	// 榜单数据：乙最快 2s；甲两次 AC 去重取最短 3s；丙 WA 不上榜
	seed := []bson.M{
		{"_id": int64(1), "uid": int64(8814), "uname": "乙", "pid": pid, "raceId": rc.ID, "status": StatusAccepted, "raceMs": int64(2000), "createdAt": time.Now().Add(-time.Minute)},
		{"_id": int64(2), "uid": int64(8813), "uname": "甲", "pid": pid, "raceId": rc.ID, "status": StatusAccepted, "raceMs": int64(5000), "createdAt": time.Now().Add(-time.Minute)},
		{"_id": int64(3), "uid": int64(8813), "uname": "甲", "pid": pid, "raceId": rc.ID, "status": StatusAccepted, "raceMs": int64(3000), "createdAt": time.Now().Add(-50 * time.Second)},
		{"_id": int64(4), "uid": int64(8815), "uname": "丙", "pid": pid, "raceId": rc.ID, "status": StatusWrongAnswer, "createdAt": time.Now().Add(-time.Minute)},
	}
	if _, err := r.submissions.InsertMany(ctx, toAnySlice(seed)); err != nil {
		t.Fatalf("seed submissions: %v", err)
	}
	board, err := r.RaceBoard(ctx, rc.ID, 50)
	if err != nil {
		t.Fatalf("RaceBoard: %v", err)
	}
	if len(board) != 2 || board[0].UID != 8814 || board[0].DurationMS != 2000 || board[1].UID != 8813 || board[1].DurationMS != 3000 {
		t.Fatalf("榜单去重/排序不对: %+v", board)
	}
	// 截止懒结算：deadline 拨到过去 → 读路径 CAS settled + 冠军固化 + 班级广播
	if _, err := r.classroomRaces.UpdateOne(ctx, bson.M{"_id": rc.ID},
		bson.M{"$set": bson.M{"deadlineAt": time.Now().Add(-time.Second)}}); err != nil {
		t.Fatalf("拨快截止: %v", err)
	}
	got, err := r.ActiveClassroomRace(ctx, classID)
	if err != nil || got != nil {
		t.Fatalf("过期 race 应结算并返回 nil，得 %v / %v", got, err)
	}
	var settled ClassroomRace
	if err := r.classroomRaces.FindOne(ctx, bson.M{"_id": rc.ID}).Decode(&settled); err != nil {
		t.Fatalf("回读 race: %v", err)
	}
	if settled.Status != RaceSettled || settled.WinnerUID != 8814 || settled.WinnerName != "乙" {
		t.Fatalf("结算结果不对: %+v", settled)
	}
	// 冠军广播：全班（含旁观成员）各一条 contest 通知；重复读不再重复结算/广播
	n, err := r.notifications.CountDocuments(ctx, bson.M{"typ": NotifyContest, "refId": rc.ID})
	if err != nil || n != 3 {
		t.Fatalf("应 3 条结算广播，得 %d / %v", n, err)
	}
	if _, err := r.ActiveClassroomRace(ctx, classID); err != nil {
		t.Fatalf("重复读: %v", err)
	}
	if n, _ := r.notifications.CountDocuments(ctx, bson.M{"typ": NotifyContest, "refId": rc.ID}); n != 3 {
		t.Fatalf("CAS 幂等：广播不应重复，得 %d", n)
	}
}

func TestFinalizeRaceOnAC(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	classID, uid, pid := int64(8821), int64(8822), int64(8823)
	seedClassMember(t, r, classID, uid, "选手")
	rc, err := r.CreateClassroomRace(ctx, uid, "选手", classID, pid, "终判测试题", 60, 0, true)
	if err != nil {
		t.Fatalf("CreateClassroomRace: %v", err)
	}
	sid, cid := int64(7001), int64(7002)
	now := time.Now()
	if _, err := r.challenges.InsertOne(ctx, bson.M{
		"_id": cid, "uid": uid, "pid": pid, "submissionId": sid,
		"startedAt": now.Add(-20 * time.Second), "deadlineAt": now.Add(time.Hour),
		"status": ChallengeAC, "durationMs": int64(12000), "finishedAt": now,
	}); err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	if _, err := r.submissions.InsertOne(ctx, bson.M{
		"_id": sid, "uid": uid, "uname": "选手", "pid": pid, "raceId": rc.ID,
		"status": StatusAccepted, "published": true, "createdAt": now.Add(-15 * time.Second),
	}); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
	sub := &Submission{ID: sid, UID: uid, ProblemID: pid, RaceID: rc.ID,
		Status: StatusAccepted, Published: true, CreatedAt: now.Add(-15 * time.Second)}
	// 首 AC：raceMs 回写 + 第 1 名倍率 200（v0.45 起并入发玉总则，不再发独立补差流水）
	if pct := r.ResolveRaceBonusMult(ctx, sub); pct != 200 {
		t.Fatalf("第 1 名倍率应为 200，得 %d", pct)
	}

	var stored struct {
		RaceMS int64 `bson:"raceMs"`
	}
	if err := r.submissions.FindOne(ctx, bson.M{"_id": sid}).Decode(&stored); err != nil {
		t.Fatalf("回读提交: %v", err)
	}
	if stored.RaceMS != 12000 {
		t.Fatalf("raceMs 应回写挑战终判用时 12000，得 %d", stored.RaceMS)
	}
	// 重测回写幂等：raceMs CAS 挡住二次解析
	if pct := r.ResolveRaceBonusMult(ctx, sub); pct != 0 {
		t.Fatalf("重测回写不应再次解析倍率，得 %d", pct)
	}
	// 同 uid 重复 AC（重交刷加成）：(race,uid) 一次性解析闸挡住
	sidX, cidX := int64(7010), int64(7011)
	if _, err := r.challenges.InsertOne(ctx, bson.M{
		"_id": cidX, "uid": uid, "pid": pid, "submissionId": sidX,
		"startedAt": now, "deadlineAt": now.Add(time.Hour),
		"status": ChallengeAC, "durationMs": int64(3000),
	}); err != nil {
		t.Fatalf("seed challengeX: %v", err)
	}
	if _, err := r.submissions.InsertOne(ctx, bson.M{
		"_id": sidX, "uid": uid, "uname": "选手", "pid": pid, "raceId": rc.ID,
		"status": StatusAccepted, "published": true, "createdAt": now.Add(-time.Second),
	}); err != nil {
		t.Fatalf("seed submissionX: %v", err)
	}
	if pct := r.ResolveRaceBonusMult(ctx, &Submission{ID: sidX, UID: uid, ProblemID: pid, RaceID: rc.ID,
		Status: StatusAccepted, Published: true, CreatedAt: now.Add(-time.Second)}); pct != 0 {
		t.Fatalf("同 uid 重交不应重复解析倍率，得 %d", pct)
	}
	// 超窗提交（race 截止后创建）：不入榜不出倍率
	sid2 := int64(7003)
	if _, err := r.submissions.InsertOne(ctx, bson.M{
		"_id": sid2, "uid": uid, "uname": "选手", "pid": pid, "raceId": rc.ID,
		"status": StatusAccepted, "published": true, "createdAt": time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed submission2: %v", err)
	}
	sub2 := &Submission{ID: sid2, UID: uid, ProblemID: pid, RaceID: rc.ID,
		Status: StatusAccepted, Published: true, CreatedAt: time.Now().Add(time.Hour)}
	if pct := r.ResolveRaceBonusMult(ctx, sub2); pct != 0 {
		t.Fatalf("超窗提交倍率应为 0，得 %d", pct)
	}
	if n, _ := r.submissions.CountDocuments(ctx, bson.M{"_id": sid2, "raceMs": bson.M{"$exists": true}}); n != 0 {
		t.Fatalf("超窗提交不应写 raceMs")
	}
	// 挑战终判非 ac（超时会话）：即便窗口内也不入榜
	sid3, cid3 := int64(7004), int64(7005)
	if _, err := r.challenges.InsertOne(ctx, bson.M{
		"_id": cid3, "uid": uid, "pid": pid + 1, "submissionId": sid3,
		"startedAt": now, "deadlineAt": now.Add(time.Hour), "status": ChallengeTimeout,
	}); err != nil {
		t.Fatalf("seed challenge3: %v", err)
	}
	if _, err := r.submissions.InsertOne(ctx, bson.M{
		"_id": sid3, "uid": uid, "uname": "选手", "pid": pid + 1, "raceId": rc.ID,
		"status": StatusAccepted, "published": true, "createdAt": now,
	}); err != nil {
		t.Fatalf("seed submission3: %v", err)
	}
	sub3 := &Submission{ID: sid3, UID: uid, ProblemID: pid + 1, RaceID: rc.ID,
		Status: StatusAccepted, Published: true, CreatedAt: now}
	if pct := r.ResolveRaceBonusMult(ctx, sub3); pct != 0 {
		t.Fatalf("挑战终判非 ac 倍率应为 0，得 %d", pct)
	}
	if n, _ := r.submissions.CountDocuments(ctx, bson.M{"_id": sid3, "raceMs": bson.M{"$exists": true}}); n != 0 {
		t.Fatalf("挑战终判非 ac 不应写 raceMs")
	}
}

func toAnySlice(ms []bson.M) []any {
	out := make([]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, m)
	}
	return out
}
