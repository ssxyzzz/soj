package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mailer"
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
	// ICP 备案号（v0.13）：页脚悬挂并链接工信部备案系统；空 = 不展示（未备案部署不显示）。
	IcpNumber string `bson:"icpNumber"`
	// 批量导入限额（v0.15，MB）：单题数据上限 / 整包 zip 上限；nil = 未设置按默认（100 / 2048）。
	BatchImportProblemMB *int `bson:"batchImportProblemMB,omitempty"`
	BatchImportZipMB     *int `bson:"batchImportZipMB,omitempty"`
	// 邮件通道（v0.16）：log = 打日志不外发（默认，人工转告）；smtp = 真实发信。
	MailChannel string `bson:"mailChannel,omitempty"`
	// SMTP 连接参数（MailChannel=smtp 时使用；启动校验配置完整性，不完整拒绝启动）。
	SMTP mailer.SMTPConfig `bson:"smtp,omitempty"`
	// 注册验证方式：none = 无（默认 = 现状）；email = 邮箱验证码。
	RegisterVerifyMode string `bson:"registerVerifyMode,omitempty"`
	// 忘记密码自助找回开关（v0.16）：默认关——开启前登录页不展示入口（老师重置仍是主路径）。
	PasswordResetEnabled *bool `bson:"passwordResetEnabled,omitempty"`
}

const (
	DefaultRateLimitPerMinute   = 3
	DefaultDataDownloadDayLimit = 5
	DefaultAutocompleteMinLevel = 5
	DefaultAllowRegister        = true
	DefaultBatchImportProblemMB = 100  // 单题数据 100MB
	DefaultBatchImportZipMB     = 2048 // 整包 zip 2GB

	MailChannelLog      = "log"
	MailChannelSMTP     = "smtp"
	RegisterVerifyNone  = "none"
	RegisterVerifyEmail = "email"
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
	if s.BatchImportProblemMB == nil || *s.BatchImportProblemMB < 1 {
		s.BatchImportProblemMB = intPtr(DefaultBatchImportProblemMB)
	}
	if s.BatchImportZipMB == nil || *s.BatchImportZipMB < 1 {
		s.BatchImportZipMB = intPtr(DefaultBatchImportZipMB)
	}
	if s.MailChannel != MailChannelSMTP {
		s.MailChannel = MailChannelLog
	}
	if s.RegisterVerifyMode != RegisterVerifyEmail {
		s.RegisterVerifyMode = RegisterVerifyNone
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
			"icpNumber":              s.IcpNumber,
			"batchImportProblemMB":   s.BatchImportProblemMBVal(),
			"batchImportZipMB":       s.BatchImportZipMBVal(),
			"mailChannel":            s.MailChannel,
			"smtp":                   s.SMTP,
			"registerVerifyMode":     s.RegisterVerifyMode,
			"passwordResetEnabled":   s.PasswordResetOpen(),
		}},
	)
	return err
}

// PasswordResetOpen 忘记密码自助找回是否开启（默认关）。
func (s *SystemSettings) PasswordResetOpen() bool {
	return s.PasswordResetEnabled != nil && *s.PasswordResetEnabled
}

// RegisterVerifyModeVal 注册验证方式（none/email，异常值回 none）。
func (s *SystemSettings) RegisterVerifyModeVal() string {
	if s.RegisterVerifyMode == RegisterVerifyEmail {
		return RegisterVerifyEmail
	}
	return RegisterVerifyNone
}

// MailChannelVal 邮件通道（log/smtp，异常值回 log）。
func (s *SystemSettings) MailChannelVal() string {
	if s.MailChannel == MailChannelSMTP {
		return MailChannelSMTP
	}
	return MailChannelLog
}

// Mailer 当前邮件 provider：smtp 通道配置完整才返回 SMTP 实现，否则一律 log
// （运行期兜底——启动校验在 server.Run；后台把通道切成 smtp 但配置不全时降级打日志）。
func (s *SystemSettings) Mailer() mailer.Provider {
	if s.MailChannelVal() == MailChannelSMTP && s.SMTP.Ready() {
		return mailer.NewSMTP(s.SMTP)
	}
	return mailer.Log{}
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

// TestDataDownloadedToday 当天已下载的不同题数（exclude 里的题不计——多题打包导出
// 的合并预检用：选中数与剩余额度比较，选中的题本来就在本次放行范围）。
func (r *Repo) TestDataDownloadedToday(ctx context.Context, uid int64, exclude []int64) (int64, error) {
	filter := bson.M{"uid": uid, "day": time.Now().Format("2006-01-02")}
	if len(exclude) > 0 {
		filter["pid"] = bson.M{"$nin": exclude}
	}
	var vals []int64
	if err := r.db.Collection("dataDownloads").Distinct(ctx, "pid", filter).Decode(&vals); err != nil {
		return 0, err
	}
	return int64(len(vals)), nil
}

// BatchImportProblemMBVal 批量导入单题数据上限（MB）：未设置/越界回默认 100。
func (s *SystemSettings) BatchImportProblemMBVal() int {
	if s.BatchImportProblemMB == nil || *s.BatchImportProblemMB < 1 {
		return DefaultBatchImportProblemMB
	}
	return *s.BatchImportProblemMB
}

// BatchImportZipMBVal 批量导入整包 zip 上限（MB）：未设置/越界回默认 2048。
func (s *SystemSettings) BatchImportZipMBVal() int {
	if s.BatchImportZipMB == nil || *s.BatchImportZipMB < 1 {
		return DefaultBatchImportZipMB
	}
	return *s.BatchImportZipMB
}
