package repo

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Assignment 班级作业：一组题库题目的引用清单。
// 无截止时间、不锁外班；学生做题即时出分（走普通练习提交）。
type Assignment struct {
	ID        int64     `bson:"_id"`
	ClassID   int64     `bson:"classId"`
	Title     string    `bson:"title"`
	Intro     string    `bson:"intro"`
	IntroHTML string    `bson:"introHtml,omitempty"` // 作业说明富文本（v0.7）；原 md intro 冻结为回退源
	Problems  []int64   `bson:"problems"`
	CreatedAt time.Time `bson:"createdAt"`
}

func (r *Repo) CreateAssignment(ctx context.Context, classID int64, title, intro string) (*Assignment, error) {
	id, err := r.nextSeq(ctx, "assignment")
	if err != nil {
		return nil, err
	}
	a := &Assignment{
		ID: id, ClassID: classID, Title: title, Intro: intro,
		Problems: []int64{}, CreatedAt: time.Now(),
	}
	_, err = r.db.Collection("assignments").InsertOne(ctx, a)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *Repo) GetAssignment(ctx context.Context, id int64) (*Assignment, error) {
	var a Assignment
	if err := r.db.Collection("assignments").FindOne(ctx, bson.M{"_id": id}).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateAssignment 编辑作业名称/说明（set 键值由 handler 校验）。
func (r *Repo) UpdateAssignment(ctx context.Context, id int64, set bson.M) error {
	res, err := r.db.Collection("assignments").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// DeleteAssignment 删除作业（引用解除，不影响题库题目本身）。
func (r *Repo) DeleteAssignment(ctx context.Context, id int64) error {
	res, err := r.db.Collection("assignments").DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// ListAssignments 班级作业列表（按创建时间正序）。
func (r *Repo) ListAssignments(ctx context.Context, classID int64) ([]*Assignment, error) {
	cursor, err := r.db.Collection("assignments").Find(ctx,
		bson.M{"classId": classID},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Assignment, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// CountAssignments 班级作业数（班级卡片展示用）。
func (r *Repo) CountAssignments(ctx context.Context, classID int64) (int, error) {
	n, err := r.db.Collection("assignments").CountDocuments(ctx, bson.M{"classId": classID})
	return int(n), err
}

func (r *Repo) AddAssignmentProblem(ctx context.Context, id, pid int64) error {
	_, err := r.db.Collection("assignments").UpdateOne(ctx,
		bson.M{"_id": id}, bson.M{"$addToSet": bson.M{"problems": pid}})
	return err
}

func (r *Repo) RemoveAssignmentProblem(ctx context.Context, id, pid int64) error {
	_, err := r.db.Collection("assignments").UpdateOne(ctx,
		bson.M{"_id": id}, bson.M{"$pull": bson.M{"problems": pid}})
	return err
}

// SetAssignmentProblems 整体替换作业题目清单（含顺序）——「调整题目」弹窗一次保存
// 增删与拖动排序；调用方需已做班级管理权限校验。
func (r *Repo) SetAssignmentProblems(ctx context.Context, id int64, pids []int64) error {
	if pids == nil {
		pids = []int64{}
	}
	res, err := r.db.Collection("assignments").UpdateOne(ctx,
		bson.M{"_id": id}, bson.M{"$set": bson.M{"problems": pids}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// ProgressCell 某学生在某题的做题情况。
type ProgressCell struct {
	Tries   int
	Best    int
	Errors  int // 未满分提交次数（角标口径）
	Solved  bool
	Attempt bool
}

// ProgressRow 作业完成情况一行（按学生）。
type ProgressRow struct {
	UID      int64
	Username string
	Nickname string
	Cells    map[int64]*ProgressCell
	Attempt  int // 做过题数
	Solved   int // 通过题数
	Total    int // 各题最高分之和
	Rank     int // 总分降序 → 通过题数降序 → UID 升序
}

// AssignmentProgress 老师视角的作业完成情况：
// 对班级成员 × 作业题目，统计提交次数 / 最高分 / 是否通过（即时出分，按已发布提交统计）。
func (r *Repo) AssignmentProgress(ctx context.Context, a *Assignment, memberUIDs []int64) ([]*ProgressRow, error) {
	if len(a.Problems) == 0 || len(memberUIDs) == 0 {
		return []*ProgressRow{}, nil
	}
	pids := bson.A{}
	for _, pid := range a.Problems {
		pids = append(pids, pid)
	}
	uids := bson.A{}
	for _, uid := range memberUIDs {
		uids = append(uids, uid)
	}

	cursor, err := r.Submissions.Find(ctx,
		bson.M{"pid": bson.M{"$in": pids}, "uid": bson.M{"$in": uids}, "published": true},
		options.Find().SetProjection(bson.M{"uid": 1, "pid": 1, "score": 1, "status": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var subs []struct {
		UID    int64  `bson:"uid"`
		PID    int64  `bson:"pid"`
		Score  int    `bson:"score"`
		Status string `bson:"status"`
	}
	if err := cursor.All(ctx, &subs); err != nil {
		return nil, err
	}

	members, err := r.GetUsersByIDs(ctx, memberUIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]*ProgressRow, 0, len(memberUIDs))
	for _, uid := range memberUIDs {
		row := &ProgressRow{UID: uid, Cells: map[int64]*ProgressCell{}}
		if u := members[uid]; u != nil {
			row.Username, row.Nickname = u.Username, u.Nickname
		}
		for _, pid := range a.Problems {
			row.Cells[pid] = &ProgressCell{}
		}
		rows = append(rows, row)
	}
	index := map[int64]*ProgressRow{}
	for _, row := range rows {
		index[row.UID] = row
	}

	for _, s := range subs {
		row := index[s.UID]
		if row == nil {
			continue
		}
		cell, ok := row.Cells[s.PID]
		if !ok {
			continue // 题目可能在作业创建后被移除
		}
		cell.Tries++
		cell.Attempt = true
		if s.Score > cell.Best {
			cell.Best = s.Score
		}
		if s.Score >= 100 {
			cell.Solved = true
		}
		// 未满分提交次数（角标口径）：排队/判题中不计。
		if s.Status != StatusPending && s.Status != StatusJudging && s.Score < 100 {
			cell.Errors++
		}
	}
	for _, row := range rows {
		for _, cell := range row.Cells {
			row.Total += cell.Best
			if cell.Attempt {
				row.Attempt++
			}
			if cell.Solved {
				row.Solved++
			}
		}
	}
	// 统一成绩表排名：总分降序 → 通过题数降序 → UID 升序
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Total != rows[j].Total {
			return rows[i].Total > rows[j].Total
		}
		if rows[i].Solved != rows[j].Solved {
			return rows[i].Solved > rows[j].Solved
		}
		return rows[i].UID < rows[j].UID
	})
	for i, row := range rows {
		row.Rank = i + 1
	}
	return rows, nil
}
