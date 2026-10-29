package repo

// 默认树 embed 资产防漂移（v0.32 第二步-2）：两棵默认树自 Go 字面量外移 JSON 后，
// 这里在无 Mongo 环境也可跑——启动即解码兜底（坏 JSON 直接 panic 于测试）+ 结构断言
// 锁住关键骨架（大类数 / 节点总数 / 深度 / 代表枝），手改 JSON 造成的漂移过不了本测试。

import "testing"

func countNodes(nodes []TagNode) (n int, depth int) {
	for _, t := range nodes {
		n++
		cn, cd := countNodes(t.Children)
		n += cn
		if d := cd + 1; d > depth {
			depth = d
		}
	}
	return n, depth
}

func findChild(nodes []TagNode, name string) *TagNode {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
	}
	return nil
}

// TestDefaultTreesDecode 默认树可解码、形状合法、ID 全零（注入时现场取号）。
func TestDefaultTreesDecode(t *testing.T) {
	for name, tree := range map[string]func() []TagNode{
		"知识树": DefaultTagTreeContent,
		"来源树": DefaultMetaTagTreeContent,
	} {
		nodes := tree()
		if len(nodes) == 0 {
			t.Fatalf("%s：解码为空", name)
		}
		if err := ValidateTagTreeShape(nodes); err != nil {
			t.Fatalf("%s：形状校验失败: %v", name, err)
		}
		var walk func(list []TagNode)
		walk = func(list []TagNode) {
			for _, n := range list {
				if n.ID != 0 {
					t.Fatalf("%s：节点 %q 带非零 ID %d（种子应为零、注入时取号）", name, n.Name, n.ID)
				}
				walk(n.Children)
			}
		}
		walk(nodes)
	}
}

// TestDefaultKnowledgeTreeSkeleton 知识树骨架断言：10 大类 / 384 节点 / 最深 4 层，
// 与 2026-10-24 生产对齐版字面量一致（防 JSON 手改漂移）。
func TestDefaultKnowledgeTreeSkeleton(t *testing.T) {
	nodes := DefaultTagTreeContent()
	if len(nodes) != 10 {
		t.Fatalf("大类数 = %d，期望 10", len(nodes))
	}
	n, depth := countNodes(nodes)
	if n != 384 {
		t.Fatalf("节点总数 = %d，期望 384", n)
	}
	if depth != 4 {
		t.Fatalf("最大深度 = %d，期望 4", depth)
	}
	// 代表枝抽查：首末大类与最深枝各一条
	if findChild(nodes, "基础语法") == nil || findChild(nodes, "杂项") == nil {
		t.Fatal("代表大类缺失：基础语法 / 杂项")
	}
	if c := findChild(nodes, "数学"); c != nil {
		if findChild(c.Children, "数论") == nil {
			t.Fatal("数学→数论 枝缺失")
		}
	} else {
		t.Fatal("数学大类缺失")
	}
}

// TestDefaultMetaTreeSkeleton 来源树骨架断言：6 顶层枝（地区/赛事/题库/年份/校内·自编/其他），
// 年份枝 2018–2026 共 9 个。
func TestDefaultMetaTreeSkeleton(t *testing.T) {
	nodes := DefaultMetaTagTreeContent()
	if len(nodes) != 6 {
		t.Fatalf("顶层枝数 = %d，期望 6", len(nodes))
	}
	year := findChild(nodes, "年份")
	if year == nil || len(year.Children) != 9 {
		t.Fatalf("年份枝缺失或子节点数 %d ≠ 9", len(year.Children))
	}
	region := findChild(nodes, "地区")
	if region == nil || findChild(region.Children, "合肥市") == nil {
		t.Fatal("地区→合肥市 枝缺失")
	}
}
