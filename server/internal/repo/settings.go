package repo

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SystemSettings 系统设置（settings 集合，_id="system"）。
type SystemSettings struct {
	RateLimitPerMinute int    `bson:"rateLimitPerMinute"`    // default 角色每分钟提交上限
	HomeNotice         string `bson:"homeNotice"`            // 首页公告（Markdown，空 = 不展示）
	BackupGuide        string `bson:"backupGuide,omitempty"` // 备份面板使用说明（Markdown，空 = 前端内置默认）
}

const DefaultRateLimitPerMinute = 3

// GetSystemSettings 读取系统设置，未初始化时返回默认值。
func (r *Repo) GetSystemSettings(ctx context.Context) (*SystemSettings, error) {
	var s SystemSettings
	err := r.db.Collection("settings").FindOne(ctx, bson.M{"_id": "system"}).Decode(&s)
	if IsNotFound(err) {
		return &SystemSettings{RateLimitPerMinute: DefaultRateLimitPerMinute}, nil
	}
	if err != nil {
		return nil, err
	}
	if s.RateLimitPerMinute <= 0 {
		s.RateLimitPerMinute = DefaultRateLimitPerMinute
	}
	return &s, nil
}

// UpdateSystemSettings 更新系统设置（文档不存在时创建）。
func (r *Repo) UpdateSystemSettings(ctx context.Context, s *SystemSettings) error {
	col := r.db.Collection("settings")
	// 确保文档存在（UpdateOne 不带 upsert 时匹配不到会静默无效）
	if _, err := col.InsertOne(ctx, bson.M{"_id": "system"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	_, err := col.UpdateOne(ctx,
		bson.M{"_id": "system"},
		bson.M{"$set": bson.M{
			"rateLimitPerMinute": s.RateLimitPerMinute,
			"homeNotice":         s.HomeNotice,
			"backupGuide":        s.BackupGuide,
		}},
	)
	return err
}
