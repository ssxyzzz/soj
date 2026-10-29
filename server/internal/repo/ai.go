package repo

// AI 底座 repo 侧（v0.33-③）：供应商配置存取（key 只写不回显）+ 调用审计 + 每日上限 +
// 计费定价（难度倍率 × 等级折扣）。提示生成缓存与找 bug 落档在 v0.33-④ 接入本层。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/ai"
)

// AiCall 一次 AI 调用审计行（root 后台可见：成本监控）。
type AiCall struct {
	ID          int64     `bson:"_id"`
	UID         int64     `bson:"uid"`
	PID         int64     `bson:"pid"`
	Kind        string    `bson:"kind"` // hint1 | hint2 | hint3 | debug
	TokensTotal int       `bson:"tokensTotal"`
	PromptChars int       `bson:"promptChars"`
	Cost        int64     `bson:"cost"` // 扣玉（教师/root 免费为 0）
	Provider    string    `bson:"provider"`
	Model       string    `bson:"model"`
	OK          bool      `bson:"ok"`
	Err         string    `bson:"err,omitempty"`
	At          time.Time `bson:"at"`
}

// GetAIConfig 读 AI 配置（nil 安全：未配置返回空配置）。
func (r *Repo) GetAIConfig(ctx context.Context) (*ai.Config, error) {
	var doc struct {
		AIConfig *ai.Config `bson:"aiConfig"`
	}
	err := r.db.Collection("settings").FindOne(ctx, bson.M{"_id": "system"}).Decode(&doc)
	if IsNotFound(err) || doc.AIConfig == nil {
		return &ai.Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	return doc.AIConfig, nil
}

// SaveAIConfig 覆盖保存（key 留空的档位保留旧 key——「不改」语义）。
func (r *Repo) SaveAIConfig(ctx context.Context, cfg *ai.Config) error {
	if _, err := r.db.Collection("settings").InsertOne(ctx, bson.M{"_id": "system"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	_, err := r.db.Collection("settings").UpdateOne(ctx,
		bson.M{"_id": "system"}, bson.M{"$set": bson.M{"aiConfig": cfg}})
	return err
}

// EnsureAiCallIndexes（Bootstrap）：{uid, at}（每日上限计数）+ {at}（审计翻页）。
func (r *Repo) EnsureAiCallIndexes(ctx context.Context) error {
	_, err := r.db.Collection("aiCalls").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "at", Value: -1}}},
	})
	return err
}

// RecordAiCall 记一行审计（尽力而为）。
func (r *Repo) RecordAiCall(ctx context.Context, c *AiCall) {
	id, err := r.nextSeq(ctx, "aiCall")
	if err != nil {
		return
	}
	c.ID, c.At = id, time.Now()
	_, _ = r.db.Collection("aiCalls").InsertOne(ctx, c)
}

// CountAiCallsToday 当日调用次数（每日上限口径，教师/root 豁免不计）。
func (r *Repo) CountAiCallsToday(ctx context.Context, uid int64) (int64, error) {
	y, m, d := time.Now().Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	return r.db.Collection("aiCalls").CountDocuments(ctx,
		bson.M{"uid": uid, "at": bson.M{"$gte": dayStart}})
}

// ListAiCalls 审计分页（root；uid>0 过滤单人）。
func (r *Repo) ListAiCalls(ctx context.Context, uid int64, page, pageSize int) ([]*AiCall, int64, error) {
	q := bson.M{}
	if uid > 0 {
		q["uid"] = uid
	}
	total, err := r.db.Collection("aiCalls").CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.db.Collection("aiCalls").Find(ctx, q,
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).
			SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var list []*AiCall
	return list, total, cursor.All(ctx, &list)
}
