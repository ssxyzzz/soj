package repo

// 玉经济体（v0.33-① 玉地基，plan §2.39）：账本 + 余额 + 首次 AC 发玉与随机掉落 + 比赛奖池结算。
//
// 账本 jadeLedger 流水照 xpAdjust 范式 {uid, delta±, kind, reason, ref, at}；余额在
// jadeBalances 单文档 $inc 物化（读免聚合）；扣减走条件原子更新（余额 ≥ 额度才 $inc）
// 防并发双花，扣后写流水失败则补偿回滚。发玉通道：award（首 AC 基础+掉落一笔合记）/
// contest_rank / contest_participate / admin_adjust；消费通道（solution_unlock / bounty_* /
// shop_order / ai_*）随第二三步启用，本文件只提供通用 credit/spend。
//
// 防刷口径（2026-10-29/30 拍板）：同题重复 AC 不重发——jadeAwards {uid,pid} 唯一索引
// 幂等（普通重交/挑战/多场同题天然防）；客观题首满分半额 max(1,⌊D/2⌋)；掉落
// p = 1−AC率（无人做过按 0.3，去重用户口径、AC 时点固化）；比赛奖池双池仅教师/admin/root
// 可设（权限闸在 server 侧 round 编辑入口），结算 N = score>0、名额 K=⌈N×比例⌉、
// 线性递减权重、并列按名次段均分、参与奖发给 score>0 未获奖者。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 玉流水 kind 枚举（消费侧随第二三步接线，先占位）。
const (
	JadeKindAward              = "award"               // 首次 AC：基础 + 掉落一笔合记
	JadeKindContestRank        = "contest_rank"        // 比赛名次奖
	JadeKindContestParticipate = "contest_participate" // 比赛参与奖
	JadeKindSolutionUnlock     = "solution_unlock"     // 题解解锁（v0.33-②）
	JadeKindBountyFreeze       = "bounty_freeze"       // 悬赏托管（v0.33-②）
	JadeKindBountyPay          = "bounty_pay"          // 悬赏打赏（v0.33-②）
	JadeKindBountyRefund       = "bounty_refund"       // 悬赏退回（v0.33-②）
	JadeKindShopOrder          = "shop_order"          // 实物兑换（v0.33-②）
	JadeKindAiHint             = "ai_hint"             // AI 分级提示（v0.33-④，ref 编码级别）
	JadeKindAiDebug            = "ai_debug"            // AI 找 bug（v0.33-④）
	JadeKindAdminAdjust        = "admin_adjust"        // root 人工加减
)

// ErrInsufficientJade 余额不足（扣减的条件原子更新未命中）。
var ErrInsufficientJade = errors.New("余额不足")

// EnsureJadeIndexes 玉三集合索引（随 Bootstrap 执行）：awards {uid,pid} 唯一防重发；
// ledger {uid,_id desc} 流水分页。
func (r *Repo) EnsureJadeIndexes(ctx context.Context) error {
	if _, err := r.jadeAwards.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "pid", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return err
	}
	if _, err := r.achievements.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "code", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return err
	}
	if _, err := r.jadeBlessings.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "level", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return err
	}
	_, err := r.jadeLedger.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "ref", Value: 1}}},
		// 题解解锁判定（v0.33-②）：{uid, kind, ref} 点查
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "kind", Value: 1}, {Key: "ref", Value: 1}}},
	})
	return err
}

// JadeBalance 当前余额（无文档 = 0）。
func (r *Repo) JadeBalance(ctx context.Context, uid int64) (int64, error) {
	var doc struct {
		Bal int64 `bson:"bal"`
	}
	err := r.jadeBalances.FindOne(ctx, bson.M{"_id": uid}).Decode(&doc)
	if IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return doc.Bal, nil
}

