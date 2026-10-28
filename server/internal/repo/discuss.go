package repo

// 讨论区（v0.24）：通用帖模型 + 三大挂载面——导航栏学习分享（global）、题内思路讨论
// （problem）、主题库比赛讨论（contest）。班级/考试刻意不做（需求定稿）；集合按
// scope+scopeId 通用设计，将来加挂载点零迁移。
//
// 防水口径（plan.md §2.25）：挂载式讨论无自由发帖入口 + 发帖必选类型 + 限速 + 冷静期；
// 题内禁图（防截图代码绕过分数门槛）；比赛赛时提问默认仅组织者可见、可升格公开。

import (
	"context"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 挂载面。
const (
	DiscussScopeProblem = "problem"
	DiscussScopeContest = "contest"
	DiscussScopeGlobal  = "global"
)

// 分类/类型（发帖必选，按挂载面各有一套）：
// global 四类面向学习分享；problem 三类面向题内互助；contest 两类赛时问答。
const (
	DiscussKindExperience = "experience" // 学习心得
	DiscussKindRoute      = "route"      // 刷题路线
	DiscussKindTutorial   = "tutorial"   // 知识讲解
	DiscussKindFeedback   = "feedback"   // 站点反馈
	DiscussKindHelp       = "help"       // 求助
	DiscussKindBug        = "bug"        // 纠错
	DiscussKindShare      = "share"      // 分享
	DiscussKindQuestion   = "question"   // 赛时提问
	DiscussKindNotice     = "notice"     // 组织者公告
)

// DiscussValidKinds 各挂载面的合法类型集合。
var DiscussValidKinds = map[string]map[string]bool{
	DiscussScopeGlobal: {DiscussKindExperience: true, DiscussKindRoute: true, DiscussKindTutorial: true, DiscussKindFeedback: true},
	DiscussScopeProblem: {DiscussKindHelp: true, DiscussKindBug: true, DiscussKindShare: true},
	DiscussScopeContest: {DiscussKindQuestion: true, DiscussKindNotice: true},
}

// DiscussTopic 主题帖。
type DiscussTopic struct {
	ID        int64     `bson:"_id"`
	Scope     string    `bson:"scope"`
	ScopeID   int64     `bson:"scopeId"` // problem/contest 的 id；global 恒 0
	UID       int64     `bson:"uid"`
	Kind      string    `bson:"kind"`
	Title     string    `bson:"title"`
	Content   string    `bson:"content"` // Markdown（global 单一事实源；题内代码块按门槛读时裁剪）
	// global 实体关联（可选）：相关题目（多选）/ 题单 / 知识树节点
	RelatedPids []int64 `bson:"relatedPids,omitempty"`
	ListID      int64   `bson:"listId,omitempty"`
	TagID       int64   `bson:"tagId,omitempty"`

	ReplyCount      int64     `bson:"replyCount"`
	LikeCount       int64     `bson:"likeCount"`
	AcceptedReplyID int64     `bson:"acceptedReplyId,omitempty"` // 题内：楼主采纳的回复（0 = 未采纳）
	Official        bool      `bson:"official,omitempty"`        // 题内：教师群体已回应
	Pinned          bool      `bson:"pinned,omitempty"`
	Featured        bool      `bson:"featured,omitempty"`
	Locked          bool      `bson:"locked,omitempty"` // 锁定 = 不能再回复，可看
	Private         bool      `bson:"private,omitempty"` // 比赛赛时：仅作者与组织者可见
	PublicAt        time.Time `bson:"publicAt,omitempty"` // 组织者升格公开时刻

	Deleted  bool   `bson:"deleted,omitempty"` // 软删除（有回复的主楼保楼层）
	Edited   bool   `bson:"edited,omitempty"`
	ReportN  int64  `bson:"reportN,omitempty"` // 未处理举报数（自动折叠阈值用）
	Folded   bool   `bson:"folded,omitempty"`  // 举报达标自动折叠（点开才显示）

	CreatedAt    time.Time `bson:"createdAt"`
	UpdatedAt    time.Time `bson:"updatedAt"`
	LastActiveAt time.Time `bson:"lastActiveAt"` // 回复时前移，列表按活跃排序
}

// DiscussReply 回复（两层楼中楼：ReplyTo=0 顶层，否则指向另一条回复）。
type DiscussReply struct {
	ID      int64     `bson:"_id"`
	TID     int64     `bson:"tid"`
	UID     int64     `bson:"uid"`
	Content string    `bson:"content"`
	ReplyTo int64     `bson:"replyTo,omitempty"`
	LikeCount int64   `bson:"likeCount"`
	Accepted  bool    `bson:"accepted,omitempty"`
	Deleted   bool    `bson:"deleted,omitempty"`
	Edited    bool    `bson:"edited,omitempty"`
	ReportN   int64   `bson:"reportN,omitempty"`
	Folded    bool    `bson:"folded,omitempty"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// discussTarget 折叠/点赞等操作的目标定位。
func discussTargetCol(r *Repo, targetType string) *mongo.Collection {
	if targetType == "reply" {
		return r.DiscussReplies
	}
	return r.Discussions
}

// ---------- 主题帖 CRUD ----------

func (r *Repo) InsertDiscussTopic(ctx context.Context, t *DiscussTopic) error {
	id, err := r.nextSeq(ctx, "discussTopic")
	if err != nil {
		return err
	}
	t.ID = id
	now := time.Now()
	t.CreatedAt, t.UpdatedAt, t.LastActiveAt = now, now, now
	_, err = r.Discussions.InsertOne(ctx, t)
	return err
}

func (r *Repo) GetDiscussTopic(ctx context.Context, tid int64) (*DiscussTopic, error) {
	var t DiscussTopic
	if err := r.Discussions.FindOne(ctx, bson.M{"_id": tid}).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// listDiscussTopics 按过滤器分页：置顶优先，其余按最后活跃倒序。
func (r *Repo) listDiscussTopics(ctx context.Context, filter bson.M, page, pageSize int) ([]*DiscussTopic, int64, error) {
	total, err := r.Discussions.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.Discussions.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "pinned", Value: -1}, {Key: "lastActiveAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(pageSize)).SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*DiscussTopic, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListGlobalTopics 导航栏讨论区列表（可按类型筛选；关键词在标题里找）。
func (r *Repo) ListGlobalTopics(ctx context.Context, kind, q string, page, pageSize int) ([]*DiscussTopic, int64, error) {
	filter := bson.M{"scope": DiscussScopeGlobal, "deleted": bson.M{"$ne": true}}
	if kind != "" {
		filter["kind"] = kind
	}
	if q != "" {
		filter["title"] = bson.M{"$regex": regexp.QuoteMeta(q), "$options": "i"}
	}
	return r.listDiscussTopics(ctx, filter, page, pageSize)
}

// ListScopeTopics 题内/比赛讨论列表。includePrivate=false 时只看公开帖 + 本人私密帖
//（比赛赛时私密提问：组织者视角传 true 看全部；赛后全量公开由调用方传 true）。
func (r *Repo) ListScopeTopics(ctx context.Context, scope string, scopeID, viewerUID int64, includePrivate bool, page, pageSize int) ([]*DiscussTopic, int64, error) {
	filter := bson.M{"scope": scope, "scopeId": scopeID, "deleted": bson.M{"$ne": true}}
	if !includePrivate {
		filter["$or"] = bson.A{
			bson.M{"private": bson.M{"$ne": true}},
			bson.M{"uid": viewerUID},
		}
	}
	return r.listDiscussTopics(ctx, filter, page, pageSize)
}

// CountUserDiscussTopicsToday 某用户当天（本地零点起）发布的主楼数（限速用；
// v0.27 根修：原 Truncate(24h) 是 UTC 零点=北京 08:00 切窗，与注释语义相反）。
func (r *Repo) CountUserDiscussTopicsToday(ctx context.Context, uid int64) (int64, error) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return r.Discussions.CountDocuments(ctx, bson.M{"uid": uid, "createdAt": bson.M{"$gte": dayStart}})
}

// LastUserReplyAt 某用户最近一条回复的时刻（回复间隔限速用；零值 = 没发过）。
func (r *Repo) LastUserReplyAt(ctx context.Context, uid int64) (time.Time, error) {
	var doc struct {
		CreatedAt time.Time `bson:"createdAt"`
	}
	err := r.DiscussReplies.FindOne(ctx, bson.M{"uid": uid},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})).Decode(&doc)
	if IsNotFound(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return doc.CreatedAt, nil
}

// UpdateDiscussTopicContent 作者编辑（保「已编辑」痕迹）。
func (r *Repo) UpdateDiscussTopicContent(ctx context.Context, tid int64, title, content, kind string, relatedPids []int64, listID, tagID int64) error {
	set := bson.M{"content": content, "edited": true, "updatedAt": time.Now()}
	if title != "" {
		set["title"] = title
	}
	if kind != "" {
		set["kind"] = kind
	}
	if relatedPids != nil {
		set["relatedPids"] = relatedPids
	}
	if listID > 0 {
		set["listId"] = listID
	}
	if tagID > 0 {
		set["tagId"] = tagID
	}
	res, err := r.Discussions.UpdateOne(ctx, bson.M{"_id": tid}, bson.M{"$set": set})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// SoftDeleteDiscussTopic 软删除主楼（保留楼层结构）。
func (r *Repo) SoftDeleteDiscussTopic(ctx context.Context, tid, byUID int64) error {
	res, err := r.Discussions.UpdateOne(ctx, bson.M{"_id": tid},
		bson.M{"$set": bson.M{"deleted": true, "updatedAt": time.Now(), "lastActiveAt": time.Now()}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// SetDiscussTopicFlags 管理动作：置顶/加精/锁定/升格公开（字段指针 nil = 不动）。
func (r *Repo) SetDiscussTopicFlags(ctx context.Context, tid int64, pinned, featured, locked, private *bool) error {
	set := bson.M{"updatedAt": time.Now()}
	if pinned != nil {
		set["pinned"] = *pinned
	}
	if featured != nil {
		set["featured"] = *featured
	}
	if locked != nil {
		set["locked"] = *locked
	}
	if private != nil {
		set["private"] = *private
		if !*private {
			set["publicAt"] = time.Now()
		}
	}
	res, err := r.Discussions.UpdateOne(ctx, bson.M{"_id": tid}, bson.M{"$set": set})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// IncDiscussTopicReplies 回复数 +1 并前移活跃时间。
func (r *Repo) IncDiscussTopicReplies(ctx context.Context, tid int64) error {
	_, err := r.Discussions.UpdateOne(ctx, bson.M{"_id": tid},
		bson.M{"$inc": bson.M{"replyCount": 1}, "$set": bson.M{"lastActiveAt": time.Now()}})
	return err
}

// AcceptDiscussReply 楼主采纳回复（一帖一条，后选覆盖）。
func (r *Repo) AcceptDiscussReply(ctx context.Context, tid, rid int64) error {
	res, err := r.Discussions.UpdateOne(ctx, bson.M{"_id": tid},
		bson.M{"$set": bson.M{"acceptedReplyId": rid, "updatedAt": time.Now()}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// SetDiscussReplyAccepted 单条回复的采纳标记。
func (r *Repo) SetDiscussReplyAccepted(ctx context.Context, rid int64, accepted bool) error {
	res, err := r.DiscussReplies.UpdateOne(ctx, bson.M{"_id": rid},
		bson.M{"$set": bson.M{"accepted": accepted}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// MarkDiscussOfficial 教师回应标记（题内求助帖官方回应徽标）。
func (r *Repo) MarkDiscussOfficial(ctx context.Context, tid int64) error {
	_, err := r.Discussions.UpdateOne(ctx, bson.M{"_id": tid}, bson.M{"$set": bson.M{"official": true}})
	return err
}

// ---------- 回复 ----------

func (r *Repo) InsertDiscussReply(ctx context.Context, rep *DiscussReply) error {
	id, err := r.nextSeq(ctx, "discussReply")
	if err != nil {
		return err
	}
	rep.ID = id
	rep.CreatedAt, rep.UpdatedAt = time.Now(), time.Now()
	_, err = r.DiscussReplies.InsertOne(ctx, rep)
	return err
}

// DiscussReplyPageSize 话题详情主楼分页大小（v0.26）。
const DiscussReplyPageSize = 20

// ListDiscussReplyPage 主楼分页（v0.26）：返回一页顶层回复（楼层正序）与顶层总数——
// 原「全部回复一次下发」在千楼热帖下响应大、前端渲染重；楼中楼由
// ListDiscussReplyChildren 按本页主楼批量取（写入端已归一到顶层，单层查询即可）。
// replyTo 带 omitempty：顶层楼字段缺失，$in [0, nil] 同时命中存量显式 0。
func (r *Repo) ListDiscussReplyPage(ctx context.Context, tid int64, page int) ([]*DiscussReply, int64, error) {
	filter := bson.M{"tid": tid, "replyTo": bson.M{"$in": bson.A{0, nil}}}
	total, err := r.DiscussReplies.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.DiscussReplies.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetSkip(int64((page - 1) * DiscussReplyPageSize)).
			SetLimit(int64(DiscussReplyPageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*DiscussReply, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListDiscussReplyChildren 指定主楼集合的全部楼中楼（批量一次取，楼层正序）。
func (r *Repo) ListDiscussReplyChildren(ctx context.Context, tid int64, topIDs []int64) ([]*DiscussReply, error) {
	if len(topIDs) == 0 {
		return nil, nil
	}
	cursor, err := r.DiscussReplies.Find(ctx,
		bson.M{"tid": tid, "replyTo": bson.M{"$in": topIDs}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*DiscussReply, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) GetDiscussReply(ctx context.Context, rid int64) (*DiscussReply, error) {
	var rep DiscussReply
	if err := r.DiscussReplies.FindOne(ctx, bson.M{"_id": rid}).Decode(&rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func (r *Repo) UpdateDiscussReplyContent(ctx context.Context, rid int64, content string) error {
	res, err := r.DiscussReplies.UpdateOne(ctx, bson.M{"_id": rid},
		bson.M{"$set": bson.M{"content": content, "edited": true, "updatedAt": time.Now()}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

func (r *Repo) SoftDeleteDiscussReply(ctx context.Context, rid int64) error {
	res, err := r.DiscussReplies.UpdateOne(ctx, bson.M{"_id": rid},
		bson.M{"$set": bson.M{"deleted": true, "updatedAt": time.Now()}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// CountUserAcceptedAnswers 用户被采纳的回复数（个人主页「解答数」）。
func (r *Repo) CountUserAcceptedAnswers(ctx context.Context, uid int64) (int64, error) {
	return r.DiscussReplies.CountDocuments(ctx, bson.M{"uid": uid, "accepted": true, "deleted": bson.M{"$ne": true}})
}

// ---------- 点赞（toggle + 计数） ----------

// ToggleDiscussLike 点赞/取消点赞：幂等 toggle，返回操作后的点赞态与计数。
func (r *Repo) ToggleDiscussLike(ctx context.Context, targetType string, targetID, uid int64) (liked bool, count int64, err error) {
	col := discussTargetCol(r, targetType)
	res, err := r.DiscussLikes.DeleteOne(ctx, bson.M{"t": targetType, "id": targetID, "uid": uid})
	if err != nil {
		return false, 0, err
	}
	delta := -1
	liked = false
	if res.DeletedCount == 0 {
		_, err = r.DiscussLikes.InsertOne(ctx, bson.M{"t": targetType, "id": targetID, "uid": uid, "at": time.Now()})
		if err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return true, 0, nil // 并发重复点赞：按已赞处理（计数以目标文档为准）
			}
			return false, 0, err
		}
		delta, liked = 1, true
	}
	var doc struct {
		LikeCount int64 `bson:"likeCount"`
	}
	err = col.FindOneAndUpdate(ctx, bson.M{"_id": targetID},
		bson.M{"$inc": bson.M{"likeCount": delta}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if err != nil {
		return liked, 0, err
	}
	if doc.LikeCount < 0 { // 计数防御：异常负值归零
		_, _ = col.UpdateOne(ctx, bson.M{"_id": targetID}, bson.M{"$set": bson.M{"likeCount": 0}})
		doc.LikeCount = 0
	}
	return liked, doc.LikeCount, nil
}

// LikedDiscussTargets 当前用户在一组目标里已点赞的集合（列表渲染高亮用）。
func (r *Repo) LikedDiscussTargets(ctx context.Context, uid int64, targetType string, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if uid <= 0 || len(ids) == 0 {
		return out, nil
	}
	cursor, err := r.DiscussLikes.Find(ctx, bson.M{"t": targetType, "uid": uid, "id": bson.M{"$in": ids}})
	if err != nil {
		return out, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc struct {
			ID int64 `bson:"id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return out, err
		}
		out[doc.ID] = true
	}
	return out, cursor.Err()
}

