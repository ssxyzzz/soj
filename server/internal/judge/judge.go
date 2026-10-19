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
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

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

	cmp := newOutputComparer(problem)
	chk := j.checkerRunnerFor(problem)

	// 文件 IO 模式（v0.5.2-3）：仅比赛/考试「本场启用文件 IO」时生效；
	// 题库/练习/作业恒为 stdio。文件名对取题目配置，缺省 problem.in / problem.out。
	fio := fileIOParams{}
	if sub.ContestID > 0 {
		if c, err := j.Repo.GetRound(ctx, repo.KindContest, sub.ContestID); err == nil {
			fio.enabled = c.FileIO
		}
	} else if sub.ExamID > 0 {
		if e, err := j.Repo.GetRound(ctx, repo.KindExam, sub.ExamID); err == nil {
			fio.enabled = e.FileIO
		}
	}
	if fio.enabled {
		fio.inName, fio.outName = problem.FileIONames()
	}

	if len(problem.Subtasks) > 0 {
		return j.evaluateSubtasks(ctx, language, fileID, sub.Code, problem, cmp, chk, fio)
	}
	return j.evaluatePoints(ctx, language, fileID, sub.Code, problem, cmp, chk, fio)
}

// evalState 评测过程中的累计状态。提交级耗时/内存为各测试点之**和**（总耗时/总内存）。
type evalState struct {
	sumTimeMS int
	sumMemKB  int
	detail    []*repo.CaseResult
}

func (s *evalState) observe(res *sandbox.Result) (timeMS, memKB int) {
	timeMS = int(res.Time / int64(time.Millisecond))
	memKB = int(res.Memory / 1024)
	s.sumTimeMS += timeMS
	s.sumMemKB += memKB
	return timeMS, memKB
}

// outputComparer 输出比对器：按题目比对模式判定选手输出与答案是否等价。
// exact = 现状语义（normalizeOutput 后全文比对，行尾空白/文末换行无关）；
// tolerant = 在 normalize 后按空白分词逐 token 比较，数字 token 按容差比较。
type outputComparer struct {
	tolerant bool
	tol      float64
}

func newOutputComparer(p *repo.Problem) outputComparer {
	c := outputComparer{}
	if p.Compare == repo.CompareTolerant {
		c.tolerant = true
		c.tol = p.FloatTolerance
		if c.tol <= 0 {
			c.tol = repo.DefaultFloatTolerance
		}
	}
	return c
}

func (c outputComparer) equal(out, ans string) bool {
	outN, ansN := normalizeOutput(out), normalizeOutput(ans)
	if !c.tolerant {
		return outN == ansN
	}
	return tolerantEqual(outN, ansN, c.tol)
}

// tolerantEqual 分词比对：按空白切分后逐 token 比较；两侧都是合法数字时按容差判定，
// 否则精确比较。token 数不同直接不等。
func tolerantEqual(a, b string, tol float64) bool {
	ta, tb := strings.Fields(a), strings.Fields(b)
	if len(ta) != len(tb) {
		return false
	}
	for i := range ta {
		na, errA := strconv.ParseFloat(ta[i], 64)
		nb, errB := strconv.ParseFloat(tb[i], 64)
		if errA == nil && errB == nil {
			if math.Abs(na-nb) > tol {
				return false
			}
			continue
		}
		if ta[i] != tb[i] {
			return false
		}
	}
	return true
}