// JadeCredit 入账（delta > 0）：余额 $inc 物化 + 流水落档；流水写失败补偿回滚余额。
func (r *Repo) JadeCredit(ctx context.Context, uid, delta int64, kind, reason string, ref int64) error {
	if uid <= 0 || delta <= 0 {
		return errors.New("入账参数非法")
	}
	if _, err := r.jadeBalances.UpdateOne(ctx,
		bson.M{"_id": uid},
		bson.M{"$inc": bson.M{"bal": delta}, "$set": bson.M{"updatedAt": time.Now()}},
		options.UpdateOne().SetUpsert(true)); err != nil {
		return err
	}
	if err := r.insertJadeLedger(ctx, uid, delta, kind, reason, ref); err != nil {
		_, _ = r.jadeBalances.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$inc": bson.M{"bal": -delta}})
		return err
	}
	return nil
}

// JadeSpend 扣减（amount > 0）：条件原子更新（余额 ≥ amount 才 $inc −amount）防并发双花；
// 流水写失败补偿回滚。余额不足返回 ErrInsufficientJade。
func (r *Repo) JadeSpend(ctx context.Context, uid, amount int64, kind, reason string, ref int64) error {
	if uid <= 0 || amount <= 0 {
		return errors.New("扣减参数非法")
	}
	res, err := r.jadeBalances.UpdateOne(ctx,
		bson.M{"_id": uid, "bal": bson.M{"$gte": amount}},
		bson.M{"$inc": bson.M{"bal": -amount}, "$set": bson.M{"updatedAt": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrInsufficientJade
	}
	if err := r.insertJadeLedger(ctx, uid, -amount, kind, reason, ref); err != nil {
		_, _ = r.jadeBalances.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$inc": bson.M{"bal": amount}})
		return err
	}
	return nil
}

func (r *Repo) insertJadeLedger(ctx context.Context, uid, delta int64, kind, reason string, ref int64) error {
	id, err := r.nextSeq(ctx, "jadeLedger")
	if err != nil {
		return err
	}
	_, err = r.jadeLedger.InsertOne(ctx, bson.M{
		"_id": id, "uid": uid, "delta": delta, "kind": kind,
		"reason": reason, "ref": ref, "at": time.Now(),
	})
	return err
}

// JadeLedgerEntry 流水行。
type JadeLedgerEntry struct {
	ID     int64     `bson:"_id"`
	UID    int64     `bson:"uid"`
	Delta  int64     `bson:"delta"`
	Kind   string    `bson:"kind"`
	Reason string    `bson:"reason"`
	Ref    int64     `bson:"ref"`
	At     time.Time `bson:"at"`
}

// JadeLedgerPage 某用户流水分页（新→旧）。
func (r *Repo) JadeLedgerPage(ctx context.Context, uid int64, page, pageSize int) ([]*JadeLedgerEntry, int64, error) {
	total, err := r.jadeLedger.CountDocuments(ctx, bson.M{"uid": uid})
	if err != nil {
		return nil, 0, err
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	cursor, err := r.jadeLedger.Find(ctx, bson.M{"uid": uid},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var list []*JadeLedgerEntry
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// JadeAward 首 AC 发放记录（base/drop 按 AC 时点固化；测试与后续调试面读出）。
type JadeAward struct {
	Base     int64     `bson:"base"`
	Drop     int64     `bson:"drop"`
	Total    int64     `bson:"total"`
	MultPct  int       `bson:"multPct,omitempty"`  // 采用倍率百分比（100=无加成；v0.45 总则）
	MultFrom string    `bson:"multFrom,omitempty"` // 采用倍率来源文案（并列取首个更高者）
	Daily    bool      `bson:"daily,omitempty"`    // 每日一题（v0.41；v0.45 起并列达标即记）
	Revenge  bool      `bson:"revenge,omitempty"`  // 一血偿还（v0.41；v0.45 起倍率被采用才记=名额消耗）
	At       time.Time `bson:"at"`
}

// GetJadeAward 某用户某题的首 AC 发放记录（无则 nil, nil）。
func (r *Repo) GetJadeAward(ctx context.Context, uid, pid int64) (*JadeAward, error) {
	var a JadeAward
	err := r.jadeAwards.FindOne(ctx, bson.M{"uid": uid, "pid": pid}).Decode(&a)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ClearDiscussBounty 悬赏冻结失败时清掉话题上的悬赏标记（话题保留为普通帖）。
func (r *Repo) ClearDiscussBounty(ctx context.Context, tid int64) error {
	_, err := r.discussions.UpdateOne(ctx, bson.M{"_id": tid},
		bson.M{"$set": bson.M{"bounty": 0, "bountyPaid": 0}})
	return err
}

// TipDiscussBounty 悬赏打赏（托管池内划转）：话题 bountyPaid 前移（条件更新防超发）+
// 入账回复作者（kind bounty_pay，ref=tid）。
func (r *Repo) TipDiscussBounty(ctx context.Context, tid, rid, toUID, amount int64) error {
	res, err := r.discussions.UpdateOne(ctx,
		bson.M{"_id": tid, "$expr": bson.M{"$gte": bson.A{
			bson.M{"$subtract": bson.A{"$bounty", "$bountyPaid"}}, amount}}},
		bson.M{
			"$inc": bson.M{"bountyPaid": amount},
			"$set": bson.M{"lastActiveAt": time.Now(), "updatedAt": time.Now()},
		})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errors.New("打赏金额超过剩余悬赏")
	}
	if err := r.JadeCredit(ctx, toUID, amount, JadeKindBountyPay,
		fmt.Sprintf("悬赏打赏（话题 #%d 回复 #%d）", tid, rid), tid); err != nil {
		// 入账失败回滚托管划转（保持一致性；极端路径，尽力而为）
		_, _ = r.discussions.UpdateOne(ctx, bson.M{"_id": tid},
			bson.M{"$inc": bson.M{"bountyPaid": -amount}})
		return err
	}
	return nil
}

// RefundDueBounties 悬赏自动退款：扫 bounty>0 ∧ 未退 ∧ 剩余>0 ∧ LastActiveAt 超期的话题，
// 剩余托管退回发帖人（bountyRefundLoop 每 10 分钟调；limit 单轮上限防大扫）。
func (r *Repo) RefundDueBounties(ctx context.Context, ttl time.Duration, limit int) (int, error) {
	deadline := time.Now().Add(-ttl)
	cursor, err := r.discussions.Find(ctx, bson.M{
		"bounty": bson.M{"$gt": 0},
		"bountyRefundedAt": bson.M{"$exists": false},
		"lastActiveAt":     bson.M{"$lt": deadline},
		"$expr":            bson.M{"$gt": bson.A{"$bounty", "$bountyPaid"}},
	}, options.Find().SetLimit(int64(limit)))
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	refunded := 0
	for cursor.Next(ctx) {
		var t DiscussTopic
		if err := cursor.Decode(&t); err != nil {
			continue
		}
		remain := t.Bounty - t.BountyPaid
		if remain <= 0 {
			continue
		}
		// 条件置退款标记（并发/重复扫幂等）
		res, err := r.discussions.UpdateOne(ctx,
			bson.M{"_id": t.ID, "bountyRefundedAt": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"bountyRefundedAt": time.Now()}})
		if err != nil || res.MatchedCount == 0 {
			continue
		}
		if err := r.JadeCredit(ctx, t.UID, remain, JadeKindBountyRefund,
			fmt.Sprintf("悬赏到期退回（话题 #%d，无人打赏部分）", t.ID), t.ID); err != nil {
			continue
		}
		refunded++
	}
	return refunded, cursor.Err()
}

// RefundTopicBountyOnDelete 删帖退悬赏（版务/作者删帖路径调用）：剩余托管即时退回。
func (r *Repo) RefundTopicBountyOnDelete(ctx context.Context, t *DiscussTopic) {
	if t.Bounty <= 0 || !t.BountyRefundedAt.IsZero() {
		return
	}
	remain := t.Bounty - t.BountyPaid
	if remain <= 0 {
		return
	}
	res, err := r.discussions.UpdateOne(ctx,
		bson.M{"_id": t.ID, "bountyRefundedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"bountyRefundedAt": time.Now()}})
	if err != nil || res.MatchedCount == 0 {
		return
	}
	if err := r.JadeCredit(ctx, t.UID, remain, JadeKindBountyRefund,
		fmt.Sprintf("话题删除退回悬赏（话题 #%d）", t.ID), t.ID); err != nil {
		slog.Warn("删帖悬赏退款失败", "component", "jade", "tid", t.ID, "uid", t.UID, "error", err)
	}
}

// HasJadeSolutionUnlock 是否已用玉解锁某题题解（解锁记录 = kind=solution_unlock 流水本身，
// 解锁永久）。
func (r *Repo) HasJadeSolutionUnlock(ctx context.Context, uid, pid int64) (bool, error) {
	n, err := r.jadeLedger.CountDocuments(ctx,
		bson.M{"uid": uid, "kind": JadeKindSolutionUnlock, "ref": pid})
	return n > 0, err
}

// UnlockSolutionWithJade 扣玉解锁题解（解锁记录即流水；重复解锁由 server 层先查拦截，
// 这里幂等兜底：已解锁直接返回已解锁）。
func (r *Repo) UnlockSolutionWithJade(ctx context.Context, uid, pid int64, amount int64, title string) (already bool, err error) {
	if has, err := r.HasJadeSolutionUnlock(ctx, uid, pid); err != nil {
		return false, err
	} else if has {
		return true, nil
	}
	if err := r.JadeSpend(ctx, uid, amount, JadeKindSolutionUnlock,
		fmt.Sprintf("解锁题解 #%d《%s》", pid, title), pid); err != nil {
		return false, err
	}
	return false, nil
}

// problemACRate 该题 AC 率（去重用户口径，与难度迁移启发式同源）：
// 做过 = 有非取消提交的不同用户，满分 = 其中该题最高分 ≥ 100 的用户。
// v0.38 加 10 分钟缓存：聚合挂在判题回写路径（MaybeAwardJade），掉落概率是
// 唯一消费面——陈旧无害（掉落额本就按 AC 时点固化进 awards）。
func (r *Repo) problemACRate(ctx context.Context, pid int64) (rate float64, attempted bool, err error) {
	r.acMu.Lock()
	if e, ok := r.acRates[pid]; ok && time.Since(e.at) < 10*time.Minute {
		r.acMu.Unlock()
		return e.rate, e.attempted, nil
	}
	r.acMu.Unlock()
	rate, attempted, err = r.problemACRateDB(ctx, pid)
	if err == nil {
		r.acMu.Lock()
		r.acRates[pid] = acRateEntry{rate: rate, attempted: attempted, at: time.Now()}
		r.acMu.Unlock()
	}
	return rate, attempted, err
}

func (r *Repo) problemACRateDB(ctx context.Context, pid int64) (rate float64, attempted bool, err error) {
	cursor, err := r.submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"pid": pid, "status": bson.M{"$ne": StatusCancelled}}},
		{"$group": bson.M{"_id": "$uid", "best": bson.M{"$max": "$score"}}},
		{"$group": bson.M{
			"_id": nil,
			"n":   bson.M{"$sum": 1},
			"ac":  bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$gte": bson.A{"$best", 100}}, 1, 0}}},
		}},
	})
	if err != nil {
		return 0, false, err
	}
	defer cursor.Close(ctx)
	var row struct {
		N  int64 `bson:"n"`
		AC int64 `bson:"ac"`
	}
	if !cursor.Next(ctx) {
		return 0, false, nil // 无人做过
	}
	if err := cursor.Decode(&row); err != nil {
		return 0, false, err
	}
	if row.N == 0 {
		return 0, false, nil
	}
	return float64(row.AC) / float64(row.N), true, nil
}

// MaybeAwardJade 首次 AC 发玉（全局幂等）：已发过直接返回；并发窗口由 {uid,pid}
// 唯一索引兜底（InsertOne 撞键 = 输了竞态，静默 no-op）。客观题首满分减半；掉落
// p=1−AC率、成功额外掉难度值玉（AC 率无人做过按 0.3；金额按 AC 时点固化进 awards）。
// 挂点：判题回写（published 提交，racePct/combo 由 judge 管道先行解析回传）、
// 客观题创建/重判（满分且 published，racePct/combo 传 0）、比赛发布补发（传 0）。
// ---- 倍率总则（v0.45）：最终玉 = (基础+掉落) × max(祝福/连击/每日/复仇/PK名次) ----

// userBlessed 用户是否处于祝福期（判题发玉路径用，投影点查）。
func (r *Repo) userBlessed(ctx context.Context, uid int64) (bool, error) {
	var u struct {
		BlessUntil time.Time `bson:"blessUntil"`
	}
	err := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{"blessUntil": 1})).Decode(&u)
	if err != nil {
		return false, err
	}
	return time.Now().Before(u.BlessUntil), nil
}

