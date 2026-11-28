package repo

// 实物兑换被拒退玉的流水类型契约（v0.101.3）：退回行记 shop_refund（流水页「类型」显示
// 「兑换退回」）。v0.33-② 起此处借用 bounty_refund，学生看到的是「悬赏退回」而说明栏写
// 「兑换被拒退回」。同文件钉死存量订正 shopRefundKindV1：按说明前缀认出误记行改写，
// 悬赏退款行不受波及。依赖本地 MongoDB（openTestRepo 同款，不可达整文件跳过）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestResolveShopOrderRefundKind(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	it := &ShopItem{Name: "写字笔", Price: 5, Stock: -1, Active: true}
	if err := r.UpsertShopItem(ctx, it); err != nil {
		t.Fatalf("建物品: %v", err)
	}
	if err := r.JadeCredit(ctx, 7, 5, JadeKindAdminAdjust, "测试入账", 0); err != nil {
		t.Fatalf("入账: %v", err)
	}
	oid, err := r.PlaceShopOrder(ctx, 7, "stu", it.ID, "")
	if err != nil {
		t.Fatalf("下单: %v", err)
	}
	if err := r.ResolveShopOrder(ctx, oid, ShopOrderRejected); err != nil {
		t.Fatalf("拒绝: %v", err)
	}
	if bal := jadeBal(t, r, 7); bal != 5 {
		t.Fatalf("拒绝后余额 = %d，期望 5（全额退回）", bal)
	}
	// 流水新→旧：退回 / 扣减 / 入账
	list, _, err := r.JadeLedgerPage(ctx, 7, 1, 10)
	if err != nil || len(list) != 3 {
		t.Fatalf("流水 = %d 条（err=%v），期望 3", len(list), err)
	}
	refund := list[0]
	if refund.Delta != 5 || refund.Kind != JadeKindShopRefund || refund.Ref != oid {
		t.Fatalf("退回流水 = kind %q / %+d / ref %d，期望 shop_refund / +5 / 订单 %d",
			refund.Kind, refund.Delta, refund.Ref, oid)
	}
	want := fmt.Sprintf("%s（订单 #%d「写字笔」）", shopRefundReasonPrefix, oid)
	if refund.Reason != want {
		t.Fatalf("退回流水说明 = %q，期望 %q", refund.Reason, want)
	}
	if list[1].Kind != JadeKindShopOrder || list[1].Delta != -5 {
		t.Fatalf("扣减流水 = kind %q / %+d，期望 shop_order / -5", list[1].Kind, list[1].Delta)
	}
}

func TestMigrateShopRefundKind(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	// 存量：误记的兑换退回行、两条真悬赏退款（到期 / 删帖）、兑换扣减行
	legacy := []bson.M{
		{"_id": int64(1), "uid": int64(7), "delta": int64(5), "kind": JadeKindBountyRefund,
			"reason": "兑换被拒退回（订单 #1「写字笔」）", "ref": int64(1), "at": time.Now()},
		{"_id": int64(2), "uid": int64(7), "delta": int64(3), "kind": JadeKindBountyRefund,
			"reason": "悬赏到期退回（话题 #4，无人打赏部分）", "ref": int64(4), "at": time.Now()},
		{"_id": int64(3), "uid": int64(7), "delta": int64(2), "kind": JadeKindBountyRefund,
			"reason": "话题删除退回悬赏（话题 #5）", "ref": int64(5), "at": time.Now()},
		{"_id": int64(4), "uid": int64(7), "delta": int64(-5), "kind": JadeKindShopOrder,
			"reason": "兑换「写字笔」", "ref": int64(9), "at": time.Now()},
	}
	for _, d := range legacy {
		if _, err := r.jadeLedger.InsertOne(ctx, d); err != nil {
			t.Fatalf("落存量流水: %v", err)
		}
	}

	n, err := r.migrateShopRefundKind(ctx)
	if err != nil || n != 1 {
		t.Fatalf("迁移 = %d 行（err=%v），期望 1", n, err)
	}
	list, _, err := r.JadeLedgerPage(ctx, 7, 1, 10)
	if err != nil || len(list) != 4 {
		t.Fatalf("流水 = %d 条（err=%v），期望 4", len(list), err)
	}
	byID := map[int64]*JadeLedgerEntry{}
	for _, e := range list {
		byID[e.ID] = e
	}
	want := map[int64]string{
		1: JadeKindShopRefund, 2: JadeKindBountyRefund, 3: JadeKindBountyRefund, 4: JadeKindShopOrder,
	}
	for id, kind := range want {
		if e := byID[id]; e == nil || e.Kind != kind {
			t.Fatalf("流水 #%d kind = %q，期望 %q", id, byID[id].Kind, kind)
		}
	}
	// 只动类型：金额与说明原样
	if e := byID[1]; e.Delta != 5 || e.Reason != "兑换被拒退回（订单 #1「写字笔」）" {
		t.Fatalf("改写行 = %+d %q，期望金额与说明原样", e.Delta, e.Reason)
	}

	// 幂等：重跑无命中
	if n, err := r.migrateShopRefundKind(ctx); err != nil || n != 0 {
		t.Fatalf("重跑 = %d 行（err=%v），期望 0", n, err)
	}
}
