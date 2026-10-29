package judge

// checker 自定义校验器（v0.5.2-2，自 judge.go 拆出——v0.32 巨型文件治理，纯搬家行为零变化）：
// 懒编译（编译产物 fileId 缓存进题目元数据，缓存失效自动重编一次）+ 三流执行。
// 调用约定与 testlib 惯例一致：checker <输入> <期望> <考生输出>，
// 退出码 0 = AC，非 0 = WA（输出/报错作为失败原因）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

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
	if err := cr.j.Repo.UpdateProblemMeta(ctx, cr.p.ID, bson.M{"checkerFileId": fileID}); err == nil {
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
	if sysErr != nil && retry {
		cr.p.CheckerFileID = "" // 产物缓存失效：重编一次再试
		fileID, err = cr.ensureCompiled(ctx)
		if err != nil {
			return false, "", err
		}
		ok, msg, sysErr, _ = cr.tryRun(ctx, fileID, tc, input, ans, stdout)
	}
	return ok, msg, sysErr
}

func (cr *checkerRunner) tryRun(ctx context.Context, fileID string, tc repo.CaseMeta, input, ans, stdout string) (ok bool, msg string, sysErr error, retry bool) {
	in, ansS, out := input, ans, stdout
	cmd := sandbox.Cmd{
		Args: []string{"./main", "input", "answer", "output"}, // 产物约定名 main，相对工作目录
		Files: []sandbox.File{
			sandbox.Pipe("stderr", maxStderr),
			sandbox.Pipe("stdout", maxStderr),
		},
		CopyIn: map[string]sandbox.CopyFile{
			"main":   {FileID: fileID},
			"input":  sandbox.CopyContent(in),
			"answer": sandbox.CopyContent(ansS),
			"output": sandbox.CopyContent(out),
		},
		CPULimit:    int64(cr.p.TimeLimitMS) * int64(time.Millisecond),
		ClockLimit:  int64(cr.p.TimeLimitMS) * int64(time.Millisecond) * 3,
		MemoryLimit: int64(cr.p.MemoryLimitKB) << 10,
		ProcLimit:   64,
	}
	results, err := cr.j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
	if err != nil || len(results) == 0 {
		return false, "", fmt.Errorf("沙箱调用失败: %v", err), false
	}
	res := results[0]
	switch res.Status {
	case "Accepted":
		return true, "", nil, false
	case "File Error":
		return false, "", nil, true // 编译产物缓存失效：交由上层重编一次
	case "Nonzero Exit Status", "Non Zero Exit Status", "Signalled":
		reason := truncate(strings.TrimSpace(strings.TrimSpace(res.Stdout())+" "+strings.TrimSpace(res.Stderr())), 256)
		if reason == "" {
			reason = "校验器判定不通过"
		}
		return false, reason, nil, false
	default:
		return false, "", fmt.Errorf("校验器执行异常: %s", res.Status), false
	}
}
