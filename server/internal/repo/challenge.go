package repo

// 挑战模式（v0.23）：限时手速 PK。
//   - 会话与记录合一文档：Start 落 running（同一 (uid,pid) 旧 running 会话被顶为 quit），
//     AC / 超时 / 主动退出写终态；AC 且 submittedAt ≤ deadline 才算挑战成功（duration 按
//     提交创建时刻起算——判题排队耗时不惩罚选手）。
//   - 计时权威在服务端：deadlineAt = startedAt + 时长（题目 challengeTimeSec 覆盖或难度×5min），
//     前端倒计时纯展示；超时走懒判定（读时清扫），不依赖定时器。
//   - 打字流水（changelog，每秒 diff 快照）随提交上传仅作事后审计留存，不做现场判定。
//   - 排行榜：status=ac 按 uid 取最短 duration；(pid,durationMs) 索引支撑题内榜，
//     扁平结构可支撑后续全站总榜聚合。

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 挑战会话状态。
const (
	ChallengeRunning = "running"
	ChallengeAC      = "ac"
	ChallengeTimeout = "timeout"
	ChallengeQuit    = "quit"
)

// ChallengeDelta 打字流水快照：ti = 相对会话开始的毫秒，dc = diff-match-patch patch 文本。
type ChallengeDelta struct {
	Ti int64  `bson:"ti"`
	Dc string `bson:"dc"`
}

// Challenge 挑战会话/记录。
type Challenge struct {
	ID           int64            `bson:"_id"`
	UID          int64            `bson:"uid"`
	PID          int64            `bson:"pid"`
	StartedAt    time.Time        `bson:"startedAt"`
	DeadlineAt   time.Time        `bson:"deadlineAt"`
	Status       string           `bson:"status"`
	FinishedAt   time.Time        `bson:"finishedAt,omitempty"`
	DurationMS   int64            `bson:"durationMs,omitempty"`  // AC 生效：提交创建时刻 - startedAt
	CharsPerSec  float64          `bson:"charsPerSec,omitempty"` // AC 生效：代码字节数 / 秒
	Lang         string           `bson:"lang,omitempty"`
	CodeLen      int              `bson:"codeLen,omitempty"`      // 提交代码长度（审计与速度计算留底）
	SubmissionID int64            `bson:"submissionId,omitempty"` // 挑战中的那次提交
	ChangeLog    []ChallengeDelta `bson:"changelog,omitempty"`    // 打字流水（审计留存，上限见 attachChallenge）
	AuditFlag    string           `bson:"auditFlag,omitempty"`    // 事后审计标记（v1 仅存储，后台复核后置）
}

// EffChallengeSec 生效挑战时长（秒）：题目显式配置优先，否则按难度 ×5 分钟线性（D1=5min … D10=50min）。
func (p *Problem) EffChallengeSec() int {
	if p.ChallengeTimeSec >= 60 && p.ChallengeTimeSec <= 14400 {
		return p.ChallengeTimeSec
	}
	return p.EffDifficulty() * 5 * 60
}

// ChallengeEligible 题目可挑战：stdio 传统题（客观题无代码、函数/交互题驱动文件、
// 文件 IO 读的不是 stdin，自测与挑战同口径排除）。
func (p *Problem) ChallengeEligible() bool {
	return !p.IsObjective() && !p.IsFunction() && !p.IsInteractive() && !p.FileIO
}