// UserBlessUntil 祝福截止时刻（零值 = 无祝福；余额接口附带展示用）。
func (r *Repo) UserBlessUntil(ctx context.Context, uid int64) (time.Time, error) {
	var u struct {
		BlessUntil time.Time `bson:"blessUntil"`
	}
	err := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{"blessUntil": 1})).Decode(&u)
	if IsNotFound(err) {
		return time.Time{}, nil
	}
	return u.BlessUntil, err
}

// EnsureLevelBlessing 检测升级并开启祝福：等级只涨不跌，当前等级 > 已祝福档即
// CAS 更新 blessedLevel + blessUntil（48h）——CAS 保证每档只祝福一次（幂等）；
// jadeBlessings 审计 {uid,level} 唯一兜底 + 站内通知。尽力而为：审计/通知失败
// 不影响祝福生效。
func (r *Repo) EnsureLevelBlessing(ctx context.Context, uid int64, level int) error {
	if level < 2 {
		return nil
	}
	// CAS：blessedLevel < level 或字段缺失（存量用户首次祝福——$lt 对缺失字段不匹配，
	// 必须显式 $exists:false，与年级晋级标记同款坑）。无 upsert：用户文档在建号时必存，
	// 已祝福到同级/更高 = 无事发生（重复插入会 E11000）
	res, err := r.users.UpdateOne(ctx,
		bson.M{"_id": uid, "$or": bson.A{
			bson.M{"blessedLevel": bson.M{"$lt": level}},
			bson.M{"blessedLevel": bson.M{"$exists": false}},
		}},
		bson.M{"$set": bson.M{"blessedLevel": level, "blessUntil": time.Now().Add(48 * time.Hour)}})
	if err != nil || res.MatchedCount == 0 {
		return err // 已祝福到同级或更高 = 无事发生
	}
	if id, serr := r.nextSeq(ctx, "jadeBlessing"); serr == nil {
		_, _ = r.jadeBlessings.InsertOne(ctx, bson.M{
			"_id": id, "uid": uid, "level": level, "at": time.Now()})
	}
	retention := DefaultNotifyRetentionDays
	if s, serr := r.GetSystemSettings(ctx); serr == nil {
		retention = s.NotifyRetentionDaysVal()
	}
	_ = r.NotifyUsers(ctx, []int64{uid}, NotifyInput{
		Typ:   NotifyLevelUp,
		Title: fmt.Sprintf("升到 Lv.%d", level),
		Body:  "升维祝福已开启：48 小时内做题的玉收入翻倍，趁热打铁！",
		Link:  "/problems",
	}, retention)
	return nil
}

