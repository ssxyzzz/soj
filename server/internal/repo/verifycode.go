package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 邮箱验证码（v0.16）：6 位数字、5 分钟有效、一次性、用途绑定（purpose =
// register/reset/bind，注册发的码不能用于找回）、验证失败 5 次即作废。
// 库里只存 HMAC-SHA256 哈希不存明文（密钥由 JWT secret 域分隔派生，见 server 层）。
//
// 发送频控（五层，发码 handler 依次检查）：
//  1. 同目标 60s 冷却；2. 同目标 1 小时 5 条；3. 同目标 24 小时 10 条；
//  4. 同 IP 每日 30 条；5. 全局每日 300 条。
//
// 文档保留 25 小时（purgeAt TTL 索引自动清扫）——频控按 createdAt 计数，
// 不能让 5 分钟过期的码被提前删掉导致计数失真。
const (
	VerifyPurposeRegister = "register"
	VerifyPurposeReset    = "reset"
	VerifyPurposeBind     = "bind"

	VerifyCodeTTL      = 5 * time.Minute  // 验证码有效期
	VerifyCodePurgeTTL = 25 * time.Hour   // 文档保留时长（频控计数窗口覆盖）
	VerifyCodeMaxFails = 5                // 同一码验证失败次数上限
	verifyCodeResend   = 60 * time.Second // 同目标重发冷却
)

var VerifyPurposes = []string{VerifyPurposeRegister, VerifyPurposeReset, VerifyPurposeBind}

type VerifyCode struct {
	ID        int64     `bson:"_id"`
	Purpose   string    `bson:"purpose"`
	Target    string    `bson:"target"` // 归一化邮箱（trim + 小写）
	CodeHash  string    `bson:"codeHash"`
	IP        string    `bson:"ip,omitempty"`
	Attempts  int       `bson:"attempts,omitempty"`
	CreatedAt time.Time `bson:"createdAt"`
	ExpiresAt time.Time `bson:"expiresAt"` // 有效期（5 分钟）
	PurgeAt   time.Time `bson:"purgeAt"`   // TTL 索引字段（25 小时后物理删除）
}

// InsertVerifyCode 发码落库（先发信后落库，调用方保证 code 已成功投递）。
// 同目标同用途的旧码一并作废（删文档）：始终只有最新一码可用。
func (r *Repo) InsertVerifyCode(ctx context.Context, vc *VerifyCode) error {
	id, err := r.nextSeq(ctx, "verifyCode")
	if err != nil {
		return err
	}
	vc.ID = id
	if _, err := r.db.Collection("verifyCodes").DeleteMany(ctx, bson.M{
		"purpose": vc.Purpose, "target": vc.Target,
	}); err != nil {
		return err
	}
	_, err = r.db.Collection("verifyCodes").InsertOne(ctx, vc)
	return err
}

// GetValidVerifyCode 取目标当前有效的码（未过期）；无则 mongo.ErrNoDocuments。
// 校验与剩余尝试次数判断共用。
func (r *Repo) GetValidVerifyCode(ctx context.Context, purpose, target string) (*VerifyCode, error) {
	var vc VerifyCode
	err := r.db.Collection("verifyCodes").FindOne(ctx, bson.M{
		"purpose": purpose, "target": target, "expiresAt": bson.M{"$gt": time.Now()},
	}, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})).Decode(&vc)
	if err != nil {
		return nil, err
	}
	return &vc, nil
}

// ConsumeVerifyCode 验证成功即删（一次性）。
func (r *Repo) ConsumeVerifyCode(ctx context.Context, id int64) error {
	_, err := r.db.Collection("verifyCodes").DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// FailVerifyCode 验证失败记一次；达到 5 次作废（删文档）。
// 返回该码剩余可尝试次数。
func (r *Repo) FailVerifyCode(ctx context.Context, id int64, already int) (int, error) {
	attempts := already + 1
	if attempts >= VerifyCodeMaxFails {
		_, err := r.db.Collection("verifyCodes").DeleteOne(ctx, bson.M{"_id": id})
		return 0, err
	}
	_, err := r.db.Collection("verifyCodes").UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{"attempts": attempts}})
	return VerifyCodeMaxFails - attempts, err
}

// VerifyCodeRateQuery 频控检查结果。
type VerifyCodeRateQuery struct {
	LastAt   time.Time // 同目标最近一条的发送时间（60s 冷却）
	Target1h int64     // 同目标近 1 小时条数
	Target1d int64     // 同目标近 24 小时条数
	IP1d     int64     // 该 IP 当日条数
	Global1d int64     // 全局当日条数
}

// QueryVerifyCodeRate 一次聚合出五层频控所需计数。
// day 窗口按自然日（同时区），与「每日上限」口径一致。
func (r *Repo) QueryVerifyCodeRate(ctx context.Context, purpose, target, ip string) (*VerifyCodeRateQuery, error) {
	col := r.db.Collection("verifyCodes")
	q := &VerifyCodeRateQuery{}
	var vc VerifyCode
	err := col.FindOne(ctx, bson.M{"purpose": purpose, "target": target},
		options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
		options.FindOne().SetProjection(bson.M{"createdAt": 1})).Decode(&vc)
	if err == nil {
		q.LastAt = vc.CreatedAt
	} else if !IsNotFound(err) {
		return nil, err
	}
	now := time.Now()
	h1 := now.Add(-time.Hour)
	d1 := now.Add(-24 * time.Hour)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if q.Target1h, err = col.CountDocuments(ctx, bson.M{
		"purpose": purpose, "target": target, "createdAt": bson.M{"$gt": h1}}); err != nil {
		return nil, err
	}
	if q.Target1d, err = col.CountDocuments(ctx, bson.M{
		"purpose": purpose, "target": target, "createdAt": bson.M{"$gt": d1}}); err != nil {
		return nil, err
	}
	if q.IP1d, err = col.CountDocuments(ctx, bson.M{
		"ip": ip, "createdAt": bson.M{"$gte": dayStart}}); err != nil {
		return nil, err
	}
	if q.Global1d, err = col.CountDocuments(ctx, bson.M{
		"createdAt": bson.M{"$gte": dayStart}}); err != nil {
		return nil, err
	}
	return q, nil
}
