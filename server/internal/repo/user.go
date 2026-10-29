package repo

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/auth"
)

// 全局角色（见 DEVELOPMENT.md 权限系统）：决定能做什么。
// guest 即未登录，不落库。
const (
	RoleDefault = "default" // 默认权限：提交代码、加入班级
	RoleAdmin   = "admin"   // 班级级管理：建题/建班/管班/办赛/看全部提交/重测
	RoleRoot    = "root"    // 系统级管理：建用户、封禁、全局管理
)

// ValidRole 合法性改为查 roles 集合（见 role.go）；此处只保留角色常量与初始管理员保护。

// InitialAdminID 初始管理员（Bootstrap 创建的 admin 账号）固定 ID；
// 其 root 角色受保护不可修改，防止误降权把自己锁在 root 之外。
const InitialAdminID int64 = 1

// UserPrefs 用户偏好（v0.4.8 个人设置）：跟随账号存储，前端消费。
// CodeTemplates 按语言各存一套（key 为判题语言 id）；空串表示该语言用系统默认模板。
type UserPrefs struct {
	DefaultLang   string            `bson:"defaultLang,omitempty" json:"defaultLang,omitempty"`
	CodeTemplates map[string]string `bson:"codeTemplates,omitempty" json:"codeTemplates,omitempty"`
	Autocomplete  *bool             `bson:"autocomplete,omitempty" json:"autocomplete,omitempty"` // 代码自动补全（v0.5.3，默认开箱开启；后续按用户等级门槛放开——预留接口）
	CodeFont      string            `bson:"codeFont,omitempty" json:"codeFont,omitempty"`         // 代码编辑器字体：Consolas / Menlo / Monaco，空为 Consolas
	CodeFontSize  int               `bson:"codeFontSize,omitempty" json:"codeFontSize,omitempty"` // 编辑器字号 px（12-48），0 为默认 15
	MdEditMode    string            `bson:"mdEditMode,omitempty" json:"mdEditMode,omitempty"`     // Markdown 编辑器默认模式：wysiwyg / ir / sv，空为 wysiwyg（v0.8.1）
	MdTheme       string            `bson:"mdTheme,omitempty" json:"mdTheme,omitempty"`           // Markdown 内容主题：light / ant-design / dark / wechat，空为 light（v0.8.1）
	// 通知邮件转发（v0.24）：开通的类型列表（NotifyMailCategories 子集），空 = 全关（默认）。
	// 仅 1:1 互动类支持邮件；发布/广播类是扇出，不做邮件。
	NotifyEmail []string `bson:"notifyEmail,omitempty" json:"notifyEmail,omitempty"`
}

type User struct {
	ID           int64      `bson:"_id"`
	Username     string     `bson:"uname"`
	PasswordHash string     `bson:"pwdHash"`
	Nickname     string     `bson:"nickname"`
	Role         string     `bson:"role"`
	CreatedAt    time.Time  `bson:"createdAt"`
	Gender       string     `bson:"gender,omitempty"` // 性别/学校/年级/地区/QQ：本人自助修改
	School       string     `bson:"school,omitempty"`
	Grade        string     `bson:"grade,omitempty"`
	Region       string     `bson:"region,omitempty"` // 地区（v0.23）：省市等自由文本，公开主页按开关展示
	QQ           string     `bson:"qq,omitempty"`
	Prefs        *UserPrefs `bson:"prefs,omitempty"`
	// 密码版本（v0.14）：改密/重置 +1，JWT claims 携带签发时的版本，不一致即 401——
	// 改密后所有旧 token 立即失效（全端下线）。存量文档缺字段按 0。
	PwdVer int `bson:"pwdVer,omitempty"`
	// 登录失败锁定（v0.14）：连续失败达到阈值锁 lockedUntil；落库重启不丢、管理端可见。
	FailedCount int       `bson:"failedCount,omitempty"`
	LockedUntil time.Time `bson:"lockedUntil,omitempty"`
	// 绑定邮箱/手机（v0.16）：邮箱经注册验证码/绑定流程验证后落库（trim+小写归一）；
	// phone 一并落库但 UI 不启用，等短信通道接入。sparse 唯一索引：空值不占号、非空必唯一。
	Email string `bson:"email,omitempty"`
	Phone string `bson:"phone,omitempty"`
	// 资料公开开关（v0.23）：逐字段控制对外主页可见性；QQ 头像绑定独立于 QQ 号可见性
	ProfileVis *ProfileVis `bson:"profileVis,omitempty"`
	QQAvatar   bool        `bson:"qqAvatar,omitempty"`
}

