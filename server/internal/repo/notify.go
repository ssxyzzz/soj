package repo

// 站内通知中心（v0.24）：单集合 notifications，事件方插入、收件方查询。
// 聚合降噪：互动类（reply/like）对同一 (uid, typ, refId) 的未读通知合并计数（aggCount），
// 点开清零；公告/发布类每事件一条不聚合。保留期 purgeAt 在插入时按当时设置写死
//（TTL 索引清扫，改动保留天数只影响新通知）。
//
// 邮件转发：仅 1:1 互动类（reply/mention/like/accept）支持——发布/广播类是扇出，
// SMTP 逐条发送会拖垮请求；用户偏好 notifyEmail 存开通的类型列表（默认全关）。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 通知类型（前端图标/文案同源）。
const (
	NotifyReply      = "reply"      // 讨论被回复 / 回复被回复
	NotifyMention    = "mention"    // 被 @
	NotifyLike       = "like"       // 被点赞
	NotifyAccept     = "accept"     // 回复被采纳
	NotifyAssignment = "assignment" // 作业发布
	NotifyExam       = "exam"       // 考试发布/开考
	NotifyContest    = "contest"    // 比赛发布/开赛
	NotifyBroadcast  = "broadcast"  // 全站公告
	NotifyWarn       = "warn"       // 管理处置告知（删帖/禁言/警告）
)

// NotifyMailCategories 邮件转发可开通的类型（1:1 互动类）。
var NotifyMailCategories = []string{NotifyReply, NotifyMention, NotifyLike, NotifyAccept}

// Notification 通知文档。
type Notification struct {
	ID        int64     `bson:"_id"`
	UID       int64     `bson:"uid"`
	Typ       string    `bson:"typ"`
	Title     string    `bson:"title"`
	Body      string    `bson:"body"`
	Link      string    `bson:"link,omitempty"` // 前端深链（/discuss/:tid 等）
	RefID     int64     `bson:"refId"`          // 聚合键：话题/回复/场次 id
	AggCount  int64     `bson:"aggCount"`       // 聚合计数（≥1）
	Read      bool      `bson:"read,omitempty"`
	ReadAt    time.Time `bson:"readAt,omitempty"`
	CreatedAt time.Time `bson:"createdAt"`
	PurgeAt   time.Time `bson:"purgeAt"`
}

// NotifyInput 通知内容。
type NotifyInput struct {
	Typ   string
	Title string
	Body  string
	Link  string
	RefID int64
}

// UpsertNotification 单用户通知：互动类对未读同源通知聚合，其余直插。
// _id 一律显式取号（int64）——upsert 缺 _id 时 Mongo 自动生成 ObjectId，读取侧按
// int64 解码即报错（v0.24 验收实锤「角标正常、列表读取失败」的根因）。
func (r *Repo) UpsertNotification(ctx context.Context, uid int64, in NotifyInput, retentionDays int) error {
	purge := time.Now().AddDate(0, 0, retentionDays)
	if in.Typ == NotifyReply || in.Typ == NotifyMention || in.Typ == NotifyLike || in.Typ == NotifyAccept {
		id, err := r.nextSeq(ctx, "notify")
		if err != nil {
			return err
		}
		// 同源未读通知合并：只增计数（首条文案保留，避免抖动）
		_, err = r.Notifications.UpdateOne(ctx,
			bson.M{"uid": uid, "typ": in.Typ, "refId": in.RefID, "read": bson.M{"$ne": true}},
			bson.M{"$inc": bson.M{"aggCount": 1}, "$setOnInsert": bson.M{
				"_id": id,
				"title": in.Title, "body": in.Body, "link": in.Link,
				"createdAt": time.Now(), "purgeAt": purge,
			}},
			options.UpdateOne().SetUpsert(true))
		return err
	}
	id, err := r.nextSeq(ctx, "notify")
	if err != nil {
		return err
	}
	_, err = r.Notifications.InsertOne(ctx, newNotification(id, uid, in, purge))
	return err
}

func newNotification(id, uid int64, in NotifyInput, purge time.Time) Notification {
	return Notification{
		ID: id,
		UID: uid, Typ: in.Typ, Title: in.Title, Body: in.Body, Link: in.Link,
		RefID: in.RefID, AggCount: 1, CreatedAt: time.Now(), PurgeAt: purge,
	}
}

