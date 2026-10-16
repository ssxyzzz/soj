package judge

import (
	"strings"
	"testing"

	"soj/internal/repo"
)

func TestNormalizeOutput(t *testing.T) {
	got := normalizeOutput("1 2  \r\n3\n\n\n")
	want := "1 2\n3"
	if got != want {
		t.Errorf("normalizeOutput = %q, want %q", got, want)
	}
}

func TestTolerantEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		tol  float64
		want bool
	}{
		{"完全一致", "1.0 2\nabc", "1.0 2\nabc", 1e-4, true},
		{"浮点容差内", "0.5", "0.49999", 1e-4, true},
		{"浮点超容差", "0.5", "0.5002", 1e-4, false},
		{"整数与等值小数", "1", "1.0000", 1e-4, true},
		{"负数容差", "-3.14159", "-3.14160", 1e-4, true},
		{"非数字 token 精确比", "yes 1", "no 1", 1e-4, false},
		{"一侧数字一侧文字", "1", "one", 1e-4, false},
		{"token 数不同", "1 2 3", "1 2", 1e-4, false},
		{"中间空白数量无关", "a    b", "a b", 1e-4, true},
		{"大小写敏感", "Yes", "yes", 1e-4, false},
	}
	for _, tc := range cases {
		normalize := func(s string) string {
			lines := strings.Split(s, "\n")
			for i := range lines {
				lines[i] = strings.TrimRight(lines[i], " \t\r")
			}
			return strings.TrimRight(strings.Join(lines, "\n"), "\n")
		}
		if got := tolerantEqual(normalize(tc.a), normalize(tc.b), tc.tol); got != tc.want {
			t.Errorf("%s: tolerantEqual(%q, %q) = %v, want %v", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestOutputComparerModes(t *testing.T) {
	p := &repo.Problem{} // Compare 为空 = exact
	exact := newOutputComparer(p)
	if exact.equal("1\n", "1\n\n") != true {
		t.Error("exact 应忽略文末换行")
	}
	if exact.equal("1.00001", "1.0") {
		t.Error("exact 不应有浮点容差")
	}
	p.Compare = "tolerant"
	p.FloatTolerance = 1e-4
	tol := newOutputComparer(p)
	if !tol.equal("1.00001", "1.0") {
		t.Error("tolerant 应按容差通过")
	}
	p.FloatTolerance = 0 // 非法值回退默认 1e-4
	tol = newOutputComparer(p)
	if !tol.equal("1.00001", "1.0") {
		t.Error("tolerance 非法值应回退默认容差")
	}
}
