package judge

import (
	"strings"
	"testing"

	"soj/internal/repo"
	"soj/internal/sandbox"
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
		// 2026-11-01 用户反馈：freopen 注释掉是普通答案错误，不得报「检测到文件操作」
		{"// freopen(\"problem.in\", \"r\", stdin);", ""},
		{"/* fopen(\"a.in\", \"r\"); */", ""},
		{"// 注释里提到 freopen 也不算命中", ""},
		{"string s = \"freopen in comment\"; freopen(\"a.in\",\"r\",stdin);", "freopen"},
	}
	for _, c := range cases {
		if got := fileIOKeyword(c.code); got != c.want {
			t.Errorf("fileIOKeyword(%q) = %q, want %q", c.code, got, c.want)
		}
	}
}

// TestWAFileIOHint v0.20：WA 提示只在「本题不需要文件 IO 且源码确有文件操作」时追加，
// 普通输出不一致（无文件操作源码）不得出现文件操作文案。
func TestWAFileIOHint(t *testing.T) {
	plain := "int main(){ std::cin >> a >> b; std::cout << a + b; }"
	freopenCode := `int main(){ freopen("problem.in","r",stdin); freopen("problem.out","w",stdout); }`
	cases := []struct {
		name    string
		code    string
		fio     fileIOParams
		wantHas bool
	}{
		{"无文件操作不追加", plain, fileIOParams{enabled: false}, false},
		{"检出 freopen 追加", freopenCode, fileIOParams{enabled: false}, true},
		{"文件 IO 题不追加", freopenCode, fileIOParams{enabled: true, inName: "problem.in", outName: "problem.out"}, false},
	}
	for _, c := range cases {
		got := waFileIOHint(c.code, c.fio)
		if c.wantHas && got == "" {
			t.Errorf("%s: 应追加提示，得到空", c.name)
		}
		if !c.wantHas && got != "" {
			t.Errorf("%s: 不应追加提示，得到 %q", c.name, got)
		}
	}
}

// TestCaseVerdictSignalHint RE 明细可读性：Signalled（被信号终止）按信号编号翻成成因
// 提示（如 11 = SIGSEGV 段错误，含爆栈改非递归的指引）；自行非零退出与被信号终止
// 文案可区分；stderr 仅在非空时拼接（不再出现裸编号与尾逗号）。
func TestCaseVerdictSignalHint(t *testing.T) {
	cmp := outputComparer{}
	cases := []struct {
		name      string
		res       sandbox.Result
		wantParts []string // 明细必须依次包含的片段
		wantGone  string   // 明细不得出现的旧文案片段
	}{
		{
			name:      "信号11翻段错误含爆栈指引",
			res:       sandbox.Result{Status: "Signalled", ExitStatus: 11},
			wantParts: []string{"运行时错误：段错误", "SIGSEGV", "递归过深爆栈"},
			wantGone:  "退出状态",
		},
		{
			name:      "信号8翻算术错误",
			res:       sandbox.Result{Status: "Signalled", ExitStatus: 8},
			wantParts: []string{"SIGFPE", "除以零"},
			wantGone:  "退出状态",
		},
		{
			name:      "未收录信号回退编号",
			res:       sandbox.Result{Status: "Signalled", ExitStatus: 99},
			wantParts: []string{"异常信号 99"},
			wantGone:  "退出状态",
		},
		{
			name:      "非零退出码区分于信号",
			res:       sandbox.Result{Status: "Nonzero Exit Status", ExitStatus: 1},
			wantParts: []string{"程序以非零退出码 1 结束"},
			wantGone:  "信号",
		},
		{
			name:      "旧拼写 Non Zero 兼容",
			res:       sandbox.Result{Status: "Non Zero Exit Status", ExitStatus: -1},
			wantParts: []string{"程序以非零退出码 -1 结束"},
			wantGone:  "",
		},
		{
			name:      "stderr非空才拼接且无尾逗号",
			res:       sandbox.Result{Status: "Signalled", ExitStatus: 11, Files: map[string]string{"stderr": "panic: runtime error"}},
			wantParts: []string{"段错误", "；stderr: ", "panic: runtime error"},
			wantGone:  ", \n",
		},
	}
	for _, c := range cases {
		_, _, got := caseVerdict(&c.res, "", "", cmp)
		for _, part := range c.wantParts {
			if !strings.Contains(got, part) {
				t.Errorf("%s: 明细 %q 缺少片段 %q", c.name, got, part)
			}
		}
		if c.wantGone != "" && strings.Contains(got, c.wantGone) {
			t.Errorf("%s: 明细 %q 不应出现 %q", c.name, got, c.wantGone)
		}
	}
	// StatusRE 契约：以上全部按 RE 收口
	if _, st, _ := caseVerdict(&sandbox.Result{Status: "Signalled", ExitStatus: 11}, "", "", cmp); st != repo.StatusRE {
		t.Errorf("Signalled 应收口为 %s，得到 %s", repo.StatusRE, st)
	}
}
