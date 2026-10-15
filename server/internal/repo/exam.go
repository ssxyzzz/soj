package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// 考试：班级级赛事，规则与比赛完全一致（见 DEVELOPMENT.md「比赛」节），
// 存 exams 集合并独立取号，与主题库比赛（contests 集合）互不干扰。
func (r *Repo) CreateExam(ctx context.Context, e *Exam) error {
	var err error
	e.ID, err = r.nextSeq(ctx, "exam")
	if err != nil {
		return err
	}
	e.CreatedAt = time.Now()
	_, err = r.db.Collection("exams").InsertOne(ctx, e)
	return err
}

func (r *Repo) GetExam(ctx context.Context, id int64) (*Exam, error) {
	var e Exam
	if err := r.db.Collection("exams").FindOne(ctx, bson.M{"_id": id}).Decode(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteExam 仅允许删除未开始的考试（调用方校验权限与状态）。
func (r *Repo) DeleteExam(ctx context.Context, id int64) error {
	res, err := r.db.Collection("exams").DeleteOne(ctx,
		bson.M{"_id": id, "start": bson.M{"$gt": time.Now()}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// ListClassExams 某班级的考试列表（按开始时间倒序）。
func (r *Repo) ListClassExams(ctx context.Context, classID int64) ([]*Exam, error) {
	return listRounds(ctx, r.db.Collection("exams"), bson.M{"classId": classID})
}

// ListUnpublishedExams 未发布的考试（锁题判定用，量小）。
func (r *Repo) ListUnpublishedExams(ctx context.Context) ([]*Exam, error) {
	return listRounds(ctx, r.db.Collection("exams"), bson.M{"published": false})
}