// EnsureLevelBlessingForUser 判题路径入口：聚合当前等级再检测（仅首 AC 调用，频次低）。
func (r *Repo) EnsureLevelBlessingForUser(ctx context.Context, uid int64) error {
	cfg, err := r.GetLevelConfig(ctx)
	if err != nil {
		return err
	}
	counts, err := r.SolvedDifficultyCounts(ctx, []int64{uid})
	if err != nil {
		return err
	}
	adjs, err := r.SumXpAdjust(ctx, []int64{uid})
	if err != nil {
		return err
	}
	lv, _, _, _, _ := LevelProgress(cfg, counts[uid], adjs[uid])
	return r.EnsureLevelBlessing(ctx, uid, lv)
}

// MaybeAwardJade 首次 AC 发玉（同题幂等）。倍率总则（v0.45，2026-10-31 拍板）：
// 最终玉 = (基础 + 掉落) × max(全部倍率)——升维祝福 ×2 / 连击加成（每满 step +pct%，
// 封顶 capPct）/ 每日一题 ×2 / 一血偿还 ×1.5 / 课堂 PK 名次（raceRankPct，0=无）
// 全部平级取最高不连乘，峰值 ×2；客观题半额已先于本段作用于 base。
// combo = 本次 AC 生效后的连击数（judge 路径由 TrackJudgeOutcome 先行回传；
// 客观题/比赛补发路径传 0 = 无连击加成）。每日复仇名额仅在倍率被采用时消耗
//（被更高倍率压制 → 顺延不扣）。
func (r *Repo) MaybeAwardJade(ctx context.Context, uid, pid int64, raceRankPct, combo int) (awarded bool, total int64, err error) {
	var exist struct {
		ID int64 `bson:"_id"`
	}
	if err := r.jadeAwards.FindOne(ctx, bson.M{"uid": uid, "pid": pid}).Decode(&exist); err == nil {
		return false, 0, nil
	} else if !IsNotFound(err) {
		return false, 0, err
	}
	p, err := r.GetProblemMeta(ctx, pid)
	if err != nil {
		return false, 0, err
	}
	d := p.EffDifficulty()
	base := d
	if p.IsObjective() {
		base = d / 2
		if base < 1 {
			base = 1
		}
	}
	drop := 0
	rate := 0.3
	if acRate, attempted, rerr := r.problemACRate(ctx, pid); rerr == nil && attempted {
		rate = acRate
	}
	if rand.Float64() < 1-rate {
		drop = d
	}
	// 倍率候选收集（百分比整数，100 = 无加成；取最高不叠乘）。
	set := &SystemSettings{}
	if ss, serr := r.GetSystemSettings(ctx); serr == nil && ss != nil {
		set = ss
	}
	multPct, multFrom := 100, ""
	claim := func(pct int, from string) {
		if pct > multPct {
			multPct, multFrom = pct, from
		}
	}
	blessed, berr := r.userBlessed(ctx, uid)
	if berr == nil && blessed {
		claim(200, "升维祝福 ×2")
	}
	if cp := set.JadeComboBonusPct(combo); cp > 100 {
		claim(cp, "连击加成")
	}
	daily := r.IsTodayDaily(ctx, pid)
	if daily {
		claim(200, "每日一题 ×2")
	}
	revengeEligible, revengeUsedToday := r.revengeState(ctx, uid, pid)
	if revengeEligible && !revengeUsedToday {
		claim(150, "一血偿还 ×1.5")
	}
	if raceRankPct > 100 {
		claim(raceRankPct, "课堂 PK 名次")
	}
	// 名额消耗：仅复仇倍率被采用（150 ≥ multPct，即并列最高）时扣当日名额；
	// 被祝福/连击/每日/PK 压制则顺延不扣。
	revengeWon := revengeEligible && !revengeUsedToday && 150 >= multPct
	if revengeWon {
		r.consumeRevengeSlot(ctx, uid)
	}
	baseAmt := int64(base) + int64(drop)
	total = (baseAmt*int64(multPct) + 99) / 100 // 向上取整
	id, err := r.nextSeq(ctx, "jadeAward")
	if err != nil {
		return false, 0, err
	}
	_, err = r.jadeAwards.InsertOne(ctx, bson.M{
		"_id": id, "uid": uid, "pid": pid,
		"base": base, "drop": drop, "total": total,
		"multPct": multPct, "multFrom": multFrom,
		"daily": daily, "revenge": revengeWon, "at": time.Now(),
	})
	if mongo.IsDuplicateKeyError(err) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if err := r.JadeCredit(ctx, uid, total, JadeKindAward,
		fmt.Sprintf("首次通过 #%d《%s》（基础 %d + 掉落 %d × %s）", pid, p.Title, base, drop, multText(multPct, multFrom)), pid); err != nil {
		return false, 0, err
	}
	return true, total, nil
}

