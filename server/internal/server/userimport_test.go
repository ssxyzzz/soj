package server

// 批量建号（v0.24 小批）单测：纯函数部分（模板跳行判定 / BOM 清理） +
// CSV 解析表头映射与行级 best-effort 的行为面（本地无 Mongo 自动 skip）。

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
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
	// 模板行结构自检：说明行以 # 开头、表头为中文花名册七列（与 AdminUserImportTemplate 同源口径）
	rows := [][]string{
		{"# SOJ 批量建号·中文花名册模板"},
		{"id", "姓名", "性别", "年级", "学校", "地区", "角色"},
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
	if got, want := strings.Join(rows[header], ","), "id,姓名,性别,年级,学校,地区,角色"; got != want {
		t.Fatalf("表头 = %s，期望 %s", got, want)
	}
	if !bytes.Contains([]byte("\xEF\xBB\xBF"), []byte("\xEF")) {
		t.Fatal("BOM 常量自检")
	}
}

// ---- v0.34 中文花名册两段式 ----

func TestPinyinUsername(t *testing.T) {
	cases := []struct {
		name string
		id   int64
		want string
	}{
		{"张三", 1001, "zhangsan1001"},
		{"李四", 1002, "lisi1002"},
		{"欧阳娜娜", 7, "ouyangnana7"},
		{"Tom", 88, "tom88"},                 // 英文名直接小写
		{"王 小 明", 9, "wangxiaoming9"},      // 空白剔除
		{"阿依古丽·买买提", 10, "ayigulimaimaiti10"}, // 分隔点剔除
	}
	for _, c := range cases {
		if got := pinyinUsername(c.name, c.id); got != c.want {
			t.Errorf("pinyinUsername(%q, %d) = %q，期望 %q", c.name, c.id, got, c.want)
		}
	}
	// 多音字取默认读音（曾→ceng、单→dan、乐→le）——预览行内修改兜底，不在此断言具体读音，
	// 只断言产出合法用户名（字母+数字、非空）
	for _, name := range []string{"曾琛", "单小雨", "乐乐"} {
		if u := pinyinUsername(name, 100); !usernameRE.MatchString(u) {
			t.Errorf("多音字 %q 生成的用户名不合法：%q", name, u)
		}
	}
}

func TestGenImportPassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p := genImportPassword()
		if len(p) != 8 {
			t.Fatalf("密码长度 = %d，期望 8（%q）", len(p), p)
		}
		for _, ch := range p {
			if !strings.ContainsRune(pwAlphabet, ch) {
				t.Fatalf("密码含池外字符 %q（%q）", ch, p)
			}
		}
		// 去混淆字符不入池
		if strings.ContainsAny(p, "0O1lI") {
			t.Fatalf("密码含易混淆字符（%q）", p)
		}
		seen[p] = true
	}
	if len(seen) < 190 {
		t.Errorf("200 次生成仅 %d 个不同密码，随机性可疑", len(seen))
	}
}

func TestDecodeUserImportCSVGBK(t *testing.T) {
	// Excel 另存 CSV 常为 GBK：中文表头 + 中文数据按 GB18030 编码后应能正确解码
	gbk, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("id,姓名,年级\n1001,张三,初一\n"))
	if err != nil {
		t.Fatal(err)
	}
	records, err := decodeUserImportCSV(gbk)
	if err != nil {
		t.Fatal(err)
	}
	if records[0][1] != "姓名" || records[1][1] != "张三" || records[1][2] != "初一" {
		t.Errorf("GBK 解码异常：%v", records)
	}
	// UTF-8 带 BOM 正常路径
	records, err = decodeUserImportCSV([]byte("\xEF\xBB\xBFid,姓名\n1001,张三\n"))
	if err != nil {
		t.Fatal(err)
	}
	if records[0][0] != "id" || records[1][1] != "张三" {
		t.Errorf("UTF-8/BOM 解码异常：%v", records)
	}
}

func TestUserImportParseRoster(t *testing.T) {
	records := [][]string{
		{"# 注释行"},
		{"姓名", "id", "性别", "年级", "学校", "地区"}, // 列序无关（中文别名映射）
		{"张三", "1001", "男", "初一", "市一中", "山东济南"},
		{"李四", "1002", "女", "8年级", "", ""},       // 数字变体年级
		{"王五", "abc", "", "大一", "", ""},           // id 非数字 + 年级不可识别
		{"赵六", "1001", "", "", "", ""},              // 表内 id 重复
		{"张三", "1001", "", "", "", ""},              // 表内用户名重复（拼音相同）
	}
	rows, err := userImportParse(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("识别行数 = %d，期望 5（%v）", len(rows), rows)
	}
	if rows[0].Username != "zhangsan1001" || rows[0].Grade != "初一" || rows[0].School != "市一中" {
		t.Errorf("张三行解析异常：%+v", rows[0])
	}
	if rows[0].Password == "" || len(rows[0].Password) != 8 {
		t.Errorf("初始密码应已生成（8 位）：%q", rows[0].Password)
	}
	if rows[1].Grade != "8年级" {
		t.Errorf("年级保留原文显示：%+v", rows[1])
	}
	// 问题行：王五（id + 年级）、赵六/张三重复行
	for _, r := range rows[2:] {
		if len(r.Problems) == 0 {
			t.Errorf("第 %d 行应有问题标记：%+v", r.Row, r)
		}
	}
	if !containsProblem(rows[2], "id 需为正整数") || !containsProblem(rows[2], "年级无法识别") {
		t.Errorf("王五行问题缺失：%v", rows[2].Problems)
	}
	if !containsProblem(rows[4], "表内 id 重复") {
		t.Errorf("赵六行应有 id 重复：%v", rows[4].Problems)
	}

	// 表头缺列：无 id 且无姓名 → 报错
	if _, err := userImportParse([][]string{{"姓名", "性别"}, {"张三", "男"}}); err == nil {
		t.Error("缺 id 列应报错")
	}
	// 老英文模板路径：username 手填时 id 可留空
	rows, err = userImportParse([][]string{
		{"username", "password", "nickname", "role"},
		{"lisi", "Passw0rd!", "李四", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ID != 0 || rows[0].Username != "lisi" || len(rows[0].Problems) != 0 {
		t.Errorf("老模板行解析异常：%+v", rows[0])
	}
}

func containsProblem(r userImportRow, sub string) bool {
	for _, p := range r.Problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}
