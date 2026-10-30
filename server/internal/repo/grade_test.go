package repo

// 年级结构化（v0.34）单测：档位名互转 / 别名解析 / 学年 9/1 边界。纯函数无库依赖。

import (
	"testing"
	"time"
)

func TestGradeNameRoundTrip(t *testing.T) {
	for no := 1; no <= 12; no++ {
		name := GradeName(no)
		if name == "" {
			t.Fatalf("档位 %d 无显示名", no)
		}
		if back := ParseGrade(name); back != no {
			t.Errorf("ParseGrade(%q) = %d，期望 %d（名称往返）", name, back, no)
		}
	}
	if GradeName(0) != "" || GradeName(14) != "" {
		t.Error("越界档位应返回空串")
	}
	if GradeName(GradeGraduated) != "已毕业" {
		t.Error("13 档应为已毕业")
	}
	if ParseGrade("已毕业") != GradeGraduated {
		t.Error("已毕业应可显式选定（终态：晋级 $inc 不碰 13 档）")
	}
}

func TestParseGradeAliases(t *testing.T) {
	cases := map[string]int{
		"初一": 7, "七年级": 7, "7年级": 7, "7": 7,
		"初三": 9, "九年级": 9,
		"高一": 10, "十年级": 10, "高一新生": 10,
		"高三": 12, "十二年级": 12,
		"小六": 6, "六年级": 6,
		"  高二  ": 11, // 首尾空白容忍
		"":        0,
		"2024级": 0,  // 大学式写法不映射（置未设置由 root 补录）
		"大一":  0,
		"13":   0, "0": 0, "99": 0,
	}
	for in, want := range cases {
		if got := ParseGrade(in); got != want {
			t.Errorf("ParseGrade(%q) = %d，期望 %d", in, got, want)
		}
	}
}

func TestSchoolYearStartBeijing(t *testing.T) {
	// 学年按北京时间 9/1 切换，与服务器时区无关：用 UTC 时刻卡边界
	cases := []struct {
		t    time.Time
		want int
		desc string
	}{
		{time.Date(2026, 8, 31, 20, 0, 0, 0, time.UTC), 2026, "8/31 20:00 UTC = 北京 9/1 04:00 → 新学年"}, // 北京 9/1 凌晨已跨线
		{time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC), 2025, "8/31 10:00 UTC = 北京 18:00 → 旧学年"},
		{time.Date(2026, 9, 1, 0, 30, 0, 0, time.UTC), 2026, "9/1 00:30 UTC = 北京 08:30 → 新学年"},
		{time.Date(2027, 1, 15, 3, 0, 0, 0, time.UTC), 2026, "寒假仍属 2026 学年"},
		{time.Date(2026, 12, 31, 16, 0, 0, 0, time.UTC), 2026, "年末即学年年末"},
	}
	for _, c := range cases {
		if got := SchoolYearStart(c.t); got != c.want {
			t.Errorf("%s：SchoolYearStart = %d，期望 %d", c.desc, got, c.want)
		}
	}
}
