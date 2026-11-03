package repo

// 用户自动取号单测（v0.61 按序补空）：起点首号、手动大号不推高、空洞按序回填、
// 大号蹲占不阻断、连满顺延、起点调大即时生效。本地无 Mongo 自动 skip（openTestRepo
// 同款：每测独立临时库，互不污染）。

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestUserSeqGapFill(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 生产由 Bootstrap 建 uname 唯一索引；测试库手工对齐（并发重试的用户名撞键分辨依赖它）
	if _, err := r.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "uname", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("建 uname 唯一索引: %v", err)
	}

	// 全新库：首个自动号 = 起点
	uid, err := r.CreateUser(ctx, "alice", "hash", "Alice", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if uid != DefaultUserSeqStart {
		t.Fatalf("首个自动号 = %d，期望起点 %d", uid, DefaultUserSeqStart)
	}

	// 手动指定大号（500）：自动号不被推高，下一个仍按起点段顺延 101
	if _, err := r.CreateUserWithID(ctx, 500, "bob", "hash", "Bob", RoleDefault); err != nil {
		t.Fatalf("CreateUserWithID 500: %v", err)
	}
	if uid, err = r.CreateUser(ctx, "carol", "hash", "Carol", RoleDefault); err != nil {
		t.Fatalf("CreateUser carol: %v", err)
	}
	if uid != DefaultUserSeqStart+1 {
		t.Fatalf("手动大号后自动号 = %d，期望 %d（不推高）", uid, DefaultUserSeqStart+1)
	}

	// 挖洞：占 102、104（跳过 103）→ 空洞 103 优先回填，之后 105
	if _, err := r.CreateUserWithID(ctx, DefaultUserSeqStart+2, "dave", "hash", "Dave", RoleDefault); err != nil {
		t.Fatalf("CreateUserWithID 102: %v", err)
	}
	if _, err := r.CreateUserWithID(ctx, DefaultUserSeqStart+4, "frank", "hash", "Frank", RoleDefault); err != nil {
		t.Fatalf("CreateUserWithID 104: %v", err)
	}
	if uid, err = r.CreateUser(ctx, "grace", "hash", "Grace", RoleDefault); err != nil {
		t.Fatalf("CreateUser grace: %v", err)
	}
	if uid != DefaultUserSeqStart+3 {
		t.Fatalf("空洞回填号 = %d，期望 %d", uid, DefaultUserSeqStart+3)
	}
	if uid, err = r.CreateUser(ctx, "heidi", "hash", "Heidi", RoleDefault); err != nil {
		t.Fatalf("CreateUser heidi: %v", err)
	}
	if uid != DefaultUserSeqStart+5 {
		t.Fatalf("回填后顺延号 = %d，期望 %d", uid, DefaultUserSeqStart+5)
	}

	// 大号蹲占（改 ID 落点类场景）：占 200 不阻断起点段继续发号
	if _, err := r.CreateUserWithID(ctx, 200, "ivan", "hash", "Ivan", RoleDefault); err != nil {
		t.Fatalf("CreateUserWithID 200: %v", err)
	}
	if uid, err = r.CreateUser(ctx, "judy", "hash", "Judy", RoleDefault); err != nil {
		t.Fatalf("CreateUser judy: %v", err)
	}
	if uid != DefaultUserSeqStart+6 {
		t.Fatalf("蹲占后自动号 = %d，期望 %d（不受大号影响）", uid, DefaultUserSeqStart+6)
	}

	// 用户名撞键不重试、如实报错（与号段竞态区分）
	if _, err := r.CreateUser(ctx, "alice", "hash", "Alice2", RoleDefault); err == nil {
		t.Fatalf("重复用户名应报错")
	}
}

func TestUserSeqStartRuntime(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 运行期调大起点：首个自动号立即落在新起点（下方更小号段不参与分配）
	start := 300
	if err := r.UpdateSystemSettings(ctx, &SystemSettings{RateLimitPerMinute: DefaultRateLimitPerMinute, UserSeqStart: &start}); err != nil {
		t.Fatalf("UpdateSystemSettings: %v", err)
	}
	uid, err := r.CreateUser(ctx, "erin", "hash", "Erin", RoleDefault)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if uid != 300 {
		t.Fatalf("起点 300 下首个自动号 = %d，期望 300", uid)
	}

	// 预读队列与实发同口径：前 3 个空缺 = 301,302,303
	ids, err := r.PeekUserSeqs(ctx, 3)
	if err != nil {
		t.Fatalf("PeekUserSeqs: %v", err)
	}
	if len(ids) != 3 || ids[0] != 301 || ids[1] != 302 || ids[2] != 303 {
		t.Fatalf("PeekUserSeqs = %v，期望 [301 302 303]", ids)
	}
	// 预读不落库：紧接一次自动取号应仍从 301 起
	if uid, err = r.CreateUser(ctx, "frank", "hash", "Frank", RoleDefault); err != nil {
		t.Fatalf("CreateUser frank: %v", err)
	}
	if uid != 301 {
		t.Fatalf("预读后自动号 = %d，期望 301（预读不消耗）", uid)
	}
}
