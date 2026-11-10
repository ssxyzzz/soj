package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ---------- 题目收藏（v0.86）----------

// EnsureProblemFavoriteIndexes 题目收藏集合索引（Bootstrap 调用，幂等）。
// {uid,pid} 唯一：一人一题至多一条收藏，ToggleProblemFavorite 并发防重。
func (r *Repo) EnsureProblemFavoriteIndexes(ctx context.Context) error {
	_, err := r.problemFavorites.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "uid", Value: 1}, {Key: "pid", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

// ToggleProblemFavorite 收藏/取消收藏题目：幂等 toggle，返回操作后的收藏态。
func (r *Repo) ToggleProblemFavorite(ctx context.Context, uid, pid int64) (bool, error) {
	res, err := r.problemFavorites.DeleteOne(ctx, bson.M{"uid": uid, "pid": pid})
	if err != nil {
		return false, err
	}
	if res.DeletedCount > 0 {
		return false, nil
	}
	_, err = r.problemFavorites.InsertOne(ctx, bson.M{"uid": uid, "pid": pid, "at": time.Now()})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) { // 并发重复收藏：按已收藏处理
			return true, nil
		}
		return false, err
	}
	return true, nil
}

// FavoritePIDs 当前用户在一组题目里已收藏的集合（列表渲染星标用）。
func (r *Repo) FavoritePIDs(ctx context.Context, uid int64, pids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if uid <= 0 || len(pids) == 0 {
		return out, nil
	}
	cursor, err := r.problemFavorites.Find(ctx, bson.M{"uid": uid, "pid": bson.M{"$in": pids}},
		options.Find().SetProjection(bson.M{"pid": 1}))
	if err != nil {
		return out, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc struct {
			PID int64 `bson:"pid"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return out, err
		}
		out[doc.PID] = true
	}
	return out, cursor.Err()
}

// FavoriteRow 个人主页收藏卡行：题目摘要 + 收藏时间（hidden 供观察者权限剔除）。
type FavoriteRow struct {
	PID        int64     `bson:"pid" json:"id"`
	Title      string    `bson:"title" json:"title"`
	Difficulty int       `bson:"difficulty" json:"difficulty"`
	Hidden     bool      `bson:"hidden" json:"-"`
	At         time.Time `bson:"at" json:"at"`
}

// FavoriteRows 用户收藏列表（v0.89 铺开批）：按收藏时间倒序，limit 有界；
// 两段查——先收藏关系后题目摘要（已删除的题整条剔除），题目顺序=收藏时间序。
func (r *Repo) FavoriteRows(ctx context.Context, uid int64, limit int) ([]FavoriteRow, error) {
	if uid <= 0 || limit <= 0 {
		return []FavoriteRow{}, nil
	}
	cursor, err := r.problemFavorites.Find(ctx, bson.M{"uid": uid},
		options.Find().SetProjection(bson.M{"pid": 1, "at": 1}).
			SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	type favDoc struct {
		PID int64     `bson:"pid"`
		At  time.Time `bson:"at"`
	}
	var favs []favDoc
	if err := cursor.All(ctx, &favs); err != nil {
		return nil, err
	}
	pids := make([]int64, 0, len(favs))
	for _, f := range favs {
		pids = append(pids, f.PID)
	}
	sums, err := r.ProblemSummariesByIDs(ctx, pids)
	if err != nil {
		return nil, err
	}
	out := make([]FavoriteRow, 0, len(favs))
	for _, f := range favs {
		if p := sums[f.PID]; p != nil {
			out = append(out, FavoriteRow{PID: p.ID, Title: p.Title, Difficulty: p.Difficulty, Hidden: p.Hidden, At: f.At})
		}
	}
	return out, nil
}
