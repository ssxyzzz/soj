package repo

// 题单（v0.18.3，plan §2.12）：全站公开自学导航——不绑班级、无截止、无成绩表。
// 结构 = 章节树最多两层：章（depth 0）→（可选）节（depth 1）→ 题目恒挂叶子；
// 同一题单内允许混用深浅（第 1 章直接挂题、第 2 章下分节），但单章不混挂
//（children 与 problems 二选一）；同题允许跨章重复挂载（正文 + 复习章），
// 完成进度由消费端按题去重。章节数与题数不设上限（拍板：形状校验只管深度与混挂）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mdrender"
)

// ListEntry 章节树节点：章/节共用一层结构，层级由位置决定——
// Chapters 里的元素是章（可带 Children 子节），Children 里的元素是节（只挂题）。
type ListEntry struct {
	Title    string      `bson:"title" json:"title"`
	Desc     string      `bson:"desc,omitempty" json:"desc,omitempty"`
	Problems []int64     `bson:"problems,omitempty" json:"problems,omitempty"`
	Children []ListEntry `bson:"children,omitempty" json:"children,omitempty"`
}

// ProblemList 题单主文档（lists 集合）。
type ProblemList struct {
	ID        int64       `bson:"_id" json:"id"`
	Title     string      `bson:"title" json:"title"`
	Intro     string      `bson:"intro,omitempty" json:"intro,omitempty"` // 简介 Markdown（单一事实源，前端渲染）
	IntroHtml string      `bson:"introHtml,omitempty" json:"-"`           // 渲染缓存（v0.30 写入时渲染；json 不下发——列表接口不带、详情由 handler 显式给）
	Hidden    bool        `bson:"hidden,omitempty" json:"hidden"`         // 隐藏开关：隐藏期间学生不可见（创建者/root 可见）
	Sort      int64       `bson:"sort" json:"sort"`                       // 手动排序键（列表页按 sort asc, _id asc）
	CreatedBy int64       `bson:"createdBy" json:"created_by"`
	CreatedAt time.Time   `bson:"createdAt" json:"created_at"`
	Chapters  []ListEntry `bson:"chapters" json:"chapters"`
}

// ValidateListShape 章节树形状校验：标题 1-32 字非空、描述 ≤128 字、
// 章深度最多两层（章→节→题）、单章不混挂（有子节的章不得直接挂题）、节不得再有子级。
func ValidateListShape(chapters []ListEntry) error {
	validateNode := func(n ListEntry, kind string) error {
		title := strings.TrimSpace(n.Title)
		if title == "" {
			return fmt.Errorf("%s标题不能为空", kind)
		}
		if len([]rune(title)) > 32 {
			return fmt.Errorf("%s标题「%s」超过 32 字", kind, title)
		}
		if len([]rune(n.Desc)) > 128 {
			return fmt.Errorf("%s「%s」的描述超过 128 字", kind, title)
		}
		return nil
	}
	for _, ch := range chapters {
		if err := validateNode(ch, "章"); err != nil {
			return err
		}
		if len(ch.Children) > 0 && len(ch.Problems) > 0 {
			return fmt.Errorf("章「%s」不能同时包含小节和题目（单章不混挂）", strings.TrimSpace(ch.Title))
		}
		for _, sec := range ch.Children {
			if err := validateNode(sec, "小节"); err != nil {
				return err
			}
			if len(sec.Children) > 0 {
				return fmt.Errorf("小节「%s」下不能再分子级（章节最多两层）", strings.TrimSpace(sec.Title))
			}
		}
	}
	return nil
}

// FlattenPIDs 按展示序拍平全部题目 ID（同题重复挂载去重，保留首次出现序）。
func FlattenPIDs(chapters []ListEntry) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, 64)
	add := func(pids []int64) {
		for _, pid := range pids {
			if !seen[pid] {
				seen[pid] = true
				out = append(out, pid)
			}
		}
	}
	for _, ch := range chapters {
		add(ch.Problems)
		for _, sec := range ch.Children {
			add(sec.Problems)
		}
	}
	return out
}

// CreateProblemList 新建题单（章节树可为空，编辑器后续填充）。
func (r *Repo) CreateProblemList(ctx context.Context, l *ProblemList) error {
	id, err := r.nextSeq(ctx, "list")
	if err != nil {
		return err
	}
	l.ID = id
	l.CreatedAt = time.Now()
	if l.Chapters == nil {
		l.Chapters = []ListEntry{}
	}
	l.IntroHtml = mdrender.Render(l.Intro)
	_, err = r.Lists.InsertOne(ctx, l)
	return err
}

// GetProblemList 按 id 取题单。
func (r *Repo) GetProblemList(ctx context.Context, id int64) (*ProblemList, error) {
	var l ProblemList
	if err := r.Lists.FindOne(ctx, bson.M{"_id": id}).Decode(&l); err != nil {
		return nil, err
	}
	return &l, nil
}

