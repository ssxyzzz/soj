package repo

// 实物兑换（v0.33-②，plan §2.39）：默认整模块关（settings.jadeShopEnabled，仅 root 可开）。
// 物品目录（名称/图 URL/价/库存/每人限购/上下架）root 管理；学生下单原子扣玉 + 条件扣库存
// （失败互回滚），通知 root 人工履约（标记已发放或拒绝退玉）。纯登记制：不做支付不做物流。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ShopItem 兑换物品。
type ShopItem struct {
	ID           int64     `bson:"_id"`
	Name         string    `bson:"name"`
	Image        string    `bson:"image,omitempty"` // 图片 URL（可空 = 纯文字卡）
	Price        int64     `bson:"price"`           // 玉
	Stock        int64     `bson:"stock"`           // -1 = 不限库存
	PerUserLimit int64     `bson:"perUserLimit,omitempty"` // 0 = 不限购
	Active       bool      `bson:"active"`
	CreatedAt    time.Time `bson:"createdAt"`
}

// ShopOrder 兑换订单（人工履约登记）。
type ShopOrder struct {
	ID        int64     `bson:"_id"`
	ItemID    int64     `bson:"itemId"`
	ItemName  string    `bson:"itemName"`
	UID       int64     `bson:"uid"`
	Uname     string    `bson:"uname"`
	Price     int64     `bson:"price"` // 下单时价（物价后调不追）
	Status    string    `bson:"status"` // pending | fulfilled | rejected
	Note      string    `bson:"note,omitempty"` // 学生留言
	CreatedAt time.Time `bson:"createdAt"`
	DoneAt    time.Time `bson:"doneAt,omitempty"`
}

const (
	ShopOrderPending   = "pending"
	ShopOrderFulfilled = "fulfilled"
	ShopOrderRejected  = "rejected"
)

// ErrShopClosed / ErrShopOutOfStock / ErrShopLimitReached 下单三类可预期失败。
var (
	ErrShopClosed       = errors.New("兑换未开放")
	ErrShopOutOfStock   = errors.New("库存不足")
	ErrShopLimitReached = errors.New("超出每人限购")
)

// EnsureShopIndexes（Bootstrap）：订单 {uid,_id}、{status,_id}。
func (r *Repo) EnsureShopIndexes(ctx context.Context) error {
	_, err := r.db.Collection("shopOrders").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "_id", Value: -1}}},
	})
	return err
}

