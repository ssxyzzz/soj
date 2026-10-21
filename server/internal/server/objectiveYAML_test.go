package server

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseObjectiveYAML_PipesAndRoundTrip(t *testing.T) {
	src := `version: 1
questions:
  - kind: single
    score: 40
    stem: |
      int f(int a, int b) {
        return a | b;
      }
      if (a || b) { return 0; }
    options:
      - "a | b"
      - "a || b"
    answer: [1]
    explanation: |
      位或 a | b；逻辑或 a || b。
  - kind: blank
    score: 60
    stem: |
      答案 ____，代码 ____。
    blanks:
      - accept: ["|x|", "a || b"]
        case_sensitive: true
      - accept: ["42"]
        caseSensitive: true
`
	qs, err := parseObjectiveYAML(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(qs) != 2 {
		t.Fatalf("questions = %d", len(qs))
	}
	if !strings.Contains(qs[0].Stem, "a | b") || !strings.Contains(qs[0].Stem, "a || b") {
		t.Fatalf("块标量里的 |/|| 丢失: %q", qs[0].Stem)
	}
	if len(qs[0].Options) != 2 || qs[0].Options[0] != "a | b" || qs[0].Options[1] != "a || b" {
		t.Fatalf("选项 |/|| 丢失: %#v", qs[0].Options)
	}
	if len(qs[1].Blanks) != 2 || qs[1].Blanks[0].Accept[0] != "|x|" {
		t.Fatalf("填空以 | 开头未保留: %#v", qs[1].Blanks)
	}
	if !qs[1].Blanks[0].CaseSensitive || !qs[1].Blanks[1].CaseSensitive {
		t.Fatalf("大小写开关未从 case_sensitive / caseSensitive 读入: %#v", qs[1].Blanks)
	}

	out, err := yaml.Marshal(objectiveQuestionsDoc{Version: 1, Questions: qs})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := parseObjectiveYAML(string(out))
	if err != nil {
		t.Fatalf("round-trip parse: %v\n%s", err, out)
	}
	if again[0].Options[0] != "a | b" || again[1].Blanks[0].Accept[0] != "|x|" {
		t.Fatalf("YAML 往返丢内容:\n%s", out)
	}
}

func TestParseObjectiveYAML_BareListAndInvalidTotal(t *testing.T) {
	bare := `- kind: judge
  score: 100
  stem: 判断：1+1=2。
  answer: [0]
`
	qs, err := parseObjectiveYAML(bare)
	if err != nil || len(qs) != 1 || qs[0].Score != 100 {
		t.Fatalf("裸列表解析失败: %v %#v", err, qs)
	}
	bad := `questions:
  - kind: single
    score: 30
    stem: 题
    options: [A, B]
    answer: [0]
`
	if _, err := parseObjectiveYAML(bad); err == nil {
		t.Fatal("分值合计不是 100 时应报错")
	}
}
