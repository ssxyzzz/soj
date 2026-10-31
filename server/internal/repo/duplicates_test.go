package repo

// 重复题扫描纯函数单测（v0.47）：归一化 / 签名一致率 / 聚类成组，不依赖 Mongo。

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeDupText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Hello, World!", "helloworld"},
		{"【题目描述】A+B 问题（v2）", "题目描述ab问题v2"},
		{"ＡＢＣ", "ａｂｃ"}, // 字母小写化（全角字形保留，不做全半角折叠）
		{"P1001 \t\n【输入格式】", "p1001输入格式"},
		{"!!!？？？。。。", ""}, // 纯标点 → 全空（空壳题口径）
	}
	for _, c := range cases {
		if got := normalizeDupText(c.in); got != c.want {
			t.Errorf("normalizeDupText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// dupVariedText 确定性生成 n 行互不相同的题面句（行内数值随行号变化，保证
// distinct gram 数与行数同阶——重复句式会人为压低 Jaccard，不能当真实题面用）。
func dupVariedText(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s第%d行：给出%d个不相同的整数按照从小到大排序之后输出到屏幕。", prefix, i, i%9+1)
	}
	return b.String()
}

func mkDupDoc(id int64, title, stmt string) dupDoc {
	norm := normalizeDupText(title + " " + stmt)
	return dupDoc{
		ProblemInfo: ProblemInfo{ID: id, Title: title},
		Preview:     truncateRunes(stmt, 80),
		Sig:         dupSigFromRunes([]rune(norm)),
	}
}

func TestDupSigSim(t *testing.T) {
	base := dupVariedText("求和题", 60)
	sigOf := func(s string) *[dupSigRows]uint32 {
		sig := dupSigFromRunes([]rune(normalizeDupText(s)))
		return &sig
	}
	if sim := dupSim(sigOf(base), sigOf(base)); sim != 1 {
		t.Fatalf("同文本一致率 = %v, want 1", sim)
	}
	// 中段插入一句仍应过阈值（Jaccard ≈0.95，落在 MinHash 估计波动内）
	half := len(base) / 2
	variant := base[:half] + "本行是被插入的变体描述请把结果写入输出文件。" + base[half:]
	if sim := dupSim(sigOf(base), sigOf(variant)); sim < dupSimThreshold {
		t.Fatalf("小幅插入一致率 = %v, want >= %v", sim, dupSimThreshold)
	}
	// 完全不同的文本必须压到阈值之下
	other := dupVariedText("图论最短路使用优先队列优化的迪杰斯特拉算法实现", 60)
	if sim := dupSim(sigOf(base), sigOf(other)); sim >= dupSimThreshold {
		t.Fatalf("无关文本一致率 = %v, want < %v", sim, dupSimThreshold)
	}
	// 空文本签名全 MaxUint32：彼此一致率 1（空壳同壳口径），对非空文本趋 0
	empty := dupSigFromRunes(nil)
	if sim := dupSim(&empty, &empty); sim != 1 {
		t.Fatalf("空文本自比一致率 = %v, want 1", sim)
	}
	if sim := dupSim(&empty, sigOf(base)); sim >= dupSimThreshold {
		t.Fatalf("空壳对实题一致率 = %v, want < %v", sim, dupSimThreshold)
	}
}

func TestDupCluster(t *testing.T) {
	stmt := dupVariedText("本题给出一个字符串请判断它是否为回文串", 40)
	variant := dupVariedText("本题给出一个字符串请你判断其是否为回文串", 40) // 行文微调的跨源重复
	other := dupVariedText("动态规划求最长上升子序列的经典状态转移方程实现", 40)
	docs := []dupDoc{
		mkDupDoc(1, "A+B Problem", stmt),
		mkDupDoc(2, "回文串判断", variant), // 题面近似、标题不同的跨源重复
		mkDupDoc(3, "字符串", stmt),      // 同题面第三份拷贝
		mkDupDoc(4, "最长上升子序列", other), // 无关题
		mkDupDoc(5, "!!!", ""),         // 空壳 A
		mkDupDoc(6, "？？。", ""),         // 空壳 B（归一化全空 → 同壳）
	}
	groups := dupCluster(docs)
	if len(groups) != 2 {
		t.Fatalf("组数 = %d, want 2（三份回文题一组 + 两空壳一组）", len(groups))
	}
	g1, g2 := groups[0], groups[1]
	if len(g1.Problems) != 3 || g1.Similarity < dupSimThreshold {
		t.Errorf("组1 = %d 题 相似度 %v, want 3 题 且 >= %v", len(g1.Problems), g1.Similarity, dupSimThreshold)
	}
	ids := map[int64]bool{}
	for _, p := range g1.Problems {
		ids[p.ID] = true
	}
	for _, want := range []int64{1, 2, 3} {
		if !ids[want] {
			t.Errorf("组1 缺题 #%d（实得 %v）", want, ids)
		}
	}
	if g1.Preview == "" {
		t.Error("组1 缺预览")
	}
	if len(g2.Problems) != 2 || g2.Similarity != 1 {
		t.Errorf("组2 = %d 题 相似度 %v, want 2 空壳题且相似度 1", len(g2.Problems), g2.Similarity)
	}
	// 排序口径：组内题数降序
	if len(g1.Problems) < len(g2.Problems) {
		t.Error("组排序应按组内题数降序")
	}
}

func TestDupClusterOversizedBucket(t *testing.T) {
	// 超员护栏：> dupMaxBucket 份同签名（精确重复）仍须成组，不因护栏丢组
	docs := make([]dupDoc, 0, dupMaxBucket+20)
	for i := 0; i < dupMaxBucket+20; i++ {
		docs = append(docs, mkDupDoc(int64(i+1), "同一道题", "完全相同的题面内容"+dupVariedText("", 3)))
	}
	groups := dupCluster(docs)
	if len(groups) != 1 || len(groups[0].Problems) != dupMaxBucket+20 || groups[0].Similarity != 1 {
		t.Fatalf("超员精确重复应聚为一组 %d 题，实得 %d 组", dupMaxBucket+20, len(groups))
	}
}