// ListActiveShopItems 上架物品（学生侧）。
func (r *Repo) ListActiveShopItems(ctx context.Context) ([]*ShopItem, error) {
	cursor, err := r.db.Collection("shopItems").Find(ctx,
		bson.M{"active": true},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var list []*ShopItem
	return list, cursor.All(ctx, &list)
}

// ListShopItems 全量目录（root 管理侧）。
func (r *Repo) ListShopItems(ctx context.Context) ([]*ShopItem, error) {
	cursor, err := r.db.Collection("shopItems").Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var list []*ShopItem
	return list, cursor.All(ctx, &list)
}

// UpsertShopItem 新建（ID=0）或更新物品。
func (r *Repo) UpsertShopItem(ctx context.Context, it *ShopItem) error {
	if it.ID == 0 {
		id, err := r.nextSeq(ctx, "shopItem")
		if err != nil {
			return err
		}
		it.ID = id
		it.CreatedAt = time.Now()
		_, err = r.db.Collection("shopItems").InsertOne(ctx, it)
		return err
	}
	_, err := r.db.Collection("shopItems").UpdateOne(ctx, bson.M{"_id": it.ID},
		bson.M{"$set": bson.M{
			"name": it.Name, "image": it.Image, "price": it.Price,
			"stock": it.Stock, "perUserLimit": it.PerUserLimit, "active": it.Active,
		}})
	return err
}

// DeleteShopItem 删除物品（不影响历史订单）。
func (r *Repo) DeleteShopItem(ctx context.Context, id int64) error {
	res, err := r.db.Collection("shopItems").DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// PlaceShopOrder 下单：限购检查 → 条件扣库存（stock>0 或 -1 不限）→ 扣玉（失败回滚库存）→
// 落订单。返回订单 ID；可预期失败走哨兵错误。
func (r *Repo) PlaceShopOrder(ctx context.Context, uid int64, uname string, itemID int64, note string) (int64, error) {
	var it ShopItem
	if err := r.db.Collection("shopItems").FindOne(ctx, bson.M{"_id": itemID}).Decode(&it); err != nil {
		return 0, err
	}
	if !it.Active {
		return 0, ErrShopClosed
	}
	if it.PerUserLimit > 0 {
		n, err := r.db.Collection("shopOrders").CountDocuments(ctx,
			bson.M{"uid": uid, "itemId": itemID, "status": bson.M{"$ne": ShopOrderRejected}})
		if err != nil {
			return 0, err
		}
		if n >= it.PerUserLimit {
			return 0, ErrShopLimitReached
		}
	}
	// 条件扣库存（-1 不限：用 $or 表达）
	dec := bson.M{"$inc": bson.M{"stock": -1}}
	cond := bson.M{"_id": itemID, "active": true, "$or": bson.A{
		bson.M{"stock": -1}, bson.M{"stock": bson.M{"$gt": 0}},
	}}
	res, err := r.db.Collection("shopItems").UpdateOne(ctx, cond, dec)
	if err != nil {
		return 0, err
	}
	if res.MatchedCount == 0 {
		return 0, ErrShopOutOfStock
	}
	rollback := func() { _, _ = r.db.Collection("shopItems").UpdateOne(ctx, bson.M{"_id": itemID}, bson.M{"$inc": bson.M{"stock": 1}}) }
	if err := r.JadeSpend(ctx, uid, it.Price, JadeKindShopOrder,
		fmt.Sprintf("兑换「%s」", it.Name), itemID); err != nil {
		if err == ErrInsufficientJade {
			rollback()
			return 0, err
		}
		rollback()
		return 0, err
	}
	id, err := r.nextSeq(ctx, "shopOrder")
	if err != nil {
		rollback()
		return 0, err
	}
	_, err = r.db.Collection("shopOrders").InsertOne(ctx, &ShopOrder{
		ID: id, ItemID: it.ID, ItemName: it.Name, UID: uid, Uname: uname,
		Price: it.Price, Status: ShopOrderPending, Note: note, CreatedAt: time.Now(),
	})
	if err != nil {
		rollback()
		return 0, err
	}
	return id, nil
}

// ListShopOrders 订单列表（root；status 空 = 全部）。
func (r *Repo) ListShopOrders(ctx context.Context, status string, page, pageSize int) ([]*ShopOrder, int64, error) {
	q := bson.M{}
	if status != "" {
		q["status"] = status
	}
	total, err := r.db.Collection("shopOrders").CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.db.Collection("shopOrders").Find(ctx, q,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var list []*ShopOrder
	return list, total, cursor.All(ctx, &list)
}

// MyShopOrders 本人订单。
func (r *Repo) MyShopOrders(ctx context.Context, uid int64) ([]*ShopOrder, error) {
	cursor, err := r.db.Collection("shopOrders").Find(ctx, bson.M{"uid": uid},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(50))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var list []*ShopOrder
	return list, cursor.All(ctx, &list)
}

// ResolveShopOrder 履约/拒绝（root）：pending → fulfilled（不退玉）/ rejected（全额退玉）。
func (r *Repo) ResolveShopOrder(ctx context.Context, orderID int64, action string) error {
	var o ShopOrder
	if err := r.db.Collection("shopOrders").FindOne(ctx, bson.M{"_id": orderID}).Decode(&o); err != nil {
		return err
	}
	res, err := r.db.Collection("shopOrders").UpdateOne(ctx,
		bson.M{"_id": orderID, "status": ShopOrderPending},
		bson.M{"$set": bson.M{"status": action, "doneAt": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errors.New("订单不存在或已处理")
	}
	if action == ShopOrderRejected {
		if err := r.JadeCredit(ctx, o.UID, o.Price, JadeKindBountyRefund,
			fmt.Sprintf("兑换被拒退回（订单 #%d「%s」）", o.ID, o.ItemName), o.ID); err != nil {
			return err
		}
	}
	return nil
}

// GetShopOrder 单订单（履约通知面）。
func (r *Repo) GetShopOrder(ctx context.Context, id int64) (*ShopOrder, error) {
	var o ShopOrder
	if err := r.db.Collection("shopOrders").FindOne(ctx, bson.M{"_id": id}).Decode(&o); err != nil {
		return nil, err
	}
	return &o, nil
}

// UserIDsByRole 某角色全部用户 uid（下单通知 root 用）。
func (r *Repo) UserIDsByRole(ctx context.Context, role string) ([]int64, error) {
	cursor, err := r.users.Find(ctx, bson.M{"role": role},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		ID int64 `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}