// ---------- 举报（自动折叠 + 后台队列） ----------

// AddDiscussReport 记一笔举报；返回目标当前累计未处理举报数与是否触发自动折叠。
func (r *Repo) AddDiscussReport(ctx context.Context, targetType string, targetID, reporterUID int64, reason string, foldThreshold int64) (count int64, folded bool, err error) {
	col := discussTargetCol(r, targetType)
	if _, err = r.DiscussReports.InsertOne(ctx, bson.M{
		"tType": targetType, "tId": targetID, "uid": reporterUID,
		"reason": reason, "status": "open", "at": time.Now(),
	}); err != nil {
		return 0, false, err
	}
	count, err = r.DiscussReports.CountDocuments(ctx, bson.M{"tType": targetType, "tId": targetID, "status": "open"})
	if err != nil {
		return 0, false, err
	}
	if foldThreshold > 0 && count >= foldThreshold {
		if _, err = col.UpdateOne(ctx, bson.M{"_id": targetID}, bson.M{"$set": bson.M{"folded": true, "reportN": count}}); err != nil {
			return count, false, err
		}
		return count, true, nil
	}
	_, _ = col.UpdateOne(ctx, bson.M{"_id": targetID}, bson.M{"$set": bson.M{"reportN": count}})
	return count, false, nil
}

