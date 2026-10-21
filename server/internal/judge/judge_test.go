package judge

import (
	"strings"
	"testing"

	"soj/internal/repo"
)

func TestWaker(t *testing.T) {
	w := NewWaker(1)
	w.Notify()
	w.Notify() // 容量 1：满即丢弃
	if len(w.C()) != 1 {
		t.Fatalf("容量 1 的通道至多缓存 1 个信号，实际 %d", len(w.C()))
	}
	select {
	case <-w.C():
	default:
		t.Fatal("应有信号可读")
	}
	// nil Waker：Notify 不 panic、C() 返回 nil（select 恒阻塞分支）
	var nilW *Waker
	nilW.Notify()
	if nilW.C() != nil {
		t.Fatal("nil Waker 的 C() 应返回 nil")
	}
	// select 含 nil 通道分支不阻塞其它分支
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	select {
	case <-nilW.C():
		t.Fatal("nil 通道不应就绪")
	case <-ch:
	}
}

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

func TestFileIOKeyword(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{"freopen(\"a.in\", \"r\", stdin);", "freopen"},
		{"FILE *f = fopen(\"a.in\", \"r\");", "fopen"},
		{"ifstream cin(\"a.in\");", "ifstream"},
		{"#include <fstream>", "fstream"},
		{"int main(){ std::cin >> s; }", ""},
		{"// 注释里提到 freopen 也算命中", "freopen"},
	}
	for _, c := range cases {
		if got := fileIOKeyword(c.code); got != c.want {
			t.Errorf("fileIOKeyword(%q) = %q, want %q", c.code, got, c.want)
		}
	}
}
