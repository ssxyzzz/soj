package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mailer"
	"soj/internal/mdrender"
)

// SystemSettings 系统设置（settings 集合，_id="system"）。
type SystemSettings struct {
	RateLimitPerMinute int    `bson:"rateLimitPerMinute"` // default 角色每分钟提交上限
	HomeNotice         string `bson:"homeNotice"`         // 首页公告（Markdown，空 = 不展示）
	// HomeNoticeHtml 写入时渲染缓存（v0.53 渲染收口）：匿名首页直发 html，
	// 访客不再为看公告下载渲染引擎；md 永远是事实源
	HomeNoticeHtml string `bson:"homeNoticeHtml,omitempty"`
	// 每日每用户可下载数据的题目数上限（problem.data.download 权限位持有者受此限额）。
	// 指针区分「旧文档未设置」与「显式 0」：nil = 未设置按默认；0 = 不限制；>0 = 每日上限。
	DataDownloadDailyLimit *int `bson:"dataDownloadDailyLimit,omitempty"`
	// 代码自动补全的解锁等级（v0.11）：等级 ≥ 该值才能开启自动补全；nil = 未设置按默认 5。
	AutocompleteMinLevel *int `bson:"autocompleteMinLevel,omitempty"`
	// 是否开放自助注册；nil = 未设置按默认开放（存量部署行为不变）。关闭后建号走用户管理。
	AllowRegister *bool `bson:"allowRegister,omitempty"`
	// ICP 备案号（v0.13）：页脚悬挂并链接工信部备案系统；空 = 不展示（未备案部署不显示）。
	IcpNumber string `bson:"icpNumber"`
	// 公安网备号（2026-11-02）：页脚悬挂并链接公安部备案查询；空 = 不展示（未备案部署不显示）。
	MpsNumber string `bson:"mpsNumber"`
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
	// 讨论区（v0.24）：三区总开关默认开；GlobalLocked = root 一键临时锁定导航栏讨论区
	//（班级考试期间防考题外流，锁定只封导航栏区，题内/比赛讨论不受影响）。
	DiscussGlobalEnabled  *bool `bson:"discussGlobalEnabled,omitempty"`
	DiscussProblemEnabled *bool `bson:"discussProblemEnabled,omitempty"`
	DiscussContestEnabled *bool `bson:"discussContestEnabled,omitempty"`
	DiscussGlobalLocked   bool  `bson:"discussGlobalLocked,omitempty"`
	// 讨论区代码门槛（discussCodeThreshold）随 v0.53 渲染收口批整体退役：功能连同题目级
	// 覆盖、读时裁剪、PermDiscussCodeBypass 全链下线；存量库值不再读写
	// 发帖限速：主楼每日上限 / 回复最小间隔秒；0 = 不限。
	DiscussTopicDailyLimit  *int `bson:"discussTopicDailyLimit,omitempty"`
	DiscussReplyIntervalSec *int `bson:"discussReplyIntervalSec,omitempty"`
	// 新号冷静期（天，v0.48 单位改天换血）：注册未满 N 天不能在导航栏讨论区发主楼
	// （题内求助不受限）；root 不受限；0 = 不限。
	DiscussNewbieDays *int `bson:"discussNewbieDays,omitempty"`
	// 举报自动折叠阈值：同一目标未处理举报达到 N 条自动折叠；0 = 不自动折叠。
	DiscussReportFold *int `bson:"discussReportFold,omitempty"`
	// 通知保留天数（TTL 在插入时按当时值写死）；0 = 按默认 90。
	NotifyRetentionDays *int `bson:"notifyRetentionDays,omitempty"`
	// root 全站广播每日上限；0 = 不限。
	NotifyBroadcastPerDay *int `bson:"notifyBroadcastPerDay,omitempty"`
	// 注册取号起点（v0.24 小批）：自动分配的用户 ID 从该号起（默认 100）；只抬高不回退
	// ——计数器已超过起点时从计数器继续；手动指定 ID 不受此限。
	UserSeqStart *int `bson:"userSeqStart,omitempty"`
	// 自动题号起点：自动分配的题目 ID 从该号起（默认 1001，1~1000 惯例留给导入题包）；
	// 只抬高不回退——计数器已超过起点时从计数器继续；导入/建题指定题号不受此限。
	ProblemSeqStart *int `bson:"problemSeqStart,omitempty"`
	// 玉经济体（v0.33）：货币名（默认「玉」，可改金币/钻石/奖励币等，1–6 字）；
	// 全站文案统一从该配置读。
	CurrencyName string `bson:"currencyName,omitempty"`
	// 比赛奖池获奖比例（百分比，N=score>0 人数 → 名次奖名额 K=⌈N×比例/100⌉）；
	// nil = 未设置按默认 50。
	JadeAwardRatePct *int `bson:"jadeAwardRatePct,omitempty"`
	// 题解解锁价系数（v0.33-②）：价 = 题目难度 × 系数（默认 5，D10=50 玉）；nil 按默认。
	JadeSolutionUnlockMult *int `bson:"jadeSolutionUnlockMult,omitempty"`
	// 悬赏额上下限（v0.33-②，默认 5 / 500）：发主楼挂悬赏的最小/最大玉数（≤余额为前提）。
	JadeBountyMin *int `bson:"jadeBountyMin,omitempty"`
	JadeBountyMax *int `bson:"jadeBountyMax,omitempty"`
	// 实物兑换（v0.33-②）：整模块默认关，仅 root 可开（目录 + 人工履约）。
	JadeShopEnabled *bool `bson:"jadeShopEnabled,omitempty"`
	// 玉折扣阶梯（v0.40）：Lv5–10 六档消费折扣百分比（95=九五折…70=七折）；
	// 仅作用于题解解锁与 AI 两能（悬赏是玉流转、实物原价——防套利拍板）。
	// 缺档/越界回默认，单调性由 normalize 保证（高等级不高于低等级）。
	JadeDiscount [6]int `bson:"jadeDiscount,omitempty"`
	// 连击玉加成（v0.45）：每满 step 连击掉落加成 pct%（10 连击 +10% 形态），
	// 加成封顶 capPct%（默认 100 = 峰值 ×2.0）；倍率总则中与祝福/每日事件平级取最高。
	JadeComboStep   *int `bson:"jadeComboStep,omitempty"`
	JadeComboPct    *int `bson:"jadeComboPct,omitempty"`
	JadeComboCapPct *int `bson:"jadeComboCapPct,omitempty"`
	// 每日复仇窗口（v0.45）：最早一次非取消未 AC 提交距今满 N 小时才可复仇（默认 24，
	// 当日顺路做对无加成——防「先 WA 再 AC」刷倍率，配每日 1 名额结构性免疫）。
	JadeRevengeHours *int `bson:"jadeRevengeHours,omitempty"`
	// AI 计费（v0.33-③）：每日合计上限（默认 5 次，提示+找 bug 合计）；基准价
	//（两级提示：理解题目 2 / 算法思路 6、找 bug 8）× 难度倍率 ⌈D/3⌉；等级折扣两档
	//（Lv≥discount1Lv 九折 / Lv≥discount2Lv 八折，向上取整）。全部后台可调。
	// 特权层每日上限（2026-10-30）：教师/管理员免费但计次（默认 20，0 = 不限）；
	// root 恒不限次。
	AiDailyLimit      *int   `bson:"aiDailyLimit,omitempty"`
	AiStaffDailyLimit *int   `bson:"aiStaffDailyLimit,omitempty"`
	AiHintPrice1      *int64 `bson:"aiHintPrice1,omitempty"`
	AiHintPrice2      *int64 `bson:"aiHintPrice2,omitempty"`
	AiDebugPrice      *int64 `bson:"aiDebugPrice,omitempty"`
	// AI 助教名字（v0.33-④ 验收调整：默认「小玉」，1–12 字，前端面板/按钮文案统一读）。
	AiTutorName string `bson:"aiTutorName,omitempty"`
	// 资料自改开关（v0.34）：五字段逐个可锁（昵称/性别/年级/学校/地区），true = 允许
	// 本人自助修改；nil = 全开（存量部署行为不变）。昵称为双闸：开关 AND user.nickname.edit
	// 权限位。QQ/邮箱不锁（头像绑定与验证链路）。root 用户管理不受限（锁定后的修改口）。
	ProfileEdit *ProfileEditConfig `bson:"profileEdit,omitempty"`
	// 年级晋级标记（v0.34）：最近已执行的学年起始年（9/1 切换）；0/缺失 = 从未执行。
	// 由晋级逻辑独占读写（EnsureGradePromotion，全自动），不经系统设置表单。
	GradePromoYear int `bson:"gradePromoYear,omitempty"`
}