// caseVerdict 把沙箱单点结果归一为 (点位状态缩写, 终止状态, 原因)。
// 终止状态非空表示该点导致整份提交按此状态失败（单点模式用）。
func caseVerdict(res *sandbox.Result, progOut, ans string, cmp outputComparer) (caseStatus, finalStatus, reason string) {
	switch res.Status {
	case "Accepted":
		if !cmp.equal(progOut, ans) {
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

// fileIOParams 文件 IO 判题参数：仅比赛/考试「本场启用文件 IO」时 enabled。
// 文件名对取题目配置，缺省 problem.in / problem.out。
type fileIOParams struct {
	enabled bool
	inName  string
	outName string
}

// programOutput 取考生输出：优先响应内联内容；copyOutCached 形态（大输出走沙箱文件
// 缓存、不经 /run 响应 JSON——MB 级输出会撑爆响应读取上限）按 fileID 拉取，取完即删。
// err 非 nil 为基础设施错误（调用方判 System Error）。
func (j *Judger) programOutput(ctx context.Context, res *sandbox.Result, fio fileIOParams) (string, bool, error) {
	name := "stdout"
	if fio.enabled {
		name = fio.outName
	}
	if s, ok := res.Files[name]; ok {
		return sandbox.DecodeContent(s), true, nil
	}
	id, ok := res.FileID(name)
	if !ok {
		return "", false, nil
	}
	fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := j.Sandbox.FetchFile(fctx, id, maxStdout)
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	if derr := j.Sandbox.DeleteFile(dctx, id); derr != nil {
		log.Printf("[judge] 清理输出缓存文件失败: %v", derr)
	}
	if err != nil {
		return "", false, fmt.Errorf("拉取考生输出失败: %w", err)
	}
	return string(data), true, nil
}

// judgeCase 单测试点判定核心（v0.5.4 ③ 去重：evaluatePoints 与 evaluateSubtasks 原各持一份同构实现）。
// 返回 caseStatus（明细行）/ finalStatus（整体终态，空 = 继续）/ reason；err 非 nil 为致命错误
// （答案读取/校验器异常），由调用方整体判 System Error。
func (j *Judger) judgeCase(ctx context.Context, tc repo.CaseMeta, res *sandbox.Result, problem *repo.Problem, cmp outputComparer, chk *checkerRunner, fio fileIOParams) (caseStatus, finalStatus, reason string, err error) {
	if res.Status != "Accepted" {
		if fio.enabled && res.Status == "File Error" {
			// 文件模式：CopyOut 收集不到配置的输出文件（程序未写/freopen 名字错）
			return "WA", repo.StatusWrongAnswer, fmt.Sprintf("未生成输出文件 %s", fio.outName), nil
		}
		caseStatus, finalStatus, reason = caseVerdict(res, res.Stdout(), "", cmp)
		return caseStatus, finalStatus, reason, nil
	}
	progOut, progOK, perr := j.programOutput(ctx, res, fio)
	if perr != nil {
		return "", "", "", fmt.Errorf("读取考生输出失败: %w", perr)
	}
	if !progOK {
		return "WA", repo.StatusWrongAnswer, fmt.Sprintf("未生成输出文件 %s", fio.outName), nil
	}
	ans, err := testdata.ReadAll(j.Provider, problem.ID, tc.AnsName)
	if err != nil {
		return "", "", "", fmt.Errorf("读取答案文件失败: %w", err)
	}
	if chk != nil {
		ok, msg, serr := chk.run(ctx, tc, string(ans), progOut)
		if serr != nil {
			return "", "", "", fmt.Errorf("校验器异常: %w", serr)
		}
		if ok {
			return "AC", "", "", nil
		}
		return "WA", repo.StatusWrongAnswer, msg, nil
	}
	caseStatus, finalStatus, reason = caseVerdict(res, progOut, string(ans), cmp)
	return caseStatus, finalStatus, reason, nil
}

// evaluatePoints 单点等分模式：每点 100/n 分。超时（TLE）只判失当前点、继续评测后续点
// （得分=通过点数×100/n，首个 TLE 点作为整卷状态）；其余失败维持首个失败即终止的历史行为。
func (j *Judger) evaluatePoints(ctx context.Context, language *lang.Language, fileID, code string, problem *repo.Problem, cmp outputComparer, chk *checkerRunner, fio fileIOParams) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	st := &evalState{}
	n := len(problem.Cases)
	passed := 0
	tleIdx := 0 // 首个超时点（1 基，0 = 无）
	for i, tc := range problem.Cases {
		res, err := j.runOne(ctx, language, fileID, code, tc, problem, fio)
		if err != nil {
			return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, partialScore(i, n),
				"沙箱调用失败: " + err.Error(), st.detail
		}
		t, m := st.observe(res)
		caseStatus, finalStatus, reason, jerr := j.judgeCase(ctx, tc, res, problem, cmp, chk, fio)
		if jerr != nil {
			return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, partialScore(i, n),
				jerr.Error(), st.detail
		}
		if caseStatus != "" {
			st.detail = append(st.detail, &repo.CaseResult{Index: tc.Index, Status: caseStatus, TimeMS: t, MemKB: m, Msg: reason})
		}
		if finalStatus == "" {
			passed++
			continue
		}
		if finalStatus == repo.StatusTLE {
			// 超时只判失当前点，继续评测后续点
			if tleIdx == 0 {
				tleIdx = i + 1
			}
			continue
		}
		return finalStatus, st.sumTimeMS, st.sumMemKB, partialScore(i, n),
			fmt.Sprintf("第 %d 个测试点: %s", i+1, reason), st.detail
	}
	if tleIdx > 0 {
		return repo.StatusTLE, st.sumTimeMS, st.sumMemKB, passed * 100 / n,
			fmt.Sprintf("第 %d 个测试点: 超出时间限制", tleIdx), st.detail
	}
	return repo.StatusAccepted, st.sumTimeMS, st.sumMemKB, 100, "", st.detail
}

// evaluateSubtasks 子任务模式：组内测试点全过才得该组分；组内失败跳过该组剩余点继续下一组。
// 总分 100 判 Accepted；否则以第一个失败点的状态作为整份提交状态（保留部分分）。
func (j *Judger) evaluateSubtasks(ctx context.Context, language *lang.Language, fileID, code string, problem *repo.Problem, cmp outputComparer, chk *checkerRunner, fio fileIOParams) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
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
				return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, total,
					"子任务配置引用了不存在的测试点", st.detail
			}
			res, err := j.runOne(ctx, language, fileID, code, tc, problem, fio)
			if err != nil {
				return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, total,
					"沙箱调用失败: " + err.Error(), st.detail
			}
			t, m := st.observe(res)
			caseStatus, finalStatus, reason, jerr := j.judgeCase(ctx, tc, res, problem, cmp, chk, fio)
			if jerr != nil {
				return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, total,
					jerr.Error(), st.detail
			}
			if caseStatus != "" {
				st.detail = append(st.detail, &repo.CaseResult{Index: tc.Index, Status: caseStatus, TimeMS: t, MemKB: m, Msg: reason})
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
		return repo.StatusAccepted, st.sumTimeMS, st.sumMemKB, 100, "", st.detail
	}
	if firstFailStatus == "" {
		firstFailStatus, firstFailReason = repo.StatusWrongAnswer, "未通过全部子任务"
	}
	return firstFailStatus, st.sumTimeMS, st.sumMemKB, total, firstFailReason, st.detail
}