// DiscussReportItem 后台举报队列条目（聚合目标快照，避免 N+1 查询由 handler 补）。
type DiscussReportItem struct {
	ID         int64     `bson:"_id"`
	TargetType string    `bson:"tType"`
	TargetID   int64     `bson:"tId"`
	UID        int64     `bson:"uid"`
	Reason     string    `bson:"reason"`
	Status     string    `bson:"status"`
	At         time.Time `bson:"at"`
	ResolvedBy int64     `bson:"resolvedBy,omitempty"`
	ResolvedAt time.Time `bson:"resolvedAt,omitempty"`
	Action     string    `bson:"action,omitempty"`
}

// ListDiscussReports 后台举报队列（状态筛选，最新在前）。
func (r *Repo) ListDiscussReports(ctx context.Context, status string, page, pageSize int) ([]*DiscussReportItem, int64, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	total, err := r.DiscussReports.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.DiscussReports.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetLimit(int64(pageSize)).SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*DiscussReportItem, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ResolveDiscussReports 批量结案（action: dismiss|delete|warn|mute，处置由 handler 执行）。
func (r *Repo) ResolveDiscussReports(ctx context.Context, ids []int64, action string, byUID int64) error {
	_, err := r.DiscussReports.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}, "status": "open"},
		bson.M{"$set": bson.M{"status": "resolved", "action": action, "resolvedBy": byUID, "resolvedAt": time.Now()}})
	return err
}