// ProfileEditConfig 资料自改逐字段开关（true = 允许本人改）。
type ProfileEditConfig struct {
	Nickname bool `bson:"nickname" json:"nickname"`
	Gender   bool `bson:"gender" json:"gender"`
	Grade    bool `bson:"grade" json:"grade"`
	School   bool `bson:"school" json:"school"`
	Region   bool `bson:"region" json:"region"`
}

// DefaultProfileEditConfig 全开（存量行为）。
func DefaultProfileEditConfig() *ProfileEditConfig {
	return &ProfileEditConfig{Nickname: true, Gender: true, Grade: true, School: true, Region: true}
}

// ProfileEditCfg 读取归一（nil 回全开默认）。
func (s *SystemSettings) ProfileEditCfg() ProfileEditConfig {
	if s.ProfileEdit == nil {
		return *DefaultProfileEditConfig()
	}
	return *s.ProfileEdit
}

// Open* 单字段判定（管理端设置回显用；nil = 全开，防御直接构造的零值结构体）。
func (s *SystemSettings) OpenNickname() bool { return s.ProfileEdit == nil || s.ProfileEdit.Nickname }
func (s *SystemSettings) OpenGender() bool   { return s.ProfileEdit == nil || s.ProfileEdit.Gender }
func (s *SystemSettings) OpenGrade() bool    { return s.ProfileEdit == nil || s.ProfileEdit.Grade }
func (s *SystemSettings) OpenSchool() bool   { return s.ProfileEdit == nil || s.ProfileEdit.School }
func (s *SystemSettings) OpenRegion() bool   { return s.ProfileEdit == nil || s.ProfileEdit.Region }

