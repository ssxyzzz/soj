package judge

// 交互题判题（v0.9.2）：interactor 与选手程序在同一个 go-judge 请求里双进程并行，
// 用命名管道互连（go-judge 语义：{name,max} 为生产端写管道，{name} 为消费端读管道）：
//
//	interactor: stdin=消费 p-user（读选手输出）、stdout=生产 p-inter（写给选手）、
//	            stderr=收集反馈；测试输入 copyIn 进工作目录，由 argv[1] 引用
//	选手程序:   stdin=消费 p-inter、stdout=生产 p-user、stderr=收集
//
// 判分约定（v1 明确子集）：interactor 退出码 0 = 该测试点通过、非 0 = 不通过，
// stderr 末行作为反馈进测试点明细；选手进程异常按其自身状态（TLE/MLE/RE）计。
// 逐点等分；交互题暂不启用子任务分组。

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

// interactorRunner 交互器判定器：懒编译 + fileId 缓存（同 checkerRunner 模式）。
type interactorRunner struct {
	j *Judger
	p *repo.Problem
}

func (j *Judger) interactorRunnerFor(p *repo.Problem) *interactorRunner {
	if p == nil || p.InteractorFile == "" {
		return nil
	}
	return &interactorRunner{j: j, p: p}
}

// ensureCompiled 编译交互器并缓存产物（题目元数据记录 fileId，失效自动重编）。
func (ir *interactorRunner) ensureCompiled(ctx context.Context) (string, error) {
	if ir.p.InteractorFileID != "" {
		return ir.p.InteractorFileID, nil
	}
	src, err := testdata.ReadAll(ir.j.Provider, ir.p.ID, ir.p.InteractorFile)
	if err != nil {
		return "", fmt.Errorf("读取交互器失败: %w", err)
	}
	l := lang.Languages["cpp"]
	if l == nil {
		return "", errors.New("无法编译交互器：C++ 语言未配置")
	}
	fileID, status, info, err := ir.j.compile(ctx, l, string(src))
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", fmt.Errorf("交互器编译失败: %s", info)
	}
	if err := ir.j.Repo.UpdateProblemMeta(ctx, ir.p.ID, bson.M{"interactorFileId": fileID}); err == nil {
		ir.p.InteractorFileID = fileID
	}
	return fileID, nil
}