// multText 倍率文案（100 = 原倍，无来源）。
func multText(pct int, from string) string {
	if pct <= 100 {
		return "无加成"
	}
	if from == "" {
		return fmt.Sprintf("×%s", trimMult(float64(pct) / 100))
	}
	return fmt.Sprintf("%s ×%s", from, trimMult(float64(pct)/100))
}

// revengeState 每日复仇资格两元组：该题最早一次非取消未 AC 提交距今是否满窗口
//（v0.45：当日顺路做对无加成——防「先 WA 再 AC」人为制造触发条件）、当日名额是否已用。
func (r *Repo) revengeState(ctx context.Context, uid, pid int64) (eligible, usedToday bool) {
	var row struct {
		CreatedAt time.Time `bson:"createdAt"`
	}
	hours := 24
	if set, serr := r.GetSystemSettings(ctx); serr == nil && set != nil {
		hours = set.JadeRevengeHoursVal()
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	err := r.submissions.FindOne(ctx,
		bson.M{"uid": uid, "pid": pid,
			"status":     bson.M{"$nin": bson.A{StatusAccepted, StatusCancelled}},
			"createdAt":  bson.M{"$lte": cutoff}},
		options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: 1}}).
			SetProjection(bson.M{"createdAt": 1})).Decode(&row)
	eligible = err == nil
	// 当日名额（user 文档 revengeDay = 北京日；非今日 = 未用）
	var u struct {
		RevengeDay string `bson:"revengeDay"`
	}
	if uerr := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{"revengeDay": 1})).Decode(&u); uerr == nil {
		usedToday = u.RevengeDay == beijingDay(time.Now())
	}
	return
}

