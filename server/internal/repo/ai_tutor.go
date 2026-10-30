package repo

// AI 两能存储（v0.33-④）：aiHints 提示缓存（{pid, level} 唯一，同题同级生成一次复用）
// + ai_hint 解锁流水查询 + aiDebugResults 找 bug 结果落档（本人回看 + 教师教学洞察）。

import (
	"context"
	"fmt"
	"time"

	"soj/internal/ai"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AiHint 一道题一个级别的生成缓存。
type AiHint struct {
	ID          int64     `bson:"_id"`
	PID         int64     `bson:"pid"`
	Level       int       `bson:"level"`
	Content     string    `bson:"content"`
	Tokens      int       `bson:"tokens,omitempty"`
	PromptChars int       `bson:"promptChars,omitempty"`
	At          time.Time `bson:"at"`
}

// AiDebugResult 一次找 bug 分析的落档。
type AiDebugResult struct {
	ID        int64     `bson:"_id"`
	UID       int64     `bson:"uid"`
	SID       int64     `bson:"sid"`
	PID       int64     `bson:"pid"`
	Content   string    `bson:"content"`
	CreatedAt time.Time `bson:"createdAt"`
}

// EnsureAiHintIndexes（Bootstrap）：aiHints {pid, level} 唯一；debug 结果 {uid, _id}。
func (r *Repo) EnsureAiHintIndexes(ctx context.Context) error {
	if _, err := r.db.Collection("aiHints").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "pid", Value: 1}, {Key: "level", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return err
	}
	// 个人提示结果：一人一级一份（方案二，2026-10-30）
	if _, err := r.db.Collection("aiHintResults").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "pid", Value: 1}, {Key: "level", Value: 1}}, Options: options.Index().SetUnique(true)},
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

// EnsureAiHint 缓存缺失则生成（gen 回调返回完整响应：内容 + tokens/素材字符元数据）
// 并落库；并发/重复由唯一索引兜底。
func (r *Repo) EnsureAiHint(ctx context.Context, p *Problem, level int, gen func() (*ai.Response, error)) (*AiHint, error) {
	if h, err := r.GetAiHint(ctx, p.ID, level); err != nil || h != nil {
		return h, err
	}
	resp, err := gen()
	if err != nil {
		return nil, err
	}
	id, err := r.nextSeq(ctx, "aiHint")
	if err != nil {
		return nil, err
	}
	h := &AiHint{ID: id, PID: p.ID, Level: level, Content: resp.Content, Tokens: resp.TotalTokens, PromptChars: resp.PromptChars, At: time.Now()}
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

// AiHintResult 学生个人最新提示结果（方案二，2026-10-30）：谁生成存谁——重新生成
// 每次另计费并覆盖自己这份；解锁记录（一次性买断查看权）不变。共享缓存 aiHints
// 仍服务「首份内容的全班复用」，个人档是各自重算后的私人版本。
type AiHintResult struct {
	UID     int64     `bson:"uid"`
	PID     int64     `bson:"pid"`
	Level   int       `bson:"level"`
	Content string    `bson:"content"`
	At      time.Time `bson:"at"`
}

// SaveAiHintResult 覆盖保存本人该级最新结果（upsert；唯一索引 {uid,pid,level} 并发兜底）。
func (r *Repo) SaveAiHintResult(ctx context.Context, uid, pid int64, level int, content string) error {
	_, err := r.db.Collection("aiHintResults").UpdateOne(ctx,
		bson.M{"uid": uid, "pid": pid, "level": level},
		bson.M{"$set": bson.M{"content": content, "at": time.Now()}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// GetAiHintResult 取本人该级最新结果；无记录返回 IsNotFound。
func (r *Repo) GetAiHintResult(ctx context.Context, uid, pid int64, level int) (*AiHintResult, error) {
	var res AiHintResult
	err := r.db.Collection("aiHintResults").FindOne(ctx,
		bson.M{"uid": uid, "pid": pid, "level": level},
	).Decode(&res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// GetAiDebugResult 取本人某次提交最近一次找 bug 结果；无记录返回 IsNotFound。
// 同一提交可重复分析，按 _id 降序取最新。
func (r *Repo) GetAiDebugResult(ctx context.Context, uid, sid int64) (*AiDebugResult, error) {
	var res AiDebugResult
	err := r.db.Collection("aiDebugResults").FindOne(ctx,
		bson.M{"uid": uid, "sid": sid},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}}),
	).Decode(&res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
