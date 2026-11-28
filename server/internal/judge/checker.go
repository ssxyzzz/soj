package judge

// checker 自定义校验器（v0.5.2-2，自 judge.go 拆出——v0.32 巨型文件治理，纯搬家行为零变化）：
// 懒编译（编译产物 fileId 缓存进题目元数据，缓存失效自动重编一次）+ 三流执行。
// 调用约定与 Codeforces / 洛谷 / UOJ / Hydro 一致（v0.101 换血）：testlib 写的 checker
// 原样可用，沙箱镜像已内置 testlib.h。
//
//	checker <输入> <考生输出> <答案>     即 registerTestlibCmd 的读序
//
// 退出码：0 = AC；3 = 校验器自身故障（testlib 的 _fail：读不了输入 / 答案文件等），
// 判 System Error 不算考生错；其余非零 = WA，原因取校验器输出（1 = 答案错、
// 2 = 输出格式错——PE 并入 WA，同 Codeforces / 洛谷对用户的表现）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"soj/internal/lang"
	"soj/internal/repo"
	"soj/internal/sandbox"
	"soj/internal/testdata"
)

type checkerRunner struct {
	j *Judger
	p *repo.Problem
}

func (j *Judger) checkerRunnerFor(p *repo.Problem) *checkerRunner {
	if p == nil || p.CheckerFile == "" {
		return nil
	}
	return &checkerRunner{j: j, p: p}
}

// ensureCompiled 编译校验器并缓存产物（题目元数据记录 fileId）。
func (cr *checkerRunner) ensureCompiled(ctx context.Context) (string, error) {
	if cr.p.CheckerFileID != "" {
		return cr.p.CheckerFileID, nil
	}
	src, err := testdata.ReadAll(cr.j.Provider, cr.p.ID, cr.p.CheckerFile)
	if err != nil {
		return "", fmt.Errorf("读取校验器失败: %w", err)
	}
	lang := lang.Languages["cpp"]
	if lang == nil {
		return "", errors.New("无法编译校验器：C++ 语言未配置")
	}
	fileID, status, info, err := cr.j.compile(ctx, lang, string(src))
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", fmt.Errorf("校验器编译失败: %s", info)
	}
	if err := cr.j.Repo.UpdateProblemMeta(ctx, cr.p.ID, repo.ProblemMetaPatch{CheckerFileID: &fileID}); err == nil {
		cr.p.CheckerFileID = fileID
	}
	return fileID, nil
}

// run 执行校验器，返回 (通过, 失败原因, 系统错误)。
// input 由 runOne 透传（快赢批⑤：原 tryRun 重复读一次 runOne 已读过的输入文件）。
func (cr *checkerRunner) run(ctx context.Context, tc repo.CaseMeta, input, ans, stdout string) (ok bool, msg string, sysErr error) {
	fileID, err := cr.ensureCompiled(ctx)
	if err != nil {
		return false, "", err
	}
	ok, msg, sysErr, retry := cr.tryRun(ctx, fileID, tc, input, ans, stdout)
	if sysErr == nil {
		return ok, msg, nil
	}
	// 缓存产物失效兜底（v0.97.9）：结果态 File Error（copyOut 收集失败）或 HTTP 态
	// file not found（copyIn 引用已删/TTL 过期的 fileId，go-judge 不走结果态）
	// 都指向产物丢失 → 清缓存重编一次再试（与 interactor 同口径）
	if retry || sandbox.StaleFileID(sysErr, fileID) {
		cr.p.CheckerFileID = ""
		fileID, err = cr.ensureCompiled(ctx)
		if err != nil {
			return false, "", err
		}
		ok, msg, sysErr, _ = cr.tryRun(ctx, fileID, tc, input, ans, stdout)
	}
	return ok, msg, sysErr
}

// tryRun 执行一次校验器，结果分三路见 checkerResult。
func (cr *checkerRunner) tryRun(ctx context.Context, fileID string, tc repo.CaseMeta, input, ans, stdout string) (ok bool, msg string, sysErr error, retry bool) {
	results, err := cr.j.Sandbox.Run(ctx, []sandbox.Cmd{checkerCmd(cr.p, fileID, input, stdout, ans)})
	if err != nil || len(results) == 0 {
		return false, "", fmt.Errorf("沙箱调用失败: %v", err), false
	}
	ok, msg, sysErr, retry = checkerResult(&results[0])
	return
}

