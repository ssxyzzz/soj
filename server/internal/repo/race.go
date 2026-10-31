package repo

// 课堂 PK（v0.42）：班级场景的全班同题手速挑战赛。教师（manageable）发起一场
// race——{classId, pid, 时长 = 题目 EffChallengeSec，可选发起倒计时}——班级成员在
// 窗口内从挑战链路作答（复用挑战会话计时/打字审计，提交自动挂 raceId），AC 记录
// 按用时升序即实时榜；截止后 CAS 懒结算并发班级通知。
//   - 入榜资格：race 窗口内创建的挑战提交且个人挑战终判为 ac（提交创建 ≤ race
//     deadline，判题排队不惩罚——与挑战终判同一口径）。
//   - 名次即 AC 回写时刻的即时名次（先到先得，无回溯）：前 3 名该题掉落额外
//     ×2/×1.5/×1.2（默认开、发起可关；掉落为 0 则加成落空——与每日一题 ×2 同构，
//     不发固定玉），以 JadeKindContestRank 补差一笔发放。
//   - 全班通知走 NotifyUsers（repo 层直发，jade 升维祝福同款先例）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// race 状态机：running →（截止懒结算）settled；发起者可中途 cancelled。
const (
	RaceRunning   = "running"
	RaceSettled   = "settled"
	RaceCancelled = "cancelled"
)

// ErrRaceActive 同班已有进行中的 race（发起互斥）。
var ErrRaceActive = errors.New("已有进行中的课堂 PK")

// ClassroomRace 一场课堂 PK。
type ClassroomRace struct {
	ID         int64     `bson:"_id"`
	ClassID    int64     `bson:"classId"`
	PID        int64     `bson:"pid"`
	PTitle     string    `bson:"ptitle"` // 冗余：榜单卡免查题
	HostUID    int64     `bson:"hostUid"`
	HostName   string    `bson:"hostName"`
	Bonus      bool      `bson:"bonus"` // 前 3 名掉落加成开关
	// BonusPaid 已发过名次加成的 uid 集（防同一人重复 AC 反复领取——榜面按 uid
	// 去重取最短，重交继承最佳名次，无此闸则每次 AC 都按最佳名次再发一笔）
	BonusPaid []int64 `bson:"bonusPaid,omitempty"`
	StartedAt  time.Time `bson:"startedAt"`
	DeadlineAt time.Time `bson:"deadlineAt"`
	Status     string    `bson:"status"`
	WinnerUID  int64     `bson:"winnerUid,omitempty"`
	WinnerName string    `bson:"winnerName,omitempty"`
	SettledAt  time.Time `bson:"settledAt,omitempty"`
	CreatedAt  time.Time `bson:"createdAt"`
}

// RaceRankRow race 榜行（published 挑战 AC 按 raceMs 升序、uid 去重取最短）。
type RaceRankRow struct {
	UID        int64     `bson:"uid"          json:"uid"`
	Username   string    `bson:"uname"        json:"uname"`
	DurationMS int64     `bson:"raceMs"       json:"duration_ms"`
	CreatedAt  time.Time `bson:"createdAt"    json:"at"` // 提交创建时刻
}

// CreateClassroomRace 发起一场 PK：同班同时仅一场进行中（并发双发起以唯一运行
// 状态索引兜底为 500 前的 ErrRaceActive——先查后插的毫秒窗口靠发起侧重试消化）。
// effSec = 题目 EffChallengeSec（handler 已取题，repo 不再回查）；startedAt =
// now + countdownSec（0 = 即开）；deadline = startedAt + effSec。
func (r *Repo) CreateClassroomRace(ctx context.Context, hostUID int64, hostName string, classID, pid int64, ptitle string, effSec, countdownSec int, bonus bool) (*ClassroomRace, error) {
	if countdownSec < 0 {
		countdownSec = 0
	}
	if countdownSec > 600 {
		countdownSec = 600 // 发起倒计时上限 10 分钟，防占坑挂机
	}
	r.sweepRaces(ctx, classID)
	n, err := r.classroomRaces.CountDocuments(ctx, bson.M{"classId": classID, "status": RaceRunning})
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrRaceActive
	}
	id, err := r.nextSeq(ctx, "race")
	if err != nil {
		return nil, err
	}
	started := time.Now().Add(time.Duration(countdownSec) * time.Second)
	rc := &ClassroomRace{
		ID: id, ClassID: classID, PID: pid, PTitle: ptitle,
		HostUID: hostUID, HostName: hostName, Bonus: bonus,
		StartedAt:  started,
		DeadlineAt: started.Add(time.Duration(effSec) * time.Second),
		Status:     RaceRunning, CreatedAt: time.Now(),
	}
	if _, err := r.classroomRaces.InsertOne(ctx, rc); err != nil {
		return nil, err
	}
	return rc, nil
}

