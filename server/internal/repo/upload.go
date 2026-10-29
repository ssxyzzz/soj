package repo

// 题面图片（v0.5.1）：文件本体在 /data/uploads 卷（随机名），这里只记元数据
// （谁传的/多大/何时）——供配额统计与后续审计；不与题目强绑，孤儿文件靠配额兜底。

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Upload struct {
	File      string    `bson:"_id"` // 随机文件名（含扩展名）
	UID       int64     `bson:"uid"`
	Size      int64     `bson:"size"`
	CreatedAt time.Time `bson:"createdAt"`
}

func (r *Repo) InsertUpload(ctx context.Context, u Upload) error {
	_, err := r.uploads.InsertOne(ctx, u)
	return err
}

// UploadsUsedBytes 某用户已用图片存储字节数（配额判定用）。
func (r *Repo) UploadsUsedBytes(ctx context.Context, uid int64) (int64, error) {
	cursor, err := r.uploads.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"uid": uid}},
		{"$group": bson.M{"_id": nil, "total": bson.M{"$sum": "$size"}}},
	})
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	if !cursor.Next(ctx) {
		return 0, cursor.Err()
	}
	var row struct {
		Total int64 `bson:"total"`
	}
	if err := cursor.Decode(&row); err != nil {
		return 0, err
	}
	return row.Total, nil
}

// DeleteUpload 删除图片元数据（文件本体由调用方删除）。
func (r *Repo) DeleteUpload(ctx context.Context, file string) error {
	_, err := r.uploads.DeleteOne(ctx, bson.M{"_id": file})
	return err
}