// checkerCmd 拼装校验器的沙箱命令。三个文件按 testlib 系顺序入参：
// input = 测试点输入、output = 考生输出、answer = 标准答案（产物约定名 main，
// 相对工作目录——绝对路径会 go-judge Internal Error）。资源限额取题目自身的时限 / 内存。
func checkerCmd(p *repo.Problem, fileID, input, output, answer string) sandbox.Cmd {
	return sandbox.Cmd{
		Args: []string{"./main", "input", "output", "answer"},
		// files 按位置是 [标准输入, 标准输出, 标准错误]：校验器不读标准输入，但占位必须给出，
		// 否则后面的管道会错位到标准输入上、标准错误无人收集（testlib 把原因写到标准错误）。
		Files: []sandbox.File{
			sandbox.Stdin(""),
			sandbox.Pipe("stdout", maxStderr),
			sandbox.Pipe("stderr", maxStderr),
		},
		CopyIn: map[string]sandbox.CopyFile{
			"main":   {FileID: fileID},
			"input":  sandbox.CopyContent(input),
			"output": sandbox.CopyContent(output),
			"answer": sandbox.CopyContent(answer),
		},
		CPULimit:    int64(p.TimeLimitMS) * int64(time.Millisecond),
		ClockLimit:  int64(p.TimeLimitMS) * int64(time.Millisecond) * 3,
		MemoryLimit: int64(p.MemoryLimitKB) << 10,
		ProcLimit:   64,
	}
}

// testlib 约定的两个特殊退出码；其余非零退出码一律按不通过处理。
const (
	checkerExitFail    = 3 // _fail：校验器自身故障
	checkerExitPartial = 7 // quitp：给出部分分（本平台未支持部分分，按不通过计）
)

// checkerResult 把一次校验器运行的沙箱结果分三路（v0.101 按退出码分流）：
//   - 通过：Accepted（退出码 0）；
//   - 不通过：其余非零退出码，原因取校验器输出；
//   - 系统错误：退出码 3（校验器自身故障）、被信号终止（校验器崩溃）、TLE / MLE 等
//     资源超限——都不是考生的错，由调用方判 System Error；
//   - retry：File Error，编译产物缓存失效，交由上层清缓存重编一次。
func checkerResult(res *sandbox.Result) (ok bool, msg string, sysErr error, retry bool) {
	switch res.Status {
	case "Accepted":
		return true, "", nil, false
	case "File Error":
		return false, "", nil, true
	case "Nonzero Exit Status", "Non Zero Exit Status":
		out := checkerOutput(res)
		switch res.ExitStatus {
		case checkerExitFail:
			if out == "" {
				out = "未说明原因"
			}
			return false, "", fmt.Errorf("校验器故障（退出码 3）: %s", out), false
		case checkerExitPartial:
			if out == "" {
				out = "未说明分值"
			}
			return false, "校验器给出部分分（本平台暂不支持部分分，按不通过计）: " + out, nil, false
		}
		if out == "" {
			out = "校验器判定不通过"
		}
		return false, out, nil, false
	case "Signalled":
		return false, "", fmt.Errorf("校验器崩溃: %s", signalName(res.ExitStatus)), false
	default:
		reason := "校验器执行异常: " + res.Status
		if res.Error != "" {
			reason += "（" + truncate(res.Error, 200) + "）"
		}
		return false, "", errors.New(reason), false
	}
}

// checkerOutput 校验器的 stdout 与 stderr 中非空的部分合并为原因文本（去首尾空白、截断）；
// 都为空返回空串，由调用方给兜底文案。
func checkerOutput(res *sandbox.Result) string {
	parts := make([]string, 0, 2)
	for _, s := range []string{res.Stdout(), res.Stderr()} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return truncate(strings.Join(parts, " "), 256)
}