const (
	DefaultRateLimitPerMinute   = 3
	DefaultDataDownloadDayLimit = 5
	DefaultAutocompleteMinLevel = 4
	DefaultAllowRegister        = true
	DefaultBatchImportProblemMB = 100  // 单题数据 100MB
	DefaultBatchImportZipMB     = 2048 // 整包 zip 2GB

	// 讨论区（v0.24）默认值。
	DefaultDiscussTopicDailyLimit = 5 // 主楼每日上限
	DefaultDiscussReplyInterval   = 30 // 回复间隔秒
	DefaultDiscussNewbieDays      = 7  // 新号冷静期（天）
	DefaultDiscussReportFold      = 3  // 举报折叠阈值
	DefaultNotifyRetentionDays    = 90 // 通知保留天数
	DefaultNotifyBroadcastPerDay  = 1  // 广播每日上限

	MailChannelLog      = "log"
	MailChannelSMTP     = "smtp"
	RegisterVerifyNone  = "none"
	RegisterVerifyEmail = "email"

	// 玉经济体（v0.33）默认值。
	DefaultCurrencyName           = "玉"
	DefaultJadeAwardRatePct       = 50
	DefaultJadeSolutionUnlockMult = 5
	DefaultJadeBountyMin          = 5
	DefaultJadeBountyMax          = 500
	// 连击玉加成/复仇窗口（v0.45）默认值。
	DefaultJadeComboStep    = 10  // 每满 10 连击一档
	DefaultJadeComboPct     = 10  // 每档 +10%
	DefaultJadeComboCapPct  = 100 // 加成封顶 +100%（×2.0）
	DefaultJadeRevengeHours = 24  // 复仇需最早错误距今 ≥24h
	// AI 计费（v0.33-③）默认值。
	DefaultAiDailyLimit      = 5
	DefaultAiStaffDailyLimit = 20 // 教师/管理员每日上限（0 = 不限；root 恒不限）

	// AI 提示/找 bug 定价锚（2026-10-30 拍板写死）：跟随题解解锁价 S = D × 解锁系数。
	// 两级提示合计 = S×70%（向下取整，保底 2 玉），理解:思路 = 1:2 拆分；找 bug = S×90%。
	// 等级折扣照常相乘、同样向下取整；任何单价下限 1 玉。原 hint1/hint2/debug 基准价
	// 设置字段退役（保留不读写，零迁移，同 hint3 先例）。
	AiHintTotalPct = 70
	AiDebugPct     = 90

	DefaultAiHintPrice1 = 2 // 已退役（上表公式接管），字段保留不读写
	DefaultAiHintPrice2 = 6 // 已退役
	DefaultAiDebugPrice = 8 // 已退役
	DefaultAiTutorName  = "小玉"
)

func downloadLimitPtr(v int) *int { return &v }
func boolPtr(v bool) *bool        { return &v }
func int64Ptr(v int64) *int64     { return &v }

// DownloadDailyLimit 限额取值：未设置回默认（存量 settings 文档无此字段时为 nil）。
func (s *SystemSettings) DownloadDailyLimit() int {
	if s.DataDownloadDailyLimit == nil || *s.DataDownloadDailyLimit < 0 {
		return DefaultDataDownloadDayLimit
	}
	return *s.DataDownloadDailyLimit
}

