package repo

// 来源库（v0.12）：来源名登记册——建题表单 datalist 建议、题包导入未匹配 tag 的归队
// 缓冲、root 归类改型与「转为标签」批处理的数据底座。条目不持有引用清单：引用数按
// 题目侧（source 字段 / rawTags 数组）实时聚合，改名与转标签直接落题目，永不漂移。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SourceEntry 来源库条目：name 全库唯一；type 空 = 未分类（导入未匹配 tag 的初始态）。
type SourceEntry struct {
	ID        int64     `bson:"_id"    json:"id"`
	Name      string    `bson:"name"   json:"name"`
	Type      string    `bson:"type,omitempty" json:"type"` // oj/contest/region/local/other；空 = 未分类
	CreatedAt time.Time `bson:"createdAt" json:"created_at"`
}

func (r *Repo) sourcesCol() *mongo.Collection { return r.db.Collection("sources") }

// UpsertSourceByName 登记来源名（建题表单/导入管道调用）：新名插入；已存在时仅在原
// 类型为空且 typ 非空时补型——root 在面板里的归类不被后续表单提交随意覆盖。
// 幂等，唯一索引兜底并发插入。
func (r *Repo) UpsertSourceByName(ctx context.Context, name, typ string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var cur SourceEntry
	err := r.sourcesCol().FindOne(ctx, bson.M{"name": name}).Decode(&cur)
	if IsNotFound(err) {
		id, ierr := r.nextSeq(ctx, "source")
		if ierr != nil {
			return ierr
		}
		_, ierr = r.sourcesCol().InsertOne(ctx, SourceEntry{ID: id, Name: name, Type: strings.TrimSpace(typ), CreatedAt: time.Now()})
		if mongo.IsDuplicateKeyError(ierr) { // 并发登记撞唯一索引：按已存在处理
			err = nil
		} else {
			return ierr
		}
	} else if err != nil {
		return err
	}
	if typ != "" && cur.Type == "" {
		if _, err = r.sourcesCol().UpdateOne(ctx, bson.M{"_id": cur.ID}, bson.M{"$set": bson.M{"type": typ}}); err != nil {
			return err
		}
	}
	return nil
}