// ProfileVis 个人资料对外可见开关：true = 对外公开（公开主页可见），false = 仅自己可见。
// json 标签必须显式小写：gin.H 响应走 encoding/json，无标签会序列化成大写字段名
//（2026-10-27 实锤：设置页回填读到 undefined，开关保存后「看起来被还原」）。
type ProfileVis struct {
	Gender bool `bson:"gender" json:"gender"`
	School bool `bson:"school" json:"school"`
	Grade  bool `bson:"grade" json:"grade"`
	// 地区（v0.23 追加）：指针语义 —— nil = 该键从未写过（本卡上线当天前保存的存量开关文档），
	// 按公开兜底（RegionPublic）；显式 false 才是「仅自己可见」。json 下发 null，前端按非 false 判公开。
	Region *bool `bson:"region,omitempty" json:"region"`
	QQ     bool  `bson:"qq" json:"qq"`
}

// RegionPublic 地区是否对外可见：未写过（nil）视同公开，与学校/年级默认口径一致。
func (v ProfileVis) RegionPublic() bool { return v.Region == nil || *v.Region }

// EffProfileVis 生效开关：存量文档未配置时沿用历史口径——性别/学校/年级/地区公开、QQ 私密。
func (u *User) EffProfileVis() ProfileVis {
	if u.ProfileVis != nil {
		return *u.ProfileVis
	}
	return ProfileVis{Gender: true, School: true, Grade: true, QQ: false}
}