// evaluateInteractive 交互题主判：逐测试点双进程管道执行。
func (j *Judger) evaluateInteractive(ctx context.Context, sub *repo.Submission, l *lang.Language, fileID string, p *repo.Problem) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	ir := j.interactorRunnerFor(p)
	if ir == nil {
		return repo.StatusSystemError, 0, 0, 0, "交互题缺少交互器（interactor.cpp），请联系老师补配", nil
	}
	irID, err := ir.ensureCompiled(ctx)
	if err != nil {
		return repo.StatusSystemError, 0, 0, 0, err.Error(), nil
	}

	n := len(p.Cases)
	passed := 0
	sumTime, sumMem := 0, 0
	failStatus, failInfo := "", ""
	cpu := int64(p.TimeLimitMS) * int64(time.Millisecond)
	mem := int64(p.MemoryLimitKB) << 10

	for i, tc := range p.Cases {
		if cancelReq, lost := j.checkStop(ctx, sub); lost {
			return statusLeaseLost, 0, 0, 0, "", nil
		} else if cancelReq {
			return repo.StatusCancelled, sumTime, sumMem, 0, "已取消评测", detail
		}
		input, err := testdata.ReadAll(j.Provider, p.ID, tc.InName)
		if err != nil {
			return repo.StatusSystemError, sumTime, sumMem, partialScore(passed, n), "读取输入文件失败: " + err.Error(), detail
		}

		// 双进程直连（pipeMapping）：go-judge 的 stdin 不吃命名管道消费端，必须走映射表：
		//   cmd0 interactor  stdin←选手stdout  stdout→选手stdin  stderr 收集
		//   cmd1 选手        对称；Files 槽位传 nil 表示由 pipeMapping 接管
		interCmd := sandbox.Cmd{
			Args: []string{"./main", "input.txt"}, // 交互器编译产物以 copyIn 名 "main" 拷入工作目录
			Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
			Files: []sandbox.File{
				sandbox.Null(),                    // stdin：pipeMapping 接入
				sandbox.Null(),                    // stdout：pipeMapping 接出
				sandbox.Pipe("stderr", maxStderr), // 反馈收集（标准名，Result.Stderr 读取）
			},
			CPULimit:    cpu,
			ClockLimit:  cpu * 3,
			MemoryLimit: mem,
			ProcLimit:   64,
			CopyIn: map[string]sandbox.CopyFile{
				"main":      {FileID: irID},
				"input.txt": {Content: string(input)},
			},
			CopyOut: []string{"stderr"},
		}
		userCmd := sandbox.Cmd{
			Args: l.RunArgs,
			Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
			Files: []sandbox.File{
				sandbox.Null(), // stdin：pipeMapping 接入（交互器输出）
				sandbox.Null(), // stdout：pipeMapping 接出（选手输出）
				sandbox.Pipe("stderr", maxStderr),
			},
			CPULimit:    cpu,
			ClockLimit:  cpu * 3,
			MemoryLimit: mem,
			ProcLimit:   64,
			CopyOut:     []string{"stderr"},
		}
		if l.NeedCompile {
			userCmd.CopyIn = map[string]sandbox.CopyFile{"main": {FileID: fileID}}
		} else {
			userCmd.CopyIn = map[string]sandbox.CopyFile{l.SourceFile: {Content: sub.Code}}
		}

		pipes := []sandbox.PipeMap{
			{In: sandbox.PipeIndex{Index: 1, Fd: 1}, Out: sandbox.PipeIndex{Index: 0, Fd: 0}}, // 选手 stdout → 交互器 stdin
			{In: sandbox.PipeIndex{Index: 0, Fd: 1}, Out: sandbox.PipeIndex{Index: 1, Fd: 0}}, // 交互器 stdout → 选手 stdin
		}
		results, err := j.Sandbox.RunPiped(ctx, []sandbox.Cmd{interCmd, userCmd}, pipes)
		if err != nil || len(results) != 2 {
			return repo.StatusSystemError, sumTime, sumMem, partialScore(passed, n), "沙箱调用失败: " + err.Error(), detail
		}
		irRes, userRes := &results[0], &results[1]
		t := int(irRes.Time+userRes.Time) / int(time.Millisecond)
		m := int(irRes.Memory+userRes.Memory) / 1024
		sumTime += t
		sumMem += m

		// 交互器判定优先（退出码 0 = 通过）：交互器在最后一轮答复后先行退出，
		// 选手随后再写会收到 SIGPIPE（退出状态 13），那是正常收尾而非选手错误
		if irRes.Status == "Accepted" && irRes.ExitStatus == 0 {
			detail = append(detail, &repo.CaseResult{Index: tc.Index, Status: "AC", TimeMS: t, MemKB: m})
			passed++
			continue
		}
		// 交互器未判过：若是选手侧超限/崩溃，按选手状态计（TLE/MLE/RE 更有诊断价值）
		if userRes.Status == "Time Limit Exceeded" || userRes.Status == "Memory Limit Exceeded" {
			cs, fs, reason := caseVerdict(userRes, "", "", outputComparer{})
			detail = append(detail, &repo.CaseResult{Index: tc.Index, Status: cs, TimeMS: t, MemKB: m, Msg: reason})
			if failStatus == "" {
				failStatus, failInfo = fs, fmt.Sprintf("第 %d 个测试点: %s", i+1, reason)
			}
			continue
		}
		reason := "交互器判定未通过"
		if s := lastLine(irRes.Stderr()); s != "" {
			reason = truncate(s, 256)
		}
		// 诊断增强：两进程状态与 stderr 一并带出（无 stderr 输出时定位用）
		if reason == "交互器判定未通过" {
			reason = fmt.Sprintf("交互器判定未通过（status=%s exit=%d ierr=%q uerr=%q ustatus=%s）",
				irRes.Status, irRes.ExitStatus, truncate(strings.TrimSpace(irRes.Stderr()), 160),
				truncate(strings.TrimSpace(userRes.Stderr()), 120), userRes.Status)
		}
		if irRes.Status != "Accepted" && irRes.Status != "Nonzero Exit Status" && irRes.Status != "Non Zero Exit Status" && irRes.Status != "Signalled" {
			// 交互器自身超限/崩溃：题目侧问题，按系统错误处理
			reason = "交互器异常(" + irRes.Status + "): " + reason
			return repo.StatusSystemError, sumTime, sumMem, partialScore(passed, n),
				fmt.Sprintf("第 %d 个测试点: %s", i+1, reason), detail
		}
		detail = append(detail, &repo.CaseResult{Index: tc.Index, Status: "WA", TimeMS: t, MemKB: m, Msg: reason})
		if failStatus == "" {
			failStatus, failInfo = repo.StatusWrongAnswer, fmt.Sprintf("第 %d 个测试点: %s", i+1, reason)
		}
	}

	if failStatus != "" {
		return failStatus, sumTime, sumMem, partialScore(passed, n), failInfo, detail
	}
	return repo.StatusAccepted, sumTime, sumMem, 100, "", detail
}

// lastLine 取文本最后一个非空行（交互器反馈惯例：末行给结论）。
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n \t\r"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
