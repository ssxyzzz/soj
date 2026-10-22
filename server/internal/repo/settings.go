package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SystemSettings 系统设置（settings 集合，_id="system"）。
type SystemSettings struct {
	RateLimitPerMinute int    `bson:"rateLimitPerMinute"`    // default 角色每分钟提交上限
	HomeNotice         string `bson:"homeNotice"`            // 首页公告（Markdown，空 = 不展示）
	BackupGuide        string `bson:"backupGuide,omitempty"` // 备份面板使用说明（Markdown，空 = 前端内置默认）
	// 每日每用户可下载数据的题目数上限（problem.data.download 权限位持有者受此限额）。
	// 指针区分「旧文档未设置」与「显式 0」：nil = 未设置按默认；0 = 不限制；>0 = 每日上限。
	DataDownloadDailyLimit *int `bson:"dataDownloadDailyLimit,omitempty"`
	// 代码自动补全的解锁等级（v0.11）：等级 ≥ 该值才能开启自动补全；nil = 未设置按默认 5。
	AutocompleteMinLevel *int `bson:"autocompleteMinLevel,omitempty"`
	// 是否开放自助注册；nil = 未设置按默认开放（存量部署行为不变）。关闭后建号走用户管理。
	AllowRegister *bool `bson:"allowRegister,omitempty"`
}

const (
	DefaultRateLimitPerMinute   = 3
	DefaultDataDownloadDayLimit = 5
	DefaultAutocompleteMinLevel = 5
	DefaultAllowRegister        = true
)

func downloadLimitPtr(v int) *int { return &v }
func boolPtr(v bool) *bool        { return &v }

// DownloadDailyLimit 限额取值：未设置回默认（存量 settings 文档无此字段时为 nil）。
func (s *SystemSettings) DownloadDailyLimit() int {
	if s.DataDownloadDailyLimit == nil || *s.DataDownloadDailyLimit < 0 {
		return DefaultDataDownloadDayLimit
	}
	return *s.DataDownloadDailyLimit
}

// GetSystemSettings 读取系统设置，未初始化时返回默认值。
func (r *Repo) GetSystemSettings(ctx context.Context) (*SystemSettings, error) {
	var s SystemSettings
	err := r.db.Collection("settings").FindOne(ctx, bson.M{"_id": "system"}).Decode(&s)
	if IsNotFound(err) {
		return &SystemSettings{
			RateLimitPerMinute:     DefaultRateLimitPerMinute,
			DataDownloadDailyLimit: downloadLimitPtr(DefaultDataDownloadDayLimit),
			AllowRegister:          boolPtr(DefaultAllowRegister),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	if s.RateLimitPerMinute <= 0 {
		s.RateLimitPerMinute = DefaultRateLimitPerMinute
	}
	if s.DataDownloadDailyLimit == nil {
		s.DataDownloadDailyLimit = downloadLimitPtr(DefaultDataDownloadDayLimit)
	}
	if s.AutocompleteMinLevel == nil || *s.AutocompleteMinLevel < 1 || *s.AutocompleteMinLevel > 10 {
		s.AutocompleteMinLevel = intPtr(DefaultAutocompleteMinLevel)
	}
	if s.AllowRegister == nil {
		s.AllowRegister = boolPtr(DefaultAllowRegister)
	}
	return &s, nil
}

func intPtr(v int) *int { return &v }

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
			"rateLimitPerMinute":     s.RateLimitPerMinute,
			"homeNotice":             s.HomeNotice,
			"backupGuide":            s.BackupGuide,
			"dataDownloadDailyLimit": s.DownloadDailyLimit(),
			"autocompleteMinLevel":   s.MinAutocompleteLevel(),
			"allowRegister":          s.RegistrationOpen(),
		}},
	)
	return err
}

// AutocompleteMinLevel 自动补全解锁等级：未设置/越界回默认 5。
func (s *SystemSettings) MinAutocompleteLevel() int {
	if s.AutocompleteMinLevel == nil || *s.AutocompleteMinLevel < 1 || *s.AutocompleteMinLevel > 10 {
		return DefaultAutocompleteMinLevel
	}
	return *s.AutocompleteMinLevel
}

// RegistrationOpen 自助注册是否开放：未设置回默认开放（存量 settings 文档无此字段时为 nil）。
func (s *SystemSettings) RegistrationOpen() bool {
	if s.AllowRegister == nil {
		return DefaultAllowRegister
	}
	return *s.AllowRegister
}

// TestDataDownloadAllowed 每日下载限额判断：limit 0 = 不限制；>0 时当天已下载的不同题数
// 达到上限且本题不在其中则拒绝（同一题当天重复下载不重复计数）。
func (r *Repo) TestDataDownloadAllowed(ctx context.Context, uid, pid int64, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	n, err := r.db.Collection("dataDownloads").CountDocuments(ctx, bson.M{
		"uid": uid, "day": time.Now().Format("2006-01-02"), "pid": bson.M{"$ne": pid},
	})
	if err != nil {
		return false, err
	}
	return n < int64(limit), nil
}

// RecordTestDataDownload 记录一次下载（按 uid+pid+day 去重，同题当天重复下载不新增计数）。
func (r *Repo) RecordTestDataDownload(ctx context.Context, uid, pid int64) error {
	_, err := r.db.Collection("dataDownloads").UpdateOne(ctx,
		bson.M{"uid": uid, "pid": pid, "day": time.Now().Format("2006-01-02")},
		bson.M{"$setOnInsert": bson.M{"at": time.Now()}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}
