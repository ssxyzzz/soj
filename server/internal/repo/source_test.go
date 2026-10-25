package repo

import (
	"testing"
)

// 标题方括号前缀识别（v0.12）：用户给的三个实例 + 边角。
// 2026-10-25 双树定稿后仍保留：ParseSourceHint 是导入挂来源树节点的线索来源。
func TestParseSourceHint(t *testing.T) {
	cases := []struct {
		title string
		want  SourceHint
	}{
		// 用户实例：来源/年份/地区三路
		{"[USACO 1.3.2] Barn Repair 修理牛棚", SourceHint{SourceType: SourceTypeOJ, Source: "USACO 1.3.2"}},
		{"[2025瑶海区小学] 逆序输出（reverse）", SourceHint{SourceType: SourceTypeRegion, Source: "小学", Year: 2025, Region: "瑶海区"}},
		{"[2025 瑶海区小学] Atbash 密码（atbash）", SourceHint{SourceType: SourceTypeRegion, Source: "小学", Year: 2025, Region: "瑶海区"}},
		// 赛事年份并写：NOIP2020 → 年份剥出、余串 NOIP（多空格归一）
		{"[NOIP2020] 小凯的疑惑", SourceHint{SourceType: SourceTypeContest, Source: "NOIP", Year: 2020}},
		{"[NOIP 2002 普及组] 过河卒", SourceHint{SourceType: SourceTypeContest, Source: "NOIP 普及组", Year: 2002}},
		// OJ 前缀（中文）
		{"[洛谷P1001] A+B Problem", SourceHint{SourceType: SourceTypeOJ, Source: "洛谷P1001"}},
		// 4 位题号不误判年份：Codeforces 1900B 的 1900 后跟字母
		{"[Codeforces 1900B] Orleni rezan", SourceHint{SourceType: SourceTypeOJ, Source: "Codeforces 1900B"}},
		// 只有年份 + 地区、无来源余串
		{"[2025瑶海区] 便条", SourceHint{SourceType: SourceTypeRegion, Year: 2025, Region: "瑶海区"}},
		// 全角方括号；地区整体剥离后无余串 → 来源留空（展示由类型+地区+年份拼装）
		{"［2019 合肥市］题目", SourceHint{SourceType: SourceTypeRegion, Year: 2019, Region: "合肥市"}},
		// 无括号 / 空括号内容 → 无提示
		{"A+B Problem", SourceHint{}},
		{"[] x", SourceHint{}},
	}
	for _, tc := range cases {
		got := ParseSourceHint(tc.title)
		if got != tc.want {
			t.Errorf("ParseSourceHint(%q) = %+v, want %+v", tc.title, got, tc.want)
		}
	}
}

// 标签按名匹配与未匹配名保留（v0.12 来源库联动）：命中去重、同名取先见节点、
// 未命中原名保序返回、空名/重复输入剔除。2026-10-25 双树后未匹配名直接丢弃，
// 但匹配核心行为不变，测试保留。
func TestMatchTagNames(t *testing.T) {
	lookup := map[string]int64{"贪心": 11, "动态规划": 20, "线性 DP": 21}
	ids, unmatched := matchTagNames(lookup, []string{"贪心", "神秘标签Q", " 线性 DP ", "", "贪心", "动态规划", "另一个未知"})
	if len(ids) != 3 || ids[0] != 11 || ids[1] != 21 || ids[2] != 20 {
		t.Fatalf("命中 ID 不符: %v", ids)
	}
	if len(unmatched) != 2 || unmatched[0] != "神秘标签Q" || unmatched[1] != "另一个未知" {
		t.Fatalf("未匹配名不符: %v", unmatched)
	}
	// 全命中 / 空输入
	if ids2, un2 := matchTagNames(lookup, []string{"贪心"}); len(ids2) != 1 || len(un2) != 0 {
		t.Fatalf("全命中: %v %v", ids2, un2)
	}
	if ids3, un3 := matchTagNames(lookup, nil); ids3 != nil || un3 != nil {
		t.Fatalf("空输入应双 nil: %v %v", ids3, un3)
	}
}
