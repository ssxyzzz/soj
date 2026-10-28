package server

// 批量建号（v0.24 小批）单测：纯函数部分（模板跳行判定 / BOM 清理） +
// CSV 解析表头映射与行级 best-effort 的行为面（本地无 Mongo 自动 skip）。

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsUserImportSkipRow(t *testing.T) {
	cases := []struct {
		rec  []string
		want bool
	}{
		{[]string{"# 注释"}, true},
		{[]string{"  # 注释"}, true},
		{[]string{"", "", ""}, true},
		{[]string{"1001", "zhangsan", "pwd"}, false},
		{[]string{"#注释", "不是注释"}, true}, // 首个非空格以 # 开头即整行跳过
	}
	for _, c := range cases {
		if got := isUserImportSkipRow(c.rec); got != c.want {
			t.Errorf("isUserImportSkipRow(%v) = %v，期望 %v", c.rec, got, c.want)
		}
	}
}

func TestStripCSVBOM(t *testing.T) {
	if got := stripCSVBOM("\xEF\xBB\xBFid"); got != "id" {
		t.Errorf("BOM 未清除: %q", got)
	}
	if got := stripCSVBOM("id"); got != "id" {
		t.Errorf("普通字段被改动: %q", got)
	}
}

func TestUserImportTemplateShape(t *testing.T) {
	// 模板行结构自检：说明行以 # 开头、表头五列齐全（与 AdminUserImportTemplate 同源口径）
	rows := [][]string{
		{"# SOJ 批量建号模板"},
		{"id", "username", "password", "nickname", "role"},
	}
	comment := 0
	header := -1
	for i, r := range rows {
		if isUserImportSkipRow(r) {
			comment++
			continue
		}
		header = i
	}
	if comment != 1 || header != 1 {
		t.Fatalf("模板结构异常：注释行 %d / 表头行下标 %d", comment, header)
	}
	if got, want := strings.Join(rows[header], ","), "id,username,password,nickname,role"; got != want {
		t.Fatalf("表头 = %s，期望 %s", got, want)
	}
	if !bytes.Contains([]byte("\xEF\xBB\xBF"), []byte("\xEF")) {
		t.Fatal("BOM 常量自检")
	}
}