// settingsCacheTTL 系统设置缓存有效期：设置变更的最长可见延迟（Update 走写穿失效，
// 实际延迟仅剩「另一个进程改库」这种不存在的路径；TTL 只是兜底）。
const settingsCacheTTL = 5 * time.Second

// GetSystemSettings 读取系统设置，未初始化时返回默认值。
// v0.37 进程缓存：提交/讨论列表/AI 计费等热路径每请求一次点查，5s 缓存全省；
// 返回 clone（bson round-trip 深拷贝，指针字段随加随对，调用方改动不伤缓存）。
func (r *Repo) GetSystemSettings(ctx context.Context) (*SystemSettings, error) {
	r.setMu.RLock()
	cached := r.setCache
	fresh := cached != nil && time.Since(r.setAt) < settingsCacheTTL
	r.setMu.RUnlock()
	if fresh {
		return cloneSystemSettings(cached), nil
	}
	s, err := r.loadSystemSettings(ctx)
	if err != nil {
		return nil, err
	}
	r.setMu.Lock()
	r.setCache, r.setAt = s, time.Now()
	r.setMu.Unlock()
	return cloneSystemSettings(s), nil
}

func (r *Repo) loadSystemSettings(ctx context.Context) (*SystemSettings, error) {
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
	s.normalizeDiscuss()
	return &s, nil
}

// cloneSystemSettings 深拷贝（bson round-trip）：结构体指针字段十余个且会随版本增，
// 手写逐字段拷贝必漏；round-trip 对该结构幂等且微秒级。
func cloneSystemSettings(s *SystemSettings) *SystemSettings {
	raw, err := bson.Marshal(s)
	if err != nil {
		return s // 理论不可达（纯数据结构），兜底退回原值
	}
	var out SystemSettings
	if err := bson.Unmarshal(raw, &out); err != nil {
		return s
	}
	return &out
}

