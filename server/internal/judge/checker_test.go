package judge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"soj/internal/repo"
	"soj/internal/sandbox"
)

// runResult 构造一次校验器运行的沙箱结果；files 按 go-judge 线上格式 base64 编码。
func runResult(status string, exit int, stdout, stderr string) sandbox.Result {
	res := sandbox.Result{Status: status, ExitStatus: exit}
	if stdout != "" || stderr != "" {
		res.Files = map[string]string{}
		if stdout != "" {
			res.Files["stdout"] = base64.StdEncoding.EncodeToString([]byte(stdout))
		}
		if stderr != "" {
			res.Files["stderr"] = base64.StdEncoding.EncodeToString([]byte(stderr))
		}
	}
	return res
}

// 校验器命令契约：三个文件按 testlib 系顺序入参（输入、考生输出、答案）。
func TestCheckerCmdOrder(t *testing.T) {
	p := &repo.Problem{TimeLimitMS: 1000, MemoryLimitKB: 256}
	cmd := checkerCmd(p, "fid-1", "3 4\n", "7\n", "SEVEN\n")
	if got, want := strings.Join(cmd.Args, " "), "./main input output answer"; got != want {
		t.Fatalf("Args = %q, want %q", got, want)
	}
	for name, want := range map[string]string{"input": "3 4\n", "output": "7\n", "answer": "SEVEN\n"} {
		cf, ok := cmd.CopyIn[name]
		if !ok || cf.Content == nil || *cf.Content != want {
			t.Errorf("CopyIn[%q] = %+v, want 内容 %q", name, cf, want)
		}
	}
	if got := cmd.CopyIn["main"].FileID; got != "fid-1" {
		t.Errorf("CopyIn[main].FileID = %q, want fid-1", got)
	}
	// 资源限额取题目自身：时限 1000ms、墙钟 3 倍、内存 256KB
	if cmd.CPULimit != 1_000_000_000 || cmd.ClockLimit != 3_000_000_000 || cmd.MemoryLimit != 256<<10 {
		t.Errorf("限额 = (cpu %d, clock %d, mem %d), want (1e9, 3e9, %d)",
			cmd.CPULimit, cmd.ClockLimit, cmd.MemoryLimit, 256<<10)
	}
	// 标准流按位置对号：[标准输入, 标准输出, 标准错误]——错位会让标准错误无人收集
	if len(cmd.Files) != 3 || cmd.Files[0].Content == nil ||
		cmd.Files[1].Name != "stdout" || cmd.Files[2].Name != "stderr" {
		t.Errorf("Files = %+v, want [stdin 占位, stdout 管道, stderr 管道]", cmd.Files)
	}
}

func TestCheckerResult(t *testing.T) {
	truncated := strings.Repeat("y", 400)
	cases := []struct {
		name      string
		res       sandbox.Result
		wantOK    bool
		wantMsg   string
		wantErr   string // 期望包含的报错片段；空 = 不应有系统错误
		wantRetry bool
	}{
		{"退出码 0 通过", runResult("Accepted", 0, "", ""), true, "", "", false},
		{"退出码 1 答案错：原因取校验器输出",
			runResult("Nonzero Exit Status", 1, "", "wrong answer 1st numbers differ - expected: '5', found: '4'\n"),
			false, "wrong answer 1st numbers differ - expected: '5', found: '4'", "", false},
		{"退出码 2 格式错并入不通过",
			runResult("Nonzero Exit Status", 2, "", "wrong output format Expected integer, but \"abc\" found"),
			false, "wrong output format Expected integer, but \"abc\" found", "", false},
		{"stdout 与 stderr 合并",
			runResult("Nonzero Exit Status", 1, "out ", " err\n"),
			false, "out err", "", false},
		{"不通过且无输出用兜底文案",
			runResult("Nonzero Exit Status", 1, "", "  \n"),
			false, "校验器判定不通过", "", false},
		{"退出码 3 校验器故障是系统错误",
			runResult("Nonzero Exit Status", 3, "", "fail ans: unexpected end of file - int expected"),
			false, "", "校验器故障（退出码 3）: fail ans: unexpected end of file - int expected", false},
		{"退出码 3 无输出",
			runResult("Nonzero Exit Status", 3, "", ""),
			false, "", "校验器故障（退出码 3）: 未说明原因", false},
		{"退出码 7 部分分暂按不通过",
			runResult("Nonzero Exit Status", 7, "", "50 half"),
			false, "校验器给出部分分（本平台暂不支持部分分，按不通过计）: 50 half", "", false},
		{"退出码 4 不通过",
			runResult("Nonzero Exit Status", 4, "", "extra information in the output file"),
			false, "extra information in the output file", "", false},
		{"大退出码不通过",
			runResult("Nonzero Exit Status", 134, "", "Aborted"),
			false, "Aborted", "", false},
		{"状态另一种拼写同样分流",
			runResult("Non Zero Exit Status", 3, "", "fail x"),
			false, "", "校验器故障（退出码 3）: fail x", false},
		{"信号终止是校验器崩溃",
			sandbox.Result{Status: "Signalled", ExitStatus: 11},
			false, "", "校验器崩溃: 段错误", false},
		{"未收录信号",
			sandbox.Result{Status: "Signalled", ExitStatus: 40},
			false, "", "校验器崩溃: 异常信号 40", false},
		{"超时是系统错误",
			sandbox.Result{Status: "Time Limit Exceeded"},
			false, "", "校验器执行异常: Time Limit Exceeded", false},
		{"异常状态带沙箱错误明细",
			sandbox.Result{Status: "Memory Limit Exceeded", Error: "rss exceed"},
			false, "", "校验器执行异常: Memory Limit Exceeded（rss exceed）", false},
		{"File Error 交上层重编",
			sandbox.Result{Status: "File Error"},
			false, "", "", true},
		{"超长输出截断",
			runResult("Nonzero Exit Status", 1, truncated, ""),
			false, strings.Repeat("y", 256) + "\n...(已截断)", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, msg, sysErr, retry := checkerResult(&tc.res)
			if ok != tc.wantOK || msg != tc.wantMsg || retry != tc.wantRetry {
				t.Fatalf("checkerResult = (%v, %q, err, %v), want (%v, %q, err, %v)",
					ok, msg, retry, tc.wantOK, tc.wantMsg, tc.wantRetry)
			}
			if tc.wantErr == "" {
				if sysErr != nil {
					t.Fatalf("不应有系统错误，实际 %v", sysErr)
				}
				return
			}
			if sysErr == nil || !strings.Contains(sysErr.Error(), tc.wantErr) {
				t.Fatalf("系统错误 = %v, want 含 %q", sysErr, tc.wantErr)
			}
		})
	}
}