// partialScore 已通过的测试点数占比（单点等分模式）。
func partialScore(passed, total int) int {
	return passed * 100 / total
}

// compile 编译并缓存产物；返回的 status 非空表示编译失败。
//
// 稳健性措施（2026-10-15，针对并发编译偶发瞬时失败）：
//   - 串行化：同一时刻只允许一个编译在沙箱内执行（编译只占判题前几秒，串行无碍吞吐）；
//   - 隔离：编译环境 TMPDIR=. 指向本次判题的独立目录，配合 -pipe 避免 gcc 争抢共享 /tmp；
//   - 重试：无 gcc 诊断（stderr 不含 "error:"）的异常终止视为瞬时失败，自动重试一次；
//   - 可观测：无论重试与否，gcc 的 stderr 都会带回 errorInfo（真实语法错误学生直接可见）。
func (j *Judger) compile(ctx context.Context, l *lang.Language, code string) (fileID, status, info string, err error) {
	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// 方案二：编译串行化
		select {
		case compileGate <- struct{}{}:
		case <-ctx.Done():
			return "", "", "", ctx.Err()
		}
		cmd := sandbox.Cmd{
			Args: l.CompileArgs,
			Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin", "TMPDIR=."}, // 方案三：临时文件写入本次判题目录
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
		<-compileGate
		if err != nil || len(results) == 0 {
			return "", "", "", err
		}
		r := results[0]
		if r.Status == "Accepted" {
			if id := r.FileIDs["main"]; id != "" {
				return id, "", "", nil
			}
			return "", repo.StatusCompileError, "编译产物缺失（沙箱未返回 main）", nil
		}

		stderr := strings.TrimSpace(r.Stderr())
		// gcc 标准诊断（file:line: error: ...）= 真实的代码编译错误，重试无意义，
		// 直接把诊断信息作为 errorInfo 返回给学生
		if strings.Contains(stderr, "error:") {
			return "", repo.StatusCompileError, truncate(stderr, 4096), nil
		}

		// 方案一：无诊断的异常终止视为瞬时失败（资源竞争等），自动重试一次
		detail := "编译进程异常终止: " + r.Status
		if stderr != "" {
			detail += "\n" + truncate(stderr, 4096)
		}
		if attempt < maxAttempts {
			log.Printf("[judge] 编译瞬时失败(%s, stderr=%q)，自动重试", r.Status, truncate(stderr, 200))
			continue
		}
		return "", repo.StatusCompileError, detail, nil
	}
	return "", repo.StatusCompileError, "编译重试次数耗尽", nil
}