// normalizeDiscuss 讨论区设置兜底（存量文档缺字段回默认）。
func (s *SystemSettings) normalizeDiscuss() {
	if s.DiscussGlobalEnabled == nil {
		s.DiscussGlobalEnabled = boolPtr(true)
	}
	if s.DiscussProblemEnabled == nil {
		s.DiscussProblemEnabled = boolPtr(true)
	}
	if s.DiscussContestEnabled == nil {
		s.DiscussContestEnabled = boolPtr(true)
	}
	if s.DiscussTopicDailyLimit == nil || *s.DiscussTopicDailyLimit < 0 {
		s.DiscussTopicDailyLimit = intPtr(DefaultDiscussTopicDailyLimit)
	}
	if s.DiscussReplyIntervalSec == nil || *s.DiscussReplyIntervalSec < 0 {
		s.DiscussReplyIntervalSec = intPtr(DefaultDiscussReplyInterval)
	}
	if s.DiscussNewbieDays == nil || *s.DiscussNewbieDays < 0 {
		s.DiscussNewbieDays = intPtr(DefaultDiscussNewbieDays)
	}
	if s.DiscussReportFold == nil || *s.DiscussReportFold < 0 {
		s.DiscussReportFold = intPtr(DefaultDiscussReportFold)
	}
	if s.NotifyRetentionDays == nil || *s.NotifyRetentionDays <= 0 {
		s.NotifyRetentionDays = intPtr(DefaultNotifyRetentionDays)
	}
	if s.NotifyBroadcastPerDay == nil || *s.NotifyBroadcastPerDay < 0 {
		s.NotifyBroadcastPerDay = intPtr(DefaultNotifyBroadcastPerDay)
	}
	if s.UserSeqStart == nil || *s.UserSeqStart < 1 || *s.UserSeqStart > 1_000_000_000 {
		v := int(DefaultUserSeqStart)
		s.UserSeqStart = &v
	}
	if s.ProblemSeqStart == nil || *s.ProblemSeqStart < 1 || *s.ProblemSeqStart > 1_000_000_000 {
		v := int(DefaultProblemSeqStart)
		s.ProblemSeqStart = &v
	}
	if s.CurrencyName == "" {
		s.CurrencyName = DefaultCurrencyName
	}
	if s.JadeAwardRatePct == nil || *s.JadeAwardRatePct < 1 || *s.JadeAwardRatePct > 100 {
		s.JadeAwardRatePct = intPtr(DefaultJadeAwardRatePct)
	}
	if s.JadeSolutionUnlockMult == nil || *s.JadeSolutionUnlockMult < 1 || *s.JadeSolutionUnlockMult > 100 {
		s.JadeSolutionUnlockMult = intPtr(DefaultJadeSolutionUnlockMult)
	}
	if s.JadeBountyMin == nil || *s.JadeBountyMin < 1 {
		s.JadeBountyMin = intPtr(DefaultJadeBountyMin)
	}
	if s.JadeBountyMax == nil || *s.JadeBountyMax < *s.JadeBountyMin {
		s.JadeBountyMax = intPtr(DefaultJadeBountyMax)
	}
	if s.JadeShopEnabled == nil {
		s.JadeShopEnabled = boolPtr(false)
	}
	if s.JadeComboStep == nil || *s.JadeComboStep < 1 || *s.JadeComboStep > 1000 {
		s.JadeComboStep = intPtr(DefaultJadeComboStep)
	}
	if s.JadeComboPct == nil || *s.JadeComboPct < 1 || *s.JadeComboPct > 100 {
		s.JadeComboPct = intPtr(DefaultJadeComboPct)
	}
	if s.JadeComboCapPct == nil || *s.JadeComboCapPct < 0 || *s.JadeComboCapPct > 900 {
		s.JadeComboCapPct = intPtr(DefaultJadeComboCapPct)
	}
	if s.JadeRevengeHours == nil || *s.JadeRevengeHours < 1 || *s.JadeRevengeHours > 24*30 {
		s.JadeRevengeHours = intPtr(DefaultJadeRevengeHours)
	}
	if s.AiDailyLimit == nil || *s.AiDailyLimit < 1 || *s.AiDailyLimit > 100 {
		s.AiDailyLimit = intPtr(DefaultAiDailyLimit)
	}
	// 特权层（教师/管理员）：0 = 不限，上界放宽到 1000
	if s.AiStaffDailyLimit == nil || *s.AiStaffDailyLimit < 0 || *s.AiStaffDailyLimit > 1000 {
		s.AiStaffDailyLimit = intPtr(DefaultAiStaffDailyLimit)
	}
	if s.AiHintPrice1 == nil || *s.AiHintPrice1 < 0 {
		s.AiHintPrice1 = int64Ptr(DefaultAiHintPrice1)
	}
	if s.AiHintPrice2 == nil || *s.AiHintPrice2 < 0 {
		s.AiHintPrice2 = int64Ptr(DefaultAiHintPrice2)
	}
	if s.AiDebugPrice == nil || *s.AiDebugPrice < 0 {
		s.AiDebugPrice = int64Ptr(DefaultAiDebugPrice)
	}
}

// 讨论区开关取值（nil 已在读取时兜底，防御直接构造的零值结构体）。
func (s *SystemSettings) DiscussGlobalOpen() bool {
	return s.DiscussGlobalEnabled == nil || *s.DiscussGlobalEnabled
}
func (s *SystemSettings) DiscussProblemOpen() bool {
	return s.DiscussProblemEnabled == nil || *s.DiscussProblemEnabled
}
func (s *SystemSettings) DiscussContestOpen() bool {
	return s.DiscussContestEnabled == nil || *s.DiscussContestEnabled
}

func (s *SystemSettings) DiscussTopicDailyLimitVal() int {
	if s.DiscussTopicDailyLimit == nil || *s.DiscussTopicDailyLimit < 0 {
		return DefaultDiscussTopicDailyLimit
	}
	return *s.DiscussTopicDailyLimit
}

func (s *SystemSettings) DiscussReplyIntervalVal() int {
	if s.DiscussReplyIntervalSec == nil || *s.DiscussReplyIntervalSec < 0 {
		return DefaultDiscussReplyInterval
	}
	return *s.DiscussReplyIntervalSec
}

func (s *SystemSettings) DiscussNewbieDaysVal() int {
	if s.DiscussNewbieDays == nil || *s.DiscussNewbieDays < 0 {
		return DefaultDiscussNewbieDays
	}
	return *s.DiscussNewbieDays
}

func (s *SystemSettings) DiscussReportFoldVal() int64 {
	if s.DiscussReportFold == nil || *s.DiscussReportFold < 0 {
		return DefaultDiscussReportFold
	}
	return int64(*s.DiscussReportFold)
}

func (s *SystemSettings) NotifyRetentionDaysVal() int {
	if s.NotifyRetentionDays == nil || *s.NotifyRetentionDays <= 0 {
		return DefaultNotifyRetentionDays
	}
	return *s.NotifyRetentionDays
}

