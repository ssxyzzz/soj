// Package judge 实现判题流程：编译 -> 逐测试点运行 -> 比较输出 -> 回写结果。
//
// 与沙箱的交互分两步：
//  1. 编译请求：源码经 stdin 交给编译器，产物通过 copyOutCached 缓存为 fileId；
//  2. 运行请求：每个测试点一次，通过 copyIn(fileId) 复用，测完删除。
//
// 任务来源是循环用 findOneAndUpdate 从数据库原子领取；
// 测试数据通过 testdata.Provider 按需逐点读取。
// 给分：无子任务配置时单点等分（首个失败即终止）；配置子任务时组内全过才得分。
package judge

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"soj/internal/lang"
	"soj/internal/repo"
	"soj/internal/sandbox"
	"soj/internal/testdata"
)

const (
	compileCPULimit    = 30 * int64(time.Second)
	compileMemoryLimit = 1 << 30 // 1GiB
	maxStdout          = 64 << 20
	maxStderr          = 1 << 20
	pollInterval       = time.Second
)

// Judger 组合判题所需依赖，可挂在 Web 进程内或独立进程运行。
type Judger struct {
	Repo     *repo.Repo
	Sandbox  *sandbox.Client
	Provider testdata.Provider
	Name     string // 判题进程标识，写入提交便于排查
}

// Pool 启动 workers 个判题循环，直到 ctx 取消。
func (j *Judger) Pool(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		go j.loop(ctx, i)
	}
}

func (j *Judger) loop(ctx context.Context, worker int) {
	name := j.Name + "-" + strconv.Itoa(worker)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		sub, err := j.Repo.ClaimSubmission(ctx, name)
		if err != nil {
			log.Printf("[judge] 领取任务失败(%s): %v", name, err)
			sleepCtx(ctx, pollInterval)
			continue
		}
		if sub == nil {
			sleepCtx(ctx, pollInterval)
			continue
		}
		j.runOneSafely(ctx, sub)
	}
}

// runOneSafely 单份提交的判题互相隔离：任何 panic 只影响当前提交。
func (j *Judger) runOneSafely(ctx context.Context, sub *repo.Submission) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[judge] 提交 #%d 判题 panic: %v", sub.ID, r)
			_ = j.Repo.FinishSubmission(ctx, sub.ID, repo.StatusSystemError, 0, 0, 0,
				"判题器内部错误", nil)
		}
	}()

	log.Printf("[judge] 开始判题 #%d uid=%d pid=%d lang=%s", sub.ID, sub.UID, sub.ProblemID, sub.Language)
	status, timeMS, memKB, score, info, detail := j.evaluate(ctx, sub)
	if err := j.Repo.FinishSubmission(ctx, sub.ID, status, timeMS, memKB, score, info, detail); err != nil {
		log.Printf("[judge] 回写提交 #%d 结果失败: %v", sub.ID, err)
	}
	log.Printf("[judge] 提交 #%d => %s (%dms / %dKB / %d分)", sub.ID, status, timeMS, memKB, score)
}

// evaluate 完整评测一份提交，按题目配置选择计分方式。
func (j *Judger) evaluate(ctx context.Context, sub *repo.Submission) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	language, ok := lang.Languages[sub.Language]
	if !ok {
		return repo.StatusSystemError, 0, 0, 0, "不支持的语言: " + sub.Language, nil
	}
	problem, err := j.Repo.GetProblem(ctx, sub.ProblemID)
	if err != nil {
		return repo.StatusSystemError, 0, 0, 0, "题目不存在或读取失败", nil
	}
	if len(problem.Cases) == 0 {
		return repo.StatusSystemError, 0, 0, 0, "题目没有测试数据", nil
	}

	var fileID string
	if language.NeedCompile {
		id, compileStatus, compileInfo, err := j.compile(ctx, language, sub.Code)
		if err != nil {
			return repo.StatusSystemError, 0, 0, 0, "沙箱调用失败: " + err.Error(), nil
		}
		if compileStatus != "" {
			return compileStatus, 0, 0, 0, compileInfo, nil
		}
		fileID = id
		defer j.releaseBinary(fileID)
	}

	if len(problem.Subtasks) > 0 {
		return j.evaluateSubtasks(ctx, language, fileID, sub.Code, problem)
	}
	return j.evaluatePoints(ctx, language, fileID, sub.Code, problem)
}

// evalState 评测过程中的累计状态。
type evalState struct {
	maxTimeMS int
	maxMemKB  int
	detail    []*repo.CaseResult
}

