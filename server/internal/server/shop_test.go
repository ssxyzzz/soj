package server

// 兑换订单履约端点契约（v0.97.13 修复）：曾因 handler 把契约动词（fulfill/reject）
// 拿去对比状态常量（fulfilled/rejected）导致每次合法点击都 400「action 需为 fulfill 或 reject」。
// 此测试钉死：契约动词在 server 边界归一为目标终态再进 repo，拒绝路径退玉 + 通知不丢。

import (
	"net/http"
	"testing"

	"soj/internal/repo"
)

func TestAdminResolveShopOrder(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	h := newTestHandlers(f)
	r := testEngine(h)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	// 学生 100 余额 5，一笔 5 玉的待处理订单
	f.jadeBal[100] = 5
	f.shopOrders[1] = &repo.ShopOrder{ID: 1, ItemID: 9, ItemName: "写字笔", UID: 100,
		Uname: "stu100", Price: 5, Status: repo.ShopOrderPending}

	// ① 契约动词 fulfill → repo 收到终态 fulfilled，玉不动
	if code, body := doJSON(r, "POST", "/api/admin/shop/orders/1/resolve", rootTok, `{"action":"fulfill"}`); code != http.StatusOK {
		t.Fatalf("fulfill = %d %v，期望 200", code, body)
	}
	if got := f.resolvedStatus[1]; got != repo.ShopOrderFulfilled {
		t.Fatalf("repo 收到终态 = %q，期望 %q", got, repo.ShopOrderFulfilled)
	}
	if f.shopOrders[1].Status != repo.ShopOrderFulfilled || f.jadeBal[100] != 5 {
		t.Fatalf("fulfill 落库态 = %q 余额 = %d，期望 fulfilled / 5 不退玉", f.shopOrders[1].Status, f.jadeBal[100])
	}

	// ② 契约动词 reject → 终态 rejected + 全额退玉 + 通知学生
	f.shopOrders[2] = &repo.ShopOrder{ID: 2, ItemID: 9, ItemName: "写字笔", UID: 100,
		Uname: "stu100", Price: 5, Status: repo.ShopOrderPending}
	if code, body := doJSON(r, "POST", "/api/admin/shop/orders/2/resolve", rootTok, `{"action":"reject"}`); code != http.StatusOK {
		t.Fatalf("reject = %d %v，期望 200", code, body)
	}
	if got := f.resolvedStatus[2]; got != repo.ShopOrderRejected {
		t.Fatalf("repo 收到终态 = %q，期望 %q", got, repo.ShopOrderRejected)
	}
	if f.shopOrders[2].Status != repo.ShopOrderRejected || f.jadeBal[100] != 10 {
		t.Fatalf("reject 落库态 = %q 余额 = %d，期望 rejected / 10（退 5 玉）", f.shopOrders[2].Status, f.jadeBal[100])
	}
	if f.notifications != 1 {
		t.Fatalf("拒绝通知 = %d 条，期望 1 条", f.notifications)
	}

	// ③ 非法动词（含旧 bug 的状态词 fulfilled）→ 400，不触 repo
	for _, bad := range []string{"fulfilled", "rejected", "accept", ""} {
		f.shopOrders[3] = &repo.ShopOrder{ID: 3, Price: 5, UID: 100, Status: repo.ShopOrderPending}
		if code, body := doJSON(r, "POST", "/api/admin/shop/orders/3/resolve", rootTok,
			`{"action":"`+bad+`"}`); code != http.StatusBadRequest {
			t.Fatalf("非法 action %q = %d %v，期望 400", bad, code, body)
		}
		if _, called := f.resolvedStatus[3]; called {
			t.Fatalf("非法 action %q 不应触达 repo", bad)
		}
	}

	// ④ 非 root → 403
	stuTok := f.addUser(h, 100, "stu100", repo.RoleDefault, 0)
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	if code, _ := doJSON(r, "POST", "/api/admin/shop/orders/3/resolve", stuTok, `{"action":"fulfill"}`); code != http.StatusForbidden {
		t.Fatalf("学生打履约端点 = %d，期望 403", code)
	}
}