// ListSources 全部条目（名称升序，后台面板用）。
func (r *Repo) ListSources(ctx context.Context) ([]SourceEntry, error) {
	cursor, err := r.sourcesCol().Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := make([]SourceEntry, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSource 按 ID 取条目。
func (r *Repo) GetSource(ctx context.Context, id int64) (*SourceEntry, error) {
	var e SourceEntry
	if err := r.sourcesCol().FindOne(ctx, bson.M{"_id": id}).Decode(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// CreateSource root 手工新增：名称唯一（重复报错）、类型可空。
func (r *Repo) CreateSource(ctx context.Context, name, typ string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return 0, fmt.Errorf("来源名需在 1-64 字")
	}
	if typ != "" && !ValidSourceType(typ) {
		return 0, fmt.Errorf("来源类型需为 oj / contest / region / local / other")
	}
	if _, err := r.GetSourceByName(ctx, name); err == nil {
		return 0, fmt.Errorf("来源「%s」已存在", name)
	}
	id, err := r.nextSeq(ctx, "source")
	if err != nil {
		return 0, err
	}
	_, err = r.sourcesCol().InsertOne(ctx, SourceEntry{ID: id, Name: name, Type: typ, CreatedAt: time.Now()})
	return id, err
}

// GetSourceByName 按名称取条目（改名时查重用）。
func (r *Repo) GetSourceByName(ctx context.Context, name string) (*SourceEntry, error) {
	var e SourceEntry
	if err := r.sourcesCol().FindOne(ctx, bson.M{"name": name}).Decode(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateSource 改名/改型：改名跟随题目（source 字段与 rawTags 数组里的旧名一并替换），
// 名称冲突提前报错。nil 参数 = 不动。
func (r *Repo) UpdateSource(ctx context.Context, id int64, name, typ *string) error {
	e, err := r.GetSource(ctx, id)
	if err != nil {
		return err
	}
	set := bson.M{}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len([]rune(n)) > 64 {
			return fmt.Errorf("来源名需在 1-64 字")
		}
		if n != e.Name {
			if other, err := r.GetSourceByName(ctx, n); err == nil && other.ID != id {
				return fmt.Errorf("来源「%s」已存在", n)
			}
			if err := r.renameSourceRefs(ctx, e.Name, n); err != nil {
				return err
			}
			set["name"] = n
		}
	}
	if typ != nil {
		t := strings.TrimSpace(*typ)
		if t != "" && !ValidSourceType(t) {
			return fmt.Errorf("来源类型需为 oj / contest / region / local / other")
		}
		set["type"] = t
	}
	if len(set) == 0 {
		return nil
	}
	_, err = r.sourcesCol().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

// renameSourceRefs 改名跟随：题目 source 字段批量替换；rawTags 是数组元素替换，
// 逐题读改写（现实规模小，无需聚合管道）。
func (r *Repo) renameSourceRefs(ctx context.Context, oldName, newName string) error {
	if _, err := r.Problems.UpdateMany(ctx,
		bson.M{"source": oldName}, bson.M{"$set": bson.M{"source": newName}}); err != nil {
		return err
	}
	cursor, err := r.Problems.Find(ctx, bson.M{"rawTags": oldName}, options.Find().SetProjection(bson.M{"rawTags": 1}))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	type doc struct {
		ID      int64    `bson:"_id"`
		RawTags []string `bson:"rawTags"`
	}
	var docs []doc
	if err := cursor.All(ctx, &docs); err != nil {
		return err
	}
	for _, d := range docs {
		tags := make([]string, 0, len(d.RawTags))
		for _, t := range d.RawTags {
			if t == oldName {
				tags = append(tags, newName)
			} else {
				tags = append(tags, t)
			}
		}
		if _, err := r.Problems.UpdateOne(ctx, bson.M{"_id": d.ID}, bson.M{"$set": bson.M{"rawTags": tags}}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSource 删除条目（仅删登记，题目上的来源字段/原标签不动）。
func (r *Repo) DeleteSource(ctx context.Context, id int64) error {
	res, err := r.sourcesCol().DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SourceRefCount 来源名的引用题数：source 字段命中 ∪ rawTags 含该名（实时聚合）。
func (r *Repo) SourceRefCount(ctx context.Context, name string) (int64, error) {
	return r.Problems.CountDocuments(ctx, bson.M{
		"$or": bson.A{bson.M{"source": name}, bson.M{"rawTags": name}},
	})
}

// SourceRefProblems 引用该来源名的题目清单（ID/标题，转标签预览与明细用）。
func (r *Repo) SourceRefProblems(ctx context.Context, name string) ([]struct {
	ID    int64  `bson:"_id"`
	Title string `bson:"title"`
}, error) {
	cursor, err := r.Problems.Find(ctx,
		bson.M{"$or": bson.A{bson.M{"source": name}, bson.M{"rawTags": name}}},
		options.Find().SetProjection(bson.M{"title": 1}).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []struct {
		ID    int64  `bson:"_id"`
		Title string `bson:"title"`
	}
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AttachSourceTag 把来源名转挂到标签节点：引用题目（source 命中 ∪ rawTags 命中）补挂
// 节点（去重）并从 rawTags 移除该名（source 字段保留——来源事实不因转标签消失）。
// 返回受影响题目数。
func (r *Repo) AttachSourceTag(ctx context.Context, name string, nodeID int64) (int64, error) {
	cursor, err := r.Problems.Find(ctx,
		bson.M{"$or": bson.A{bson.M{"source": name}, bson.M{"rawTags": name}}},
		options.Find().SetProjection(bson.M{"tags": 1, "rawTags": 1}))
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	type doc struct {
		ID      int64    `bson:"_id"`
		Tags    []int64  `bson:"tags"`
		RawTags []string `bson:"rawTags"`
	}
	var docs []doc
	if err := cursor.All(ctx, &docs); err != nil {
		return 0, err
	}
	for _, d := range docs {
		set := bson.M{"tags": d.Tags}
		if !containsInt64(d.Tags, nodeID) {
			set["tags"] = append(append([]int64(nil), d.Tags...), nodeID)
		}
		raws := make([]string, 0, len(d.RawTags))
		for _, t := range d.RawTags {
			if t != name {
				raws = append(raws, t)
			}
		}
		if len(raws) > 0 {
			set["rawTags"] = raws
		} else {
			set["rawTags"] = []string{}
		}
		if _, err := r.Problems.UpdateOne(ctx, bson.M{"_id": d.ID}, bson.M{"$set": set}); err != nil {
			return 0, err
		}
	}
	return int64(len(docs)), nil
}

func containsInt64(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// AppendTagNode 在标签树指定父节点下追加新节点（0 = 根级）：同级重名报错，
// 返回新节点 ID。root 来源面板「转为标签（新建节点）」用。
func (r *Repo) AppendTagNode(ctx context.Context, parentID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 32 {
		return 0, fmt.Errorf("标签名需在 1-32 字")
	}
	tree, err := r.GetTagTree(ctx)
	if err != nil {
		return 0, err
	}
	id, err := r.nextSeq(ctx, "tag")
	if err != nil {
		return 0, err
	}
	if parentID == 0 {
		for _, n := range tree {
			if n.Name == name {
				return 0, fmt.Errorf("根级已存在同名标签「%s」", name)
			}
		}
		if err := r.ReplaceTagTree(ctx, append(tree, TagNode{ID: id, Name: name})); err != nil {
			return 0, err
		}
		return id, nil
	}
	var attach func(list []TagNode) ([]TagNode, bool)
	attach = func(list []TagNode) ([]TagNode, bool) {
		for i, n := range list {
			if n.ID == parentID {
				for _, ch := range n.Children {
					if ch.Name == name {
						return nil, false // 同级重名
					}
				}
				list[i].Children = append(n.Children, TagNode{ID: id, Name: name})
				return list, true
			}
			if ch, ok := attach(n.Children); ok {
				list[i].Children = ch
				return list, true
			}
		}
		return nil, false
	}
	newTree, ok := attach(tree)
	if !ok {
		return 0, fmt.Errorf("父节点 %d 不存在，或同级已存在同名标签「%s」", parentID, name)
	}
	if err := r.ReplaceTagTree(ctx, newTree); err != nil {
		return 0, err
	}
	return id, nil
}

// SourceMeta 公开元数据（表单 datalist + 题库筛选）：names = 来源库名 ∪ 题目 source
// 实有值（标题识别等历史来源未登记也能被建议）；years = 题目 year 实有值降序。
func (r *Repo) SourceMeta(ctx context.Context) (names []string, years []int, err error) {
	entries, err := r.ListSources(ctx)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	names = make([]string, 0, len(entries))
	for _, e := range entries {
		if !seen[e.Name] {
			seen[e.Name] = true
			names = append(names, e.Name)
		}
	}
	var srcVals []string
	if err := r.Problems.Distinct(ctx, "source", bson.M{"source": bson.M{"$ne": ""}}).Decode(&srcVals); err != nil {
		return nil, nil, err
	}
	for _, s := range srcVals {
		if s != "" && !seen[s] {
			seen[s] = true
			names = append(names, s)
		}
	}
	sort.Strings(names)
	var yearVals []int
	if err := r.Problems.Distinct(ctx, "year", bson.M{"year": bson.M{"$gt": 0}}).Decode(&yearVals); err != nil {
		return nil, nil, err
	}
	years = yearVals
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return names, years, nil
}
