package repo

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 标题方括号前缀识别（v0.12）：用户给的三个实例 + 边角。
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

// 来源四字段归一：直传 > yaml > 标题识别，仅全空才走识别；枚举/年份校验。
func TestApplySourceFields(t *testing.T) {
	run := func(im Importer, title string, meta *problemYAML) error {
		return applySourceFields(im, title, &meta.SourceType, &meta.Source, &meta.Year, &meta.Region)
	}
	t.Run("全空走标题识别", func(t *testing.T) {
		meta := &problemYAML{}
		if err := run(Importer{}, "[2025瑶海区小学] 逆序输出", meta); err != nil {
			t.Fatal(err)
		}
		if meta.Year != 2025 || meta.Region != "瑶海区" || meta.Source != "小学" || meta.SourceType != SourceTypeRegion {
			t.Fatalf("识别结果不符: %+v", meta)
		}
	})
	t.Run("yaml 已有来源则整组跳过识别", func(t *testing.T) {
		meta := &problemYAML{Source: "校本题库"}
		if err := run(Importer{}, "[2025瑶海区小学] 逆序输出", meta); err != nil {
			t.Fatal(err)
		}
		if meta.Source != "校本题库" || meta.Year != 0 || meta.Region != "" || meta.SourceType != "" {
			t.Fatalf("yaml 优先被破坏: %+v", meta)
		}
	})
	t.Run("表单直传最高优先", func(t *testing.T) {
		meta := &problemYAML{Source: "yaml 来源"}
		im := Importer{Source: "表单来源", SourceType: SourceTypeLocal}
		if err := run(im, "[USACO 1.3.2] x", meta); err != nil {
			t.Fatal(err)
		}
		if meta.Source != "表单来源" || meta.SourceType != SourceTypeLocal {
			t.Fatalf("直传未生效: %+v", meta)
		}
	})
	t.Run("非法枚举报错", func(t *testing.T) {
		meta := &problemYAML{SourceType: "magazine"}
		if err := run(Importer{}, "无括号题", meta); err == nil || !strings.Contains(err.Error(), "source_type") {
			t.Fatalf("want source_type error, got %v", err)
		}
	})
	t.Run("年份越界报错", func(t *testing.T) {
		meta := &problemYAML{Year: 1800}
		if err := run(Importer{}, "无括号题", meta); err == nil || !strings.Contains(err.Error(), "year") {
			t.Fatalf("want year error, got %v", err)
		}
	})
}

// yaml 往返：导出带来源四字段，重新解析可读回。
func TestSourceYAMLRoundTrip(t *testing.T) {
	im := &Importer{}
	p := &Problem{
		Title: "修理牛棚", TimeLimitMS: 1000, MemoryLimitKB: 262144,
		SourceType: SourceTypeOJ, Source: "USACO 1.3.2", Year: 2020, Region: "合肥市瑶海区",
		Cases: []CaseMeta{{Index: 1}},
	}
	var meta problemYAML
	if err := yaml.Unmarshal(im.RenderProblemYAML(nil, p), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.SourceType != SourceTypeOJ || meta.Source != "USACO 1.3.2" || meta.Year != 2020 || meta.Region != "合肥市瑶海区" {
		t.Fatalf("yaml 往返丢失来源字段: %+v", meta)
	}
}
