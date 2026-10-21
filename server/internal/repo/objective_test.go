package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateObjective(t *testing.T) {
	cases := []struct {
		name string
		q    *ObjectiveQuestion
		ok   bool
	}{
		{"单选正常", &ObjectiveQuestion{Kind: ObjSingle, Options: []string{"A", "B"}, Answer: []int{1}}, true},
		{"单选多答案", &ObjectiveQuestion{Kind: ObjSingle, Options: []string{"A", "B"}, Answer: []int{0, 1}}, false},
		{"单选越界", &ObjectiveQuestion{Kind: ObjSingle, Options: []string{"A", "B"}, Answer: []int{2}}, false},
		{"单选选项不足", &ObjectiveQuestion{Kind: ObjSingle, Options: []string{"A"}, Answer: []int{0}}, false},
		{"多选正常", &ObjectiveQuestion{Kind: ObjMultiple, Options: []string{"A", "B", "C"}, Answer: []int{0, 2}}, true},
		{"多选重复索引", &ObjectiveQuestion{Kind: ObjMultiple, Options: []string{"A", "B"}, Answer: []int{0, 0}}, false},
		{"判断默认选项", &ObjectiveQuestion{Kind: ObjJudge, Answer: []int{0}}, true},
		{"判断自带对错", &ObjectiveQuestion{Kind: ObjJudge, Options: []string{"对", "错"}, Answer: []int{1}}, true},
		{"判断选项错误", &ObjectiveQuestion{Kind: ObjJudge, Options: []string{"真", "假"}, Answer: []int{0}}, false},
		{"填空正常", &ObjectiveQuestion{Kind: ObjBlank, Blanks: []ObjectiveBlank{{Accept: []string{"42"}}}}, true},
		{"填空缺答案", &ObjectiveQuestion{Kind: ObjBlank, Blanks: []ObjectiveBlank{{}}}, false},
		{"填空无空位", &ObjectiveQuestion{Kind: ObjBlank}, false},
		{"未知类型", &ObjectiveQuestion{Kind: "essay"}, false},
		{"空定义", nil, false},
	}
	for _, c := range cases {
		if err := ValidateObjective(c.q); (err == nil) != c.ok {
			t.Errorf("%s: err=%v want_ok=%v", c.name, err, c.ok)
		}
	}
}

func TestGradeObjective(t *testing.T) {
	single := &ObjectiveQuestion{Kind: ObjSingle, Options: []string{"A", "B", "C"}, Answer: []int{1}}
	if st, sc := GradeObjective(single, []int{1}, nil); st != StatusAccepted || sc != 100 {
		t.Errorf("单选答对: %s/%d", st, sc)
	}
	if st, sc := GradeObjective(single, []int{2}, nil); st != StatusWrongAnswer || sc != 0 {
		t.Errorf("单选答错: %s/%d", st, sc)
	}
	multi := &ObjectiveQuestion{Kind: ObjMultiple, Options: []string{"A", "B", "C"}, Answer: []int{0, 2}}
	if st, _ := GradeObjective(multi, []int{2, 0}, nil); st != StatusAccepted { // 顺序无关
		t.Errorf("多选乱序全中应 AC: %s", st)
	}
	if st, _ := GradeObjective(multi, []int{0}, nil); st != StatusWrongAnswer {
		t.Errorf("多选漏选应 WA: %s", st)
	}
	blank := &ObjectiveQuestion{Kind: ObjBlank, Blanks: []ObjectiveBlank{
		{Accept: []string{"42", "四十二"}},
		{Accept: []string{"O(n)"}, CaseSensitive: true},
	}}
	if st, sc := GradeObjective(blank, nil, []string{" 42 ", "O(n)"}); st != StatusAccepted || sc != 100 {
		t.Errorf("填空全对（trim+多答案）: %s/%d", st, sc)
	}
	if st, _ := GradeObjective(blank, nil, []string{"42", "O(N)"}); st != StatusWrongAnswer {
		t.Errorf("大小写敏感空应 WA: %s", st)
	}
	if st, _ := GradeObjective(blank, nil, []string{"43", "O(n)"}); st != StatusWrongAnswer {
		t.Errorf("填空答错应 WA: %s", st)
	}
}

func TestCountBlanksInStatement(t *testing.T) {
	cases := []struct {
		md   string
		want int
	}{
		{"无占位", 0},
		{"答案是 ____。", 1},
		{"第一空 ____ 第二空 ______ 第三空 ___", 2}, // 3 个下划线不算
		{"连续很长 ____________ 也只算一个", 1},
	}
	for _, c := range cases {
		if got := CountBlanksInStatement(c.md); got != c.want {
			t.Errorf("CountBlanksInStatement(%q) = %d, want %d", c.md, got, c.want)
		}
	}
}

func mkDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// 无感嗅探三路互斥 + DOMjudge/未知结构的明确报错。
func TestDetectAndNormalize(t *testing.T) {
	// SOJ：根 problem.yaml
	dir := mkDir(t, map[string]string{"problem.yaml": "title: t\n", "cases/1/in": "x"})
	src, err := DetectAndNormalize(dir)
	if err != nil || src != PkgSourceSOJ {
		t.Fatalf("SOJ 包: src=%s err=%v", src, err)
	}

	// Hydro：testdata/ 子目录 → flatten + 题面上翻 + config 到根
	dir = mkDir(t, map[string]string{
		"problem.yaml":         "title: hydro题\n",
		"testdata/config.yaml": "time: 1s\nmemory: 256m\n",
		"testdata/1.in":        "x",
		"testdata/1.out":       "y",
		"statement/zh.md":      "题面",
	})
	src, err = DetectAndNormalize(dir)
	if err != nil || src != PkgSourceHydro {
		t.Fatalf("Hydro 包: src=%s err=%v", src, err)
	}
	for _, want := range []string{"config.yaml", "1.in", "1.out", "statement.md"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("Hydro flatten 后缺 %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "testdata")); !os.IsNotExist(err) {
		t.Error("testdata/ 应被移除")
	}

	// Hydro interactive（v0.9.2 放行）：归一后 type 保留在 config.yaml，交给导入管道
	dir = mkDir(t, map[string]string{"testdata/config.yaml": "type: interactive\n"})
	if src, err := DetectAndNormalize(dir); err != nil || src != PkgSourceHydro {
		t.Errorf("Hydro 交互题应放行: src=%v err=%v", src, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Error("交互题 config.yaml 应上翻到根")
	}

	// 裸数据：只有 .in/.out
	dir = mkDir(t, map[string]string{"1.in": "x", "1.out": "y"})
	src, err = DetectAndNormalize(dir)
	if err != nil || src != PkgSourceBare {
		t.Fatalf("裸数据: src=%s err=%v", src, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "problem.yaml")); err != nil {
		t.Error("裸数据应生成最小 problem.yaml")
	}

	// DOMjudge：明确拒绝
	dir = mkDir(t, map[string]string{"problem.yaml": "name: x\n", "data/testdata/1.in": "x"})
	if _, err := DetectAndNormalize(dir); err == nil {
		t.Error("DOMjudge 包应报暂不支持")
	}

	// 无法识别
	dir = mkDir(t, map[string]string{"readme.txt": "hello"})
	if _, err := DetectAndNormalize(dir); err == nil {
		t.Error("无法识别的结构应报错")
	}
}

// 客观题导入：question 定义 + 填空占位校验 + 无 cases 入库路径（不触库的部分由端到端覆盖）。
func TestParseObjectiveYAML(t *testing.T) {
	dir := mkDir(t, map[string]string{
		"problem.yaml": `title: 单选题
type: objective
question:
  kind: single
  options: [甲, 乙, 丙]
  answer: [1]
  explanation: 选乙
`,
		"statement.md": "下列哪个……",
	})
	raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var meta problemYAML
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if meta.Type != TypeObjective || meta.Question == nil || meta.Question.Kind != ObjSingle {
		t.Fatalf("objective 解析结果不对: %+v", meta)
	}
	if err := ValidateObjective(NormalizeObjective(meta.Question)); err != nil {
		t.Fatalf("校验失败: %v", err)
	}
}

// 附带文件收集（v0.9 验收修复）：平铺测试数据归一后不双份入档；
// __MACOSX/.DS_Store/._* 打包杂物一律不入档。
func TestCollectExtraFilesFiltersJunk(t *testing.T) {
	dir := mkDir(t, map[string]string{
		"atbash1.in":            "x",
		"atbash1.out":           "y",
		"atbash10.in":           "x",
		"atbash10.out":          "y",
		"gen.py":                "print(1)",
		"std.cpp":               "int main(){}",
		"__MACOSX/._atbash1.in": "junk",
		".DS_Store":             "junk",
		"._config.yaml":         "junk",
		"problem.yaml":          "title: t\n",
		"config.yaml":           "filename: atbash\n",
	})
	if err := normalizeFlatCases(dir); err != nil {
		t.Fatalf("平铺归一失败: %v", err)
	}
	rels, err := collectExtraFiles(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 || rels[0] != "gen.py" || rels[1] != "std.cpp" {
		t.Errorf("extra = %v, want [gen.py std.cpp]", rels)
	}
}

// 导出主形态（v0.9 验收调整）：时限/内存带单位字符串、文件 IO 用 filename 直白字段。
func TestRenderProblemYAMLStyle(t *testing.T) {
	im := &Importer{}
	p := &Problem{
		Title: "T", TimeLimitMS: 1500, MemoryLimitKB: 262144,
		FileIO: true, FileIOInput: "atbash.in", FileIOOutput: "atbash.out",
		Cases: []CaseMeta{{Index: 1, Sample: true}},
	}
	yb := im.RenderProblemYAML(nil, p)
	got := string(yb)
	for _, want := range []string{"time: 1.5s", "memory: 256MB", "filename: atbash", "samples:"} {
		if !strings.Contains(got, want) {
			t.Errorf("导出缺 %q:\n%s", want, got)
		}
	}
	for _, stale := range []string{"timeLimitMs", "memoryLimitKb", "file_io_input"} {
		if strings.Contains(got, stale) {
			t.Errorf("导出不应再含旧字段 %q", stale)
		}
	}
}