// UpdatePassword 修改密码：写新哈希并 pwdVer+1（原子），所有旧 token 随即失效。
func (r *Repo) UpdatePassword(ctx context.Context, uid int64, passwordHash string) error {
	res, err := r.users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{
		"$set": bson.M{"pwdHash": passwordHash},
		"$inc": bson.M{"pwdVer": 1},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// BumpPwdVer 仅递增密码版本（管理员重置密码走 UpdateUser 裸 $set 后补一刀，v0.14）。
func (r *Repo) BumpPwdVer(ctx context.Context, uid int64) error {
	_, err := r.users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$inc": bson.M{"pwdVer": 1}})
	return err
}

// RecordLoginFailure 记一次登录失败（原子聚合更新）：failedCount+1；达到 maxFail 时
// 计数清零并锁定 lockDur。返回更新后的连续失败次数（锁定触发时为 0）与是否处于锁定。
// ① 锁定时间在 Go 侧算好以字面量传入——更新管道里 $$NOW 不可用（mongo 7 实测报
// "undefined variable: now"）；② 存量用户文档没有 failedCount 字段，$add 缺失字段
// 得 null 且比较恒 false，必须 $ifNull 兜底为 0——两处都曾让锁定整体失效，
// 回归测试见 security_test.go。
func (r *Repo) RecordLoginFailure(ctx context.Context, uid int64, maxFail int, lockDur time.Duration) (failedCount int, locked bool, err error) {
	newCount := bson.D{{Key: "$add", Value: bson.A{
		bson.D{{Key: "$ifNull", Value: bson.A{"$failedCount", 0}}}, 1,
	}}}
	hitMax := bson.D{{Key: "$gte", Value: bson.A{newCount, maxFail}}}
	lockedAt := time.Now().Add(lockDur)
	var u User
	err = r.users.FindOneAndUpdate(ctx, bson.M{"_id": uid}, mongo.Pipeline{
		{{Key: "$set", Value: bson.D{
			{Key: "failedCount", Value: bson.D{{Key: "$cond", Value: bson.A{hitMax, 0, newCount}}}},
			{Key: "lockedUntil", Value: bson.D{{Key: "$cond", Value: bson.A{
				hitMax,
				lockedAt,
				"$lockedUntil",
			}}}},
		}}},
	}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&u)
	if err != nil {
		return 0, false, err
	}
	return u.FailedCount, u.LockedUntil.After(time.Now()), nil
}

// ClearLoginFailures 登录成功后清零失败计数与锁定时间（best-effort，失败不阻断登录）。
func (r *Repo) ClearLoginFailures(ctx context.Context, uid int64) error {
	_, err := r.users.UpdateOne(ctx, bson.M{"_id": uid},
		bson.M{"$set": bson.M{"failedCount": 0, "lockedUntil": time.Time{}}})
	return err
}

func (r *Repo) CreateUser(ctx context.Context, username, passwordHash, nickname, role string) (int64, error) {
	uid, err := r.nextUserSeq(ctx)
	if err != nil {
		return 0, err
	}
	_, err = r.users.InsertOne(ctx, bson.M{
		"_id": uid, "uname": username, "pwdHash": passwordHash,
		"nickname": nickname, "role": role, "createdAt": time.Now(),
	})
	if err != nil {
		return 0, err
	}
	return uid, nil
}

// DefaultUserSeqStart 注册取号起点默认值（后台「注册取号起点」可调，见 SystemSettings.UserSeqStart）。
const DefaultUserSeqStart int64 = 100

// nextUserSeq 自动分配用户 ID：计数器低于后台设定的「注册取号起点」时垫到 start-1 重取
// ——管理员运行期调大起点即时生效（首个注册号 = 起点）；计数器已超起点（或起点调小）
// 时从计数器继续，只前进不回退。手动指定 ID 走 CreateUserWithID，不经过此处。
func (r *Repo) nextUserSeq(ctx context.Context) (int64, error) {
	start := DefaultUserSeqStart
	if s, err := r.GetSystemSettings(ctx); err == nil {
		start = s.UserSeqStartVal()
	}
	uid, err := r.nextSeq(ctx, "user")
	if err != nil {
		return 0, err
	}
	if uid < start {
		var doc struct {
			Seq int64 `bson:"seq"`
		}
		if merr := r.counters.FindOneAndUpdate(ctx,
			bson.M{"_id": "user"},
			bson.M{"$max": bson.M{"seq": start - 1}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&doc); merr == nil {
			return r.nextSeq(ctx, "user")
		}
	}
	return uid, nil
}

// SeedUserCounter 注册取号起点守卫（Bootstrap 幂等快路径）：user 计数器 $max 到起点-1，
// 让首个注册免走 nextUserSeq 的垫高重取；运行期改动由 nextUserSeq 兜底，两者口径一致。
func (r *Repo) SeedUserCounter(ctx context.Context) error {
	start := DefaultUserSeqStart
	if s, err := r.GetSystemSettings(ctx); err == nil {
		start = s.UserSeqStartVal()
	}
	var before struct {
		Seq int64 `bson:"seq"`
	}
	err := r.counters.FindOneAndUpdate(ctx,
		bson.M{"_id": "user"},
		bson.M{"$max": bson.M{"seq": start - 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before),
	).Decode(&before)
	if IsNotFound(err) {
		return nil // 全新计数器，本次 $max 已写入起点-1
	}
	if err == nil && before.Seq < start-1 {
		slog.Info("user 计数器已垫高（注册取号起点）", "component", "boot", "from", before.Seq, "to", start-1, "start", start)
	}
	return err
}

// CreateUserWithID 管理端手动指定 ID 建号：落库后把 user 计数器 $max 到该 ID，
// 保证后续自增取号不会撞上手动占用的号（$max 幂等只会前进）。
func (r *Repo) CreateUserWithID(ctx context.Context, id int64, username, password, nickname, role string) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	_, err = r.users.InsertOne(ctx, bson.M{
		"_id": id, "uname": username, "pwdHash": hash,
		"nickname": nickname, "role": role, "createdAt": time.Now(),
	})
	if err != nil {
		return 0, err
	}
	var doc struct {
		Seq int64 `bson:"seq"`
	}
	if err := r.counters.FindOneAndUpdate(ctx,
		bson.M{"_id": "user"},
		bson.M{"$max": bson.M{"seq": id}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc); err != nil {
		return id, err
	}
	return id, nil
}

// CreateUserWithEmail 注册建号（v0.16）：邮箱验证码注册路径——验证通过后带邮箱落库。
func (r *Repo) CreateUserWithEmail(ctx context.Context, username, password, nickname, role, email string) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	uid, err := r.nextUserSeq(ctx)
	if err != nil {
		return 0, err
	}
	_, err = r.users.InsertOne(ctx, bson.M{
		"_id": uid, "uname": username, "pwdHash": hash,
		"nickname": nickname, "role": role, "createdAt": time.Now(),
		"email": email,
	})
	if err != nil {
		return 0, err
	}
	return uid, nil
}

// GetUserByEmail 按绑定邮箱查用户（找回密码/注册查重）。
func (r *Repo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SetUserEmail 绑定/换绑邮箱（验证通过后调用）。换绑成功前旧值已被读方留存用于通知。
func (r *Repo) SetUserEmail(ctx context.Context, uid int64, email string) error {
	res, err := r.users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$set": bson.M{"email": email}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// UnbindEmail 管理端解绑邮箱（root，学生换邮箱场景）。
func (r *Repo) UnbindEmail(ctx context.Context, uid int64) error {
	res, err := r.users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$unset": bson.M{"email": ""}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// GetUserByUsername 返回用户与密码哈希；未找到时返回 mongo.ErrNoDocuments。
func (r *Repo) GetUserByUsername(ctx context.Context, username string) (*User, string, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"uname": username}).Decode(&u)
	if err != nil {
		return nil, "", err
	}
	return &u, u.PasswordHash, nil
}

func (r *Repo) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := r.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUsersByIDs 批量取用户（榜单昵称补齐等场景）。
func (r *Repo) GetUsersByIDs(ctx context.Context, ids []int64) (map[int64]*User, error) {
	out := make(map[int64]*User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cursor, err := r.users.Find(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"pwdHash": 0}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var u User
		if err := cursor.Decode(&u); err != nil {
			return nil, err
		}
		out[u.ID] = &u
	}
	return out, cursor.Err()
}

// LookupUsers 按用户名/昵称子串模糊找账号（提交记录/所有代码的「选手」筛选选人用）：
// 大小写不敏感，按 _id 稳定排序，最多 limit 条。不返回密码哈希。
func (r *Repo) LookupUsers(ctx context.Context, kw string, limit int) ([]*User, error) {
	pat := regexp.QuoteMeta(kw)
	filter := bson.M{"$or": bson.A{
		bson.M{"uname": bson.M{"$regex": pat, "$options": "i"}},
		bson.M{"nickname": bson.M{"$regex": pat, "$options": "i"}},
	}}
	cursor, err := r.users.Find(ctx, filter,
		options.Find().SetProjection(bson.M{"pwdHash": 0}).
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := make([]*User, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateUserWithPassword 组合密码哈希与建号，供注册与引导使用。
func (r *Repo) CreateUserWithPassword(ctx context.Context, username, password, nickname, role string) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	return r.CreateUser(ctx, username, hash, nickname, role)
}

// SyncUnameReferences 改名后同步冗余存储的用户名：提交记录（提交列表与比赛榜单
// 共用其 uname 字段）与班级成员（成员列表展示）。引用键恒为 uid，这里只修展示名。
func (r *Repo) SyncUnameReferences(ctx context.Context, uid int64, uname string) error {
	if _, err := r.submissions.UpdateMany(ctx,
		bson.M{"uid": uid},
		bson.M{"$set": bson.M{"uname": uname}}); err != nil {
		return err
	}
	_, err := r.db.Collection("classmembers").UpdateMany(ctx,
		bson.M{"uid": uid},
		bson.M{"$set": bson.M{"uname": uname}})
	return err
}

func IsNotFound(err error) bool { return errors.Is(err, mongo.ErrNoDocuments) }

// ListUsers 管理端用户分页列表（不含密码哈希）；keyword 非空时按用户名/昵称模糊过滤，
// role 非空时只取该角色（管理后台按权限分组展示）。
func (r *Repo) ListUsers(ctx context.Context, page, pageSize int, keyword, role string) ([]*User, int64, error) {
	filter := bson.M{}
	if role != "" {
		filter["role"] = role
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		pattern := regexp.QuoteMeta(kw)
		filter["$or"] = bson.A{
			bson.M{"uname": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"nickname": bson.M{"$regex": pattern, "$options": "i"}},
		}
	}
	total, err := r.users.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.users.Find(ctx, filter,
		options.Find().
			SetProjection(bson.M{"pwdHash": 0}).
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetLimit(int64(pageSize)).
			SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*User, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// UpdateUser 管理端更新（set 中的字段由调用方校验，pwdHash 仅在重置密码时出现）。
func (r *Repo) UpdateUser(ctx context.Context, id int64, set bson.M) error {
	res, err := r.users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// UpdateNotifyEmailPrefs 通知邮件转发偏好（v0.24）：点路径 $set，prefs 子文档缺失时自动创建。
func (r *Repo) UpdateNotifyEmailPrefs(ctx context.Context, id int64, cats []string) error {
	if cats == nil {
		cats = []string{}
	}
	return r.UpdateUser(ctx, id, bson.M{"prefs.notifyEmail": cats})
}

// migrateRoles 一次性迁移：v0.3 之前的 user/teacher/admin 角色体系 → default/admin/root。
// 两阶段映射，避免顺序执行时 teacher→admin 再被 admin→root 重复提升。
// 完成标记由 runMigrations 统一管理。
func (r *Repo) migrateRoles(ctx context.Context) error {
	pairs := bson.A{
		bson.M{"from": "user", "to": "__tmp_default"},
		bson.M{"from": "teacher", "to": "__tmp_admin"},
		bson.M{"from": "admin", "to": "__tmp_root"},
		bson.M{"from": "__tmp_default", "to": RoleDefault},
		bson.M{"from": "__tmp_admin", "to": RoleAdmin},
		bson.M{"from": "__tmp_root", "to": RoleRoot},
	}
	for _, p := range pairs {
		m := p.(bson.M)
		if _, err := r.users.UpdateMany(ctx,
			bson.M{"role": m["from"]},
			bson.M{"$set": bson.M{"role": m["to"]}}); err != nil {
			return err
		}
	}
	return nil
}
