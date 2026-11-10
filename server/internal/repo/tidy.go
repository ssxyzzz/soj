package repo

// 题库一键整理（v0.43）数据面：整理任务的专用读法与写库水位。
// AI/规则只产建议、root 预览确认后才经 UpdateProblemMeta 写库（两段式，plan §2.45）。
// v0.92 难度整理换血五源判读（题面+知识点+题解+AC代码+网上答案），通过率链路全面退役。

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

// TidyProblemMetas apply 落历史前的旧值快照（批量读难度/双树标签/标题，root 后台链路）。
func (r *Repo) TidyProblemMetas(ctx context.Context, pids []int64) (map[int64]*Problem, error) {
	out := make(map[int64]*Problem, len(pids))
	if len(pids) == 0 {
		return out, nil
	}
	cursor, err := r.problems.Find(ctx, bson.M{"_id": bson.M{"$in": pids}},
		options.Find().SetProjection(bson.M{"title": 1, "difficulty": 1, "tags": 1, "meta_tags": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var p Problem
		if err := cursor.Decode(&p); err != nil {
			return nil, err
		}
		out[p.ID] = &p
	}
	return out, cursor.Err()
}

// TidyProblemSolutions 难度五源之「文字题解」素材：置顶优先、最新次之，最多 limit 篇
//（每篇正文由调用方按预算截断；contentHtml 渲染缓存与判读无关不下发）。
func (r *Repo) TidyProblemSolutions(ctx context.Context, pid int64, limit int64) ([]*Solution, error) {
	cursor, err := r.solutions.Find(ctx, bson.M{"pid": pid},
		options.Find().
			SetProjection(bson.M{"content": 1, "pinned": 1}).
			SetSort(bson.D{{Key: "pinned", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Solution, 0, limit)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// TidyACCode AC 代码单份素材（code 由调用方按预算截断）。
type TidyACCode struct {
	ID     int64  `bson:"_id"`
	Lang   string `bson:"lang"`
	TimeMS int    `bson:"timeMs"`
	Code   string `bson:"code"`
}

// TidyACCodes 难度五源之「历史提交 AC 代码」素材（v0.92 拍板口径）：每题最多 6 份——
// 代码长度最短 3 份（最简实现代表算法本质）+ 总耗时最短 3 份（常数/写法侧证），
// 两路按提交 ID 去重合并；返回顺序 = 最短组在前、耗时组新增在后。
// 满分口径与旧通过率链路一致：status=AC 且 score=100。
func (r *Repo) TidyACCodes(ctx context.Context, pid int64) ([]TidyACCode, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	proj := bson.M{"_id": 1, "lang": 1, "timeMs": 1, "code": 1}
	match := bson.M{"pid": pid, "status": StatusAccepted, "score": 100}

	// 最短 3 份：$strLenCP 计算长度后排序（find 排不了计算字段，走聚合）
	var shortest []TidyACCode
	cur, err := r.submissions.Aggregate(ctx, []bson.M{
		{"$match": match},
		{"$addFields": bson.M{"codeLen": bson.M{"$strLenCP": "$code"}}},
		{"$sort": bson.D{{Key: "codeLen", Value: 1}, {Key: "_id", Value: 1}}},
		{"$limit": 3},
		{"$project": proj},
	})
	if err != nil {
		return nil, err
	}
	if err := cur.All(ctx, &shortest); err != nil {
		return nil, err
	}

	// 耗时最短 3 份（timeMs 升序；AC 提交的耗时已终判无竞态）
	var fastest []TidyACCode
	cur2, err := r.submissions.Find(ctx, match,
		options.Find().
			SetProjection(proj).
			SetSort(bson.D{{Key: "timeMs", Value: 1}, {Key: "_id", Value: 1}}).
			SetLimit(3))
	if err != nil {
		return nil, err
	}
	if err := cur2.All(ctx, &fastest); err != nil {
		return nil, err
	}

	seen := map[int64]bool{}
	out := make([]TidyACCode, 0, len(shortest)+len(fastest))
	for _, s := range shortest {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	for _, s := range fastest {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out, nil
}

// MarkTidied 整理应用水位：apply 成功后按类打点（题级 tidiedAt.<kind>）。
// 失败不致命——水位落后只会让下次任务重复建议，root 预览可再弃。
func (r *Repo) MarkTidied(ctx context.Context, kind string, pids []int64) error {
	if len(pids) == 0 {
		return nil
	}
	_, err := r.problems.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": pids}},
		bson.M{"$set": bson.M{"tidiedAt." + kind: time.Now()}})
	return err
}
