package repo

// 题库一键整理（v0.43）数据面：整理任务的两个专用读法。
// AI/规则只产建议、root 预览确认后才经 UpdateProblemMeta 写库（两段式，plan §2.45）。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TidyListProblems 整理任务全量题读：含 statement（AI prompt 素材）但排除 statementHtml
//（渲染缓存与整理无关，256KB/题的传输大头）；后台任务串行消费，量级几百题无压力。
func (r *Repo) TidyListProblems(ctx context.Context) ([]*Problem, error) {
	cursor, err := r.problems.Find(ctx, bson.M{},
		options.Find().
			SetProjection(bson.M{"statementHtml": 0, "checkerFile": 0, "driverFile": 0,
				"templateFile": 0, "interactorFile": 0}).
			SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Problem, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// TidyPassRates 全量题的去重用户通过率（难度整理辅助参考列，与 migrateDifficultyEstimate
// 同口径：做过 = 有非取消提交的不同用户，满分 = 其中最高分 ≥100 的用户）。
// 返回 rate（attempted>0 时才有意义）与 attempted（做过人数，0 = 无人做过）。
func (r *Repo) TidyPassRates(ctx context.Context) (rates map[int64]float64, attempted map[int64]int64, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	type cntRow struct {
		ID int64 `bson:"_id"`
		N  int64 `bson:"n"`
	}
	distinctUsers := func(match bson.M) (map[int64]int64, error) {
		cursor, err := r.submissions.Aggregate(ctx, []bson.M{
			{"$match": match},
			{"$group": bson.M{"_id": bson.M{"pid": "$pid", "uid": "$uid"}}},
			{"$group": bson.M{"_id": "$_id.pid", "n": bson.M{"$sum": 1}}},
		})
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)
		m := map[int64]int64{}
		for cursor.Next(ctx) {
			var row cntRow
			if err := cursor.Decode(&row); err != nil {
				return nil, err
			}
			m[row.ID] = row.N
		}
		return m, cursor.Err()
	}
	solved, err := distinctUsers(bson.M{"score": 100})
	if err != nil {
		return nil, nil, err
	}
	attempted, err = distinctUsers(bson.M{"status": bson.M{"$ne": StatusCancelled}})
	if err != nil {
		return nil, nil, err
	}
	rates = make(map[int64]float64, len(attempted))
	for pid, att := range attempted {
		if att > 0 {
			rates[pid] = float64(solved[pid]) / float64(att)
		}
	}
	return rates, attempted, nil
}
