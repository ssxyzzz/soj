package repo

// 改用户 ID（v0.50 管理端改 ID）：uid 是全站身份主键（users._id），被 30+ 集合直接
// 引用——改 ID 本质是跨集合引用搬家。单点收口在 ChangeUserID：mongo 事务内原子完成
// 两份 _id=uid 文档换血 + 引用全量搬家 + 残留全零断言，任何一步失败整体回滚。
// 无事务环境（standalone）直接报错拒绝执行，不做裸搬家降级（半套引用比失败更危险）。
//
// 引用面纪律：新增含 uid 字段的集合必须登记进 uidRefCollections / uidOwnerFields
// / classroomRaces 三段之一（changeuid_test.go 的清单计数断言 + 全集合搬家测试双重
// 防回归）；事务内用户数据量为班级规模（单用户记录至多数千条），远够事务时限。

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 改 ID 校验错误（handler 按类型映射 4xx 文案）。
var (
	ErrChangeIDBadArgs  = errors.New("新旧 ID 非法（须为不相等的正整数）")
	ErrChangeIDNotFound = errors.New("用户不存在")
	ErrChangeIDBusy     = errors.New("目标 ID 已被占用")
	ErrChangeIDRoot     = errors.New("root 账号 ID 受保护，不可修改")
)

// uidRefCollections uid 引用面清单：字段名固定为 "uid" 的集合（不含 classroomRaces，
// 其 uid 语义字段见下方专门处理）。users / jadeBalances 两份 _id=uid 文档不在其列——
// 它们走换血而不是改字段。
var uidRefCollections = []string{
	"submissions", "solutions",
	"discussions", "discussReplies", "discussLikes", "discussReports", "discussMutes",
	"notifications",
	"jadeLedger", "jadeAwards", "jadeBlessings",
	"achievements", "challenges", "shopOrders", "uploads",
	"aiCalls", "aiHints", "aiHintResults", "aiDebugResults",
	"dataDownloads", "classmembers", "classmarks", "securityLogs", "xpAdjust",
}

// uidOwnerFields 字段名不为 "uid" 的创建者/操作者引用：集合 → 字段名
// （xpAdjust 同时在两张清单——uid 归属行 + operatorUid 操作者审计行）。
var uidOwnerFields = map[string]string{
	"problems":       "createdBy",
	"contests":       "createdBy",
	"exams":          "createdBy",
	"lists":          "createdBy",
	"broadcasts":     "byUid",
	"xpAdjust":       "operatorUid",
	"discussReports": "resolvedBy",
}

