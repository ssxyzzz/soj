package repo

// 重复题扫描（v0.18.1 验收追加）：按「标题 + 题面 markdown」双键判重，题库规模
// 几百题，全表读入内存分组即可（不值得为偶发操作建聚合管道/哈希索引）。
// 只列疑似重复供管理员自行处置（编辑/隐藏），不做任何自动删除。

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// DuplicateProblemGroup 一组疑似重复题（同标题 + 同题面）。
type DuplicateProblemGroup struct {
	Title    string        `json:"title"`
	Preview  string        `json:"preview"` // 题面前 80 字（列表辨识用）
	Problems []ProblemInfo `json:"problems"`
}

// ProblemInfo 组内单题摘要（列表展示；处理入口=编辑页链接）。
type ProblemInfo struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Hidden    bool   `json:"hidden"`
	CreatedBy int64  `json:"created_by"`
	CreatedAt string `json:"created_at"` // RFC3339（前端自行格式化）
}

// ScanDuplicateProblems 返回全部疑似重复组（组内 ≥2 题），按组内题数降序、
// 组内按题号升序。题面为空的空壳题只要标题+题面都相同同样计入（正是重复建壳的信号）。
func (r *Repo) ScanDuplicateProblems(ctx context.Context) ([]DuplicateProblemGroup, error) {
	cursor, err := r.Problems.Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{"title": 1, "statement": 1, "type": 1, "hidden": 1, "createdBy": 1, "createdAt": 1}).
			SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID        int64     `bson:"_id"`
		Title     string    `bson:"title"`
		Statement string    `bson:"statement"`
		Type      string    `bson:"type"`
		Hidden    bool      `bson:"hidden"`
		CreatedBy int64     `bson:"createdBy"`
		CreatedAt time.Time `bson:"createdAt"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	type key struct{ title, statement string }
	groups := map[key][]ProblemInfo{}
	for _, row := range rows {
		k := key{row.Title, row.Statement}
		preview := []rune(row.Statement)
		if len(preview) > 80 {
			preview = preview[:80]
		}
		groups[k] = append(groups[k], ProblemInfo{
			ID: row.ID, Title: row.Title, Type: row.Type, Hidden: row.Hidden,
			CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Format(time.RFC3339),
		})
		_ = preview // preview 取组内第一条时用
	}
	out := make([]DuplicateProblemGroup, 0, 4)
	for k, ps := range groups {
		if len(ps) < 2 {
			continue
		}
		preview := []rune(k.statement)
		if len(preview) > 80 {
			preview = preview[:80]
		}
		out = append(out, DuplicateProblemGroup{
			Title: k.title, Preview: string(preview), Problems: ps,
		})
	}
	// 组内题数降序，同数量按标题稳定输出
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Problems) != len(out[j].Problems) {
			return len(out[i].Problems) > len(out[j].Problems)
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}
