package repo

// 站点公开统计与导入预检的查询契约（v0.32 repo 分层收紧：自 server/stats.go、
// server/importbatch.go 的裸 collection 查询下沉为具名方法——「查询契约」回归
// repo 单点，索引命中有人把关；server 包不再直连 collection）。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// visibleProblemFilter 可见题池（hidden/duplicate ≠ true，v0.47 并入重复题）；
// difficulty > 0 时限同难度档。
func visibleProblemFilter(difficulty int) bson.M {
	q := bson.M{"hidden": bson.M{"$ne": true}, "duplicate": bson.M{"$ne": true}}
	if difficulty > 0 {
		q["difficulty"] = difficulty
	}
	return q
}

// CountVisibleProblems 可见题池总数（首页「收录题目」统计 / 题库页量；今日挑战
// 选题走 repo/game.go 的 dailyProblemFilter 专用池，不用本宽松口径）。
func (r *Repo) CountVisibleProblems(ctx context.Context, difficulty int) (int64, error) {
	return r.problems.CountDocuments(ctx, visibleProblemFilter(difficulty))
}

// CountAllUsers 注册用户总数。
func (r *Repo) CountAllUsers(ctx context.Context) (int64, error) {
	return r.users.CountDocuments(ctx, bson.M{})
}

// CountSubmissionsSince 某时刻以来的提交数（今日提交 = 当天零点起）。
func (r *Repo) CountSubmissionsSince(ctx context.Context, since time.Time) (int64, error) {
	return r.submissions.CountDocuments(ctx, bson.M{"createdAt": bson.M{"$gte": since}})
}

// AvgJudgeMSRecent 平均判题时长：最近 1000 条提交中已完成（timeMs>0）的窗口平均，
// 避免全表聚合；无样本返回 0。v0.66 批④：$sort 提前吃 {createdAt:-1} 索引 + $limit
// 收窗再 $match——原 $match timeMs 前置无索引可走，每分钟（60s 缓存到期）一次全表
// COLLSCAN；口径随之从「最近 1000 条已完成」微调为「最近 1000 条中的已完成」（展示
// 型近似，可接受）。
func (r *Repo) AvgJudgeMSRecent(ctx context.Context) (float64, error) {
	cur, err := r.submissions.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: -1}}}},
		{{Key: "$limit", Value: 1000}},
		{{Key: "$match", Value: bson.M{"timeMs": bson.M{"$gt": 0}}}},
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
	n, err := r.problems.CountDocuments(ctx, bson.M{"_id": pid})
	return n > 0, err
}

// ProblemTitleExists 同标题题是否已存在（批量导入的同名跳过预检）。
func (r *Repo) ProblemTitleExists(ctx context.Context, title string) (bool, error) {
	n, err := r.problems.CountDocuments(ctx, bson.M{"title": title})
	return n > 0, err
}
