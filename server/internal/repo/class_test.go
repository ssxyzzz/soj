package repo

// 班级级联解散单测（v0.7.2 验收调整：解散自动清理考试/作业/成员）。
// 复用 submission_test.go 的 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestDeleteClassCascade(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	cid := int64(60)
	mustCount := func(coll string, filter bson.M, want int64) {
		t.Helper()
		n, err := r.db.Collection(coll).CountDocuments(ctx, filter)
		if err != nil {
			t.Fatalf("count %s: %v", coll, err)
		}
		if n != want {
			t.Fatalf("%s 期望 %d 条，实际 %d", coll, want, n)
		}
	}
	mustInsert := func(coll string, doc bson.M) {
		t.Helper()
		if _, err := r.db.Collection(coll).InsertOne(ctx, doc); err != nil {
			t.Fatalf("insert %s: %v", coll, err)
		}
	}

	mustInsert("classes", bson.M{"_id": cid, "name": "冒烟班", "ownerId": int64(1)})
	mustInsert("classmembers", bson.M{"classId": cid, "uid": int64(1), "role": "owner"})
	mustInsert("classmembers", bson.M{"classId": cid, "uid": int64(2), "role": "member"})
	mustInsert("assignments", bson.M{"_id": int64(61), "classId": cid, "title": "作业"})
	mustInsert("exams", bson.M{"_id": int64(62), "classId": cid, "title": "考试"})
	// 别班的资料不应被波及
	mustInsert("assignments", bson.M{"_id": int64(63), "classId": int64(99), "title": "别班作业"})
	mustInsert("classmembers", bson.M{"classId": int64(99), "uid": int64(3), "role": "member"})

	if err := r.DeleteClass(ctx, cid); err != nil {
		t.Fatalf("DeleteClass: %v", err)
	}
	mustCount("classes", bson.M{"_id": cid}, 0)
	mustCount("classmembers", bson.M{"classId": cid}, 0)
	mustCount("assignments", bson.M{"classId": cid}, 0)
	mustCount("exams", bson.M{"classId": cid}, 0)
	mustCount("assignments", bson.M{"classId": int64(99)}, 1)
	mustCount("classmembers", bson.M{"classId": int64(99)}, 1)

	// 重复解散：班级已不存在 → ErrNoDocuments
	if err := r.DeleteClass(ctx, cid); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("重复解散应 ErrNoDocuments，得到 %v", err)
	}
}

func TestTransferClass(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	cid := int64(70)
	setup := func(docs ...bson.M) {
		t.Helper()
		for _, d := range docs {
			if _, err := r.db.Collection("classes").InsertOne(ctx, d); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
	}
	member := func(cid, uid int64, role string) {
		t.Helper()
		if _, err := r.db.Collection("classmembers").InsertOne(ctx,
			bson.M{"classId": cid, "uid": uid, "role": role}); err != nil {
			t.Fatalf("insert member: %v", err)
		}
	}
	roleOf := func(cid, uid int64) string {
		t.Helper()
		var row struct {
			Role string `bson:"role"`
		}
		if err := r.db.Collection("classmembers").FindOne(ctx,
			bson.M{"classId": cid, "uid": uid}).Decode(&row); err != nil {
			t.Fatalf("member %d: %v", uid, err)
		}
		return row.Role
	}
	ownerOf := func(cid int64) int64 {
		t.Helper()
		var row struct {
			OwnerID int64 `bson:"ownerId"`
		}
		if err := r.db.Collection("classes").FindOne(ctx, bson.M{"_id": cid}).Decode(&row); err != nil {
			t.Fatalf("class: %v", err)
		}
		return row.OwnerID
	}

	t.Run("换主与角色互换", func(t *testing.T) {
		setup(bson.M{"_id": cid, "name": "转让班", "ownerId": int64(1)})
		member(cid, 1, "owner")
		member(cid, 2, "member")
		member(cid, 3, "admin")
		if err := r.TransferClass(ctx, cid, 1, 2); err != nil {
			t.Fatalf("TransferClass: %v", err)
		}
		if ownerOf(cid) != 2 {
			t.Fatalf("新建班人应为 uid=2，实际 %d", ownerOf(cid))
		}
		if roleOf(cid, 2) != ClassRoleOwner {
			t.Fatalf("受让人角色应为 owner，实际 %q", roleOf(cid, 2))
		}
		if roleOf(cid, 1) != ClassRoleAdmin {
			t.Fatalf("原建班人应降为 admin，实际 %q", roleOf(cid, 1))
		}
	})

	t.Run("非成员拒", func(t *testing.T) {
		if err := r.TransferClass(ctx, cid, 2, 999); !errors.Is(err, ErrNotMember) {
			t.Fatalf("非成员受让应 ErrNotMember，得到 %v", err)
		}
	})

	t.Run("班级不存在", func(t *testing.T) {
		if err := r.TransferClass(ctx, 999, 1, 2); !errors.Is(err, mongo.ErrNoDocuments) {
			t.Fatalf("班级不存在应 ErrNoDocuments，得到 %v", err)
		}
	})
}

// genInviteCode 规格（2026-10-25 拍板）：4 位，26 小写 + 10 数字随机组合。
func TestGenInviteCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := genInviteCode()
		if err != nil {
			t.Fatalf("genInviteCode: %v", err)
		}
		if len(code) != 4 {
			t.Fatalf("len = %d, want 4", len(code))
		}
		for _, ch := range code {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
				t.Fatalf("码 %s 含越界字符 %q", code, ch)
			}
		}
		seen[code] = true
	}
	if len(seen) < 100 {
		t.Fatalf("200 次采样仅 %d 种取值，随机性可疑", len(seen))
	}
}
