package repo

// 题库列表下推查询（v0.26）语义测试：q（题号精确 ∨ 标题子串）、tags/metaTags 并集、
// difficulty 生效语义（未设置按 D3）、反向排除、分页与总数。
// 本地无 Mongo 自动 skip（openTestRepo 同款）。

import (
	"context"
	"testing"
)

func TestListProblemsPage(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	seed := func(id int64, title string, tags, meta []int64, difficulty int) {
		p := &Problem{ID: id, Title: title, Tags: tags, MetaTags: meta, Difficulty: difficulty,
			TimeLimitMS: 1000, MemoryLimitKB: 262144}
		if err := r.UpsertProblem(ctx, p); err != nil {
			t.Fatalf("UpsertProblem #%d: %v", id, err)
		}
	}
	seed(101, "贪心基础", []int64{1}, []int64{10}, 0)  // difficulty 未设置 → 生效 D3
	seed(102, "动态规划入门", []int64{2}, nil, 5)
	seed(103, "二分查找", []int64{1, 2}, []int64{20}, 3)
	seed(104, "线段树进阶", []int64{3}, nil, 7)

	// 标题子串（不分大小写语义对 CJK 无差，ASCII 验证）
	list, total, err := r.ListProblemsPage(ctx, ProblemListFilter{Q: "dp"}, 1, 20) // 无命中
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("Q=无命中: total=%d err=%v", total, err)
	}
	list, total, err = r.ListProblemsPage(ctx, ProblemListFilter{Q: "贪心"}, 1, 20)
	if err != nil || total != 1 || len(list) != 1 || list[0].ID != 101 {
		t.Fatalf("Q=贪心: total=%d list=%v err=%v", total, list, err)
	}

	// 题号精确：纯数字且与格式化结果一致才按 ID 匹配（"0102" 不算 #102）
	list, total, err = r.ListProblemsPage(ctx, ProblemListFilter{Q: "102"}, 1, 20)
	if err != nil || total != 1 || list[0].ID != 102 {
		t.Fatalf("Q=102: total=%d err=%v", total, err)
	}
	list, total, err = r.ListProblemsPage(ctx, ProblemListFilter{Q: "0102"}, 1, 20)
	if err != nil || total != 0 {
		t.Fatalf("Q=0102（应不命中）: total=%d err=%v", total, err)
	}

	// 标签并集：tags=1 命中 101/103；tags=1,2 命中 101/102/103
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Tags: []int64{1}}, 1, 20)
	if total != 2 {
		t.Fatalf("tags=1: total=%d", total)
	}
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Tags: []int64{1, 2}}, 1, 20)
	if total != 3 {
		t.Fatalf("tags=1,2: total=%d", total)
	}

	// 来源并集
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{MetaTags: []int64{10, 20}}, 1, 20)
	if total != 2 {
		t.Fatalf("meta=10,20: total=%d", total)
	}

	// 生效难度：difficulty=3 须含「未设置」文档（101 未设置按 D3），不含 5/7
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Difficulty: []int{3}}, 1, 20)
	if total != 2 {
		t.Fatalf("difficulty=3（含未设置）: total=%d", total)
	}
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Difficulty: []int{5}}, 1, 20)
	if total != 1 {
		t.Fatalf("difficulty=5: total=%d", total)
	}
	// 非默认档筛选不收未设置文档
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Difficulty: []int{1, 2}}, 1, 20)
	if total != 0 {
		t.Fatalf("difficulty=1,2（未设置不算）: total=%d", total)
	}

	// 反向排除 + q + 标签叠加
	f := ProblemListFilter{ExcludeIDs: []int64{103}, Q: "查找", Tags: []int64{1}}
	_, total, _ = r.ListProblemsPage(ctx, f, 1, 20)
	if total != 0 {
		t.Fatalf("排除+q+tags 叠加: total=%d", total)
	}
	f.ExcludeIDs = []int64{102, 104}
	_, total, _ = r.ListProblemsPage(ctx, f, 1, 20)
	if total != 1 {
		t.Fatalf("排除后 q+tags: total=%d", total)
	}

	// 分页：_id 升序，skip/limit 与总数一致
	p1, total, _ := r.ListProblemsPage(ctx, ProblemListFilter{}, 1, 3)
	p2, total2, _ := r.ListProblemsPage(ctx, ProblemListFilter{}, 2, 3)
	if total != 4 || total2 != 4 || len(p1) != 3 || len(p2) != 1 || p1[0].ID != 101 || p2[0].ID != 104 {
		t.Fatalf("分页: total=%d/%d p1=%d p2=%d", total, total2, len(p1), len(p2))
	}
	// 越界页返回空但总数不变
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{}, 9, 3)
	if total != 4 {
		t.Fatalf("越界页: total=%d", total)
	}

	// ---- v0.26.1 搜索命中知识点/标签（与标题/题号并集，与显式筛选 AND 正交） ----
	// SearchTags=1 命中 101/103；与 Q=贪心（标题命中 101）并集仍为 101/103
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Q: "贪心", SearchTags: []int64{1}}, 1, 20)
	if total != 2 {
		t.Fatalf("搜索并集（标题∨知识点）: total=%d", total)
	}
	// 与显式筛选 AND：显式 tags=3 只留 104，搜索命中集不含它 → 空
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Q: "贪心", SearchTags: []int64{1}, Tags: []int64{3}}, 1, 20)
	if total != 0 {
		t.Fatalf("搜索与显式筛选正交（AND）: total=%d", total)
	}
	// SearchMetaTags 并集：meta=10 命中 101，与 Q=二分（标题命中 103）并集为 101/103
	_, total, _ = r.ListProblemsPage(ctx, ProblemListFilter{Q: "二分", SearchMetaTags: []int64{10}}, 1, 20)
	if total != 2 {
		t.Fatalf("搜索并集（标题∨来源标签）: total=%d", total)
	}
	// 搜索条件与反向排除叠加：排除 101 后并集只剩 103
	f2 := ProblemListFilter{Q: "贪心", SearchTags: []int64{1}, ExcludeIDs: []int64{101}}
	_, total, _ = r.ListProblemsPage(ctx, f2, 1, 20)
	if total != 1 {
		t.Fatalf("搜索+排除叠加: total=%d", total)
	}

	// 树名字收集（不分大小写）+ 子树展开
	tree := []TagNode{
		{ID: 1, Name: "算法", Children: []TagNode{
			{ID: 11, Name: "贪心"},
			{ID: 12, Name: "DP", Children: []TagNode{{ID: 121, Name: "区间DP"}}},
		}},
		{ID: 2, Name: "数据结构"},
	}
	if got := CollectTagIDsByName(tree, "dp"); len(got) != 2 || got[0] != 12 || got[1] != 121 {
		t.Fatalf("CollectTagIDsByName(dp): %v", got)
	}
	if got := CollectTagIDsByName(tree, "贪心"); len(got) != 1 || got[0] != 11 {
		t.Fatalf("CollectTagIDsByName(贪心): %v", got)
	}
	if got := CollectTagIDsByName(tree, ""); got != nil {
		t.Fatalf("空搜索词应返回 nil: %v", got)
	}
	// 命中父节点 → 子树全收（搜「算法」= 1/11/12/121）
	if got := CollectSubtreeIDs(tree, CollectTagIDsByName(tree, "算法")); len(got) != 4 {
		t.Fatalf("子树展开(算法): %v", got)
	}
}
