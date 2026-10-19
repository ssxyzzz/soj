package repo

import (
	"context"
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Role 角色 = 权限位的集合（roles 集合）。内置三角色与历史 user.role 字符串同 id，
// 存量用户数据零迁移。root 由权限判定函数特判（见 perm.go），其文档仅作展示。
type Role struct {
	ID      string   `bson:"_id"`
	Name    string   `bson:"name"`
	Perms   []string `bson:"perms"`
	Builtin bool     `bson:"builtin"`
}

// builtinRoles 内置角色：初始权限精确复刻 v0.4.6 行为（v0.5.2-1 起 admin 增题解管理，
// v0.5.2-2 起 admin 增查看用户提交明细），此后 root 的每次调整都是显式决定。
// 存量库由 solutionManageV1 / submissionViewUserV1 迁移补发新位。
var builtinRoles = []Role{
	{ID: RoleDefault, Name: "普通用户", Perms: []string{}, Builtin: true},
	{ID: RoleAdmin, Name: "管理员", Perms: []string{
		PermProblemViewAll, PermProblemCreate, PermProblemEditOwn,
		PermClassCreate, PermClassViewAll,
		PermSubmissionViewAll, PermSubmissionViewUser, PermSubmissionRateExempt,
		PermSolutionManage,
	}, Builtin: true},
	{ID: RoleRoot, Name: "超级管理员", Perms: []string{PermAll}, Builtin: true},
}

// EnsureBuiltinRoles 启动种子：内置角色不存在则创建（沿用 settings.go 的
// InsertOne + 容忍重复键惯例；已存在则不动，保留 root 的后续调整）。
func (r *Repo) EnsureBuiltinRoles(ctx context.Context) error {
	for _, role := range builtinRoles {
		if _, err := r.Roles.InsertOne(ctx, role); err != nil && !mongo.IsDuplicateKeyError(err) {
			return err
		}
	}
	return nil
}

// ListRoles 全量角色：内置按 default/admin/root 固定序，自定义按 id 字典序。
func (r *Repo) ListRoles(ctx context.Context) ([]*Role, error) {
	cursor, err := r.Roles.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Role, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	rank := map[string]int{RoleDefault: 0, RoleAdmin: 1, RoleRoot: 2}
	sort.SliceStable(list, func(i, j int) bool {
		ri, oki := rank[list[i].ID]
		rj, okj := rank[list[j].ID]
		if oki && okj {
			return ri < rj
		}
		if oki != okj {
			return oki // 内置在前
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// GetRole 按 id 取角色；不存在返回 mongo.ErrNoDocuments。
func (r *Repo) GetRole(ctx context.Context, id string) (*Role, error) {
	var role Role
	err := r.Roles.FindOne(ctx, bson.M{"_id": id}).Decode(&role)
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// CreateRole 新建自定义角色（存在性/合法性由调用方校验）。
func (r *Repo) CreateRole(ctx context.Context, role Role) error {
	_, err := r.Roles.InsertOne(ctx, role)
	return err
}

// UpdateRole 更新角色显示名与权限位（id 不可改）。
func (r *Repo) UpdateRole(ctx context.Context, id, name string, perms []string) error {
	res, err := r.Roles.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"name": name, "perms": perms}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// DeleteRole 删除角色（调用方保证为自定义角色且无用户挂靠）。
func (r *Repo) DeleteRole(ctx context.Context, id string) error {
	_, err := r.Roles.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// CountUsersWithRole 统计挂靠某角色的用户数（删除前置校验）。
func (r *Repo) CountUsersWithRole(ctx context.Context, role string) (int64, error) {
	return r.Users.CountDocuments(ctx, bson.M{"role": role})
}