// ChangeUserID 把用户主键从 oldUID 迁到 newUID。返回各集合搬家条数（引用面复核输出，
// handler 透传前端展示），key 为集合名。
func (r *Repo) ChangeUserID(ctx context.Context, oldUID, newUID int64) (map[string]int64, error) {
	if oldUID <= 0 || newUID <= 0 || oldUID == newUID {
		return nil, ErrChangeIDBadArgs
	}
	// 前置校验（事务外先挡一道拿友好错误；事务内快照上还会再验，防并发竞态）
	u, err := r.GetUserByID(ctx, oldUID)
	if IsNotFound(err) {
		return nil, ErrChangeIDNotFound
	} else if err != nil {
		return nil, err
	}
	if u.Role == RoleRoot {
		return nil, ErrChangeIDRoot
	}

	sess, err := r.cli.StartSession()
	if err != nil {
		return nil, err
	}
	defer sess.EndSession(ctx)

	stats := map[string]int64{} // WithTransaction 对瞬态错误会重跑回调，每次先清零
	_, txErr := sess.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		clear(stats)
		// 事务内权威校验：目标占用 / root（快照读防并发窗口）
		var target bson.M
		if err := r.users.FindOne(ctx, bson.M{"_id": newUID}).Decode(&target); !IsNotFound(err) {
			if err != nil {
				return nil, err
			}
			return nil, ErrChangeIDBusy
		}
		var src bson.M
		if err := r.users.FindOne(ctx, bson.M{"_id": oldUID}).Decode(&src); err != nil {
			if IsNotFound(err) {
				return nil, ErrChangeIDNotFound
			}
			return nil, err
		}
		if src["role"] == RoleRoot {
			return nil, ErrChangeIDRoot
		}

		// ① 两份 _id=uid 文档换血（_id 不可 $set：insert 新 + delete 旧，字段全带走；
		// jadeBalances 无记录 = 从未发过玉，跳过）。必须先删后插——users.uname 唯一索引
		// 下先插新文档会同 uname 撞 E11000；先删的窗口由事务原子性兜底（失败整体回滚）。
		// 其余带 {uid,…} 复合唯一索引的集合（jadeAwards/aiHintResults/dataDownloads 等）
		// 若目标号存在孤儿脏数据会撞键回滚——属保护行为（孤儿数据需先清）。
		for _, col := range []*mongo.Collection{r.users, r.jadeBalances} {
			var doc bson.M
			if err := col.FindOne(ctx, bson.M{"_id": oldUID}).Decode(&doc); err != nil {
				if !IsNotFound(err) {
					return nil, err
				}
				continue
			}
			if _, err := col.DeleteOne(ctx, bson.M{"_id": oldUID}); err != nil {
				return nil, err
			}
			doc["_id"] = newUID
			if _, err := col.InsertOne(ctx, doc); err != nil {
				return nil, err
			}
			stats[col.Name()]++
		}

		// ② uid 字段搬家
		for _, name := range uidRefCollections {
			res, err := r.db.Collection(name).UpdateMany(ctx,
				bson.M{"uid": oldUID}, bson.M{"$set": bson.M{"uid": newUID}})
			if err != nil {
				return nil, err
			}
			if res.ModifiedCount > 0 {
				stats[name] += res.ModifiedCount
			}
		}

		// ③ 创建者/操作者字段搬家
		for name, field := range uidOwnerFields {
			res, err := r.db.Collection(name).UpdateMany(ctx,
				bson.M{field: oldUID}, bson.M{"$set": bson.M{field: newUID}})
			if err != nil {
				return nil, err
			}
			if res.ModifiedCount > 0 {
				stats[name+"."+field] += res.ModifiedCount
			}
		}

		// ④ 课堂 PK：两个标量 + bonusPaid 数组换元（防同一人重复领取的 uid 集）
		for _, field := range []string{"hostUid", "winnerUid"} {
			res, err := r.classroomRaces.UpdateMany(ctx,
				bson.M{field: oldUID}, bson.M{"$set": bson.M{field: newUID}})
			if err != nil {
				return nil, err
			}
			if res.ModifiedCount > 0 {
				stats["classroomRaces."+field] += res.ModifiedCount
			}
		}
		res, err := r.classroomRaces.UpdateMany(ctx,
			bson.M{"bonusPaid": oldUID},
			bson.M{"$set": bson.M{"bonusPaid.$[m]": newUID}},
			options.UpdateMany().SetArrayFilters([]any{bson.M{"m": oldUID}}))
		if err != nil {
			return nil, err
		}
		if res.ModifiedCount > 0 {
			stats["classroomRaces.bonusPaid"] += res.ModifiedCount
		}

		// ⑤ 残留全零断言（事务内读可见自身写入；非零 = 引用面漏登，报错整体回滚）
		var residual int64
		for _, name := range uidRefCollections {
			n, err := r.db.Collection(name).CountDocuments(ctx, bson.M{"uid": oldUID})
			if err != nil {
				return nil, err
			}
			residual += n
		}
		for name, field := range uidOwnerFields {
			n, err := r.db.Collection(name).CountDocuments(ctx, bson.M{field: oldUID})
			if err != nil {
				return nil, err
			}
			residual += n
		}
		for _, q := range []bson.M{
			{"hostUid": oldUID}, {"winnerUid": oldUID}, {"bonusPaid": oldUID},
		} {
			n, err := r.classroomRaces.CountDocuments(ctx, q)
			if err != nil {
				return nil, err
			}
			residual += n
		}
		for _, col := range []*mongo.Collection{r.users, r.jadeBalances} {
			n, err := col.CountDocuments(ctx, bson.M{"_id": oldUID})
			if err != nil {
				return nil, err
			}
			residual += n
		}
		if residual > 0 {
			return nil, fmt.Errorf("引用搬家后仍有 %d 条残留（引用面清单漏登），已整体回滚", residual)
		}
		return nil, nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return stats, nil
}
