package judge

// 自测直跑（v0.23）：在线编程 / 挑战页的「自测」——编译 + 单次运行用户自定义输入。
// 与真实判题的分界：不走 Mongo 判题队列、不留提交记录、不占判题 worker；
// 编译与判题共用 compileGate 串行闸与沙箱客户端，运行并发由 server 层信号量约束。

import (
	"context"
	"time"

	"soj/internal/lang"
	"soj/internal/repo"
	"soj/internal/sandbox"
)

// 自测运行的资源上限：输出管道 1MB（超出截断）、展示再截 64KB，
// 时限取题目配置但墙钟至少 3s（给解释型语言启动留余地）。
const (
	selfTestStdoutMax  = 1 << 20
	selfTestStderrMax  = 64 << 10
	selfTestShowMax    = 64 << 10
	selfTestClockFloor = 3 * time.Second
)

// SelfTestResult 自测单次运行结果。
type SelfTestResult struct {
	Status    string // 沙箱状态：Accepted / Time Limit Exceeded / ...
	Stdout    string
	Stderr    string
	TimeMS    int
	MemKB     int
	Truncated bool // stdout 超出展示上限被截断
}

// SelfTestRun 编译并按用户输入运行一次。tlMS/memKB 取题目限制；编译失败时
// Status = Compile Error 并带诊断信息（与判题同口径）。
func (j *Judger) SelfTestRun(ctx context.Context, langName, code, input string, tlMS, memKB int) (*SelfTestResult, error) {
	l := lang.Languages[langName]
	fileID := ""
	if l.NeedCompile {
		var status, info string
		var err error
		fileID, status, info, err = j.compile(ctx, l, code)
		if err != nil {
			return nil, err
		}
		if status != "" {
			return &SelfTestResult{Status: repo.StatusCompileError, Stderr: info}, nil
		}
		defer j.releaseBinary(fileID)
	}
	cpu := int64(tlMS) * int64(time.Millisecond)
	clock := cpu * 3
	if d := time.Duration(clock); d < selfTestClockFloor {
		clock = int64(selfTestClockFloor)
	}
	cmd := sandbox.Cmd{
		Args: l.RunArgs,
		Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Files: []sandbox.File{
			sandbox.Stdin(input),
			sandbox.Pipe("stdout", selfTestStdoutMax),
			sandbox.Pipe("stderr", selfTestStderrMax),
		},
		CPULimit:    cpu,
		ClockLimit:  clock,
		MemoryLimit: int64(memKB) << 10,
		ProcLimit:   64,
		CopyOut:     []string{"stdout", "stderr"},
	}
	if l.NeedCompile {
		cmd.CopyIn = map[string]sandbox.CopyFile{"main": {FileID: fileID}}
	} else {
		cmd.CopyIn = map[string]sandbox.CopyFile{l.SourceFile: sandbox.CopyContent(code)}
	}
	results, err := j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
	if err != nil || len(results) == 0 {
		return nil, err
	}
	r := results[0]
	out := r.Stdout()
	res := &SelfTestResult{
		Status: r.Status,
		Stdout: truncate(out, selfTestShowMax),
		Stderr: truncate(r.Stderr(), selfTestShowMax/4),
		TimeMS: int(r.Time / int64(time.Millisecond)),
		MemKB:  int(r.Memory / 1024),
	}
	res.Truncated = len(out) > len(res.Stdout)
	return res, nil
}
