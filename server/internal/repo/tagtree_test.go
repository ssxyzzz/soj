package repo

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func testTree() []TagNode {
	return []TagNode{
		{ID: 1, Name: "动态规划", Children: []TagNode{
			{ID: 2, Name: "树形 DP"},
			{ID: 3, Name: "状压 DP"},
		}},
		{ID: 4, Name: "图论", Children: []TagNode{
			{ID: 5, Name: "最短路"},
		}},
		{ID: 6, Name: "贪心"},
	}
}

func TestReconcileTagTree(t *testing.T) {
	next := 100
	nextID := func() (int64, error) { next++; return int64(next), nil }

	t.Run("同名继承身份，未变树零删除", func(t *testing.T) {
		out, removed, err := reconcileTagTree(testTree(), testTree(), nextID)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if len(removed) != 0 {
			t.Fatalf("未变树不应有删除: %v", removed)
		}
		if out[0].ID != 1 || out[0].Children[0].ID != 2 || out[2].ID != 6 {
			t.Fatalf("同名应继承旧 ID: %+v", out)
		}
	})

	t.Run("新名字取新号并计入删除差集", func(t *testing.T) {
		newTree := []TagNode{
			{ID: 0, Name: "动态规划", Children: []TagNode{
				{ID: 0, Name: "树形 DP"},
				{ID: 0, Name: "状压 DP"},
			}},
			{ID: 0, Name: "贪心"},
			{ID: 0, Name: "新增分类", Children: []TagNode{{ID: 0, Name: "构造"}}},
		} // 删除「图论」子树（4、5），新增 2 个
		out, removed, err := reconcileTagTree(testTree(), newTree, nextID)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if len(removed) != 2 || removed[0] != 4 || removed[1] != 5 {
			t.Fatalf("removed = %v, want [4 5]", removed)
		}
		if out[0].ID != 1 || out[1].ID != 6 {
			t.Fatalf("保留节点应继承旧 ID: %+v", out)
		}
		if out[2].ID == 0 || out[2].Children[0].ID == 0 {
			t.Fatalf("新节点应取新号: %+v", out[2])
		}
	})

	t.Run("同名取先见者且整树去重", func(t *testing.T) {
		// 旧树有两个同名「重名」（不同层级）：lookup 取先见者
		old := []TagNode{
			{ID: 1, Name: "DP", Children: []TagNode{{ID: 2, Name: "DP"}}},
		}
		newTree := []TagNode{{ID: 0, Name: "DP"}}
		out, removed, err := reconcileTagTree(old, newTree, nextID)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if out[0].ID != 1 {
			t.Fatalf("同名应继承先见者 ID: %+v", out)
		}
		if len(removed) != 1 || removed[0] != 2 {
			t.Fatalf("removed = %v, want [2]", removed)
		}
	})
}

func TestTagIDNamesAndLookup(t *testing.T) {
	names := TagIDNames(testTree())
	if names[2] != "树形 DP" || names[6] != "贪心" || len(names) != 6 {
		t.Fatalf("TagIDNames 异常: %v", names)
	}
	lookup := TagNameLookup(testTree())
	if lookup["最短路"] != 5 || lookup["动态规划"] != 1 {
		t.Fatalf("TagNameLookup 异常: %v", lookup)
	}
}

func TestValidateTagTreeShape(t *testing.T) {
	cases := []struct {
		name    string
		tree    []TagNode
		wantErr bool
	}{
		{"正常树", testTree(), false},
		{"空树合法", nil, false},
		{"空名", []TagNode{{Name: "  "}}, true},
		{"超长名", []TagNode{{Name: strings.Repeat("标", 33)}}, true},
		{"同级重名", []TagNode{
			{ID: 1, Name: "DP"},
			{ID: 2, Name: "DP"},
		}, true},
		{"不同级重名合法", []TagNode{
			{ID: 1, Name: "DP", Children: []TagNode{{ID: 2, Name: "DP"}}},
		}, false},
	}
	for _, tc := range cases {
		err := ValidateTagTreeShape(tc.tree)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tc.name, err, tc.wantErr)
		}
	}
}

// TestTagTreeShapeNodeCap 节点数上限按节点计数：新节点未取号（ID=0）也计入，
// 不按 ID 集合计数（重复 0 会合并少计、绕过上限）。
func TestTagTreeShapeNodeCap(t *testing.T) {
	mk := func(n int) []TagNode {
		list := make([]TagNode, 0, n)
		for i := 0; i < n; i++ {
			id := int64(0) // 奇数位保持未取号，模拟后台保存前的草稿新节点
			if i%2 == 0 {
				id = int64(i + 1)
			}
			list = append(list, TagNode{ID: id, Name: fmt.Sprintf("标签%d", i)})
		}
		return list
	}
	if err := ValidateTagTreeShape(mk(1000)); err != nil {
		t.Fatalf("1000 节点应合法: %v", err)
	}
	if err := ValidateTagTreeShape(mk(1001)); err == nil {
		t.Fatal("1001 节点应超上限")
	}
}

// TestAppendTagNodeCap 来源库「转为标签」追加也要过节点数上限（满编拒绝、
// 未满成功），复用 openTestRepo（本地无 Mongo 自动 skip）。
func TestAppendTagNodeCap(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	full := make([]TagNode, 0, 1000)
	for i := 0; i < 1000; i++ {
		full = append(full, TagNode{ID: int64(i + 1), Name: fmt.Sprintf("标签%d", i)})
	}
	if err := r.ReplaceTagTree(ctx, full); err != nil {
		t.Fatalf("预置满编树: %v", err)
	}
	if _, err := r.AppendTagNode(ctx, 0, "新标签"); err == nil || !strings.Contains(err.Error(), "1000") {
		t.Fatalf("满编追加应被上限拦截: %v", err)
	}
	if err := r.ReplaceTagTree(ctx, full[:999]); err != nil {
		t.Fatalf("预置 999 节点树: %v", err)
	}
	id, err := r.AppendTagNode(ctx, 0, "新标签")
	if err != nil || id <= 0 {
		t.Fatalf("未满编追加应成功: id=%d err=%v", id, err)
	}
	tree, err := r.GetTagTree(ctx)
	if err != nil || len(tree) != 1000 {
		t.Fatalf("追加后应恰为 1000 节点: len=%d err=%v", len(tree), err)
	}
}