// consumeRevengeSlot 记当日复仇名额已用（北京日；$ne 今日防并发双扣；upsert 兜底
// 无文档用户——真实路径 user 文档恒存在，测试直灌场景防落空）。
func (r *Repo) consumeRevengeSlot(ctx context.Context, uid int64) {
	_, _ = r.users.UpdateOne(ctx,
		bson.M{"_id": uid, "revengeDay": bson.M{"$ne": beijingDay(time.Now())}},
		bson.M{"$set": bson.M{"revengeDay": beijingDay(time.Now())}},
		options.UpdateOne().SetUpsert(true))
}

// contestFullScores 场内各题满分（server 侧 roundFullScores 同口径）。
func contestFullScores(c *Contest) map[int64]int {
	m := make(map[int64]int, len(c.Problems))
	for _, cp := range c.Problems {
		s := cp.Score
		if s <= 0 {
			s = 100
		}
		m[cp.PID] = s
	}
	return m
}

// publishRoundJade 比赛发布后的玉结算（PublishDueRounds 钩子，全部尽力而为不阻断发布）：
// ① 该场满分提交的 (uid,pid) 去重补发首 AC 玉（MaybeAwardJade 全局幂等，赛前练习已发过的跳过）；
// ② 双奖池结算 settleRoundJadePools。
func (r *Repo) publishRoundJade(ctx context.Context, kind string, c *Contest) {
	cursor, err := r.submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{roundsIDField(kind): c.ID, "score": bson.M{"$gte": 100}}},
		{"$group": bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}},
	})
	if err != nil {
		slog.Warn("比赛发布玉补发聚合失败", "component", "jade", "round", c.ID, "error", err)
		return
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID struct {
				UID int64 `bson:"uid"`
				PID int64 `bson:"pid"`
			} `bson:"_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			slog.Warn("比赛发布玉补发解码失败", "component", "jade", "round", c.ID, "error", err)
			continue
		}
		if _, _, err := r.MaybeAwardJade(ctx, row.ID.UID, row.ID.PID, 0, 0); err != nil {
			slog.Warn("比赛发布玉补发失败", "component", "jade", "round", c.ID,
				"uid", row.ID.UID, "pid", row.ID.PID, "error", err)
		}
	}
	r.settleRoundJadePools(ctx, kind, c)
}

// settleRoundJadePools 双奖池结算（成绩发布时一次）：
//   - 真正参赛 N = 最终榜 Total>0 的人数；N=0 两池都不发。
//   - 名次奖（P）：获奖名额 K = clamp(⌈N×比例⌉, 1, N)（比例系统设置，默认 50%）；
//     前 K 名线性递减权重（第 i 名 K−i+1，权重和 K(K+1)/2）；并列（Total 相同连续段）
//     均分该段所占权重和——总额始终 ≤ P。
//   - 参与奖（Q）：score>0 但未进前 K 者均分；无人可发时 Q 滚入名次奖（N≤K）。
//   - 全部向下取整；名次池与参与池的取整零头统一归第 1 名。
//   - 幂等闸：kind=contest_rank ∧ ref=场次ID 已有流水则跳过（发布单向，此闸防重入）。
func (r *Repo) settleRoundJadePools(ctx context.Context, kind string, c *Contest) {
	pool, partPool := c.JadePool, c.JadeParticipatePool
	if pool <= 0 && partPool <= 0 {
		return
	}
	if n, err := r.jadeLedger.CountDocuments(ctx,
		bson.M{"kind": JadeKindContestRank, "ref": c.ID}); err == nil && n > 0 {
		return
	}
	rows, err := r.RoundLeaderboard(ctx, kind, c.ID, contestFullScores(c))
	if err != nil {
		slog.Warn("奖池结算读榜失败", "component", "jade", "round", c.ID, "error", err)
		return
	}
	eligible := make([]*LeaderRow, 0, len(rows))
	for _, row := range rows {
		if row.Total > 0 {
			eligible = append(eligible, row)
		}
	}
	N := len(eligible)
	if N == 0 {
		return
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].Total != eligible[j].Total {
			return eligible[i].Total > eligible[j].Total
		}
		return eligible[i].LastScoring.Before(eligible[j].LastScoring)
	})
	s, _ := r.GetSystemSettings(ctx)
	ratePct := int64(50)
	if s != nil {
		ratePct = int64(s.JadeAwardRatePctVal())
	}
	K := (int64(N)*ratePct + 99) / 100
	if K < 1 {
		K = 1
	}
	if K > int64(N) {
		K = int64(N)
	}
	if int64(N) <= K { // 没有参与奖人群：参与池滚入名次奖
		pool += partPool
		partPool = 0
	}
	totalWeight := K * (K + 1) / 2

	type payout struct {
		uid  int64
		amt  int64
		kind string
	}
	payouts := make([]payout, 0, N)
	rankUIDs := map[int64]bool{}
	spent, pos := int64(0), int64(0)
	for i := 0; i < N && pos < K; {
		j := i
		for j+1 < N && eligible[j+1].Total == eligible[i].Total {
			j++
		}
		end := pos + int64(j-i+1)
		if end > K {
			end = K
		}
		segWeight := (end - pos) * ((K - pos) + (K - end + 1)) / 2
		segAmt := pool * segWeight / totalWeight
		each := segAmt / int64(j-i+1)
		for _, row := range eligible[i : j+1] {
			payouts = append(payouts, payout{uid: row.UID, amt: each, kind: JadeKindContestRank})
			rankUIDs[row.UID] = true
			spent += each
		}
		pos = end
		i = j + 1
	}
	partCount := int64(0)
	if partPool > 0 {
		for _, row := range eligible {
			if !rankUIDs[row.UID] {
				partCount++
			}
		}
	}
	partEach := int64(0)
	if partCount > 0 {
		partEach = partPool / partCount
	}
	leftover := (pool - spent) + (partPool - partEach*partCount)
	roundName := fmt.Sprintf("%s《%s》", roundKindName(kind), c.Title)
	for _, p := range payouts {
		if p.amt <= 0 {
			continue
		}
		if err := r.JadeCredit(ctx, p.uid, p.amt, JadeKindContestRank,
			roundName+"名次奖", c.ID); err != nil {
			slog.Warn("奖池名次奖入账失败", "component", "jade", "round", c.ID, "uid", p.uid, "error", err)
		}
	}
	if partEach > 0 {
		for _, row := range eligible {
			if rankUIDs[row.UID] {
				continue
			}
			if err := r.JadeCredit(ctx, row.UID, partEach, JadeKindContestParticipate,
				roundName+"参与奖", c.ID); err != nil {
				slog.Warn("奖池参与奖入账失败", "component", "jade", "round", c.ID, "uid", row.UID, "error", err)
			}
		}
	}
	// 取整零头归第 1 名（名次池 + 参与池两处的余数）
	if leftover > 0 {
		first := eligible[0].UID
		if err := r.JadeCredit(ctx, first, leftover, JadeKindContestRank,
			roundName+"名次奖（取整零头）", c.ID); err != nil {
			slog.Warn("奖池零头入账失败", "component", "jade", "round", c.ID, "uid", first, "error", err)
		}
	}
}

func roundKindName(kind string) string {
	if kind == KindExam {
		return "考试"
	}
	return "比赛"
}
