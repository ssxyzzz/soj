package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func mkDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

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
	}
	for _, tc := range cases {
		if err := ValidateObjective(tc.q); (err == nil) != tc.ok {
			t.Errorf("%s: ValidateObjective err=%v want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// Hydro 适配器归一（v0.15）：题面三命名优先级 problem_zh > problem > problem_en、
// title/tag 进 SOJ problem.yaml、testdata/* 上翻 data/、交互题放行、其余类型拒绝。
func TestNormalizeHydroDir(t *testing.T) {
	t.Run("官方导出形态", func(t *testing.T) {
		dir := mkDir(t, map[string]string{
			"problem.yaml":          "owner: 1\ntitle: 玉屏索道(way)\ntag:\n  - 语法基础\n  - 神秘标签Q\n",
			"problem_zh.md":         "## 题目描述\n",
			"testdata/config.yaml":  "type: default\ntime: 1s\nmemory: 256m\nfilename: way\n",
			"testdata/test1.in":     "x",
			"testdata/test1.out":    "y",
			"testdata/gen.py":       "print(1)",
			"testdata/std.cpp":      "int main(){}",
			"导入指南.txt":            "hydro 自带说明",
		})
		if err := NormalizeHydroDir(dir); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"problem.md", "problem.yaml", "data/config.yaml", "data/test1.in", "data/test1.out", "data/gen.py", "data/std.cpp"} {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
				t.Errorf("归一后缺 %s: %v", want, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "testdata")); !os.IsNotExist(err) {
			t.Error("testdata/ 应被移除")
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "problem.md"))
		if !strings.Contains(string(raw), "题目描述") {
			t.Error("题面应为 problem_zh.md 内容")
		}
		var meta problemYAML
		yraw, _ := os.ReadFile(filepath.Join(dir, "problem.yaml"))
		if err := yaml.Unmarshal(yraw, &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Title != "玉屏索道(way)" || len(meta.Tag) != 2 {
			t.Fatalf("title/tag 不符: %+v", meta)
		}
	})

	t.Run("题面命名优先级 zh > en", func(t *testing.T) {
		dir := mkDir(t, map[string]string{
			"problem.yaml":        "title: T\n",
			"problem_en.md":       "EN",
			"testdata/1.in":       "x",
			"testdata/1.out":      "y",
			"testdata/config.yaml": "time: 1s\n",
		})
		if err := NormalizeHydroDir(dir); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "problem.md"))
		if strings.TrimSpace(string(raw)) != "EN" {
			t.Fatalf("无 zh 时应回落 problem_en.md: %q", raw)
		}
	})

	t.Run("缺题面与缺 testdata 报错", func(t *testing.T) {
		dir := mkDir(t, map[string]string{"problem.yaml": "title: T\n", "testdata/1.in": "x", "testdata/1.out": "y"})
		if err := NormalizeHydroDir(dir); err == nil || !strings.Contains(err.Error(), "题面") {
			t.Fatalf("缺题面应报错: %v", err)
		}
		dir2 := mkDir(t, map[string]string{"problem.md": "x"})
		if err := NormalizeHydroDir(dir2); err == nil || !strings.Contains(err.Error(), "testdata") {
			t.Fatalf("缺 testdata 应报错: %v", err)
		}
	})

	t.Run("submit_answer 拒绝", func(t *testing.T) {
		dir := mkDir(t, map[string]string{
			"problem.yaml":         "title: T\ntype: submit_answer\n",
			"problem_zh.md":        "x",
			"testdata/config.yaml": "time: 1s\n",
		})
		if err := NormalizeHydroDir(dir); err == nil || !strings.Contains(err.Error(), "submit_answer") {
			t.Fatalf("submit_answer 应拒绝: %v", err)
		}
	})
}

