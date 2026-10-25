package repo

import (
	"context"
	"errors"
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
}

type User struct {
	ID           int64      `bson:"_id"`
	Username     string     `bson:"uname"`
	PasswordHash string     `bson:"pwdHash"`
	Nickname     string     `bson:"nickname"`
	Role         string     `bson:"role"`
	CreatedAt    time.Time  `bson:"createdAt"`
	Gender       string     `bson:"gender,omitempty"` // 性别/学校/年级/QQ：本人自助修改，他人不可见
	School       string     `bson:"school,omitempty"`
	Grade        string     `bson:"grade,omitempty"`
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
}

// UpdatePassword 修改密码：写新哈希并 pwdVer+1（原子），所有旧 token 随即失效。
func (r *Repo) UpdatePassword(ctx context.Context, uid int64, passwordHash string) error {
	res, err := r.Users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{
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
	_, err := r.Users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$inc": bson.M{"pwdVer": 1}})
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
	err = r.Users.FindOneAndUpdate(ctx, bson.M{"_id": uid}, mongo.Pipeline{
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
	_, err := r.Users.UpdateOne(ctx, bson.M{"_id": uid},
		bson.M{"$set": bson.M{"failedCount": 0, "lockedUntil": time.Time{}}})
	return err
}

func (r *Repo) CreateUser(ctx context.Context, username, passwordHash, nickname, role string) (int64, error) {
	uid, err := r.nextSeq(ctx, "user")
	if err != nil {
		return 0, err
	}
	_, err = r.Users.InsertOne(ctx, bson.M{
		"_id": uid, "uname": username, "pwdHash": passwordHash,
		"nickname": nickname, "role": role, "createdAt": time.Now(),
	})
	if err != nil {
		return 0, err
	}
	return uid, nil
}

// CreateUserWithEmail 注册建号（v0.16）：邮箱验证码注册路径——验证通过后带邮箱落库。
func (r *Repo) CreateUserWithEmail(ctx context.Context, username, password, nickname, role, email string) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	uid, err := r.nextSeq(ctx, "user")
	if err != nil {
		return 0, err
	}
	_, err = r.Users.InsertOne(ctx, bson.M{
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
	err := r.Users.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SetUserEmail 绑定/换绑邮箱（验证通过后调用）。换绑成功前旧值已被读方留存用于通知。
func (r *Repo) SetUserEmail(ctx context.Context, uid int64, email string) error {
	res, err := r.Users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$set": bson.M{"email": email}})
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
	res, err := r.Users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$unset": bson.M{"email": ""}})
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
	err := r.Users.FindOne(ctx, bson.M{"uname": username}).Decode(&u)
	if err != nil {
		return nil, "", err
	}
	return &u, u.PasswordHash, nil
}

func (r *Repo) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := r.Users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
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
	cursor, err := r.Users.Find(ctx,
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
	cursor, err := r.Users.Find(ctx, filter,
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
	if _, err := r.Submissions.UpdateMany(ctx,
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
	total, err := r.Users.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.Users.Find(ctx, filter,
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
	res, err := r.Users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
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
		if _, err := r.Users.UpdateMany(ctx,
			bson.M{"role": m["from"]},
			bson.M{"$set": bson.M{"role": m["to"]}}); err != nil {
			return err
		}
	}
	return nil
}