// UnfoldDiscussTarget 举报结案为「不成立」时解除折叠。
func (r *Repo) UnfoldDiscussTarget(ctx context.Context, targetType string, targetID int64) error {
	col := discussTargetCol(r, targetType)
	_, err := col.UpdateOne(ctx, bson.M{"_id": targetID}, bson.M{"$set": bson.M{"folded": false, "reportN": 0}})
	return err
}

// ---------- 禁言 ----------

// AddDiscussMute 禁言（days=0 永久）；同用户重复禁言覆盖延长。
func (r *Repo) AddDiscussMute(ctx context.Context, uid int64, days int, reason string, byUID int64) error {
	until := time.Now().AddDate(0, 0, days)
	permanent := days <= 0
	if permanent {
		until = time.Now().AddDate(100, 0, 0)
	}
	// 旧记录保留历史，查询只认未过期记录；直接插新记录（含 permanent 标记）
	_, err := r.DiscussMutes.InsertOne(ctx, bson.M{
		"uid": uid, "until": until, "permanent": permanent,
		"reason": reason, "byUid": byUID, "at": time.Now(),
	})
	return err
}

// ActiveDiscussMute 生效中的禁言（无则 nil, nil）。
func (r *Repo) ActiveDiscussMute(ctx context.Context, uid int64) (*DiscussReportItem, error) {
	var doc struct {
		ID        int64     `bson:"_id"`
		Until     time.Time `bson:"until"`
		Permanent bool      `bson:"permanent"`
		Reason    string    `bson:"reason"`
	}
	err := r.DiscussMutes.FindOne(ctx,
		bson.M{"uid": uid, "until": bson.M{"$gt": time.Now()}},
		options.FindOne().SetSort(bson.D{{Key: "until", Value: -1}})).Decode(&doc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &DiscussReportItem{ID: doc.ID, UID: uid, Reason: doc.Reason}, nil
}

// DiscussMuteRow 禁言列表条目。
type DiscussMuteRow struct {
	ID        int64     `bson:"_id"`
	UID       int64     `bson:"uid"`
	Until     time.Time `bson:"until"`
	Permanent bool      `bson:"permanent"`
	Reason    string    `bson:"reason"`
	ByUID     int64     `bson:"byUid"`
	At        time.Time `bson:"at"`
}

// ListActiveDiscussMutes 全部生效中的禁言。
func (r *Repo) ListActiveDiscussMutes(ctx context.Context) ([]*DiscussMuteRow, error) {
	cursor, err := r.DiscussMutes.Find(ctx, bson.M{"until": bson.M{"$gt": time.Now()}},
		options.Find().SetSort(bson.D{{Key: "until", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*DiscussMuteRow, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// LiftDiscussMute 解除禁言（截止时间改为立即过期）。
func (r *Repo) LiftDiscussMute(ctx context.Context, id int64) error {
	res, err := r.DiscussMutes.UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{"until": time.Now()}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// EnsureDiscussIndexes 讨论区/通知集合索引（Bootstrap 调用，幂等）。
func (r *Repo) EnsureDiscussIndexes(ctx context.Context) error {
	if _, err := r.Discussions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "scopeId", Value: 1}, {Key: "lastActiveAt", Value: -1}}},
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "createdAt", Value: -1}}},
	}); err != nil {
		return err
	}
	if _, err := r.DiscussReplies.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "tid", Value: 1}, {Key: "_id", Value: 1}}},
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "accepted", Value: 1}}},
	}); err != nil {
		return err
	}
	if _, err := r.DiscussLikes.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "t", Value: 1}, {Key: "id", Value: 1}, {Key: "uid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}
	if _, err := r.DiscussReports.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "tType", Value: 1}, {Key: "tId", Value: 1}, {Key: "status", Value: 1}}},
	}); err != nil {
		return err
	}
	if _, err := r.DiscussMutes.Indexes().CreateOne(ctx,
		mongo.IndexModel{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "until", Value: -1}}}); err != nil {
		return err
	}
	// 通知：收件箱查询 + purgeAt TTL（保留天数在插入时按当时设置写死，改动只影响新通知）
	if _, err := r.Notifications.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "read", Value: 1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "purgeAt", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0)},
	}); err != nil {
		return err
	}
	return nil
}

// sortInt64Asc 供 handler 侧去重排序使用。