// 客观题三源合并：problem.yaml 小题定义（无答案）+ answer.yaml 答案 + answer.md 解析。
func TestMergeObjective(t *testing.T) {
	defs := []objQuestionDef{
		{Kind: ObjSingle, Stem: "1+1=?", Options: []string{"1", "2"}, Score: 30},
		{Kind: ObjMultiple, Stem: "选偶数", Options: []string{"1", "2", "4"}, Score: 30},
		{Kind: ObjJudge, Stem: "对吗", Score: 10},
		{Kind: ObjBlank, Stem: "填 ____ 和 ____", Score: 30},
	}
	answers, err := parseAnswerYAML([]byte(`1: 1
2: [1, 2]
3: 0
4:
  - ["42", "042"]
  - accept: ["hello"]
    case_sensitive: true
`))
	if err != nil {
		t.Fatal(err)
	}
	exps := splitAnswerMarkdown("### 第 1 题\n\n选 2。\n\n### 第 2 题\n\n偶数定义。\n")
	qs, err := mergeObjective(defs, answers, exps)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 4 {
		t.Fatalf("want 4 got %d", len(qs))
	}
	if len(qs[0].Answer) != 1 || qs[0].Answer[0] != 1 || qs[0].Explanation != "选 2。" {
		t.Fatalf("单选合并不符: %+v", qs[0])
	}
	if len(qs[1].Answer) != 2 || qs[1].Answer[1] != 2 {
		t.Fatalf("多选合并不符: %+v", qs[1])
	}
	if len(qs[3].Blanks) != 2 || !qs[3].Blanks[1].CaseSensitive {
		t.Fatalf("填空合并不符: %+v", qs[3])
	}

	// answer.yaml 往返：渲染→解析→逐题对齐
	rt, err := parseAnswerYAML(RenderAnswerYAML(qs))
	if err != nil {
		t.Fatal(err)
	}
	qs2, err := mergeObjective(defs, rt, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range qs {
		a, b := qs[i], qs2[i]
		if strings.Join(intsToStrs(a.Answer), ",") != strings.Join(intsToStrs(b.Answer), ",") ||
			a.Blanks[len(a.Blanks):len(a.Blanks)] == nil && len(b.Blanks) != len(a.Blanks) {
			t.Fatalf("answer.yaml 往返第 %d 题不符: %+v vs %+v", i+1, a, b)
		}
		if len(a.Blanks) != len(b.Blanks) {
			t.Fatalf("填空空数不符: %+v vs %+v", a, b)
		}
		for j := range a.Blanks {
			if strings.Join(a.Blanks[j].Accept, "|") != strings.Join(b.Blanks[j].Accept, "|") ||
				a.Blanks[j].CaseSensitive != b.Blanks[j].CaseSensitive {
				t.Fatalf("第 %d 题第 %d 空不符", i+1, j+1)
			}
		}
	}
}

func intsToStrs(ns []int) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, string(rune('0'+n)))
	}
	return out
}

// 权威导出形态（v0.15 新格式）：problem.yaml 只含元数据（tag 单数）、
// data/config.yaml 承载时限/内存/文件 IO/子任务，二者不得互窜。
func TestRenderPackageYAMLStyle(t *testing.T) {
	im := &Importer{}
	p := &Problem{
		Title: "T", TimeLimitMS: 1500, MemoryLimitKB: 262144,
		FileIO: true, FileIOInput: "atbash.in", FileIOOutput: "atbash.out",
		Cases: []CaseMeta{{Index: 1, InName: "data/test1.in", AnsName: "data/test1.out"}},
	}
	yb := string(im.RenderConfigYAML(p))
	for _, want := range []string{"time: 1.5s", "memory: 256MB", "filename: atbash"} {
		if !strings.Contains(yb, want) {
			t.Errorf("config 导出缺 %q:\n%s", want, yb)
		}
	}
	for _, stale := range []string{"title", "tag", "difficulty", "source_type"} {
		if strings.Contains(yb, stale+":") {
			t.Errorf("config.yaml 不应含元数据字段 %q", stale)
		}
	}
}
