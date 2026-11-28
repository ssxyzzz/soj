package judge

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"soj/internal/lang"
	"soj/internal/repo"
	"soj/internal/sandbox"
)

// testSandboxAddr 本地 go-judge 地址（SOJ_TEST_SANDBOX_ADDR 可覆盖；compose 把沙箱发布在 5050）。
func testSandboxAddr() string {
	if a := os.Getenv("SOJ_TEST_SANDBOX_ADDR"); a != "" {
		return a
	}
	return "http://127.0.0.1:5050"
}

// TestCheckerSandboxMatrix 真沙箱矩阵：把各色校验器经真实 go-judge 编译并运行（沙箱镜像
// 内置 testlib.h），核对退出码 → 判定分流与原因文本。覆盖单测里假不出来的三件事：
// testlib 的真实退出码、镜像里的 testlib.h 能否编过、校验器标准错误是否被收集。
// 本地无沙箱时跳过；沙箱镜像没带 testlib.h 时 testlib 各例会编译失败（提示重建镜像）。
func TestCheckerSandboxMatrix(t *testing.T) {
	addr := testSandboxAddr()
	j := &Judger{Sandbox: sandbox.New(addr)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if !j.Sandbox.Available(ctx) {
		t.Skipf("本地沙箱 %s 不可达（docker compose up -d sandbox 可启用），跳过", addr)
	}
	cpp := lang.Languages["cpp"]
	compile := func(t *testing.T, src string) string {
		t.Helper()
		fileID, status, info, err := j.compile(ctx, cpp, src)
		if err != nil || status != "" {
			t.Fatalf("校验器编译失败：status %q，info %s，err %v（testlib 各例编译失败说明沙箱镜像没带 testlib.h，需重建）",
				status, truncate(info, 300), err)
		}
		t.Cleanup(func() { j.releaseBinary(fileID) })
		return fileID
	}
	const in, ans = "1 2\n", "3\n"
	run := func(t *testing.T, fileID, stdout string) (bool, string, error, bool) {
		t.Helper()
		cr := &checkerRunner{j: j, p: &repo.Problem{TimeLimitMS: 300, MemoryLimitKB: 262144}}
		return cr.tryRun(ctx, fileID, repo.CaseMeta{Index: 1}, in, ans, stdout)
	}

	const testlibCmp = `#include "testlib.h"
using namespace std;
int main(int argc, char* argv[]) {
    registerTestlibCmd(argc, argv);
    int found = ouf.readInt();
    int expected = ans.readInt();
    if (found != expected) quitf(_wa, "expected %d, found %d", expected, found);
    quitf(_ok, "sum is correct");
}`
	const testlibFail = `#include "testlib.h"
using namespace std;
int main(int argc, char* argv[]) {
    registerTestlibCmd(argc, argv);
    quitf(_fail, "jury answer is broken");
}`
	const testlibAnsShort = `#include "testlib.h"
using namespace std;
int main(int argc, char* argv[]) {
    registerTestlibCmd(argc, argv);
    int a = ans.readInt();
    int b = ans.readInt();
    quitf(_ok, "%d", a + b);
}`
	// quitp 前须把考生输出读完：testlib 收尾时发现输出还有剩余会先判 dirt（退出码 4）
	const testlibPoints = `#include "testlib.h"
using namespace std;
int main(int argc, char* argv[]) {
    registerTestlibCmd(argc, argv);
    int found = ouf.readInt();
    int expected = ans.readInt();
    quitp(found == expected ? 100 : 50, "half credit");
}`

	cmpID := compile(t, testlibCmp)
	cases := []struct {
		name    string
		fileID  string
		src     string
		stdout  string
		wantOK  bool
		wantMsg string // 期望原因包含的片段（不通过时）
		wantErr string // 期望系统错误包含的片段；空 = 不应有系统错误
	}{
		{"testlib 通过", cmpID, "", "3\n", true, "", ""},
		{"testlib 答案错", cmpID, "", "4\n", false, "wrong answer expected 3, found 4", ""},
		{"testlib 格式错并入不通过", cmpID, "", "abc\n", false, "wrong output format", ""},
		{"testlib 校验器故障", "", testlibFail, "3\n", false, "", "校验器故障（退出码 3）"},
		{"答案文件读越界是校验器故障", "", testlibAnsShort, "3\n", false, "", "校验器故障（退出码 3）"},
		{"testlib 部分分暂按不通过", "", testlibPoints, "3\n", false, "校验器给出部分分", ""},
		{"校验器崩溃", "", "int main() { *(volatile int*)0 = 1; return 0; }", "3\n", false, "", "校验器崩溃: 段错误"},
		{"校验器超时", "", "int main() { for (;;) {} }", "3\n", false, "", "校验器执行异常: Time Limit Exceeded"},
		{"自写校验器返回 3", "", "#include <cstdio>\nint main() { fprintf(stderr, \"input file is malformed\"); return 3; }",
			"3\n", false, "", "校验器故障（退出码 3）: input file is malformed"},
		{"自写校验器只写标准错误", "", "#include <cstdio>\nint main() { fprintf(stderr, \"custom reject\"); return 1; }",
			"3\n", false, "custom reject", ""},
		{"自写校验器无输出非零", "", "int main() { return 2; }", "3\n", false, "校验器判定不通过", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fileID := tc.fileID
			if fileID == "" {
				fileID = compile(t, tc.src)
			}
			ok, msg, sysErr, retry := run(t, fileID, tc.stdout)
			if ok != tc.wantOK || retry {
				t.Fatalf("结果 = (ok %v, msg %q, err %v, retry %v)，want ok %v", ok, msg, sysErr, retry, tc.wantOK)
			}
			if tc.wantErr == "" {
				if sysErr != nil || !strings.Contains(msg, tc.wantMsg) {
					t.Fatalf("应不通过且无系统错误，实际 msg %q，err %v", msg, sysErr)
				}
				return
			}
			if sysErr == nil || !strings.Contains(sysErr.Error(), tc.wantErr) {
				t.Fatalf("应为系统错误含 %q，实际 msg %q，err %v", tc.wantErr, msg, sysErr)
			}
		})
	}
}
