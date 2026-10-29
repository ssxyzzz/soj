package repo

// 标签树（v0.5.5）：单文档嵌套存储（tags 集合 _id="tree"），root 整树覆盖保存；
// 题目侧只存节点 ID（Problem.Tags []int64），名字与层级由树实时解析——
// 同名节点保存时继承旧 ID（既有引用不断），删除被引用节点在保存入口拦截
//（server/tagtree.go）。纯文本大纲里「改名」与「删除＋新增」不可区分，
// 语义定为后者：旧名被引用时改名会被拦截，先解绑再改。

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// GetTagTree 读取整棵知识树；从未保存过时返回空树（非错误）。
func (r *Repo) GetTagTree(ctx context.Context) ([]TagNode, error) {
	return r.GetTagTreeDoc(ctx, TreeDocKnowledge)
}

// GetMetaTagTree 读取整棵来源标签树；从未保存过时返回空树（非错误）。
func (r *Repo) GetMetaTagTree(ctx context.Context) ([]TagNode, error) {
	return r.GetTagTreeDoc(ctx, TreeDocMeta)
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
// 嵌套 ≤5 级（v0.20：展示端五级配色定档，标签树同限）、总节点数 ≤10000（2026-10-25 双树扩容，原 1000）。
// 节点数按树中节点计数（不看 ID 是否已取号，后台保存时新节点尚无正式号，
// 按 ID 集合计数会被绕过/少计）。
func ValidateTagTreeShape(nodes []TagNode) error {
	total := 0
	var walk func(list []TagNode, depth int) error
	walk = func(list []TagNode, depth int) error {
		names := map[string]bool{}
		for _, n := range list {
			total++
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
			if depth >= 5 {
				return fmt.Errorf("标签嵌套最多 5 级")
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
	if total > 10000 {
		return fmt.Errorf("节点总数超过 10000（当前 %d）", total)
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

// UnbindTags 从指定题目上批量摘除指定标签（$pull），题目其余标签不动——
// 标签树保存被删拦截后的「一键/逐题解绑」用。返回实际改动的题数。
func (r *Repo) UnbindTags(ctx context.Context, problemIDs, tagIDs []int64) (int64, error) {
	if len(problemIDs) == 0 || len(tagIDs) == 0 {
		return 0, nil
	}
	res, err := r.Problems.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": problemIDs}, "tags": bson.M{"$in": tagIDs}},
		bson.M{"$pull": bson.M{"tags": bson.M{"$in": tagIDs}}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
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

// TagUsageCountsField 引用数统计（字段参数化）：tags=知识树角标，meta_tags=来源树角标。
func (r *Repo) TagUsageCountsField(ctx context.Context, field string) (map[int64]int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$unwind", Value: "$" + field}},
		{{Key: "$group", Value: bson.M{"_id": "$" + field, "n": bson.M{"$sum": 1}}}},
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

// TagRefProblemsField 引用题目明细（删除/改名拦截用，字段参数化）。
func (r *Repo) TagRefProblemsField(ctx context.Context, field string, tagIDs []int64) ([]*Problem, error) {
	cursor, err := r.Problems.Find(ctx, bson.M{field: bson.M{"$in": tagIDs}},
		options.Find().SetProjection(bson.M{"title": 1, field: 1}).
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

// UnbindTagsField 从指定题目批量摘除指定节点（字段参数化：$pull 只动目标数组）。
func (r *Repo) UnbindTagsField(ctx context.Context, field string, problemIDs, tagIDs []int64) (int64, error) {
	if len(problemIDs) == 0 || len(tagIDs) == 0 {
		return 0, nil
	}
	res, err := r.Problems.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": problemIDs}, field: bson.M{"$in": tagIDs}},
		bson.M{"$pull": bson.M{field: bson.M{"$in": tagIDs}}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
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

// ValidateMetaTagRefs 校验节点 ID 全部存在于来源标签树（建题/编辑来源直传 ID 的入口用）。
func (r *Repo) ValidateMetaTagRefs(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tree, err := r.GetMetaTagTree(ctx)
	if err != nil {
		return err
	}
	exists := map[int64]bool{}
	CollectTagIDs(tree, exists)
	for _, id := range ids {
		if !exists[id] {
			return fmt.Errorf("来源节点 %d 不存在于来源标签树", id)
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

// ---- 默认标签树（v0.11）：全新部署空树时一次性注入（defaultTagsV1） ----

// DefaultTagTreeContent 默认标签树：2026-10-24 起与生产站点标签树对齐
// （知识点体系：10 大类 384 节点、最深 4 层；此前是约 50 节点的两级 NOI 大纲树）。
// 只在树为空时注入一次，之后随 root 自由编辑；「重置为默认」按钮二期。
// v0.32 巨型文件治理：500+ 行 Go 字面量外移 tags_default.json（go:embed），
// 内容由原字面量经 json.Marshal 机械生成（搬家零漂移）；运行时惰性解码，
// 防漂移由 tagtree_default_test.go 的结构断言承担（大类数/节点总数/深度/代表枝）。
var DefaultTagTreeContent = sync.OnceValue(mustDecodeDefaultTree("tags_default.json"))

// EnsureDefaultTagTree 树不存在或为空时注入默认树（节点现场取号），已有树返回 false 不动。
func (r *Repo) EnsureDefaultTagTree(ctx context.Context) (bool, error) {
	tree, err := r.GetTagTree(ctx)
	if err != nil {
		return false, err
	}
	if len(tree) > 0 {
		return false, nil
	}
	var assign func(list []TagNode) ([]TagNode, error)
	assign = func(list []TagNode) ([]TagNode, error) {
		res := make([]TagNode, 0, len(list))
		for _, n := range list {
			id, err := r.nextSeq(ctx, "tag")
			if err != nil {
				return nil, err
			}
			ch, err := assign(n.Children)
			if err != nil {
				return nil, err
			}
			res = append(res, TagNode{ID: id, Name: n.Name, Children: ch})
		}
		return res, nil
	}
	nodes, err := assign(DefaultTagTreeContent())
	if err != nil {
		return false, err
	}
	if err := ValidateTagTreeShape(nodes); err != nil {
		return false, err
	}
	return true, r.ReplaceTagTree(ctx, nodes)
}

// 双树（2026-10-25 双树体系统一）：知识树（原标签树）与来源标签树共用一套存储、
// 校验与面板交互，按 tags 集合内的文档 ID 区分；题目侧分两份挂载
// （Problem.Tags 知识点 / Problem.MetaTags 来源）。
const (
	TreeDocKnowledge = "tree"      // 知识树（原标签树）
	TreeDocMeta      = "tree:meta" // 来源标签树：地区/赛事/题库/年份/其他
)

// GetTagTreeDoc 读指定树；从未保存过时返回空树（非错误）。
func (r *Repo) GetTagTreeDoc(ctx context.Context, docID string) ([]TagNode, error) {
	var doc struct {
		Nodes []TagNode `bson:"nodes"`
	}
	err := r.Tags.FindOne(ctx, bson.M{"_id": docID}).Decode(&doc)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return doc.Nodes, nil
}

// ReplaceTagTreeDoc 整树覆盖保存（调用方负责形状校验、引用拦截与 ID 分配）。
func (r *Repo) ReplaceTagTreeDoc(ctx context.Context, docID string, nodes []TagNode) error {
	if nodes == nil {
		nodes = []TagNode{}
	}
	doc := bson.M{"nodes": nodes, "updatedAt": time.Now()}
	_, err := r.Tags.ReplaceOne(ctx, bson.M{"_id": docID}, doc, options.Replace().SetUpsert(true))
	return err
}

// ReconcileTagTreeDoc 整树覆盖前的身份对齐（同名继承旧 ID、新名取新号）。
// 两棵树独立取号（tag / tag_meta），ID 空间互不相干——题目分两份挂载无歧义。
func (r *Repo) ReconcileTagTreeDoc(ctx context.Context, docID, counterKey string, newNodes []TagNode) ([]TagNode, []int64, error) {
	oldNodes, err := r.GetTagTreeDoc(ctx, docID)
	if err != nil {
		return nil, nil, err
	}
	return reconcileTagTree(oldNodes, newNodes, func() (int64, error) {
		return r.nextSeq(ctx, counterKey)
	})
}

// DefaultMetaTagTreeContent 来源标签树种子：地区（合肥市域为主）/ 赛事 / 题库 / 年份 / 其他。
// 年份枝 root 手动维护（种子预置 2018–2026，不加不自动建）。
// v0.32 巨型文件治理：字面量外移 meta_default.json，同知识树口径。
var DefaultMetaTagTreeContent = sync.OnceValue(mustDecodeDefaultTree("meta_default.json"))

//go:embed tags_default.json meta_default.json
var defaultTreesFS embed.FS

// mustDecodeDefaultTree 从 embed FS 解码默认树：JSON 坏内容属构建期资产损坏，
// 首次使用时 panic（tagtree_default_test.go 启动即解码兜底，坏不了线上）。
func mustDecodeDefaultTree(name string) func() []TagNode {
	return func() []TagNode {
		b, err := defaultTreesFS.ReadFile(name)
		if err != nil {
			panic(err)
		}
		var nodes []TagNode
		if err := json.Unmarshal(b, &nodes); err != nil {
			panic(err)
		}
		return nodes
	}
}

// EnsureDefaultMetaTagTree 来源标签树不存在或为空时注入种子（取号 counter tag_meta），
// 已有树返回 false 不动。
func (r *Repo) EnsureDefaultMetaTagTree(ctx context.Context) (bool, error) {
	tree, err := r.GetTagTreeDoc(ctx, TreeDocMeta)
	if err != nil {
		return false, err
	}
	if len(tree) > 0 {
		return false, nil
	}
	var assign func(list []TagNode) ([]TagNode, error)
	assign = func(list []TagNode) ([]TagNode, error) {
		res := make([]TagNode, 0, len(list))
		for _, n := range list {
			id, err := r.nextSeq(ctx, "tag_meta")
			if err != nil {
				return nil, err
			}
			ch, err := assign(n.Children)
			if err != nil {
				return nil, err
			}
			res = append(res, TagNode{ID: id, Name: n.Name, Children: ch})
		}
		return res, nil
	}
	nodes, err := assign(DefaultMetaTagTreeContent())
	if err != nil {
		return false, err
	}
	if err := ValidateTagTreeShape(nodes); err != nil {
		return false, err
	}
	return true, r.ReplaceTagTreeDoc(ctx, TreeDocMeta, nodes)
}

// MatchMetaTags 把导入解析出的来源线索（SourceHint）映射到来源标签树节点 ID：
// Region/Source 按节点名精确匹配（任一层级），Year 按年份枝子节点名匹配；
// 全部命中失败的线索不返回（2026-10-25 定稿：匹配不上直接丢弃）。
func (r *Repo) MatchMetaTags(ctx context.Context, hint SourceHint) []int64 {
	nodes, err := r.GetTagTreeDoc(ctx, TreeDocMeta)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	byName := map[string]int64{}
	var walk func(list []TagNode)
	walk = func(list []TagNode) {
		for _, n := range list {
			if _, dup := byName[n.Name]; !dup {
				byName[n.Name] = n.ID
			}
			walk(n.Children)
		}
	}
	walk(nodes)

	ids := make([]int64, 0, 3)
	add := func(name string) {
		if name == "" {
			return
		}
		if id, ok := byName[name]; ok {
			ids = append(ids, id)
		}
	}
	if hint.Year > 0 {
		add(strconv.Itoa(hint.Year))
	}
	add(strings.TrimSpace(hint.Region))
	add(strings.TrimSpace(hint.Source))
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// CollectTagIDsByName 收集名字包含 nameContain（不分大小写）的节点 ID（v0.26.1
// 题目搜索：搜「贪心」命中知识点「贪心」，配合 CollectSubtreeIDs 展开子孙）。
func CollectTagIDsByName(nodes []TagNode, nameContain string) []int64 {
	q := strings.ToLower(nameContain)
	if q == "" {
		return nil
	}
	ids := make([]int64, 0, 8)
	var walk func(list []TagNode)
	walk = func(list []TagNode) {
		for _, n := range list {
			if strings.Contains(strings.ToLower(n.Name), q) {
				ids = append(ids, n.ID)
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return ids
}

// CollectSubtreeIDs 汇集 wanted 各节点自身与其全部子孙的 ID（来源筛选选父节点
// 含子节点：选「合肥市」命中全部区县），返回去重后的 ID 集。
func CollectSubtreeIDs(nodes []TagNode, wanted []int64) []int64 {
	want := map[int64]bool{}
	for _, id := range wanted {
		want[id] = true
	}
	out := make([]int64, 0, 16)
	var walk func(list []TagNode, inherited bool)
	walk = func(list []TagNode, inherited bool) {
		for _, n := range list {
			hit := inherited || want[n.ID]
			if hit {
				out = append(out, n.ID)
			}
			walk(n.Children, hit)
		}
	}
	walk(nodes, false)
	return out
}
