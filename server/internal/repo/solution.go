package repo

// 题解（v0.5.2-1）：多作者模型——满分用户与老师群体均可发布，一题每人一篇（2026-10-26
// 收紧：应用层检查 + {pid, uid} 唯一索引兜底并发，Bootstrap 启动时清理历史重复）；
// 记录作者与发布/更新时间；置顶与删除他人题解需 solution.manage 权限位。

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mdrender"
)

type Solution struct {
	ID          int64     `bson:"_id"`
	PID         int64     `bson:"pid"`
	UID         int64     `bson:"uid"`
	Content     string    `bson:"content"`               // 题解正文（Markdown，v0.8 起单一事实源）
	ContentHtml string    `bson:"contentHtml,omitempty"` // 渲染缓存（v0.30 写入时渲染；缺失时前端回退 md 渲染）
	Pinned      bool      `bson:"pinned,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

func (r *Repo) InsertSolution(ctx context.Context, s *Solution) error {
	id, err := r.nextSeq(ctx, "solution")
	if err != nil {
		return err
	}
	s.ID = id
	s.ContentHtml = mdrender.Render(s.Content)
	_, err = r.Solutions.InsertOne(ctx, s)
	return err
}

// GetUserSolution 取某用户在某题下的那篇题解（每人每题一篇；无则返回 nil, nil）。
func (r *Repo) GetUserSolution(ctx context.Context, pid, uid int64) (*Solution, error) {
	var s Solution
	err := r.Solutions.FindOne(ctx, bson.M{"pid": pid, "uid": uid}).Decode(&s)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// EnsureSolutionUnique 题解唯一化：先清历史重复（同 (pid, uid) 保留最早一篇），
// 再建 {pid, uid} 唯一索引兜底并发。幂等，随 Bootstrap 每次启动执行。
func (r *Repo) EnsureSolutionUnique(ctx context.Context) (int64, error) {
	var removed int64
	cursor, err := r.Solutions.Aggregate(ctx, []bson.M{
		{"$group": bson.M{
			"_id":  bson.M{"pid": "$pid", "uid": "$uid"},
			"n":    bson.M{"$sum": 1},
			"keep": bson.M{"$min": "$_id"},
		}},
		{"$match": bson.M{"n": bson.M{"$gt": 1}}},
	})
	if err != nil {
		return removed, err
	}
	for cursor.Next(ctx) {
		var row struct {
			ID struct {
				PID int64 `bson:"pid"`
				UID int64 `bson:"uid"`
			} `bson:"_id"`
			Keep int64 `bson:"keep"`
		}
		if err := cursor.Decode(&row); err != nil {
			cursor.Close(ctx)
			return removed, err
		}
		res, err := r.Solutions.DeleteMany(ctx, bson.M{
			"pid": row.ID.PID, "uid": row.ID.UID, "_id": bson.M{"$ne": row.Keep},
		})
		if err != nil {
			cursor.Close(ctx)
			return removed, err
		}
		removed += res.DeletedCount
		log.Printf("[boot] 题解去重：同题同人多篇保留最早一篇，清理 %d 篇（pid=%d uid=%d）",
			res.DeletedCount, row.ID.PID, row.ID.UID)
	}
	if err := cursor.Err(); err != nil {
		return removed, err
	}
	cursor.Close(ctx)
	if _, err := r.Solutions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "pid", Value: 1}, {Key: "uid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return removed, err
	}
	return removed, nil
}

// ListSolutions 某题的题解列表：置顶优先，其余按发布时间倒序（分页）。
func (r *Repo) ListSolutions(ctx context.Context, pid int64, page, pageSize int) ([]*Solution, int64, error) {
	filter := bson.M{"pid": pid}
	total, err := r.Solutions.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.Solutions.Find(ctx,
		filter,
		options.Find().SetSort(bson.D{
			{Key: "pinned", Value: -1},
			{Key: "createdAt", Value: -1},
		}).
			SetLimit(int64(pageSize)).
			SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*Solution, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *Repo) GetSolution(ctx context.Context, sid int64) (*Solution, error) {
	var s Solution
	err := r.Solutions.FindOne(ctx, bson.M{"_id": sid}).Decode(&s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSolutionContent 作者更新自己的题解内容。
func (r *Repo) UpdateSolutionContent(ctx context.Context, sid int64, content string) error {
	res, err := r.Solutions.UpdateOne(ctx,
		bson.M{"_id": sid},
		bson.M{"$set": bson.M{"content": content, "contentHtml": mdrender.Render(content), "updatedAt": time.Now()}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

// SetSolutionPinned 置顶/取消置顶（solution.manage 权限位）。
func (r *Repo) SetSolutionPinned(ctx context.Context, sid int64, pinned bool) error {
	res, err := r.Solutions.UpdateOne(ctx,
		bson.M{"_id": sid},
		bson.M{"$set": bson.M{"pinned": pinned}})
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

func (r *Repo) DeleteSolution(ctx context.Context, sid int64) error {
	res, err := r.Solutions.DeleteOne(ctx, bson.M{"_id": sid})
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}