// compileGate 编译串行化信号量：同一时刻仅允许一个编译任务进入沙箱。
var compileGate = make(chan struct{}, 1)

func (j *Judger) runOne(ctx context.Context, l *lang.Language, fileID, code string, tc repo.CaseMeta, p *repo.Problem, fio fileIOParams) (*sandbox.Result, error) {
	input, err := testdata.ReadAll(j.Provider, p.ID, tc.InName)
	if err != nil {
		return nil, fmt.Errorf("读取输入文件: %w", err)
	}
	cpuLimit := int64(p.TimeLimitMS) * int64(time.Millisecond)
	cmd := sandbox.Cmd{
		Args: l.RunArgs,
		Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Files: []sandbox.File{
			sandbox.Stdin(string(input)),      // 文件模式下程序不读 stdin，占位保持 fd 结构
			sandbox.Pipe("stdout", maxStdout), // 文件模式下 stdout 仅留存展示，不参与比对（NOI 惯例）
			sandbox.Pipe("stderr", maxStderr),
		},
		CPULimit:    cpuLimit,
		ClockLimit:  cpuLimit * 3, // 墙钟上限，防止 sleep 绕过 CPU 限制
		MemoryLimit: int64(p.MemoryLimitKB) << 10,
		ProcLimit:   64,
		CopyOut:     []string{"stderr"},
		// stdout 走 copyOutCached（沙箱文件缓存，按 fileID 拉取）：MB 级输出内联在
		// /run 响应 JSON 里会撑爆响应读取上限（曾致大输出测试点误判 System Error）
		CopyOutCached: []string{"stdout"},
	}
	if l.NeedCompile {
		cmd.CopyIn = map[string]sandbox.CopyFile{"main": {FileID: fileID}}
	} else {
		cmd.CopyIn = map[string]sandbox.CopyFile{l.SourceFile: {Content: code}}
	}
	if fio.enabled {
		// 文件 IO：测试点输入按题目配置的文件名拷入工作目录，
		// 程序 freopen 读入、把答案写到输出文件名，判题收集该文件比对（同样走缓存形态）
		cmd.CopyIn[fio.inName] = sandbox.CopyFile{Content: string(input)}
		cmd.CopyOutCached = append(cmd.CopyOutCached, fio.outName)
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

// ---- checker（v0.5.2-2）：自定义校验器 ----

// checkerRunner checker 判定器：懒编译（编译产物 fileId 缓存进题目元数据，
// 缓存失效自动重编一次）+ 三流执行。调用约定与 testlib 惯例一致：
// checker <输入> <期望> <考生输出>，退出码 0 = AC，非 0 = WA（输出/报错作为失败原因）。
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
func (cr *checkerRunner) run(ctx context.Context, tc repo.CaseMeta, ans, stdout string) (ok bool, msg string, sysErr error) {
	fileID, err := cr.ensureCompiled(ctx)
	if err != nil {
		return false, "", err
	}
	ok, msg, sysErr, retry := cr.tryRun(ctx, fileID, tc, ans, stdout)
	if sysErr != nil && retry {
		cr.p.CheckerFileID = "" // 产物缓存失效：重编一次再试
		fileID, err = cr.ensureCompiled(ctx)
		if err != nil {
			return false, "", err
		}
		ok, msg, sysErr, _ = cr.tryRun(ctx, fileID, tc, ans, stdout)
	}
	return ok, msg, sysErr
}

func (cr *checkerRunner) tryRun(ctx context.Context, fileID string, tc repo.CaseMeta, ans, stdout string) (ok bool, msg string, sysErr error, retry bool) {
	input, err := testdata.ReadAll(cr.j.Provider, cr.p.ID, tc.InName)
	if err != nil {
		return false, "", fmt.Errorf("读取输入文件失败: %w", err), false
	}
	in, ansS, out := string(input), ans, stdout
	cmd := sandbox.Cmd{
		Args: []string{"./main", "input", "answer", "output"}, // 产物约定名 main，相对工作目录
		Files: []sandbox.File{
			sandbox.Pipe("stderr", maxStderr),
			sandbox.Pipe("stdout", maxStderr),
		},
		CopyIn: map[string]sandbox.CopyFile{
			"main":   {FileID: fileID},
			"input":  {Content: in},
			"answer": {Content: ansS},
			"output": {Content: out},
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
