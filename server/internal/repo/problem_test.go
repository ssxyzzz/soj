package repo

import (
	"strings"
	"testing"
)

func TestAppendSamplesMdForType(t *testing.T) {
	samples := []SampleIO{{Input: "42\n"}}
	got := AppendSamplesMdForType("# 猜数\n", samples, TypeInteractive)
	if !strings.Contains(got, "输入：") || !strings.Contains(got, "42") {
		t.Fatalf("交互题样例应展示输入: %s", got)
	}
	if strings.Contains(got, "输出：\n\n```") {
		t.Fatalf("交互题样例不应生成输出代码块: %s", got)
	}
	if !strings.Contains(got, "没有固定输出") {
		t.Fatalf("交互题样例缺少说明: %s", got)
	}
	if again := AppendSamplesMdForType(got, samples, TypeInteractive); again != got {
		t.Fatal("题面已含样例章节时应保持幂等")
	}

	def := AppendSamplesMdForType("# A+B\n", []SampleIO{{Input: "1 2\n", Output: "3\n"}}, TypeDefault)
	if !strings.Contains(def, "输出：") || !strings.Contains(def, "3") {
		t.Fatalf("传统题样例仍应展示输出: %s", def)
	}
}