func (s *evalState) observe(res *sandbox.Result) (timeMS, memKB int) {
	timeMS = int(res.Time / int64(time.Millisecond))
	memKB = int(res.Memory / 1024)
	if timeMS > s.maxTimeMS {
		s.maxTimeMS = timeMS
	}
	if memKB > s.maxMemKB {
		s.maxMemKB = memKB
	}
	return timeMS, memKB
}

// caseVerdict 把沙箱单点结果归一为 (点位状态缩写, 终止状态, 原因)。
// 终止状态非空表示该点导致整份提交按此状态失败（单点模式用）。
func caseVerdict(res *sandbox.Result, ans string) (caseStatus, finalStatus, reason string) {
	switch res.Status {
	case "Accepted":
		if normalizeOutput(res.Stdout()) != normalizeOutput(ans) {
			return "WA", repo.StatusWrongAnswer, "输出与答案不一致"
		}
		return "AC", "", ""
	case "Time Limit Exceeded":
		return "TLE", repo.StatusTLE, "超出时间限制"
	case "Memory Limit Exceeded":
		return "MLE", repo.StatusMLE, "超出内存限制"
	case "Nonzero Exit Status", "Non Zero Exit Status", "Signalled": // 两种拼写均兼容（实测为 Nonzero）
		return "RE", repo.StatusRE,
			fmt.Sprintf("退出状态 %d, %s", res.ExitStatus, truncate(res.Stderr(), 512))
	case "Output Limit Exceeded":
		return "OLE", repo.StatusRE, "输出超过限制"
	default:
		return "SE", repo.StatusSystemError, "沙箱状态异常: " + res.Status
	}
}

// evaluatePoints 单点等分模式：每点 100/n 分，首个失败即终止（历史行为）。
func (j *Judger) evaluatePoints(ctx context.Context, language *lang.Language, fileID, code string, problem *repo.Problem) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	st := &evalState{}
	for i, tc := range problem.Cases {
		res, err := j.runOne(ctx, language, fileID, code, tc, problem)
		if err != nil {
			return repo.StatusSystemError, st.maxTimeMS, st.maxMemKB, partialScore(i, len(problem.Cases)),
				"沙箱调用失败: " + err.Error(), st.detail
		}
		t, m := st.observe(res)
		caseStatus, finalStatus, reason := "", "", ""
		if res.Status == "Accepted" {
			ans, err := testdata.ReadAll(j.Provider, problem.ID, tc.AnsName)
			if err != nil {
				return repo.StatusSystemError, st.maxTimeMS, st.maxMemKB, partialScore(i, len(problem.Cases)),
					"读取答案文件失败: " + err.Error(), st.detail
			}
			caseStatus, finalStatus, reason = caseVerdict(res, string(ans))
		} else {
			caseStatus, finalStatus, reason = caseVerdict(res, "")
		}
		if caseStatus != "" {
			st.detail = append(st.detail, &repo.CaseResult{Index: tc.Index, Status: caseStatus, TimeMS: t, MemKB: m})
		}
		if finalStatus != "" {
			return finalStatus, st.maxTimeMS, st.maxMemKB, partialScore(i, len(problem.Cases)),
				fmt.Sprintf("第 %d 个测试点: %s", i+1, reason), st.detail
		}
	}
	return repo.StatusAccepted, st.maxTimeMS, st.maxMemKB, 100, "", st.detail
}

