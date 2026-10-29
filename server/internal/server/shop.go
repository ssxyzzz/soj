package server

// 实物兑换 HTTP 面（v0.33-②）：学生侧列表/下单/我的订单；root 侧目录 CRUD + 订单履约。
// 模块默认关（settings.jadeShopEnabled，root 后台开）；开启前学生侧一律 403。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// shopOpen 模块开关（关 = 学生侧全拒）。
func (h *Handlers) shopOpen(c *gin.Context) bool {
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil || !s.JadeShopOpen() {
		fail(c, http.StatusForbidden, "兑换暂未开放")
		return false
	}
	return true
}

func shopItemJSON(it *repo.ShopItem) gin.H {
	return gin.H{
		"id": it.ID, "name": it.Name, "image": it.Image, "price": it.Price,
		"stock": it.Stock, "per_user_limit": it.PerUserLimit, "active": it.Active,
	}
}

func shopOrderJSON(o *repo.ShopOrder) gin.H {
	return gin.H{
		"id": o.ID, "item_id": o.ItemID, "item_name": o.ItemName,
		"price": o.Price, "status": o.Status, "note": o.Note,
		"created_at": o.CreatedAt.Format("2006-01-02 15:04"),
		"done_at":    o.DoneAt.Format("2006-01-02 15:04"),
		"uid":        o.UID, "uname": o.Uname,
	}
}

// ListShop GET /api/shop —— 上架物品（模块开关内）。
func (h *Handlers) ListShop(c *gin.Context) {
	if !h.shopOpen(c) {
		return
	}
	list, err := h.repo.ListActiveShopItems(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取兑换目录失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, it := range list {
		items = append(items, shopItemJSON(it))
	}
	bal, _ := h.repo.JadeBalance(c.Request.Context(), uidOf(c))
	ok(c, gin.H{"list": items, "balance": bal})
}

// uidOf 当前登录 uid（0 = 未登录）。
func uidOf(c *gin.Context) int64 {
	uid, _, _, _ := currentUser(c)
	return uid
}

// PlaceShopOrder POST /api/shop/:id/order {note} —— 扣玉扣库存下单，通知 root 履约。
func (h *Handlers) PlaceShopOrder(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	if !h.shopOpen(c) {
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req) // note 可缺省
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > 100 {
		fail(c, http.StatusBadRequest, "留言过长（上限 100 字）")
		return
	}
	ctx := c.Request.Context()
	orderID, err := h.repo.PlaceShopOrder(ctx, uid, uname, id, note)
	if err != nil {
		switch err {
		case repo.ErrInsufficientJade:
			fail(c, http.StatusBadRequest, "余额不足")
		case repo.ErrShopOutOfStock:
			fail(c, http.StatusBadRequest, "库存不足")
		case repo.ErrShopLimitReached:
			fail(c, http.StatusBadRequest, "已达每人限购上限")
		case repo.ErrShopClosed:
			fail(c, http.StatusBadRequest, "该物品已下架")
		default:
			fail(c, http.StatusInternalServerError, "下单失败")
		}
		return
	}
	// 通知全体 root 人工履约（尽力而为）
	s, _ := h.repo.GetSystemSettings(ctx)
	if roots, rerr := h.repo.UserIDsByRole(ctx, repo.RoleRoot); rerr == nil {
		for _, ruid := range roots {
			h.notifyOne(ctx, ruid, repo.NotifyInput{
				Typ: repo.NotifyWarn, Title: "新兑换订单待处理",
				Body: uname + " 下单订单 #" + strconv.FormatInt(orderID, 10) + "，请到后台处理",
				Link: "/admin?section=shop",
			}, s, "")
		}
	}
	bal, _ := h.repo.JadeBalance(ctx, uid)
	ok(c, gin.H{"order_id": orderID, "balance": bal})
}

// MyShopOrders GET /api/shop/my-orders —— 本人最近订单。
func (h *Handlers) MyShopOrders(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	list, err := h.repo.MyShopOrders(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取订单失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, o := range list {
		items = append(items, shopOrderJSON(o))
	}
	ok(c, gin.H{"list": items})
}

// AdminShopItems GET /api/admin/shop/items —— 目录全量（root）。
func (h *Handlers) AdminShopItems(c *gin.Context) {
	list, err := h.repo.ListShopItems(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取目录失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, it := range list {
		items = append(items, shopItemJSON(it))
	}
	ok(c, gin.H{"list": items})
}

// AdminUpsertShopItem POST /api/admin/shop/items —— 新建/更新物品（root）。
func (h *Handlers) AdminUpsertShopItem(c *gin.Context) {
	var req struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Image        string `json:"image"`
		Price        int64  `json:"price"`
		Stock        int64  `json:"stock"`
		PerUserLimit int64  `json:"per_user_limit"`
		Active       bool   `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(c, http.StatusBadRequest, "请填写物品名称")
		return
	}
	if req.Price < 1 || req.Price > 1_000_000 {
		fail(c, http.StatusBadRequest, "价格需在 1–1000000 玉之间")
		return
	}
	if req.Stock < -1 || req.Stock > 1_000_000 {
		fail(c, http.StatusBadRequest, "库存需 ≥ 0（-1 = 不限）")
		return
	}
	if len(req.Image) > 512 {
		fail(c, http.StatusBadRequest, "图片 URL 过长")
		return
	}
	it := &repo.ShopItem{
		ID: req.ID, Name: strings.TrimSpace(req.Name), Image: strings.TrimSpace(req.Image),
		Price: req.Price, Stock: req.Stock, PerUserLimit: req.PerUserLimit, Active: req.Active,
	}
	if err := h.repo.UpsertShopItem(c.Request.Context(), it); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"id": it.ID})
}

// AdminDeleteShopItem DELETE /api/admin/shop/items/:id（root）。
func (h *Handlers) AdminDeleteShopItem(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	if err := h.repo.DeleteShopItem(c.Request.Context(), id); err != nil {
		fail(c, http.StatusNotFound, "物品不存在")
		return
	}
	ok(c, gin.H{"deleted": id})
}

// AdminShopOrders GET /api/admin/shop/orders?status=&page=（root）。
func (h *Handlers) AdminShopOrders(c *gin.Context) {
	status := c.Query("status")
	if status != "" && status != repo.ShopOrderPending && status != repo.ShopOrderFulfilled && status != repo.ShopOrderRejected {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListShopOrders(c.Request.Context(), status, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取订单失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, o := range list {
		items = append(items, shopOrderJSON(o))
	}
	ok(c, gin.H{"list": items, "total": total})
}

// AdminResolveShopOrder POST /api/admin/shop/orders/:id/resolve {action: fulfill|reject}（root）。
func (h *Handlers) AdminResolveShopOrder(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil ||
		(req.Action != repo.ShopOrderFulfilled && req.Action != repo.ShopOrderRejected) {
		fail(c, http.StatusBadRequest, "action 需为 fulfill 或 reject")
		return
	}
	ctx := c.Request.Context()
	if err := h.repo.ResolveShopOrder(ctx, id, req.Action); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Action == repo.ShopOrderRejected {
		// 拒绝已由 repo 退玉；再通知学生
		if o, err := h.repo.GetShopOrder(ctx, id); err == nil {
			s, _ := h.repo.GetSystemSettings(ctx)
			h.notifyOne(ctx, o.UID, repo.NotifyInput{
				Typ: repo.NotifyWarn, Title: "兑换订单被拒绝，玉已退回",
				Body: "「" + o.ItemName + "」订单 #" + strconv.FormatInt(o.ID, 10),
			}, s, "")
		}
	}
	ok(c, gin.H{"resolved": id})
}