func (s *SystemSettings) NotifyBroadcastPerDayVal() int {
	if s.NotifyBroadcastPerDay == nil || *s.NotifyBroadcastPerDay < 0 {
		return DefaultNotifyBroadcastPerDay
	}
	return *s.NotifyBroadcastPerDay
}

// UserSeqStartVal 注册取号起点（默认 100；越界回默认）。
func (s *SystemSettings) UserSeqStartVal() int64 {
	if s.UserSeqStart == nil || *s.UserSeqStart < 1 || *s.UserSeqStart > 1_000_000_000 {
		return DefaultUserSeqStart
	}
	return int64(*s.UserSeqStart)
}

// ProblemSeqStartVal 自动题号起点（默认 1001；越界回默认）。
func (s *SystemSettings) ProblemSeqStartVal() int64 {
	if s.ProblemSeqStart == nil || *s.ProblemSeqStart < 1 || *s.ProblemSeqStart > 1_000_000_000 {
		return DefaultProblemSeqStart
	}
	return int64(*s.ProblemSeqStart)
}

// CurrencyNameVal 货币名（空回默认「玉」；长度在后台保存入口校验，读侧兜底截断）。
func (s *SystemSettings) CurrencyNameVal() string {
	if s.CurrencyName == "" {
		return DefaultCurrencyName
	}
	if runes := []rune(s.CurrencyName); len(runes) > 6 {
		return DefaultCurrencyName
	}
	return s.CurrencyName
}

// DefaultJadeDiscount 玉折扣阶梯默认（Lv5=九五折 → Lv10=七折，v0.40 拍板六档逐级）。
var DefaultJadeDiscount = [6]int{95, 90, 85, 80, 75, 70}

// normalizeJadeDiscount 折扣归一：逐档夹取 [10,100]，并保证高等级不高于低等级
// （违者把高档提到低档值——折扣只能随等级加深）。
func normalizeJadeDiscount(d [6]int) [6]int {
	for i := range d {
		if d[i] < 10 || d[i] > 100 {
			d[i] = DefaultJadeDiscount[i]
		}
	}
	for i := 4; i >= 0; i-- {
		if d[i] < d[i+1] {
			d[i] = d[i+1]
		}
	}
	return d
}

// JadeDiscountPct 玉折扣单点收口（v0.40）：返回该等级的折扣百分比（100 = 不折）。
// 消费面仅题解解锁与 AI 两能；调用方按 ceil(base*pct/100) 计折后价。教师等免费群体
// 不经过此函数（0 价直通）。
func (s *SystemSettings) JadeDiscountPct(level int) int {
	if level < 5 || level > 10 {
		return 100
	}
	d := normalizeJadeDiscount(s.JadeDiscount)
	return d[level-5]
}

// JadeAwardRatePctVal 比赛奖池获奖比例（越界回默认 50）。
func (s *SystemSettings) JadeAwardRatePctVal() int {
	if s.JadeAwardRatePct == nil || *s.JadeAwardRatePct < 1 || *s.JadeAwardRatePct > 100 {
		return DefaultJadeAwardRatePct
	}
	return *s.JadeAwardRatePct
}

// JadeComboCfg 连击玉加成三参（step/pct/capPct，越界回默认）。
func (s *SystemSettings) JadeComboCfg() (step, pct, capPct int) {
	if s.JadeComboStep == nil || *s.JadeComboStep < 1 || *s.JadeComboStep > 1000 {
		step = DefaultJadeComboStep
	} else {
		step = *s.JadeComboStep
	}
	if s.JadeComboPct == nil || *s.JadeComboPct < 1 || *s.JadeComboPct > 100 {
		pct = DefaultJadeComboPct
	} else {
		pct = *s.JadeComboPct
	}
	if s.JadeComboCapPct == nil || *s.JadeComboCapPct < 0 || *s.JadeComboCapPct > 900 {
		capPct = DefaultJadeComboCapPct
	} else {
		capPct = *s.JadeComboCapPct
	}
	return
}

// JadeComboBonusPct 连击 N 对应的倍率百分比（100 = 无加成；不足一档无加成）。
func (s *SystemSettings) JadeComboBonusPct(combo int) int {
	step, pct, capPct := s.JadeComboCfg()
	if combo < step {
		return 100
	}
	add := (combo / step) * pct
	if add > capPct {
		add = capPct
	}
	return 100 + add
}