// ListProblemLists 全量题单（列表页：sort asc, _id asc；隐藏题单的可见性由调用方过滤）。
func (r *Repo) ListProblemLists(ctx context.Context) ([]*ProblemList, error) {
	cursor, err := r.Lists.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "sort", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*ProblemList, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateProblemList 更新题单（指针语义由 handler 组装 $set；chapters 整体覆盖）。
func (r *Repo) UpdateProblemList(ctx context.Context, id int64, set bson.M) error {
	setRenderedHtml(set, "intro", "introHtml")
	res, err := r.Lists.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// DeleteProblemList 硬删题单（进度为读时聚合，无软删包袱）。
func (r *Repo) DeleteProblemList(ctx context.Context, id int64) error {
	res, err := r.Lists.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// MaxListSort 现存最大排序键（新建题单排到队尾；空表回 0）。
func (r *Repo) MaxListSort(ctx context.Context) (int64, error) {
	var doc struct {
		Sort int64 `bson:"sort"`
	}
	err := r.Lists.FindOne(ctx, bson.M{}, options.FindOne().SetSort(bson.D{{Key: "sort", Value: -1}})).Decode(&doc)
	if IsNotFound(err) {
		return 0, nil
	}
	return doc.Sort, err
}

// ProblemSummary 题目挂载展示所需的最小投影（题单条目渲染）。
type ProblemSummary struct {
	ID         int64   `bson:"_id" json:"id"`
	Title      string  `bson:"title" json:"title"`
	Difficulty int     `bson:"difficulty" json:"difficulty"`
	Hidden     bool    `bson:"hidden" json:"hidden"`
	Tags       []int64 `bson:"tags,omitempty" json:"tags,omitempty"`
}

// ProblemSummariesByIDs 批量取题目摘要（title/difficulty/hidden）；返回 map，缺项 = 题已删除。
func (r *Repo) ProblemSummariesByIDs(ctx context.Context, pids []int64) (map[int64]*ProblemSummary, error) {
	out := make(map[int64]*ProblemSummary, len(pids))
	if len(pids) == 0 {
		return out, nil
	}
	cursor, err := r.Problems.Find(ctx,
		bson.M{"_id": bson.M{"$in": pids}},
		options.Find().SetProjection(bson.M{"title": 1, "difficulty": 1, "hidden": 1, "tags": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var p ProblemSummary
		if err := cursor.Decode(&p); err != nil {
			return nil, err
		}
		if p.Difficulty < 1 || p.Difficulty > 10 {
			p.Difficulty = 3 // EffectiveDifficulty 同规则：缺省按 D3
		}
		out[p.ID] = &p
	}
	return out, cursor.Err()
}

// ListParticipRow 题单参与记录：(uid, pid) 去重对——参与人数 = 卡片可见题集内
// 有公开提交（published ∧ 非 Cancelled）的去重用户数（尝试即算参与）。
type ListParticipRow struct {
	UID int64 `bson:"uid"`
	PID int64 `bson:"pid"`
}

// ListParticipantPairs 拉取题目集合上的 (uid, pid) 参与对（已去重）。
func (r *Repo) ListParticipantPairs(ctx context.Context, pids []int64) ([]ListParticipRow, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"pid":       bson.M{"$in": pids},
			"published": true,
			"status":    bson.M{"$ne": StatusCancelled},
		}}},
		{{Key: "$project", Value: bson.M{"uid": 1, "pid": 1}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}}},
		{{Key: "$project", Value: bson.M{"uid": "$_id.uid", "pid": "$_id.pid"}}},
	}
	cursor, err := r.Submissions.Aggregate(ctx, pipe)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]ListParticipRow, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ListRankRow 题单排名一行：solved = 该用户在本题单（可见题去重集合）内的 AC 题数。
type ListRankRow struct {
	UID    int64 `bson:"_id"`
	Solved int64 `bson:"solved"`
}

// ListRankRows 题单完成排名聚合：仅收 solved > 0 的用户，solved 降序、uid 升序决胜
// （并列同名次由 handler 侧按同分跳号计算）。口径与全站榜单一致（Accepted ∧ published）。
func (r *Repo) ListRankRows(ctx context.Context, pids []int64) ([]ListRankRow, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"pid": bson.M{"$in": pids}, "status": StatusAccepted, "published": true}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}}},
		{{Key: "$group", Value: bson.M{"_id": "$_id.uid", "solved": bson.M{"$sum": 1}}}},
		{{Key: "$match", Value: bson.M{"solved": bson.M{"$gt": 0}}}},
		{{Key: "$sort", Value: bson.D{{Key: "solved", Value: -1}, {Key: "_id", Value: 1}}}},
	}
	cursor, err := r.Submissions.Aggregate(ctx, pipe)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]ListRankRow, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
