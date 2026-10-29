package repo

// 重复题扫描（v0.18.1 验收追加）：按「标题 + 题面 markdown」双键判重，只列疑似重复
// 供管理员自行处置（编辑/隐藏），不做任何自动删除。
// v0.25 万题体检：原「全表读入内存分组」在万题规模峰值 ~2.5GB（单题题面上限 256KB），
// 改游标流式扫描；分组键用题面 sha256 定长摘要——判重语义（题面逐字相同）不变，
// 内存只随题目数线性小步增长，与题面长度脱钩。

import (
	"context"
	"crypto/sha256"
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
	cursor, err := r.problems.Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{"title": 1, "statement": 1, "type": 1, "hidden": 1, "createdBy": 1, "createdAt": 1}).
			SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	type key struct {
		title string
		sum   [sha256.Size]byte
	}
	groups := map[key][]ProblemInfo{}
	previews := map[key]string{} // 组内首条题面前 80 字（展示用）
	var row struct {
		ID        int64     `bson:"_id"`
		Title     string    `bson:"title"`
		Statement string    `bson:"statement"`
		Type      string    `bson:"type"`
		Hidden    bool      `bson:"hidden"`
		CreatedBy int64     `bson:"createdBy"`
		CreatedAt time.Time `bson:"createdAt"`
	}
	for cursor.Next(ctx) {
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		k := key{row.Title, sha256.Sum256([]byte(row.Statement))}
		if _, seen := groups[k]; !seen {
			previews[k] = truncateRunes(row.Statement, 80)
		}
		groups[k] = append(groups[k], ProblemInfo{
			ID: row.ID, Title: row.Title, Type: row.Type, Hidden: row.Hidden,
			CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Format(time.RFC3339),
		})
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	out := make([]DuplicateProblemGroup, 0, 4)
	for k, ps := range groups {
		if len(ps) < 2 {
			continue
		}
		out = append(out, DuplicateProblemGroup{
			Title: k.title, Preview: previews[k], Problems: ps,
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
