package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 可见性（v0.2 仅 public；v0.3 班级引入后扩展）
const VisibilityPublic = "public"

// CaseMeta 测试点元数据：内容本体在 testdata 存储，这里只存清单与校验信息。
type CaseMeta struct {
	Index   int    `bson:"index"   json:"index"`
	Sample  bool   `bson:"sample"  json:"sample"`
	InName  string `bson:"inName"  json:"inName"`
	AnsName string `bson:"ansName" json:"ansName"`
	InSize  int64  `bson:"inSize"  json:"inSize"`
	AnsSize int64  `bson:"ansSize" json:"ansSize"`
	InSHA   string `bson:"inSha"   json:"inSha"`
	AnsSHA  string `bson:"ansSha"  json:"ansSha"`
}

type Problem struct {
	ID            int64      `bson:"_id"`
	Title         string     `bson:"title"`
	Statement     string     `bson:"statement"`
	TimeLimitMS   int        `bson:"timeLimitMs"`
	MemoryLimitKB int        `bson:"memoryLimitKb"`
	Visibility    string     `bson:"visibility"`
	Hidden        bool       `bson:"hidden"`    // 主题库隐藏：default/guest 永不可见不可交
	CreatedBy     int64      `bson:"createdBy"` // 导入者 uid（0 = 系统/命令行导入，仅 root 可改其隐藏态）
	Tags          []string   `bson:"tags"`
	Subtasks      []Subtask  `bson:"subtasks,omitempty"` // nil = 单点等分
	Cases         []CaseMeta `bson:"cases"`
	CreatedAt     time.Time  `bson:"createdAt"`
	UpdatedAt     time.Time  `bson:"updatedAt"`
}

// Subtask 子任务分组（problem.yaml 可选配置）：组内测试点全过才得该组分。
type Subtask struct {
	Cases []int `bson:"cases"`
	Score int   `bson:"score"`
}

// UpsertProblem 按 ID 整体覆盖题目（导入工具调用）。
func (r *Repo) UpsertProblem(ctx context.Context, p *Problem) error {
	now := time.Now()
	p.UpdatedAt = now
	// 新建时补 createdAt 并把计数器推进到已占用的最大 pid
	if err := r.Problems.FindOne(ctx, bson.M{"_id": p.ID}).Err(); IsNotFound(err) {
		p.CreatedAt = now
		var probe struct {
			Seq int64 `bson:"seq"`
		}
		if err := r.Counters.FindOneAndUpdate(ctx,
			bson.M{"_id": "problem"},
			bson.M{"$max": bson.M{"seq": p.ID}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&probe); err != nil && !IsNotFound(err) {
			return err
		}
	} else if err != nil {
		return err
	}

	_, err := r.Problems.ReplaceOne(ctx,
		bson.M{"_id": p.ID}, p, options.Replace().SetUpsert(true))
	return err
}

func (r *Repo) GetProblem(ctx context.Context, id int64) (*Problem, error) {
	var p Problem
	if err := r.Problems.FindOne(ctx, bson.M{"_id": id}).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProblems 题库全量列表（v0.4 规模小，隐藏/锁题过滤与分页在 handler 按视角处理后切片）。
func (r *Repo) ListProblems(ctx context.Context) ([]*Problem, error) {
	cursor, err := r.Problems.Find(ctx, bson.M{},
		options.Find().
			SetProjection(bson.M{
				"statement": 0, "cases": 0,
			}).
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

// SetProblemHidden 设置主题库隐藏标记（权限由调用方校验）。
func (r *Repo) SetProblemHidden(ctx context.Context, pid int64, hidden bool) error {
	res, err := r.Problems.UpdateOne(ctx,
		bson.M{"_id": pid}, bson.M{"$set": bson.M{"hidden": hidden}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SetProblemStatement 更新题面（Markdown，权限由调用方校验）。
func (r *Repo) SetProblemStatement(ctx context.Context, pid int64, statement string) error {
	res, err := r.Problems.UpdateOne(ctx,
		bson.M{"_id": pid},
		bson.M{"$set": bson.M{"statement": statement, "updatedAt": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}