// StartChallenge 开启挑战会话：同 (uid,pid) 的旧 running 会话顶为 quit（刷新/重开不背双会话）。
// startedAt 顺延 challengePrerollMS：对齐前端 3-2-1-GO 前摇，GO 落地才真正起表。
func (r *Repo) StartChallenge(ctx context.Context, uid int64, p *Problem) (*Challenge, error) {
	if _, err := r.Challenges.UpdateMany(ctx,
		bson.M{"uid": uid, "pid": p.ID, "status": ChallengeRunning},
		bson.M{"$set": bson.M{"status": ChallengeQuit, "finishedAt": time.Now()}}); err != nil {
		return nil, err
	}
	id, err := r.nextSeq(ctx, "challenge")
	if err != nil {
		return nil, err
	}
	started := time.Now().Add(challengePrerollMS)
	c := &Challenge{
		ID:         id,
		UID:        uid,
		PID:        p.ID,
		StartedAt:  started,
		DeadlineAt: started.Add(time.Duration(p.EffChallengeSec()) * time.Second),
		Status:     ChallengeRunning,
	}
	if _, err := r.Challenges.InsertOne(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// challengePrerollMS 开场倒数前摇（与前端 3-2-1 动画时长对齐）：前摇内的提交在
// 终判 duration 上不会为负，超时口径也从 GO 起算。
const challengePrerollMS = 3 * time.Second

// sweepExpired 会话超时懒判定：把到期 running 会话置 timeout。随读取路径调用。
func (r *Repo) sweepExpired(ctx context.Context, uid, pid int64) {
	if _, err := r.Challenges.UpdateMany(ctx,
		bson.M{"uid": uid, "pid": pid, "status": ChallengeRunning, "deadlineAt": bson.M{"$lt": time.Now()}},
		bson.M{"$set": bson.M{"status": ChallengeTimeout, "finishedAt": time.Now()}}); err != nil {
		slog.Warn("超时清扫失败", "component", "challenge", "uid", uid, "pid", pid, "error", err)
	}
}

// ActiveChallenge 当前 running 会话（含到期懒清扫）；无会话返回 nil。
func (r *Repo) ActiveChallenge(ctx context.Context, uid, pid int64) (*Challenge, error) {
	r.sweepExpired(ctx, uid, pid)
	var c Challenge
	err := r.Challenges.FindOne(ctx, bson.M{"uid": uid, "pid": pid, "status": ChallengeRunning}).Decode(&c)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// LastChallenge 最近一份会话（任意状态）；无记录返回 nil。挑战页刷新后恢复展示用。
func (r *Repo) LastChallenge(ctx context.Context, uid, pid int64) (*Challenge, error) {
	r.sweepExpired(ctx, uid, pid)
	var c Challenge
	err := r.Challenges.FindOne(ctx, bson.M{"uid": uid, "pid": pid},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&c)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// QuitChallenge 主动退出：running → quit。返回是否确有会话被退出。
func (r *Repo) QuitChallenge(ctx context.Context, uid, pid int64) (bool, error) {
	res, err := r.Challenges.UpdateOne(ctx,
		bson.M{"uid": uid, "pid": pid, "status": ChallengeRunning},
		bson.M{"$set": bson.M{"status": ChallengeQuit, "finishedAt": time.Now()}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// attachChallengeLimits 打字流水的入库上限：条数与总字节双帽（超过整体丢弃——
// 流水只是审计留底，不值得为它拒绝一次正常提交）。
const (
	challengeMaxDeltas    = 5400 // 50min 会话每秒一条的理论上限
	challengeMaxDeltaText = 256 << 10
)

// ChallengeRunningOK 挑战会话可用性校验：属于本人、同题、仍在跑（含到期懒清扫）。
func (r *Repo) ChallengeRunningOK(ctx context.Context, cid, uid, pid int64) (bool, error) {
	r.sweepExpired(ctx, uid, pid)
	n, err := r.Challenges.CountDocuments(ctx,
		bson.M{"_id": cid, "uid": uid, "pid": pid, "status": ChallengeRunning})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AttachChallengeSubmission 挑战中的提交回填会话：绑定提交 ID / 语言 / 代码长度 /
// 打字流水。调用方先以 ChallengeRunningOK 校验再创建提交，这里绑定失败只可能来自
// 毫秒级竞态（会话恰好到期），由调用方记日志兜底。
func (r *Repo) AttachChallengeSubmission(ctx context.Context, cid, sid int64, langName string, codeLen int, changelog []ChallengeDelta) (bool, error) {
	res, err := r.Challenges.UpdateOne(ctx,
		bson.M{"_id": cid, "status": ChallengeRunning},
		bson.M{"$set": bson.M{"submissionId": sid, "lang": langName, "codeLen": codeLen}})
	if err != nil || res.MatchedCount == 0 {
		return false, err
	}
	if total := changelogSize(changelog); total > 0 {
		if total <= challengeMaxDeltaText && len(changelog) <= challengeMaxDeltas {
			if _, err := r.Challenges.UpdateOne(ctx, bson.M{"_id": cid},
				bson.M{"$set": bson.M{"changelog": changelog}}); err != nil {
				slog.Warn("打字流水留存失败", "component", "challenge", "cid", cid, "error", err)
			}
		} else {
			slog.Warn("打字流水超限丢弃", "component", "challenge", "cid", cid, "entries", len(changelog), "bytes", total)
		}
	}
	return true, nil
}

func changelogSize(log []ChallengeDelta) int {
	n := 0
	for _, d := range log {
		n += len(d.Dc) + 16
	}
	return n
}

// FinalizeChallengeOnAC 判题 AC 回写后的挑战终判（judge 管道调用）：
// 提交创建时刻 ≤ deadline 才算成功（排队耗时不惩罚）；超期补交按 timeout 落终态。
func (r *Repo) FinalizeChallengeOnAC(ctx context.Context, uid, pid, sid int64, codeLen int) {
	r.sweepExpired(ctx, uid, pid)
	var c Challenge
	err := r.Challenges.FindOne(ctx, bson.M{"uid": uid, "pid": pid, "submissionId": sid, "status": ChallengeRunning}).Decode(&c)
	if err != nil {
		if !IsNotFound(err) {
			slog.Error("AC 终判读取失败", "component", "challenge", "uid", uid, "pid", pid, "sid", sid, "error", err)
		}
		return
	}
	var sub struct {
		CreatedAt time.Time `bson:"createdAt"`
	}
	if err := r.Submissions.FindOne(ctx, bson.M{"_id": sid}, options.FindOne().SetProjection(bson.M{"createdAt": 1})).Decode(&sub); err != nil {
		slog.Error("AC 终判读提交失败", "component", "challenge", "sid", sid, "error", err)
		return
	}
	status, duration := ChallengeAC, sub.CreatedAt.Sub(c.StartedAt).Milliseconds()
	if duration < 0 {
		// 前摇内的极端提交：钳 0（正常流程编辑器在 GO 前不可写，到不了这里）
		duration = 0
	}
	if sub.CreatedAt.After(c.DeadlineAt) {
		status, duration = ChallengeTimeout, c.DeadlineAt.Sub(c.StartedAt).Milliseconds()
	}
	set := bson.M{"status": status, "finishedAt": time.Now(), "durationMs": duration}
	if status == ChallengeAC {
		set["charsPerSec"] = float64(codeLen) / (float64(duration) / 1000.0)
	}
	if _, err := r.Challenges.UpdateOne(ctx, bson.M{"_id": c.ID, "status": ChallengeRunning}, bson.M{"$set": set}); err != nil {
		slog.Error("AC 终判回写失败", "component", "challenge", "cid", c.ID, "error", err)
	}
}

// ChallengeRankRow 挑战榜行：每人取最短 AC 用时。
type ChallengeRankRow struct {
	UID          int64
	DurationMS   int64
	CharsPerSec  float64
	Lang         string
	SubmissionID int64
	CreatedAt    time.Time // 达成时刻（该条记录的 finishedAt）
}

// ChallengeRank 题内挑战榜：AC 记录按用时升序逐行扫描、按 uid 去重取最佳，截前 limit 名。
func (r *Repo) ChallengeRank(ctx context.Context, pid int64, limit int) ([]ChallengeRankRow, error) {
	if limit <= 0 {
		limit = 50
	}
	cur, err := r.Challenges.Find(ctx, bson.M{"pid": pid, "status": ChallengeAC},
		options.Find().
			SetSort(bson.D{{Key: "durationMs", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"uid": 1, "durationMs": 1, "charsPerSec": 1, "lang": 1, "submissionId": 1, "finishedAt": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []ChallengeRankRow
	seen := map[int64]bool{}
	for cur.Next(ctx) {
		var c Challenge
		if err := cur.Decode(&c); err != nil {
			return nil, err
		}
		if seen[c.UID] {
			continue
		}
		seen[c.UID] = true
		rows = append(rows, ChallengeRankRow{
			UID: c.UID, DurationMS: c.DurationMS, CharsPerSec: c.CharsPerSec,
			Lang: c.Lang, SubmissionID: c.SubmissionID, CreatedAt: c.FinishedAt,
		})
		if len(rows) >= limit {
			break
		}
	}
	return rows, cur.Err()
}

// ensureChallengeIndexes Bootstrap 挂载：会话查询与榜单聚合。
func (r *Repo) ensureChallengeIndexes(ctx context.Context) error {
	_, err := r.Challenges.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "pid", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "pid", Value: 1}, {Key: "durationMs", Value: 1}}},
	})
	return err
}