// evaluateSubtasks 子任务模式：组内测试点全过才得该组分；组内失败跳过该组剩余点继续下一组。
// 总分 100 判 Accepted；否则以第一个失败点的状态作为整份提交状态（保留部分分）。
func (j *Judger) evaluateSubtasks(ctx context.Context, language *lang.Language, fileID, code string, problem *repo.Problem) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	byIndex := map[int]repo.CaseMeta{}
	for _, tc := range problem.Cases {
		byIndex[tc.Index] = tc
	}
	st := &evalState{}
	total := 0
	firstFailStatus, firstFailReason := "", ""

	for _, subtask := range problem.Subtasks {
		passed := true
		for _, caseIdx := range subtask.Cases {
			tc, ok := byIndex[caseIdx]
			if !ok {
				return repo.StatusSystemError, st.maxTimeMS, st.maxMemKB, total,
					"子任务配置引用了不存在的测试点", st.detail
			}
			res, err := j.runOne(ctx, language, fileID, code, tc, problem)
			if err != nil {
				return repo.StatusSystemError, st.maxTimeMS, st.maxMemKB, total,
					"沙箱调用失败: " + err.Error(), st.detail
			}
			t, m := st.observe(res)
			caseStatus, finalStatus, reason := "", "", ""
			if res.Status == "Accepted" {
				ans, err := testdata.ReadAll(j.Provider, problem.ID, tc.AnsName)
				if err != nil {
					return repo.StatusSystemError, st.maxTimeMS, st.maxMemKB, total,
						"读取答案文件失败: " + err.Error(), st.detail
				}
				caseStatus, finalStatus, reason = caseVerdict(res, string(ans))
			} else {
				caseStatus, finalStatus, reason = caseVerdict(res, "")
			}
			if caseStatus != "" {
				st.detail = append(st.detail, &repo.CaseResult{Index: tc.Index, Status: caseStatus, TimeMS: t, MemKB: m})
			}
			if finalStatus != "" {
				passed = false
				if firstFailStatus == "" {
					firstFailStatus, firstFailReason = finalStatus,
						fmt.Sprintf("子任务(测试点 %d): %s", tc.Index, reason)
				}
				break // 该组剩余测试点跳过
			}
		}
		if passed {
			total += subtask.Score
		}
	}

	if total == 100 {
		return repo.StatusAccepted, st.maxTimeMS, st.maxMemKB, 100, "", st.detail
	}
	if firstFailStatus == "" {
		firstFailStatus, firstFailReason = repo.StatusWrongAnswer, "未通过全部子任务"
	}
	return firstFailStatus, st.maxTimeMS, st.maxMemKB, total, firstFailReason, st.detail
}

// partialScore 已通过的测试点数占比（单点等分模式）。
func partialScore(passed, total int) int {
	return passed * 100 / total
}

// compile 编译并缓存产物；返回的 status 非空表示编译失败。
func (j *Judger) compile(ctx context.Context, l *lang.Language, code string) (fileID, status, info string, err error) {
	cmd := sandbox.Cmd{
		Args: l.CompileArgs,
		Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Files: []sandbox.File{
			sandbox.Stdin(code),            // stdin：源码
			sandbox.Pipe("stdout", 64<<10), // 管道收集
			sandbox.Pipe("stderr", 256<<10),
		},
		CPULimit:      compileCPULimit,
		MemoryLimit:   compileMemoryLimit,
		ProcLimit:     64,
		CopyOut:       []string{"stdout", "stderr"},
		CopyOutCached: []string{"main"},
	}
	results, err := j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
	if err != nil || len(results) == 0 {
		return "", "", "", err
	}
	r := results[0]
	if r.Status != "Accepted" {
		return "", repo.StatusCompileError, "编译进程异常终止: " + r.Status, nil
	}
	if id := r.FileIDs["main"]; id != "" {
		return id, "", "", nil
	}
	return "", repo.StatusCompileError, truncate(r.Stderr(), 8192), nil
}

func (j *Judger) runOne(ctx context.Context, l *lang.Language, fileID, code string, tc repo.CaseMeta, p *repo.Problem) (*sandbox.Result, error) {
	input, err := testdata.ReadAll(j.Provider, p.ID, tc.InName)
	if err != nil {
		return nil, fmt.Errorf("读取输入文件: %w", err)
	}
	cpuLimit := int64(p.TimeLimitMS) * int64(time.Millisecond)
	cmd := sandbox.Cmd{
		Args: l.RunArgs,
		Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Files: []sandbox.File{
			sandbox.Stdin(string(input)),
			sandbox.Pipe("stdout", maxStdout),
			sandbox.Pipe("stderr", maxStderr),
		},
		CPULimit:    cpuLimit,
		ClockLimit:  cpuLimit * 3, // 墙钟上限，防止 sleep 绕过 CPU 限制
		MemoryLimit: int64(p.MemoryLimitKB) << 10,
		ProcLimit:   64,
		CopyOut:     []string{"stdout", "stderr"},
	}
	if l.NeedCompile {
		cmd.CopyIn = map[string]sandbox.CopyFile{"main": {FileID: fileID}}
	} else {
		cmd.CopyIn = map[string]sandbox.CopyFile{l.SourceFile: {Content: code}}
	}
	results, err := j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
	if err != nil || len(results) == 0 {
		return nil, err
	}
	return &results[0], nil
}

func (j *Judger) releaseBinary(fileID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := j.Sandbox.DeleteFile(ctx, fileID); err != nil {
		log.Printf("[judge] 清理编译产物失败: %v", err)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// normalizeOutput 忽略每行行尾空白与文末空行后比较，是 OJ 常见的宽松比较。
func normalizeOutput(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n...(已截断)"
}
