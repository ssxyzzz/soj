package repo

import (
	"context"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 安全审计日志（v0.14）：登录成功/失败、锁定触发、改密、管理员重置、限流事件。
// securityLogs 集合按 at 建 TTL 索引滚动保留 90 天（Bootstrap 创建，mongod 每分钟清扫）。
type SecurityLog struct {
	// omitempty：零值时不序列化 _id，由服务端生成——否则会显式写入全零 ObjectId，
	// 第二条起全部 E11000 重复键（v0.14 验收实测，回归测试见 security_test.go）。
	ID       bson.ObjectID `bson:"_id,omitempty"`
	At       time.Time     `bson:"at"`
	Type     string        `bson:"type"`
	UID      int64         `bson:"uid,omitempty"`
	Username string        `bson:"uname,omitempty"` // 登录尝试的目标用户名（账号不存在也记录）
	IP       string        `bson:"ip,omitempty"`
	UA       string        `bson:"ua,omitempty"`
	Detail   string        `bson:"detail,omitempty"`
}

// 事件类型（后台安全日志页的类型筛选同源）。
const (
	SecLoginSuccess  = "login_success"
	SecLoginFailure  = "login_failure"
	SecLoginLocked   = "login_locked"
	SecPwdChange     = "password_change"
	SecPwdResetAdmin = "password_reset_admin"
	SecRateLimited   = "rate_limited"
)

var SecurityLogTypes = []string{
	SecLoginSuccess, SecLoginFailure, SecLoginLocked,
	SecPwdChange, SecPwdResetAdmin, SecRateLimited,
}

func (r *Repo) AddSecurityLog(ctx context.Context, e SecurityLog) error {
	e.At = time.Now()
	_, err := r.db.Collection("securityLogs").InsertOne(ctx, e)
	return err
}

// ListSecurityLogs 分页倒序；typ 精确过滤，q 按用户名/IP 模糊（ QuoteMeta 防注入正则）。
func (r *Repo) ListSecurityLogs(ctx context.Context, page, pageSize int, typ, q string) ([]SecurityLog, int64, error) {
	filter := bson.M{}
	if typ != "" {
		filter["type"] = typ
	}
	if kw := strings.TrimSpace(q); kw != "" {
		pattern := regexp.QuoteMeta(kw)
		filter["$or"] = bson.A{
			bson.M{"uname": bson.M{"$regex": pattern, "$options": "i"}},
			bson.M{"ip": bson.M{"$regex": pattern, "$options": "i"}},
		}
	}
	total, err := r.db.Collection("securityLogs").CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.db.Collection("securityLogs").Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "at", Value: -1}}).
			SetLimit(int64(pageSize)).
			SetSkip(int64((page - 1) * pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]SecurityLog, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