// JadeRevengeHoursVal 每日复仇窗口小时（默认 24）。
func (s *SystemSettings) JadeRevengeHoursVal() int {
	if s.JadeRevengeHours == nil || *s.JadeRevengeHours < 1 || *s.JadeRevengeHours > 24*30 {
		return DefaultJadeRevengeHours
	}
	return *s.JadeRevengeHours
}

// JadeSolutionUnlockMultVal 题解解锁价系数（难度 × 系数；越界回默认 5）。
func (s *SystemSettings) JadeSolutionUnlockMultVal() int {
	if s.JadeSolutionUnlockMult == nil || *s.JadeSolutionUnlockMult < 1 || *s.JadeSolutionUnlockMult > 100 {
		return DefaultJadeSolutionUnlockMult
	}
	return *s.JadeSolutionUnlockMult
}

// JadeBountyMinVal / JadeBountyMaxVal 悬赏额上下限（越界回默认 5 / 500）。
func (s *SystemSettings) JadeBountyMinVal() int {
	if s.JadeBountyMin == nil || *s.JadeBountyMin < 1 {
		return DefaultJadeBountyMin
	}
	return *s.JadeBountyMin
}

func (s *SystemSettings) JadeBountyMaxVal() int {
	if s.JadeBountyMax == nil || *s.JadeBountyMax < s.JadeBountyMinVal() {
		return DefaultJadeBountyMax
	}
	return *s.JadeBountyMax
}

// JadeShopOpen 实物兑换模块是否开放（默认关）。
func (s *SystemSettings) JadeShopOpen() bool {
	return s.JadeShopEnabled != nil && *s.JadeShopEnabled
}

// ---- AI 计费取值（v0.33-③）：难度倍率 × 等级折扣，向上取整 ----

func (s *SystemSettings) AiDailyLimitVal() int {
	if s.AiDailyLimit == nil || *s.AiDailyLimit < 1 || *s.AiDailyLimit > 100 {
		return DefaultAiDailyLimit
	}
	return *s.AiDailyLimit
}

// AiStaffDailyLimitVal 特权层（教师/管理员）每日上限；0 = 不限（读侧兜底同 normalize）。
func (s *SystemSettings) AiStaffDailyLimitVal() int {
	if s.AiStaffDailyLimit == nil || *s.AiStaffDailyLimit < 0 || *s.AiStaffDailyLimit > 1000 {
		return DefaultAiStaffDailyLimit
	}
	return *s.AiStaffDailyLimit
}

func (s *SystemSettings) AiHintPrice1Val() int64 {
	if s.AiHintPrice1 == nil || *s.AiHintPrice1 < 0 {
		return DefaultAiHintPrice1
	}
	return *s.AiHintPrice1
}

func (s *SystemSettings) AiHintPrice2Val() int64 {
	if s.AiHintPrice2 == nil || *s.AiHintPrice2 < 0 {
		return DefaultAiHintPrice2
	}
	return *s.AiHintPrice2
}

func (s *SystemSettings) AiDebugPriceVal() int64 {
	if s.AiDebugPrice == nil || *s.AiDebugPrice < 0 {
		return DefaultAiDebugPrice
	}
	return *s.AiDebugPrice
}

// AiTutorNameVal AI 助教名字（空/超长回默认「小玉」）。
func (s *SystemSettings) AiTutorNameVal() string {
	if s.AiTutorName == "" {
		return DefaultAiTutorName
	}
	if runes := []rune(s.AiTutorName); len(runes) > 12 {
		return DefaultAiTutorName
	}
	return s.AiTutorName
}

// AiPrice AI 单次定价（2026-10-30 拍板：写死锚定题解解锁价，全向下取整）。
//
//	题解解锁价 S = 难度 × 解锁系数（后台可调，默认 5）
//	两级提示合计 = S×70%（向下取整，保底 2 玉），理解:思路 = 1:2 拆分
//	找 bug = S×90%（向下取整）
//	任何单价下限 1 玉。
func (s *SystemSettings) AiPrice(kind string, difficulty, level int) int64 {
	solution := int64(difficulty) * int64(s.JadeSolutionUnlockMultVal())
	var price int64
	switch kind {
	case "hint1", "hint2":
		total := solution * AiHintTotalPct / 100
		if total < 2 {
			total = 2 // 保底两级各 ≥1 玉
		}
		p1 := total / 3
		if p1 < 1 {
			p1 = 1
		}
		if kind == "hint1" {
			price = p1
		} else {
			price = total - p1
		}
	default: // debug
		price = solution * AiDebugPct / 100
	}
	// 玉折扣阶梯（v0.40）：Lv5–10 消费折扣（向上取整防折穿 1 玉；level<5 = 不折）
	if pct := s.JadeDiscountPct(level); pct < 100 {
		price = (price*int64(pct) + 99) / 100
	}
	if price < 1 {
		price = 1
	}
	return price
}

