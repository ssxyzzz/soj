package repo

// 用户公开主页（v0.17）读侧聚合：全部读时现算，不建集合不落库——与「等级是读时
// 聚合值」同哲学。口径全部对齐既有公开面：
//   - AC 口径 = status Accepted ∧ published（与 RankBoard / SolvedDifficultyCounts 一致），
//     客观题整卷满分同为 Accepted，天然同口径；
//   - 提交统计分母排除 Cancelled（作废件不加分不进榜，不该拉低 AC 率）；
//   - 比赛历史只含 contest（考试是班级内私密成绩），参与 = 场内有过提交；
//   - 近期提交只出 published（比赛/考试隐藏期内的提交不从主页冒头，防赛中推测）。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// FirstACEntry 某题首次 AC 条目（主页按难度分组列表与计数卡的原始数据；
// 时间窗由前端本地切，服务端一次下发全量）。
type FirstACEntry struct {
	PID        int64     `bson:"pid"         json:"pid"`
	Title      string    `bson:"title"       json:"title"`
	Difficulty int       `bson:"difficulty"  json:"difficulty"`
	Hidden     bool      `bson:"hidden"      json:"hidden"`
	FirstACAt  time.Time `bson:"first_ac_at" json:"first_ac_at"`
}

// UserFirstAccepted 每题首次 AC：时间取提交自身时刻（judgedAt 缺失回退 createdAt，
// 直觉真实；比赛题揭榜后数据「回填」到 AC 当时的窗口属合理行为）。
// 仅 published 的 AC；题目已删除的不计（与 SolvedDifficultyCounts 同口径，unwind 不保留）。
// 返回按首次 AC 时间倒序。
func (r *Repo) UserFirstAccepted(ctx context.Context, uid int64) ([]FirstACEntry, error) {
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"uid": uid, "status": StatusAccepted, "published": true}}},
		{{Key: "$addFields", Value: bson.M{"t": bson.M{"$ifNull": bson.A{"$judgedAt", "$createdAt"}}}}},
		// 多键排序必须 bson.D（bson.M 键序随机，曾让题库状态列偶发「不是满分」）
		{{Key: "$sort", Value: bson.D{{Key: "pid", Value: 1}, {Key: "t", Value: 1}}}},
		{{Key: "$group", Value: bson.M{"_id": "$pid", "t": bson.M{"$first": "$t"}}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "problems",
			"localField":   "_id",
			"foreignField": "_id",
			"pipeline":     bson.A{bson.M{"$project": bson.M{"title": 1, "difficulty": 1, "hidden": 1}}},
			"as":           "p",
		}}},
		{{Key: "$unwind", Value: "$p"}},
		{{Key: "$project", Value: bson.M{
			"pid":         "$_id",
			"title":       "$p.title",
			"difficulty":  bson.M{"$ifNull": bson.A{"$p.difficulty", 3}}, // EffectiveDifficulty 同规则
			"hidden":      bson.M{"$ifNull": bson.A{"$p.hidden", false}},
			"first_ac_at": "$t",
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "first_ac_at", Value: -1}}}},
	}
	cursor, err := r.Submissions.Aggregate(ctx, pipe)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]FirstACEntry, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// UserSubmissionStats 主页统计卡：提交总数（published ∧ 非 Cancelled）与 AC 条数
//（不去重按题——按题去重的解题数由 first-AC 清单/等级侧承担）。
func (r *Repo) UserSubmissionStats(ctx context.Context, uid int64) (total, accepted int64, err error) {
	total, err = r.Submissions.CountDocuments(ctx, bson.M{
		"uid": uid, "published": true, "status": bson.M{"$ne": StatusCancelled},
	})
	if err != nil {
		return 0, 0, err
	}
	accepted, err = r.Submissions.CountDocuments(ctx, bson.M{
		"uid": uid, "published": true, "status": StatusAccepted,
	})
	return total, accepted, err
}

// UserRecentSubmissions 主页「近期提交」：最新 limit 条 published 提交的投影，
// 不带代码/作答/明细/报错——代码与明细仍走提交详情接口的既有权限规则。
func (r *Repo) UserRecentSubmissions(ctx context.Context, uid int64, limit int) ([]*Submission, error) {
	cursor, err := r.Submissions.Find(ctx,
		bson.M{"uid": uid, "published": true},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetLimit(int64(limit)).
			SetProjection(bson.M{
				"code": 0, "objAnswers": 0, "answer": 0, "blanks": 0,
				"detail": 0, "errorInfo": 0, "heartbeatAt": 0,
			}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Submission, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// UserContests 主页「比赛历史」的场次集合：该用户提交过的比赛（不含考试），
// 按开赛时间倒序；名次由 handler 在场次发布后现算（repo 不管权限口径）。
// 已删除场次自然缺席（rounds 查无此文档）。
func (r *Repo) UserContests(ctx context.Context, uid int64) ([]*Contest, error) {
	cursor, err := r.Submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"uid": uid, "contestId": bson.M{"$gt": 0}}},
		{"$group": bson.M{"_id": "$contestId"}},
	})
	if err != nil {
		return nil, err
	}
	var ids []struct {
		ID int64 `bson:"_id"`
	}
	if err := cursor.All(ctx, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	oidArr := make([]int64, 0, len(ids))
	for _, row := range ids {
		oidArr = append(oidArr, row.ID)
	}
	rcursor, err := r.roundsCol(KindContest).Find(ctx,
		bson.M{"_id": bson.M{"$in": oidArr}},
		options.Find().SetSort(bson.D{{Key: "start", Value: -1}, {Key: "_id", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer rcursor.Close(ctx)
	rounds := make([]*Contest, 0)
	if err := rcursor.All(ctx, &rounds); err != nil {
		return nil, err
	}
	return rounds, nil
}
