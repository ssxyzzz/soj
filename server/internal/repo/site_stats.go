package repo

// 站点公开统计与导入预检的查询契约（v0.32 repo 分层收紧：自 server/stats.go、
// server/importbatch.go 的裸 collection 查询下沉为具名方法——「查询契约」回归
// repo 单点，索引命中有人把关；server 包不再直连 collection）。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// visibleProblemFilter 可见题池（hidden ≠ true）；difficulty > 0 时限同难度档。
func visibleProblemFilter(difficulty int) bson.M {
	q := bson.M{"hidden": bson.M{"$ne": true}}
	if difficulty > 0 {
		q["difficulty"] = difficulty
	}
	return q
}

// CountVisibleProblems 可见题池总数（今日一题的池大小 / 首页题量）。
func (r *Repo) CountVisibleProblems(ctx context.Context, difficulty int) (int64, error) {
	return r.Problems.CountDocuments(ctx, visibleProblemFilter(difficulty))
}

// FeaturedRow 今日一题候选行的最小投影。
type FeaturedRow struct {
	ID         int64   `bson:"_id"`
	Title      string  `bson:"title"`
	Difficulty int     `bson:"difficulty,omitempty"`
	Tags       []int64 `bson:"tags"`
}

// VisibleProblemAt 可见题池按 _id 升序偏移 skip 取一题（今日一题的稳定取样）；
// 池空返回 (nil, nil)。
func (r *Repo) VisibleProblemAt(ctx context.Context, difficulty int, skip int64) (*FeaturedRow, error) {
	cur, err := r.Problems.Find(ctx, visibleProblemFilter(difficulty),
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetSkip(skip).SetLimit(1))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []FeaturedRow
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ProblemSubmitCounts 某题提交总数与 AC 数（题库同口径；accepted 计数失败按 0，
// 与原 stats 降级行为一致）。
func (r *Repo) ProblemSubmitCounts(ctx context.Context, pid int64) (total, accepted int64, err error) {
	if total, err = r.Submissions.CountDocuments(ctx, bson.M{"pid": pid}); err != nil {
		return 0, 0, err
	}
	ac, _ := r.Submissions.CountDocuments(ctx, bson.M{"pid": pid, "status": StatusAccepted})
	return total, ac, nil
}

// CountAllUsers 注册用户总数。
func (r *Repo) CountAllUsers(ctx context.Context) (int64, error) {
	return r.Users.CountDocuments(ctx, bson.M{})
}

// CountSubmissionsSince 某时刻以来的提交数（今日提交 = 当天零点起）。
func (r *Repo) CountSubmissionsSince(ctx context.Context, since time.Time) (int64, error) {
	return r.Submissions.CountDocuments(ctx, bson.M{"createdAt": bson.M{"$gte": since}})
}

// AvgJudgeMSRecent 平均判题时长：最近 1000 条已完成（timeMs>0）的窗口平均，
// 避免全表聚合；无样本返回 0。
func (r *Repo) AvgJudgeMSRecent(ctx context.Context) (float64, error) {
	cur, err := r.Submissions.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"timeMs": bson.M{"$gt": 0}}}},
		{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: -1}}}},
		{{Key: "$limit", Value: 1000}},
		{{Key: "$group", Value: bson.M{"_id": nil, "avg": bson.M{"$avg": "$timeMs"}}}},
	})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		Avg float64 `bson:"avg"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Avg, nil
}

// ProblemIDExists 题号是否已存在（批量导入的按号幂等跳过预检）。
func (r *Repo) ProblemIDExists(ctx context.Context, pid int64) (bool, error) {
	n, err := r.Problems.CountDocuments(ctx, bson.M{"_id": pid})
	return n > 0, err
}

// ProblemTitleExists 同标题题是否已存在（批量导入的同名跳过预检）。
func (r *Repo) ProblemTitleExists(ctx context.Context, title string) (bool, error) {
	n, err := r.Problems.CountDocuments(ctx, bson.M{"title": title})
	return n > 0, err
}
