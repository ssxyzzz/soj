package repo

// AI 两能存储（v0.33-④）：aiHints 提示缓存（{pid, level} 唯一，同题同级生成一次复用）
// + ai_hint 解锁流水查询 + aiDebugResults 找 bug 结果落档（本人回看 + 教师教学洞察）。

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AiHint 一道题一个级别的生成缓存。
type AiHint struct {
	ID      int64     `bson:"_id"`
	PID     int64     `bson:"pid"`
	Level   int       `bson:"level"`
	Content string    `bson:"content"`
	Tokens  int       `bson:"tokens,omitempty"`
	At      time.Time `bson:"at"`
}

// AiDebugResult 一次找 bug 分析的落档。
type AiDebugResult struct {
	ID          int64     `bson:"_id"`
	UID         int64     `bson:"uid"`
	SID         int64     `bson:"sid"`
	PID         int64     `bson:"pid"`
	Content     string    `bson:"content"`
	CreatedAt   time.Time `bson:"createdAt"`
}

// EnsureAiHintIndexes（Bootstrap）：aiHints {pid, level} 唯一；debug 结果 {uid, _id}。
func (r *Repo) EnsureAiHintIndexes(ctx context.Context) error {
	if _, err := r.db.Collection("aiHints").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "pid", Value: 1}, {Key: "level", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return err
	}
	_, err := r.db.Collection("aiDebugResults").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "_id", Value: -1}}},
	})
	return err
}

// GetAiHint 取缓存（无则 nil, nil）。
func (r *Repo) GetAiHint(ctx context.Context, pid int64, level int) (*AiHint, error) {
	var h AiHint
	err := r.db.Collection("aiHints").FindOne(ctx, bson.M{"pid": pid, "level": level}).Decode(&h)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// EnsureAiHint 缓存缺失则生成（gen 回调返回内容）并落库；并发/重复由唯一索引兜底。
func (r *Repo) EnsureAiHint(ctx context.Context, p *Problem, level int, gen func() (string, error)) (*AiHint, error) {
	if h, err := r.GetAiHint(ctx, p.ID, level); err != nil || h != nil {
		return h, err
	}
	content, err := gen()
	if err != nil {
		return nil, err
	}
	id, err := r.nextSeq(ctx, "aiHint")
	if err != nil {
		return nil, err
	}
	h := &AiHint{ID: id, PID: p.ID, Level: level, Content: content, At: time.Now()}
	_, err = r.db.Collection("aiHints").InsertOne(ctx, h)
	if mongo.IsDuplicateKeyError(err) {
		return r.GetAiHint(ctx, p.ID, level) // 并发赢家已生成
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}

// RecordAiHintUnlock 记一次提示解锁标记（免费群体——教师/满分学生的解锁态已有
// solutionAllowed 覆盖，此标记供特殊场景落 0 元记录；付费学生的扣费流水即解锁记录）。
func (r *Repo) RecordAiHintUnlock(ctx context.Context, uid, pid int64, level int, price int64) error {
	return r.insertJadeLedger(ctx, uid, -price, JadeKindAiHint,
		aiHintReason(level, pid), pid*10+int64(level))
}

// HasAiHintUnlock 是否已解锁该级（免费流水价 0 也算解锁记录）。
func (r *Repo) HasAiHintUnlock(ctx context.Context, uid, pid int64, level int) (bool, error) {
	n, err := r.jadeLedger.CountDocuments(ctx,
		bson.M{"uid": uid, "kind": JadeKindAiHint, "ref": pid*10 + int64(level)})
	return n > 0, err
}

func aiHintReason(level int, pid int64) string {
	names := map[int]string{1: "方向提示", 2: "算法选型", 3: "实现要点"}
	return "AI 助教 · " + names[level] + "（题目 #" + fmt.Sprint(pid) + "）"
}

// SaveAiDebugResult 找 bug 结果落档（可回看）。
func (r *Repo) SaveAiDebugResult(ctx context.Context, uid, sid, pid int64, content string) error {
	id, err := r.nextSeq(ctx, "aiDebugResult")
	if err != nil {
		return err
	}
	_, err = r.db.Collection("aiDebugResults").InsertOne(ctx, &AiDebugResult{
		ID: id, UID: uid, SID: sid, PID: pid, Content: content, CreatedAt: time.Now(),
	})
	return err
}
