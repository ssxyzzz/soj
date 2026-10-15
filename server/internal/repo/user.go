package repo

import (
	"context"
	"errors"
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

// ValidRoles 合法角色集合（管理端改角色时校验）。
var ValidRoles = map[string]bool{RoleDefault: true, RoleAdmin: true, RoleRoot: true}

type User struct {
	ID           int64     `bson:"_id"`
	Username     string    `bson:"uname"`
	PasswordHash string    `bson:"pwdHash"`
	Nickname     string    `bson:"nickname"`
	Role         string    `bson:"role"`
	CreatedAt    time.Time `bson:"createdAt"`
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

// ListUsers 管理端用户分页列表（不含密码哈希）。
func (r *Repo) ListUsers(ctx context.Context, page, pageSize int) ([]*User, int64, error) {
	total, err := r.Users.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.Users.Find(ctx, bson.M{},
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