// NotifyUsers 扇出：给一组用户各插一条（不聚合）。取号一次拿一段连续序号，
// 避免逐条取号的 N 次往返；量大时（全站广播）由调用方分批。
func (r *Repo) NotifyUsers(ctx context.Context, uids []int64, in NotifyInput, retentionDays int) error {
	if len(uids) == 0 {
		return nil
	}
	base, err := r.nextSeqBatch(ctx, "notify", int64(len(uids)))
	if err != nil {
		return err
	}
	purge := time.Now().AddDate(0, 0, retentionDays)
	start := base - int64(len(uids)) + 1
	docs := make([]any, 0, len(uids))
	now := time.Now()
	for i, uid := range uids {
		docs = append(docs, Notification{
			ID: start + int64(i),
			UID: uid, Typ: in.Typ, Title: in.Title, Body: in.Body, Link: in.Link,
			RefID: in.RefID, AggCount: 1, CreatedAt: now, PurgeAt: purge,
		})
	}
	_, err = r.Notifications.InsertMany(ctx, docs)
	return err
}

// ListNotifications 收件箱分页（最新在前；可筛未读/类型）。
func (r *Repo) ListNotifications(ctx context.Context, uid int64, unreadOnly bool, typ string, page, pageSize int) ([]*Notification, int64, error) {
	filter := bson.M{"uid": uid}
	if unreadOnly {
		filter["read"] = bson.M{"$ne": true}
	}
	if typ != "" {
		filter["typ"] = typ
	}
	total, err := r.Notifications.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.Notifications.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetLimit(int64(pageSize)).SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*Notification, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// UnreadNotificationCount 未读数（顶栏角标轮询）。
func (r *Repo) UnreadNotificationCount(ctx context.Context, uid int64) (int64, error) {
	return r.Notifications.CountDocuments(ctx, bson.M{"uid": uid, "read": bson.M{"$ne": true}})
}

// MarkNotificationsRead 指定通知标已读（空列表 = 全部）。
func (r *Repo) MarkNotificationsRead(ctx context.Context, uid int64, ids []int64) error {
	filter := bson.M{"uid": uid, "read": bson.M{"$ne": true}}
	if len(ids) > 0 {
		filter["_id"] = bson.M{"$in": ids}
	}
	_, err := r.Notifications.UpdateMany(ctx, filter,
		bson.M{"$set": bson.M{"read": true, "readAt": time.Now()}})
	return err
}

// AllUserIDs 全站用户 id（比赛发布/全站广播扇出用；班级/社团量级直查安全）。
func (r *Repo) AllUserIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	if err := r.Users.Distinct(ctx, "_id", bson.M{}).Decode(&ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// ClassMemberIDs 班级全体成员 id（作业/考试发布扇出用）。
func (r *Repo) ClassMemberIDs(ctx context.Context, classID int64) ([]int64, error) {
	members, err := r.ListMembers(ctx, classID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UID)
	}
	return ids, nil
}

// ListRoundsStartingSoon 即将开赛/开考的场次（开赛提醒扫描；startNotified 防重发）。
// 窗口：start ∈ (now−1h, now+ahead]——服务重启错过整点的也能补发，过旧的不补。
// 两集合合并返回，kind 由调用方按 ClassID 判定（考试恒 classId>0，比赛恒 0）。
func (r *Repo) ListRoundsStartingSoon(ctx context.Context, ahead time.Duration) ([]*Contest, error) {
	lo := time.Now().Add(-time.Hour)
	hi := time.Now().Add(ahead)
	filter := bson.M{"start": bson.M{"$gt": lo, "$lte": hi}, "startNotified": bson.M{"$ne": true}}
	var out []*Contest
	for _, kind := range []string{KindContest, KindExam} {
		cursor, err := r.roundsCol(kind).Find(ctx, filter)
		if err != nil {
			return nil, err
		}
		var rounds []*Contest
		if err := cursor.All(ctx, &rounds); err != nil {
			cursor.Close(ctx)
			return nil, err
		}
		cursor.Close(ctx)
		out = append(out, rounds...)
	}
	return out, nil
}

// MarkRoundStartNotified 开赛提醒已发标记（防重发）。
func (r *Repo) MarkRoundStartNotified(ctx context.Context, kind string, id int64) error {
	_, err := r.roundsCol(kind).UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{"startNotified": true}})
	return err
}

// InsertBroadcastRecord 全站广播留档（频控计数 + 审计）。
func (r *Repo) InsertBroadcastRecord(ctx context.Context, byUID int64, title, body string) (int64, error) {
	id, err := r.nextSeq(ctx, "broadcast")
	if err != nil {
		return 0, err
	}
	_, err = r.db.Collection("broadcasts").InsertOne(ctx, bson.M{
		"_id": id, "byUid": byUID, "title": title, "body": body, "at": time.Now(),
	})
	return id, err
}

// CountBroadcastsToday 当天已发广播数（频控；本地零点起算——Truncate(24h) 是 UTC 零点
// = 北京 08:00 切窗，v0.27 根修为 time.Local 零点）。
func (r *Repo) CountBroadcastsToday(ctx context.Context) (int64, error) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return r.db.Collection("broadcasts").CountDocuments(ctx, bson.M{"at": bson.M{"$gte": dayStart}})
}
