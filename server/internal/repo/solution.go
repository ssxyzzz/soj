package repo

// 题解（v0.5b-1）：多作者模型——满分用户与老师群体均可发布，一条题解一条记录，
// 记录作者与发布/更新时间；置顶与删除他人题解需 solution.manage 权限位。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Solution struct {
	ID        int64     `bson:"_id"`
	PID       int64     `bson:"pid"`
	UID       int64     `bson:"uid"`
	Content   string    `bson:"content"`
	Pinned    bool      `bson:"pinned,omitempty"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

func (r *Repo) InsertSolution(ctx context.Context, s *Solution) error {
	id, err := r.nextSeq(ctx, "solution")
	if err != nil {
		return err
	}
	s.ID = id
	_, err = r.Solutions.InsertOne(ctx, s)
	return err
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
		bson.M{"$set": bson.M{"content": content, "updatedAt": time.Now()}})
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