func intPtr(v int) *int { return &v }

// UpdateSystemSettings 更新系统设置（文档不存在时创建）。
func (r *Repo) UpdateSystemSettings(ctx context.Context, s *SystemSettings) error {
	col := r.db.Collection("settings")
	// 确保文档存在（UpdateOne 不带 upsert 时匹配不到会静默无效）
	if _, err := col.InsertOne(ctx, bson.M{"_id": "system"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	comboStep, comboPct, comboCap := s.JadeComboCfg()
	_, err := col.UpdateOne(ctx,
		bson.M{"_id": "system"},
		bson.M{"$set": bson.M{
			"rateLimitPerMinute":      s.RateLimitPerMinute,
			"homeNotice":              s.HomeNotice,
			"homeNoticeHtml":          mdrender.Render(s.HomeNotice), // 写入时渲染（v0.53 渲染收口）
			"dataDownloadDailyLimit":  s.DownloadDailyLimit(),
			"autocompleteMinLevel":    s.MinAutocompleteLevel(),
			"allowRegister":           s.RegistrationOpen(),
			"icpNumber":               s.IcpNumber,
			"mpsNumber":               s.MpsNumber,
			"batchImportProblemMB":    s.BatchImportProblemMBVal(),
			"batchImportZipMB":        s.BatchImportZipMBVal(),
			"mailChannel":             s.MailChannel,
			"smtp":                    s.SMTP,
			"registerVerifyMode":      s.RegisterVerifyMode,
			"passwordResetEnabled":    s.PasswordResetOpen(),
			"discussGlobalEnabled":    s.DiscussGlobalOpen(),
			"discussProblemEnabled":   s.DiscussProblemOpen(),
			"discussContestEnabled":   s.DiscussContestOpen(),
			"discussGlobalLocked":     s.DiscussGlobalLocked,
			"discussTopicDailyLimit":  s.DiscussTopicDailyLimitVal(),
			"discussReplyIntervalSec": s.DiscussReplyIntervalVal(),
			"discussNewbieDays":       s.DiscussNewbieDaysVal(),
			"discussReportFold":       int(s.DiscussReportFoldVal()),
			"notifyRetentionDays":     s.NotifyRetentionDaysVal(),
			"notifyBroadcastPerDay":   s.NotifyBroadcastPerDayVal(),
			"userSeqStart":            s.UserSeqStartVal(),
			"problemSeqStart":         s.ProblemSeqStartVal(),
			"currencyName":            s.CurrencyNameVal(),
			"jadeAwardRatePct":        s.JadeAwardRatePctVal(),
			"jadeSolutionUnlockMult":  s.JadeSolutionUnlockMultVal(),
			"jadeBountyMin":           s.JadeBountyMinVal(),
			"jadeBountyMax":           s.JadeBountyMaxVal(),
			"jadeShopEnabled":         s.JadeShopOpen(),
			"jadeDiscount":            normalizeJadeDiscount(s.JadeDiscount),
			"jadeComboStep":           comboStep,
			"jadeComboPct":            comboPct,
			"jadeComboCapPct":         comboCap,
			"jadeRevengeHours":        s.JadeRevengeHoursVal(),
			"aiDailyLimit":            s.AiDailyLimitVal(),
			"aiStaffDailyLimit":       s.AiStaffDailyLimitVal(), // 特权层每日上限（v0.33-④ 特权分层；曾漏登本清单导致保存不生效）
			"aiHintPrice1":            s.AiHintPrice1Val(),
			"aiHintPrice2":            s.AiHintPrice2Val(),
			"aiDebugPrice":            s.AiDebugPriceVal(),
			"aiTutorName":             s.AiTutorNameVal(),
			// 资料自改开关（v0.34）：归一后整组覆盖；gradePromoYear 不在此处——晋级逻辑独占
			"profileEdit": s.ProfileEditCfg(),
		},
			// v0.48 冷静期单位改天换血：顺手清掉老的小时字段（不存在时 $unset 为空操作）
			"$unset": bson.M{"discussNewbieHours": ""},
		},
	)
	if err == nil {
		r.setMu.Lock()
		r.setCache = nil // 写穿失效（v0.37 缓存配套）
		r.setMu.Unlock()
	}
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