// 校验器真实拿到的命令行：借 httptest 假扮 go-judge，核对判题侧发出的请求体——
// 位置 2 是考生输出、位置 3 是答案（与 testlib 的 registerTestlibCmd 读序一致）。
func TestCheckerTryRunWire(t *testing.T) {
	type wireCopy struct {
		Content string `json:"content"`
		FileID  string `json:"fileId"`
	}
	type wireFile struct {
		Content *string `json:"content"`
		Name    string  `json:"name"`
	}
	var sent struct {
		Cmd []struct {
			Args   []string            `json:"args"`
			Files  []*wireFile         `json:"files"`
			CopyIn map[string]wireCopy `json:"copyIn"`
		} `json:"cmd"`
	}
	reply := `[{"status":"Accepted"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()

	cr := &checkerRunner{
		j: &Judger{Sandbox: sandbox.New(srv.URL)},
		p: &repo.Problem{TimeLimitMS: 1000, MemoryLimitKB: 256},
	}
	run := func() (bool, string, error, bool) {
		sent.Cmd = nil
		return cr.tryRun(context.Background(), "fid-9", repo.CaseMeta{Index: 2}, "1 2\n", "3\n", "THREE\n")
	}

	ok, msg, sysErr, retry := run()
	if !ok || msg != "" || sysErr != nil || retry {
		t.Fatalf("Accepted 应通过，实际 (%v, %q, %v, %v)", ok, msg, sysErr, retry)
	}
	if len(sent.Cmd) != 1 {
		t.Fatalf("应恰好发送一条命令，实际 %d", len(sent.Cmd))
	}
	if got, want := strings.Join(sent.Cmd[0].Args, " "), "./main input output answer"; got != want {
		t.Fatalf("线上 Args = %q, want %q", got, want)
	}
	for name, want := range map[string]string{"input": "1 2\n", "output": "THREE\n", "answer": "3\n"} {
		if got := sent.Cmd[0].CopyIn[name].Content; got != want {
			t.Errorf("线上 CopyIn[%q] 内容 = %q, want %q", name, got, want)
		}
	}
	if got := sent.Cmd[0].CopyIn["main"].FileID; got != "fid-9" {
		t.Errorf("线上 CopyIn[main].FileID = %q, want fid-9", got)
	}
	// go-judge 按位置解释 files：[0] 标准输入占位、[1] stdout 收集、[2] stderr 收集
	files := sent.Cmd[0].Files
	if len(files) != 3 || files[0] == nil || files[0].Content == nil ||
		files[1] == nil || files[1].Name != "stdout" || files[2] == nil || files[2].Name != "stderr" {
		t.Errorf("线上 files = %+v, want [stdin 占位, stdout, stderr]", files)
	}

	// 校验器自身故障（退出码 3）走系统错误通道，不算考生错
	reply = `[{"status":"Nonzero Exit Status","exitStatus":3,"files":{"stderr":"` +
		base64.StdEncoding.EncodeToString([]byte("fail broken answer")) + `"}}]`
	ok, msg, sysErr, retry = run()
	if ok || msg != "" || retry || sysErr == nil || !strings.Contains(sysErr.Error(), "校验器故障（退出码 3）: fail broken answer") {
		t.Fatalf("退出码 3 应为系统错误，实际 (%v, %q, %v, %v)", ok, msg, sysErr, retry)
	}
}
