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
	MdEditMode    string            `bson:"mdEditMode,omitempty" json:"mdEditMode,omitempty"` // wysiwyg / ir / sv，空为默认所见即所得
	Autocomplete  *bool             `bson:"autocomplete,omitempty" json:"autocomplete,omitempty"` // 代码自动补全（v0.5d，默认开箱开启；后续按用户等级门槛放开——预留接口）
	CodeFont      string            `bson:"codeFont,omitempty" json:"codeFont,omitempty"`         // 代码编辑器字体：Consolas / Menlo / Monaco，空为 Consolas
	CodeFontSize  int               `bson:"codeFontSize,omitempty" json:"codeFontSize,omitempty"` // 编辑器字号 px（12-48），0 为默认 15
}

type User struct {
	ID           int64      `bson:"_id"`
	Username     string     `bson:"uname"`
	PasswordHash string     `bson:"pwdHash"`
	Nickname     string     `bson:"nickname"`
	Role         string     `bson:"role"`
	CreatedAt    time.Time  `bson:"createdAt"`
	RealName     string     `bson:"realName,omitempty"` // 真实姓名：仅管理员可改，仅本人与管理员后台可见
	Gender       string     `bson:"gender,omitempty"`   // 性别/学校/年级/QQ：本人自助修改，他人不可见
	School       string     `bson:"school,omitempty"`
	Grade        string     `bson:"grade,omitempty"`
	QQ           string     `bson:"qq,omitempty"`
	Prefs        *UserPrefs `bson:"prefs,omitempty"`
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

// CreateUserWithPassword 组合密码哈希与建号，供注册与引导使用。
func (r *Repo) CreateUserWithPassword(ctx context.Context, username, password, nickname, role string) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	return r.CreateUser(ctx, username, hash, nickname, role)
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
