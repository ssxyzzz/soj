package repo

// 改用户 ID 集成测试（v0.50，真 Mongo，本地不可达自动 skip 同 openTestRepo）：
// 全引用面搬家与残留断言 / 前置校验守卫 / 唯一索引撞键整体回滚三线。
// 引用面防回归：测试给清单内每个集合都铺数据并断言全部迁走——清单被删项即刻炸测；
// 清单计数断言锁「新增 uid 集合必登记」纪律（新集合未登记时本测试测不到，靠残留
// 断言在生产首次改号时兜底回滚，不丢数据）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	cuidOld = int64(11)
	cuidNew = int64(99)
	cuidBys = int64(12) // 旁观者：其数据不得被误迁
)

func cuidSeed(t *testing.T, r *Repo) {
	t.Helper()
	ctx := context.Background()
	ins := func(col string, doc bson.M) {
		t.Helper()
		if _, err := r.db.Collection(col).InsertOne(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", col, err)
		}
	}
	ins("users", bson.M{"_id": cuidOld, "uname": "oldguy", "nickname": "老号", "role": RoleDefault, "createdAt": time.Now()})
	ins("users", bson.M{"_id": cuidBys, "uname": "bys", "role": RoleDefault, "createdAt": time.Now()})
	ins("jadeBalances", bson.M{"_id": cuidOld, "bal": int64(50), "comboCur": int64(3)})
	for _, col := range uidRefCollections {
		doc := bson.M{"uid": cuidOld}
		switch col { // 复合唯一索引集合补齐键其余部分，防同键自撞
		case "jadeAwards":
			doc["pid"] = int64(5)
		case "aiHintResults":
			doc["pid"] = int64(5)
			doc["level"] = 1
		case "dataDownloads":
			doc["pid"] = int64(5)
			doc["day"] = "2026-11-01"
		}
		ins(col, doc)
	}
	for col, field := range uidOwnerFields {
		ins(col, bson.M{field: cuidOld})
	}
	ins("classroomRaces", bson.M{"classId": int64(1), "pid": int64(5), "hostUid": cuidOld,
		"winnerUid": cuidOld, "bonusPaid": bson.A{cuidOld, cuidBys}, "status": "running"})
	ins("submissions", bson.M{"uid": cuidBys, "uname": "bys", "pid": int64(5)}) // 旁观者提交
}

