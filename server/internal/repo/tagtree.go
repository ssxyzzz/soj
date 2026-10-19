package repo

// 标签树（v0.5.5）：单文档嵌套存储（tags 集合 _id="tree"），root 整树覆盖保存；
// 题目侧只存节点 ID（Problem.Tags []int64），名字与层级由树实时解析——
// 同名节点保存时继承旧 ID（既有引用不断），删除被引用节点在保存入口拦截
//（server/tagtree.go）。纯文本大纲里「改名」与「删除＋新增」不可区分，
// 语义定为后者：旧名被引用时改名会被拦截，先解绑再改。

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

// TagNode 标签树节点（嵌套 children 表达层级，数组顺序即展示顺序）。
type TagNode struct {
	ID       int64     `bson:"id" json:"id"`
	Name     string    `bson:"name" json:"name"`
	Children []TagNode `bson:"children,omitempty" json:"children,omitempty"`
}

// GetTagTree 读取整棵标签树；从未保存过时返回空树（非错误）。
func (r *Repo) GetTagTree(ctx context.Context) ([]TagNode, error) {
	var doc struct {
		Nodes []TagNode `bson:"nodes"`
	}
	err := r.Tags.FindOne(ctx, bson.M{"_id": "tree"}).Decode(&doc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return doc.Nodes, nil
}

// ReplaceTagTree 整树覆盖保存（调用方负责形状校验、引用拦截与 ID 分配）。
func (r *Repo) ReplaceTagTree(ctx context.Context, nodes []TagNode) error {
	if nodes == nil {
		nodes = []TagNode{}
	}
	doc := bson.M{"nodes": nodes, "updatedAt": time.Now()}
	_, err := r.Tags.ReplaceOne(ctx, bson.M{"_id": "tree"}, doc, options.Replace().SetUpsert(true))
	return err
}

// ReconcileTagTree 整树覆盖前的身份对齐：新树与旧树按名字对齐（全局同名取先见者），
// 同名节点继承旧 ID（既有题目引用不断），新名字取新号；旧树中未被继承的节点
// 视为删除。返回带 ID 的新树与被移除的旧节点 ID（升序），供调用方做引用拦截。
func (r *Repo) ReconcileTagTree(ctx context.Context, newNodes []TagNode) ([]TagNode, []int64, error) {
	oldNodes, err := r.GetTagTree(ctx)
	if err != nil {
		return nil, nil, err
	}
	return reconcileTagTree(oldNodes, newNodes, func() (int64, error) {
		return r.nextSeq(ctx, "tag")
	})
}

// reconcileTagTree 纯函数核心（单测覆盖）：按名字继承身份，新名字走 nextID 取号。
func reconcileTagTree(oldNodes, newNodes []TagNode, nextID func() (int64, error)) ([]TagNode, []int64, error) {
	lookup := TagNameLookup(oldNodes)
	used := map[int64]bool{}
	var assign func(list []TagNode) ([]TagNode, error)
	assign = func(list []TagNode) ([]TagNode, error) {
		res := make([]TagNode, 0, len(list))
		for _, n := range list {
			name := strings.TrimSpace(n.Name)
			var id int64
			if old, ok := lookup[name]; ok && !used[old] {
				id = old
			} else {
				seq, err := nextID()
				if err != nil {
					return nil, err
				}
				id = seq
			}
			used[id] = true
			ch, err := assign(n.Children)
			if err != nil {
				return nil, err
			}
			res = append(res, TagNode{ID: id, Name: name, Children: ch})
		}
		return res, nil
	}
	out, err := assign(newNodes)
	if err != nil {
		return nil, nil, err
	}
	oldIDs := map[int64]bool{}
	CollectTagIDs(oldNodes, oldIDs)
	removed := make([]int64, 0, 8)
	for id := range oldIDs {
		if !used[id] {
			removed = append(removed, id)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	return out, removed, nil
}

// ValidateTagTreeShape 保存前形状校验：名称非空 ≤32 字、同级不重名、
// 嵌套 ≤32 层、总节点数 ≤500。
func ValidateTagTreeShape(nodes []TagNode) error {
	var walk func(list []TagNode, depth int) error
	walk = func(list []TagNode, depth int) error {
		names := map[string]bool{}
		for _, n := range list {
			name := strings.TrimSpace(n.Name)
			if name == "" {
				return fmt.Errorf("标签名不能为空")
			}
			if len([]rune(name)) > 32 {
				return fmt.Errorf("标签名「%s」超过 32 字", name)
			}
			if names[name] {
				return fmt.Errorf("同级存在重名标签「%s」", name)
			}
			names[name] = true
			if depth >= 32 {
				return fmt.Errorf("标签嵌套超过 32 层")
			}
			if err := walk(n.Children, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(nodes, 0); err != nil {
		return err
	}
	all := map[int64]bool{}
	CollectTagIDs(nodes, all)
	delete(all, 0) // 未取号的新节点不计入上限
	if len(all) > 500 {
		return fmt.Errorf("标签总数超过 500（当前 %d）", len(all))
	}
	return nil
}

// CollectTagIDs 收集树中全部节点 ID（含指定集合）。
func CollectTagIDs(nodes []TagNode, into map[int64]bool) {
	for _, n := range nodes {
		into[n.ID] = true
		CollectTagIDs(n.Children, into)
	}
}

// TagRefProblems 返回引用了指定标签节点 ID 的题目（ID/标题/标签），删除拦截明细用。
func (r *Repo) TagRefProblems(ctx context.Context, tagIDs []int64) ([]*Problem, error) {
	cursor, err := r.Problems.Find(ctx, bson.M{"tags": bson.M{"$in": tagIDs}},
		options.Find().SetProjection(bson.M{"title": 1, "tags": 1}).
			SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []*Problem
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TagUsageCounts 统计每个标签节点被题目直接引用的题数（聚合 Problem.Tags，
// 与题库筛选「精确匹配」同口径，不做子树扩散）。管理面板角标与删除/改名的前置拦截用。
func (r *Repo) TagUsageCounts(ctx context.Context) (map[int64]int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$unwind", Value: "$tags"}},
		{{Key: "$group", Value: bson.M{"_id": "$tags", "n": bson.M{"$sum": 1}}}},
	}
	cursor, err := r.Problems.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID int64 `bson:"_id"`
		N  int64 `bson:"n"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, row := range rows {
		out[row.ID] = row.N
	}
	return out, nil
}

// ValidateTagRefs 校验节点 ID 全部存在于标签树（建题/编辑元数据直传 ID 的入口用）。
func (r *Repo) ValidateTagRefs(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tree, err := r.GetTagTree(ctx)
	if err != nil {
		return err
	}
	exists := map[int64]bool{}
	CollectTagIDs(tree, exists)
	for _, id := range ids {
		if !exists[id] {
			return fmt.Errorf("标签节点 %d 不存在于标签树", id)
		}
	}
	return nil
}

// TagIDNames 树的 ID→名字映射。
func TagIDNames(nodes []TagNode) map[int64]string {
	m := map[int64]string{}
	var walk func(list []TagNode)
	walk = func(list []TagNode) {
		for _, n := range list {
			m[n.ID] = n.Name
			walk(n.Children)
		}
	}
	walk(nodes)
	return m
}

// TagNameLookup 名字→ID 映射（同名取先出现者——problem.yaml tags 按名挂载语义）。
func TagNameLookup(nodes []TagNode) map[string]int64 {
	m := map[string]int64{}
	var walk func(list []TagNode)
	walk = func(list []TagNode) {
		for _, n := range list {
			if _, ok := m[n.Name]; !ok {
				m[n.Name] = n.ID
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return m
}