// CancelClassroomRace 中途取消（发起者侧 manageable 校验在 handler）：running → cancelled。
func (r *Repo) CancelClassroomRace(ctx context.Context, classID, raceID int64) (bool, error) {
	res, err := r.classroomRaces.UpdateOne(ctx,
		bson.M{"_id": raceID, "classId": classID, "status": RaceRunning},
		bson.M{"$set": bson.M{"status": RaceCancelled}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// GetClassroomRace 按 id 读一场（不存在返回 nil）。
func (r *Repo) GetClassroomRace(ctx context.Context, id int64) (*ClassroomRace, error) {
	var rc ClassroomRace
	err := r.classroomRaces.FindOne(ctx, bson.M{"_id": id}).Decode(&rc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rc, nil
}

// ActiveClassroomRace 班级当前进行中的 race（含截止懒结算）；无则返回 nil。
func (r *Repo) ActiveClassroomRace(ctx context.Context, classID int64) (*ClassroomRace, error) {
	r.sweepRaces(ctx, classID)
	var rc ClassroomRace
	err := r.classroomRaces.FindOne(ctx,
		bson.M{"classId": classID, "status": RaceRunning},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&rc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rc, nil
}

// LatestClassroomRace 最近一场任意状态 race（结算名次表展示用）；无历史返回 nil。
func (r *Repo) LatestClassroomRace(ctx context.Context, classID int64) (*ClassroomRace, error) {
	var rc ClassroomRace
	err := r.classroomRaces.FindOne(ctx,
		bson.M{"classId": classID},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&rc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rc, nil
}

// userClassIDs 用户已加入的班级 id 集（classmembers 反查；参赛资格的数据口径）。
func (r *Repo) userClassIDs(ctx context.Context, uid int64) ([]int64, error) {
	cur, err := r.db.Collection("classmembers").Find(ctx,
		bson.M{"uid": uid}, options.Find().SetProjection(bson.M{"classId": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []int64
	for cur.Next(ctx) {
		var m struct {
			ClassID int64 `bson:"classId"`
		}
		if err := cur.Decode(&m); err != nil {
			return nil, err
		}
		out = append(out, m.ClassID)
	}
	return out, cur.Err()
}

// ActiveRaceForUser 用户在某题上的生效 race：本人所在班级 ∩ running ∩ 窗口内
// （startedAt ≤ now ≤ deadline），取最新一场。挑战提交创建时调用（自动挂 raceId）。
func (r *Repo) ActiveRaceForUser(ctx context.Context, uid, pid int64) (*ClassroomRace, error) {
	ids, err := r.userClassIDs(ctx, uid)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	now := time.Now()
	var rc ClassroomRace
	err = r.classroomRaces.FindOne(ctx, bson.M{
		"classId": bson.M{"$in": ids}, "pid": pid, "status": RaceRunning,
		"startedAt": bson.M{"$lte": now}, "deadlineAt": bson.M{"$gte": now},
	}, options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&rc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rc, nil
}

// RaceBoard race 实时榜：挂 raceId 的 AC 提交按 raceMs 升序、uid 去重取最短。
// 已截止时榜单即终版（有效提交以创建时刻 ≤ deadline 为准，写入时把关）。
func (r *Repo) RaceBoard(ctx context.Context, raceID int64, limit int) ([]RaceRankRow, error) {
	if limit <= 0 {
		limit = 50
	}
	cur, err := r.submissions.Find(ctx,
		bson.M{"raceId": raceID, "status": StatusAccepted},
		options.Find().
			SetSort(bson.D{{Key: "raceMs", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"uid": 1, "uname": 1, "raceMs": 1, "createdAt": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []RaceRankRow
	seen := map[int64]bool{}
	for cur.Next(ctx) {
		var s struct {
			UID       int64     `bson:"uid"`
			Username  string    `bson:"uname"`
			RaceMS    int64     `bson:"raceMs"`
			CreatedAt time.Time `bson:"createdAt"`
		}
		if err := cur.Decode(&s); err != nil {
			return nil, err
		}
		if s.RaceMS <= 0 || seen[s.UID] {
			continue
		}
		seen[s.UID] = true
		rows = append(rows, RaceRankRow{UID: s.UID, Username: s.Username, DurationMS: s.RaceMS, CreatedAt: s.CreatedAt})
		if len(rows) >= limit {
			break
		}
	}
	return rows, cur.Err()
}

// ResolveRaceBonusMult AC 回写后的 race 名次倍数解析（v0.45 并入倍率总则 max，
// 取代原「按首 AC 掉落独立补差」流水）：raceMs CAS 写入（重测回写幂等闸）→
// 即时名次（先到先得）→ 前 3 名且 race.Bonus → 返回名次倍率百分比（200/150/120），
// 由 MaybeAwardJade 与祝福/连击/每日/复仇取最高一次性发放。(race,uid) CAS 抢坑
// 防重交/并发重复解析。返回 0 = 无加成（无 race / 迟到 / 非挑战 AC / 已结算或取消 /
// 榜外 / 加成已处理）。注意：race 提交 AC 时该题已有首 AC 记录（此前 AC 过）的，
// MaybeAwardJade 幂等跳过、玉不重发，名次倍数随之作废——加成只随首 AC 一次性发生
//（顺带堵住「先 AC 再进 race 白拿补差」的套利）。
func (r *Repo) ResolveRaceBonusMult(ctx context.Context, sub *Submission) int {
	rc, err := r.GetClassroomRace(ctx, sub.RaceID)
	if err != nil || rc == nil {
		if err != nil {
			slog.Error("race 读取失败", "component", "race", "rid", sub.RaceID, "sid", sub.ID, "error", err)
		}
		return 0
	}
	// 上榜资格：提交创建时刻在 race 窗口内（参赛即挑战作答，迟到补交不计）
	if sub.CreatedAt.After(rc.DeadlineAt) {
		return 0
	}
	// 用时取个人挑战终判（调用方排在 FinalizeChallengeOnAC 之后，ac 会话已有 durationMs）
	var ch struct {
		Status     string `bson:"status"`
		DurationMS int64  `bson:"durationMs"`
	}
	err = r.challenges.FindOne(ctx,
		bson.M{"uid": sub.UID, "pid": sub.ProblemID, "submissionId": sub.ID},
		options.FindOne().SetProjection(bson.M{"status": 1, "durationMs": 1})).Decode(&ch)
	if err != nil {
		if !IsNotFound(err) {
			slog.Error("race 终判读挑战会话失败", "component", "race", "sid", sub.ID, "error", err)
		}
		return 0
	}
	if ch.Status != ChallengeAC || ch.DurationMS <= 0 {
		return 0
	}
	// raceMs CAS 写入：MatchedCount==0 = 重测回写，加成已处理过，跳过
	res, err := r.submissions.UpdateOne(ctx,
		bson.M{"_id": sub.ID, "raceMs": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"raceMs": ch.DurationMS}})
	if err != nil {
		slog.Error("race 用时回写失败", "component", "race", "sid", sub.ID, "error", err)
		return 0
	}
	if res.MatchedCount == 0 {
		return 0
	}
	if rc.Status != RaceRunning {
		return 0 // 已结算/已取消：只留数据不计加成
	}
	return r.raceRankMultPct(ctx, rc, sub)
}

// raceBonusMultPcts 前 3 名掉落倍率百分比（×2/×1.5/×1.2，plan §2.44 拍板；
// v0.45 由浮点表改整数百分比并入倍率总则）。
var raceBonusMultPcts = [3]int{200, 150, 120}

// raceRankMultPct 即时名次 → 倍率百分比；(race, uid) 一次性解析闸（CAS 抢坑），
// 重测/重交的后续 AC 一律 no-op。
func (r *Repo) raceRankMultPct(ctx context.Context, rc *ClassroomRace, sub *Submission) int {
	if !rc.Bonus {
		return 0
	}
	board, err := r.RaceBoard(ctx, rc.ID, 3)
	if err != nil {
		slog.Warn("race 名次计算失败", "component", "race", "rid", rc.ID, "sid", sub.ID, "error", err)
		return 0
	}
	rank := 0 // 1-based；榜内无我（理论上不可能——刚写入）视为榜外
	for i, row := range board {
		if row.UID == sub.UID {
			rank = i + 1
			break
		}
	}
	if rank < 1 || rank > 3 {
		return 0
	}
	res, err := r.classroomRaces.UpdateOne(ctx,
		bson.M{"_id": rc.ID, "bonusPaid": bson.M{"$ne": sub.UID}},
		bson.M{"$push": bson.M{"bonusPaid": sub.UID}})
	if err != nil {
		slog.Warn("race 加成幂等标记失败", "component", "race", "rid", rc.ID, "uid", sub.UID, "error", err)
		return 0
	}
	if res.MatchedCount == 0 {
		return 0
	}
	return raceBonusMultPcts[rank-1]
}

func trimMult(m float64) string {
	if m == float64(int64(m)) {
		return fmt.Sprintf("%d", int64(m))
	}
	return fmt.Sprintf("%g", m)
}

// sweepRaces 截止懒结算：到期 running → settled（CAS 防并发双结算），冠军随
// CAS 固化并全班广播（空榜不广播——败局/冷场不施压）。随 race 读路径调用。
func (r *Repo) sweepRaces(ctx context.Context, classID int64) {
	expired, err := r.classroomRaces.Find(ctx,
		bson.M{"classId": classID, "status": RaceRunning, "deadlineAt": bson.M{"$lt": time.Now()}},
		options.Find().SetLimit(5))
	if err != nil {
		slog.Warn("race 过期清扫读取失败", "component", "race", "classId", classID, "error", err)
		return
	}
	defer expired.Close(ctx)
	for expired.Next(ctx) {
		var rc ClassroomRace
		if err := expired.Decode(&rc); err != nil {
			return
		}
		board, err := r.RaceBoard(ctx, rc.ID, 1)
		if err != nil {
			slog.Warn("race 结算读榜失败", "component", "race", "rid", rc.ID, "error", err)
			continue
		}
		set := bson.M{"status": RaceSettled, "settledAt": time.Now()}
		var winner RaceRankRow
		if len(board) > 0 {
			winner = board[0]
			set["winnerUid"] = winner.UID
			set["winnerName"] = winner.Username
		}
		res, err := r.classroomRaces.UpdateOne(ctx,
			bson.M{"_id": rc.ID, "status": RaceRunning}, bson.M{"$set": set})
		if err != nil {
			slog.Warn("race 结算失败", "component", "race", "rid", rc.ID, "error", err)
			continue
		}
		if res.MatchedCount == 0 || winner.UID == 0 {
			continue // 并发双结算落败方 / 空榜不广播
		}
		r.notifyRaceSettled(ctx, &rc, &winner)
	}
}

// notifyRaceSettled 结算广播：班级通知「本场 PK 冠军是 xxx」。
func (r *Repo) notifyRaceSettled(ctx context.Context, rc *ClassroomRace, winner *RaceRankRow) {
	uids, err := r.ClassMemberIDs(ctx, rc.ClassID)
	if err != nil {
		slog.Warn("race 结算通知取成员失败", "component", "race", "rid", rc.ID, "error", err)
		return
	}
	retention := DefaultNotifyRetentionDays
	if s, serr := r.GetSystemSettings(ctx); serr == nil {
		retention = s.NotifyRetentionDaysVal()
	}
	body := fmt.Sprintf("本场 PK《%s》冠军是 %s（%.1f 秒）", rc.PTitle, winner.Username, float64(winner.DurationMS)/1000.0)
	if err := r.NotifyUsers(ctx, uids, NotifyInput{
		Typ: NotifyContest, Title: "课堂 PK 结算", Body: body,
		Link: fmt.Sprintf("/classes/%d", rc.ClassID), RefID: rc.ID,
	}, retention); err != nil {
		slog.Warn("race 结算通知失败", "component", "race", "rid", rc.ID, "error", err)
	}
}

// ensureRaceIndexes Bootstrap 挂载：班级活动查 + 用户参赛反查 + 题维度反查。
func (r *Repo) ensureRaceIndexes(ctx context.Context) error {
	_, err := r.classroomRaces.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "classId", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "pid", Value: 1}, {Key: "status", Value: 1}}},
	})
	return err
}