func TestChangeUserIDMovesEverything(t *testing.T) {
	r := openTestRepo(t)
	cuidSeed(t, r)
	ctx := context.Background()

	stats, err := r.ChangeUserID(ctx, cuidOld, cuidNew)
	if err != nil {
		t.Fatalf("ChangeUserID: %v", err)
	}
	if stats["users"] != 1 || stats["jadeBalances"] != 1 {
		t.Errorf("换血计数异常: users=%d jadeBalances=%d", stats["users"], stats["jadeBalances"])
	}
	// 主文档换血（字段全带走）
	var u struct {
		Username string `bson:"uname"`
		Nickname string `bson:"nickname"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": cuidNew}).Decode(&u); err != nil {
		t.Fatalf("新 ID 用户缺失: %v", err)
	}
	if u.Username != "oldguy" || u.Nickname != "老号" {
		t.Errorf("换血丢字段: %+v", u)
	}
	var bal struct {
		Bal      int64 `bson:"bal"`
		ComboCur int64 `bson:"comboCur"`
	}
	if err := r.jadeBalances.FindOne(ctx, bson.M{"_id": cuidNew}).Decode(&bal); err != nil {
		t.Fatalf("新 ID 余额缺失: %v", err)
	}
	if bal.Bal != 50 || bal.ComboCur != 3 {
		t.Errorf("余额字段未随迁: %+v", bal)
	}
	// uid 字段逐集合断言迁移
	for _, col := range uidRefCollections {
		n, err := r.db.Collection(col).CountDocuments(ctx, bson.M{"uid": cuidNew})
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s: 新 uid 记录 = %d，期望 1", col, n)
		}
	}
	// 创建者/操作者字段逐集合断言
	for col, field := range uidOwnerFields {
		n, err := r.db.Collection(col).CountDocuments(ctx, bson.M{field: cuidNew})
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s.%s: 新 uid 记录 = %d，期望 1", col, field, n)
		}
	}
	// 课堂 PK：标量 + 数组换元（旁观者 12 保留在数组里）
	var race struct {
		HostUID   int64   `bson:"hostUid"`
		WinnerUID int64   `bson:"winnerUid"`
		BonusPaid []int64 `bson:"bonusPaid"`
	}
	if err := r.classroomRaces.FindOne(ctx, bson.M{"hostUid": cuidNew}).Decode(&race); err != nil {
		t.Fatalf("PK 场次未迁: %v", err)
	}
	if race.WinnerUID != cuidNew {
		t.Errorf("winnerUid = %d", race.WinnerUID)
	}
	if len(race.BonusPaid) != 2 || race.BonusPaid[0] != cuidNew || race.BonusPaid[1] != cuidBys {
		t.Errorf("bonusPaid = %v", race.BonusPaid)
	}
	// 旁观者数据原封不动
	n, _ := r.submissions.CountDocuments(ctx, bson.M{"uid": cuidBys})
	if n != 1 {
		t.Errorf("旁观者提交被误迁/丢失: %d", n)
	}
}

func TestChangeUserIDGuards(t *testing.T) {
	r := openTestRepo(t)
	cuidSeed(t, r)
	ctx := context.Background()

	if _, err := r.ChangeUserID(ctx, cuidOld, cuidOld); !errors.Is(err, ErrChangeIDBadArgs) {
		t.Errorf("同号改 = %v，期望 ErrChangeIDBadArgs", err)
	}
	if _, err := r.ChangeUserID(ctx, cuidOld, -5); !errors.Is(err, ErrChangeIDBadArgs) {
		t.Errorf("负数 = %v，期望 ErrChangeIDBadArgs", err)
	}
	if _, err := r.ChangeUserID(ctx, 404, cuidNew); !errors.Is(err, ErrChangeIDNotFound) {
		t.Errorf("源不存在 = %v，期望 ErrChangeIDNotFound", err)
	}
	if _, err := r.ChangeUserID(ctx, cuidOld, cuidBys); !errors.Is(err, ErrChangeIDBusy) {
		t.Errorf("目标占用 = %v，期望 ErrChangeIDBusy", err)
	}
	// root 禁改
	_, _ = r.users.InsertOne(ctx, bson.M{"_id": int64(2), "uname": "rooter", "role": RoleRoot, "createdAt": time.Now()})
	if _, err := r.ChangeUserID(ctx, 2, cuidNew); !errors.Is(err, ErrChangeIDRoot) {
		t.Errorf("root 改号 = %v，期望 ErrChangeIDRoot", err)
	}
}

func TestChangeUserIDRollsBackOnConflict(t *testing.T) {
	r := openTestRepo(t)
	cuidSeed(t, r)
	ctx := context.Background()

	// 撞键器：jadeAwards 有 {uid,pid} 唯一索引（Bootstrap 建；测试库手工补），
	// 预埋目标号新号的同键孤儿 → 事务内 updateMany 撞 E11000 → 整体回滚
	if _, err := r.jadeAwards.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "uid", Value: 1}, {Key: "pid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.jadeAwards.InsertOne(ctx, bson.M{"uid": cuidNew, "pid": int64(5)}); err != nil {
		t.Fatal(err)
	}

	_, err := r.ChangeUserID(ctx, cuidOld, cuidNew)
	if err == nil {
		t.Fatal("撞键未报错")
	}
	if errors.Is(err, ErrChangeIDBusy) || errors.Is(err, ErrChangeIDRoot) {
		t.Fatalf("撞键被误判为业务守卫: %v", err)
	}
	// 回滚完整性：旧号数据原封如初
	if err := r.users.FindOne(ctx, bson.M{"_id": cuidOld}).Err(); err != nil {
		t.Fatalf("回滚后旧用户丢失: %v", err)
	}
	if err := r.jadeBalances.FindOne(ctx, bson.M{"_id": cuidOld}).Err(); err != nil {
		t.Fatalf("回滚后旧余额丢失: %v", err)
	}
	n, _ := r.submissions.CountDocuments(ctx, bson.M{"uid": cuidOld})
	if n != 1 {
		t.Errorf("回滚后旧提交 = %d，期望 1", n)
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": cuidNew}).Err(); !IsNotFound(err) {
		t.Errorf("回滚后不该有新号用户: %v", err)
	}
}

func TestChangeUserIDRefListShape(t *testing.T) {
	// 清单纪律锁：段落数变化必须是有意扩引用面（同步补测试与残留断言），不是漏登
	if len(uidRefCollections) != 24 {
		t.Errorf("uidRefCollections = %d 项，期望 24（新增 uid 集合须登记并同步本测试）", len(uidRefCollections))
	}
	if len(uidOwnerFields) != 7 {
		t.Errorf("uidOwnerFields = %d 项，期望 7", len(uidOwnerFields))
	}
}
