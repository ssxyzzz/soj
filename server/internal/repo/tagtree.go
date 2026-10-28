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
	"strconv"
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
var DefaultTagTreeContent = []TagNode{
	{Name: "基础语法", Children: []TagNode{
		{Name: "顺序结构", Children: []TagNode{
			{Name: "简单输出"},
			{Name: "输入输出"},
			{Name: "数位分离"},
			{Name: "交换变量"},
			{Name: "小数处理"},
			{Name: "数学计算"},
			{Name: "模拟过程"},
		}},
		{Name: "分支结构", Children: []TagNode{
			{Name: "单分支"},
			{Name: "双分支"},
			{Name: "多分支"},
			{Name: "分支嵌套"},
			{Name: "switch语句"},
		}},
		{Name: "循环结构", Children: []TagNode{
			{Name: "for循环"},
			{Name: "while循环"},
			{Name: "do-while循环"},
			{Name: "循环输入"},
			{Name: "循环嵌套"},
		}},
		{Name: "数组", Children: []TagNode{
			{Name: "一维数组"},
			{Name: "增删改"},
			{Name: "数组标记"},
			{Name: "数组计数"},
			{Name: "连续性"},
			{Name: "字符数组"},
			{Name: "字符串"},
			{Name: "二维数组"},
			{Name: "多维数组"},
		}},
		{Name: "函数", Children: []TagNode{
			{Name: "形参与实参"},
			{Name: "变量作用域"},
			{Name: "引用传递"},
			{Name: "常用库函数"},
		}},
		{Name: "结构体", Children: []TagNode{
			{Name: "结构体数组"},
			{Name: "结构体排序"},
		}},
		{Name: "STL", Children: []TagNode{
			{Name: "vector"},
			{Name: "stack"},
			{Name: "queue"},
			{Name: "priority_queue"},
			{Name: "deque"},
			{Name: "set/multiset"},
			{Name: "map/multimap"},
			{Name: "pair"},
			{Name: "bitset"},
		}},
		{Name: "复杂度分析", Children: []TagNode{
			{Name: "时间复杂度"},
			{Name: "空间复杂度"},
		}},
	}},
	{Name: "基础算法", Children: []TagNode{
		{Name: "贪心", Children: []TagNode{
			{Name: "简单贪心"},
			{Name: "区间贪心"},
			{Name: "反悔贪心"},
			{Name: "排序贪心"},
		}},
		{Name: "枚举", Children: []TagNode{
			{Name: "简单枚举"},
			{Name: "嵌套枚举"},
		}},
		{Name: "二分", Children: []TagNode{
			{Name: "二分查找"},
			{Name: "二分答案"},
			{Name: "三分法"},
			{Name: "01分数规划"},
			{Name: "整体二分"},
		}},
		{Name: "前后缀", Children: []TagNode{
			{Name: "前缀和"},
			{Name: "后缀和"},
			{Name: "前缀积"},
			{Name: "后缀积"},
			{Name: "前后缀最值"},
			{Name: "前后缀异或"},
			{Name: "二维前缀"},
		}},
		{Name: "差分", Children: []TagNode{
			{Name: "一维差分"},
			{Name: "二维差分"},
		}},
		{Name: "递推", Children: []TagNode{
			{Name: "顺序递推"},
			{Name: "倒序递推"},
		}},
		{Name: "递归", Children: []TagNode{
			{Name: "记忆化递归"},
		}},
		{Name: "排序", Children: []TagNode{
			{Name: "冒泡排序"},
			{Name: "选择排序"},
			{Name: "插入排序"},
			{Name: "桶排序"},
			{Name: "计数排序"},
			{Name: "基数排序"},
			{Name: "堆排序"},
			{Name: "sort函数", Children: []TagNode{
				{Name: "自定义比较"},
			}},
		}},
		{Name: "分治", Children: []TagNode{
			{Name: "归并排序"},
			{Name: "快速排序"},
			{Name: "快速选择"},
			{Name: "逆序对"},
			{Name: "CDQ分治"},
		}},
		{Name: "倍增", Children: []TagNode{
			{Name: "倍增思想"},
			{Name: "快速幂"},
		}},
		{Name: "高精度", Children: []TagNode{
			{Name: "高精度加法"},
			{Name: "高精度减法"},
			{Name: "高精度乘法"},
			{Name: "高精度除法"},
		}},
		{Name: "位运算", Children: []TagNode{
			{Name: "位运算基础"},
			{Name: "位运算技巧"},
			{Name: "状态压缩"},
		}},
		{Name: "离散化"},
	}},
	{Name: "搜索", Children: []TagNode{
		{Name: "DFS", Children: []TagNode{
			{Name: "指数型枚举"},
			{Name: "排列型枚举"},
			{Name: "组合型枚举"},
			{Name: "网格搜索"},
			{Name: "连通块搜索"},
			{Name: "回溯法"},
		}},
		{Name: "BFS", Children: []TagNode{
			{Name: "网格最短路"},
			{Name: "多源BFS"},
			{Name: "状态压缩搜索"},
		}},
		{Name: "剪枝", Children: []TagNode{
			{Name: "可行性剪枝"},
			{Name: "最优性剪枝"},
			{Name: "搜索顺序剪枝"},
			{Name: "排除等效冗余"},
		}},
		{Name: "迭代加深", Children: []TagNode{
			{Name: "迭代加深DFS"},
			{Name: "IDA*"},
		}},
		{Name: "A*算法"},
		{Name: "双向搜索"},
	}},
	{Name: "数据结构", Children: []TagNode{
		{Name: "栈", Children: []TagNode{
			{Name: "括号匹配"},
			{Name: "表达式求值"},
			{Name: "单调栈"},
		}},
		{Name: "队列", Children: []TagNode{
			{Name: "循环队列"},
			{Name: "单调队列"},
			{Name: "双端队列"},
		}},
		{Name: "链表", Children: []TagNode{
			{Name: "单链表"},
			{Name: "双链表"},
			{Name: "循环链表"},
		}},
		{Name: "哈希表", Children: []TagNode{
			{Name: "哈希函数"},
			{Name: "拉链法"},
			{Name: "开放寻址法"},
		}},
		{Name: "树", Children: []TagNode{
			{Name: "树的存储"},
			{Name: "树的遍历"},
			{Name: "二叉树", Children: []TagNode{
				{Name: "先序遍历"},
				{Name: "中序遍历"},
				{Name: "后序遍历"},
				{Name: "层序遍历"},
			}},
		}},
		{Name: "堆", Children: []TagNode{
			{Name: "二叉堆"},
			{Name: "对顶堆"},
			{Name: "懒删除堆"},
			{Name: "左偏树"},
		}},
		{Name: "并查集", Children: []TagNode{
			{Name: "路径压缩"},
			{Name: "按秩合并"},
			{Name: "带权并查集"},
			{Name: "扩展域并查集"},
		}},
		{Name: "ST表"},
		{Name: "树状数组", Children: []TagNode{
			{Name: "单点修改区间查询"},
			{Name: "区间修改单点查询"},
			{Name: "区间修改区间查询"},
		}},
		{Name: "线段树", Children: []TagNode{
			{Name: "懒标记"},
			{Name: "权值线段树"},
			{Name: "动态开点"},
			{Name: "扫描线"},
			{Name: "区间合并"},
			{Name: "李超线段树"},
			{Name: "线段树合并"},
		}},
		{Name: "平衡树", Children: []TagNode{
			{Name: "Treap"},
			{Name: "Splay"},
			{Name: "无旋Treap"},
		}},
		{Name: "字典树", Children: []TagNode{
			{Name: "01字典树"},
		}},
		{Name: "可持久化", Children: []TagNode{
			{Name: "可持久化线段树"},
		}},
		{Name: "分块", Children: []TagNode{
			{Name: "数列分块"},
		}},
		{Name: "莫队", Children: []TagNode{
			{Name: "普通莫队"},
			{Name: "带修改莫队"},
			{Name: "树上莫队"},
			{Name: "回滚莫队"},
		}},
		{Name: "树链剖分", Children: []TagNode{
			{Name: "重链剖分"},
			{Name: "长链剖分"},
		}},
	}},
	{Name: "图论", Children: []TagNode{
		{Name: "图的存储", Children: []TagNode{
			{Name: "邻接矩阵"},
			{Name: "邻接表"},
			{Name: "链式前向星"},
		}},
		{Name: "图的遍历", Children: []TagNode{
			{Name: "DFS遍历"},
			{Name: "BFS遍历"},
			{Name: "连通块"},
		}},
		{Name: "拓扑排序"},
		{Name: "最短路", Children: []TagNode{
			{Name: "Dijkstra", Children: []TagNode{
				{Name: "朴素Dijkstra"},
				{Name: "堆优化Dijkstra"},
			}},
			{Name: "Bellman-Ford"},
			{Name: "SPFA"},
			{Name: "Floyd"},
			{Name: "差分约束"},
			{Name: "分层图最短路"},
			{Name: "负环判定"},
		}},
		{Name: "最小生成树", Children: []TagNode{
			{Name: "Prim"},
			{Name: "Kruskal"},
			{Name: "次小生成树"},
		}},
		{Name: "树上问题", Children: []TagNode{
			{Name: "树的直径"},
			{Name: "树的重心"},
			{Name: "最近公共祖先", Children: []TagNode{
				{Name: "倍增LCA"},
				{Name: "Tarjan离线LCA"},
			}},
			{Name: "树上差分"},
			{Name: "树上启发式合并"},
			{Name: "点分治"},
			{Name: "虚树"},
			{Name: "基环树"},
		}},
		{Name: "连通性", Children: []TagNode{
			{Name: "强连通分量", Children: []TagNode{
				{Name: "Tarjan"},
				{Name: "Kosaraju"},
			}},
			{Name: "割点与桥"},
			{Name: "边双连通分量"},
			{Name: "点双连通分量"},
			{Name: "缩点"},
		}},
		{Name: "欧拉图", Children: []TagNode{
			{Name: "欧拉路径"},
			{Name: "欧拉回路"},
		}},
		{Name: "网络流", Children: []TagNode{
			{Name: "最大流", Children: []TagNode{
				{Name: "EK算法"},
				{Name: "Dinic算法"},
			}},
			{Name: "最小割"},
			{Name: "费用流"},
		}},
		{Name: "二分图", Children: []TagNode{
			{Name: "二分图判定"},
			{Name: "匈牙利算法"},
			{Name: "二分图最大权匹配"},
		}},
		{Name: "2-SAT"},
	}},
	{Name: "动态规划", Children: []TagNode{
		{Name: "线性DP", Children: []TagNode{
			{Name: "数字三角形"},
			{Name: "最长上升子序列"},
			{Name: "最长公共子序列"},
			{Name: "最大子段和"},
			{Name: "状态机DP"},
		}},
		{Name: "背包DP", Children: []TagNode{
			{Name: "01背包"},
			{Name: "完全背包"},
			{Name: "多重背包", Children: []TagNode{
				{Name: "二进制优化"},
				{Name: "单调队列优化"},
			}},
			{Name: "分组背包"},
			{Name: "二维费用背包"},
			{Name: "有依赖的背包"},
		}},
		{Name: "区间DP", Children: []TagNode{
			{Name: "环形区间DP"},
		}},
		{Name: "树形DP", Children: []TagNode{
			{Name: "树上背包"},
			{Name: "换根DP"},
		}},
		{Name: "状态压缩DP", Children: []TagNode{
			{Name: "集合状压DP"},
			{Name: "插头DP"},
		}},
		{Name: "数位DP"},
		{Name: "概率期望DP"},
		{Name: "记忆化搜索"},
		{Name: "DP优化", Children: []TagNode{
			{Name: "单调队列优化"},
			{Name: "斜率优化"},
			{Name: "决策单调性"},
			{Name: "矩阵加速"},
			{Name: "线段树优化"},
			{Name: "wqs二分"},
		}},
	}},
	{Name: "数学", Children: []TagNode{
		{Name: "数论", Children: []TagNode{
			{Name: "因数倍数约数"},
			{Name: "质数与合数"},
			{Name: "质数筛", Children: []TagNode{
				{Name: "埃氏筛法"},
				{Name: "线性筛法"},
			}},
			{Name: "分解质因数"},
			{Name: "约数个数与约数和"},
			{Name: "最大公约数", Children: []TagNode{
				{Name: "辗转相除法"},
			}},
			{Name: "最小公倍数"},
			{Name: "同余"},
			{Name: "乘法逆元"},
			{Name: "费马小定理"},
			{Name: "欧拉函数"},
			{Name: "裴蜀定理"},
			{Name: "扩展欧几里得"},
			{Name: "中国剩余定理"},
			{Name: "BSGS算法"},
			{Name: "整除分块"},
			{Name: "积性函数"},
			{Name: "莫比乌斯反演"},
			{Name: "杜教筛"},
		}},
		{Name: "组合数学", Children: []TagNode{
			{Name: "加法与乘法原理"},
			{Name: "排列与组合"},
			{Name: "组合数计算", Children: []TagNode{
				{Name: "杨辉三角递推"},
				{Name: "逆元求组合数"},
				{Name: "卢卡斯定理"},
			}},
			{Name: "容斥原理"},
			{Name: "抽屉原理"},
			{Name: "卡特兰数"},
			{Name: "错位排列"},
		}},
		{Name: "线性代数", Children: []TagNode{
			{Name: "矩阵", Children: []TagNode{
				{Name: "矩阵乘法"},
				{Name: "矩阵快速幂"},
			}},
			{Name: "高斯消元", Children: []TagNode{
				{Name: "异或方程组"},
			}},
			{Name: "线性基"},
		}},
		{Name: "多项式", Children: []TagNode{
			{Name: "FFT"},
			{Name: "NTT"},
			{Name: "拉格朗日插值"},
			{Name: "多项式求逆"},
			{Name: "多项式开根"},
		}},
		{Name: "进制转换"},
		{Name: "博弈论", Children: []TagNode{
			{Name: "巴什博弈"},
			{Name: "Nim游戏"},
			{Name: "威佐夫博弈"},
			{Name: "SG函数"},
		}},
		{Name: "概率与期望", Children: []TagNode{
			{Name: "概率基础"},
			{Name: "期望的线性性"},
		}},
	}},
	{Name: "计算几何", Children: []TagNode{
		{Name: "二维几何基础", Children: []TagNode{
			{Name: "点与向量"},
			{Name: "点积与叉积"},
			{Name: "极角排序"},
		}},
		{Name: "多边形", Children: []TagNode{
			{Name: "凸包", Children: []TagNode{
				{Name: "Graham扫描法"},
				{Name: "Andrew算法"},
			}},
			{Name: "多边形面积"},
			{Name: "点与多边形位置关系"},
		}},
		{Name: "半平面交"},
		{Name: "旋转卡壳"},
		{Name: "平面最近点对"},
		{Name: "自适应辛普森积分"},
	}},
	{Name: "字符串", Children: []TagNode{
		{Name: "字符串哈希"},
		{Name: "KMP", Children: []TagNode{
			{Name: "前缀函数"},
			{Name: "失配树"},
		}},
		{Name: "扩展KMP"},
		{Name: "Manacher"},
		{Name: "AC自动机", Children: []TagNode{
			{Name: "fail指针"},
		}},
		{Name: "后缀数组", Children: []TagNode{
			{Name: "height数组与LCP"},
		}},
		{Name: "后缀自动机"},
		{Name: "回文自动机"},
		{Name: "最小表示法"},
	}},
	{Name: "杂项", Children: []TagNode{
		{Name: "构造"},
		{Name: "打表"},
		{Name: "对拍"},
		{Name: "交互题"},
		{Name: "随机化", Children: []TagNode{
			{Name: "模拟退火"},
		}},
	}},
}

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
	nodes, err := assign(DefaultTagTreeContent)
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
var DefaultMetaTagTreeContent = []TagNode{
	{Name: "地区", Children: []TagNode{
		{Name: "合肥市", Children: []TagNode{
			{Name: "瑶海区"}, {Name: "庐阳区"}, {Name: "蜀山区"}, {Name: "包河区"},
			{Name: "经开区"}, {Name: "高新区"}, {Name: "新站区"},
			{Name: "肥东县"}, {Name: "肥西县"}, {Name: "长丰县"}, {Name: "庐江县"}, {Name: "巢湖市"},
		}},
	}},
	{Name: "赛事", Children: []TagNode{
		{Name: "NOI"}, {Name: "NOIP"}, {Name: "CSP-J"}, {Name: "CSP-S"}, {Name: "APIO"}, {Name: "蓝桥杯"},
	}},
	{Name: "题库", Children: []TagNode{
		{Name: "洛谷"}, {Name: "Codeforces"}, {Name: "USACO"}, {Name: "AtCoder"}, {Name: "牛客"},
	}},
	{Name: "年份", Children: []TagNode{
		{Name: "2018"}, {Name: "2019"}, {Name: "2020"}, {Name: "2021"}, {Name: "2022"},
		{Name: "2023"}, {Name: "2024"}, {Name: "2025"}, {Name: "2026"},
	}},
	{Name: "校内·自编", Children: []TagNode{
		{Name: "校内考试"}, {Name: "教师自编"},
	}},
	{Name: "其他"},
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
	nodes, err := assign(DefaultMetaTagTreeContent)
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
