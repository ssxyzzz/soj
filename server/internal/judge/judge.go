// Package judge 实现判题流程：编译 -> 逐测试点运行 -> 比较输出 -> 回写结果。
//
// 与沙箱的交互分两步：
//  1. 编译请求：源码经 stdin 交给编译器，产物通过 copyOutCached 缓存为 fileId；
//  2. 运行请求：每个测试点一次，通过 copyIn(fileId) 复用，测完删除。
//
// 任务来源是循环用 findOneAndUpdate 从数据库原子领取；
// 测试数据通过 testdata.Provider 按需逐点读取。
// 给分：无子任务配置时单点等分（全量评测，按通过点数给分）；配置子任务时组内全过才得分（同样全量评测）。
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
	Waker    *Waker // 提交即时唤醒（v0.7.1）：Web 进程入队后非阻塞通知，空闲 worker 近零延迟领取
}

// Waker 提交入队的即时唤醒信号（Web+判题同进程部署形态下的近零延迟领取；
// --judge-only 多进程形态收不到信号，判题循环仍靠 1s 轮询兜底）。
type Waker struct{ ch chan struct{} }

// NewWaker 建 n 容量的信号通道（n = worker 数即可：忙时丢弃、空闲必有接盘者）。
func NewWaker(n int) *Waker {
	if n < 1 {
		n = 1
	}
	return &Waker{ch: make(chan struct{}, n)}
}

// Notify 非阻塞发信号：通道满（所有 worker 都被占满/已被唤醒）直接丢弃，
// worker 空闲后本就会领取任务，不丢单。
func (w *Waker) Notify() {
	if w == nil {
		return
	}
	select {
	case w.ch <- struct{}{}:
	default:
	}
}

// C 返回信号通道；nil Waker 返回 nil（select 恒阻塞，等效关闭该分支）。
func (w *Waker) C() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.ch
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
			// 空闲：等唤醒信号（同进程 Web 入队即醒）或 1s 轮询兜底
			select {
			case <-ctx.Done():
				return
			case <-j.Waker.C():
			case <-time.After(pollInterval):
			}
			continue
		}
		j.runOneSafely(ctx, sub)
	}
}

// statusLeaseLost 内部哨兵：租约已被回收者收走（或提交已转入取消流程），
// 本次判题结果弃写——runOneSafely 见此值直接跳过回写。
const statusLeaseLost = "(lease lost)"

// runOneSafely 单份提交的判题互相隔离：任何 panic 只影响当前提交。
// 全程持有租约心跳（LeaseStaleAfter 内不刷新即会被 Web 进程回收重排队）。
func (j *Judger) runOneSafely(ctx context.Context, sub *repo.Submission) {
	stopHB := make(chan struct{})
	go j.heartbeatLoop(ctx, sub, stopHB)
	defer func() {
		close(stopHB)
		if r := recover(); r != nil {
			log.Printf("[judge] 提交 #%d 判题 panic: %v", sub.ID, r)
			_ = j.Repo.FinishSubmission(ctx, sub.ID, sub.Judge, repo.StatusSystemError, 0, 0, 0,
				"判题器内部错误", nil)
		}
	}()

	log.Printf("[judge] 开始判题 #%d uid=%d pid=%d lang=%s", sub.ID, sub.UID, sub.ProblemID, sub.Language)
	status, timeMS, memKB, score, info, detail := j.evaluate(ctx, sub)
	if status == statusLeaseLost {
		log.Printf("[judge] 提交 #%d 租约已失效，弃写结果", sub.ID)
		return
	}
	if err := j.Repo.FinishSubmission(ctx, sub.ID, sub.Judge, status, timeMS, memKB, score, info, detail); err != nil {
		if errors.Is(err, repo.ErrLeaseLost) {
			log.Printf("[judge] 提交 #%d 回写被拒（租约已转移/回收），弃写", sub.ID)
		} else {
			log.Printf("[judge] 回写提交 #%d 结果失败: %v", sub.ID, err)
		}
		return
	}
	log.Printf("[judge] 提交 #%d => %s (%dms / %dKB / %d分)", sub.ID, status, timeMS, memKB, score)
	// 挑战模式（v0.23）：AC 触发挑战终判（时长按提交创建时刻起算，判题排队耗时不惩罚选手；
	// 已终态的会话重复触发为 no-op，重测幂等）
	if status == repo.StatusAccepted && sub.ChallengeID != 0 {
		j.Repo.FinalizeChallengeOnAC(ctx, sub.UID, sub.ProblemID, sub.ID, len(sub.Code))
	}
}

// heartbeatLoop 判题租约心跳：每 HeartbeatInterval 刷一次 heartbeatAt，
// 覆盖编译与单个长测试点等「点间空隙」之外的时段。stop 关闭即退出。
func (j *Judger) heartbeatLoop(ctx context.Context, sub *repo.Submission, stop chan struct{}) {
	t := time.NewTicker(repo.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := j.Repo.HeartbeatSubmission(hctx, sub.ID, sub.Judge); err != nil {
				log.Printf("[judge] 提交 #%d 心跳失败: %v", sub.ID, err)
			}
			cancel()
		}
	}
}

// checkStop 测试点之间的取消/租约检查。返回 cancelRequested 表示用户已请求取消
// （调用方停止评测并写终态 Cancelled）；leaseLost 表示租约已被回收者收走（弃写）。
// 查询失败时保守继续评测（不因瞬时抖动误杀）。
func (j *Judger) checkStop(ctx context.Context, sub *repo.Submission) (cancelRequested, leaseLost bool) {
	status, held, err := j.Repo.SubmissionLease(ctx, sub.ID, sub.Judge)
	if err != nil {
		return false, false
	}
	if !held {
		return false, true
	}
	return status == repo.StatusCancelRequested, false
}

// cancelledResult 取消时的回写内容：已跑测试点的耗时/内存如实保留，得分 0。
func cancelledResult(st *evalState) (string, int, int, int, string, []*repo.CaseResult) {
	return repo.StatusCancelled, st.sumTimeMS, st.sumMemKB, 0, "已取消评测", st.detail
}

// evaluate 完整评测一份提交，按题目配置选择计分方式。
// 每个测试点之间检查取消请求与租约：取消即停（终态 Cancelled），租约丢失弃写。
func (j *Judger) evaluate(ctx context.Context, sub *repo.Submission) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	// 瘦读（快赢批）：判题只要元数据，排除 statement/statementHtml 两大字段（各 256KB 上限）
	problem, err := j.Repo.GetProblemMeta(ctx, sub.ProblemID)
	if err != nil {
		return repo.StatusSystemError, 0, 0, 0, "题目不存在或读取失败", nil
	}
	// 客观题（v0.9）：提交时已同步判分；走到这里只可能是批量重测置 Pending 后
	// 被 worker 领取——按存储的作答对当前题目定义重判（题目答案改后重测生效）
	if problem.IsObjective() {
		status, score, detail := repo.GradeObjectiveSet(problem.Questions, sub.ObjectiveAnswers)
		return status, 0, 0, score, "", detail
	}
	language, ok := lang.Languages[sub.Language]
	if !ok {
		return repo.StatusSystemError, 0, 0, 0, "不支持的语言: " + sub.Language, nil
	}
	// 函数题（v0.9.1）：driver 在前、学生实现随后，同一编译单元合编；
	// driver 顶部自带函数原型声明，学生只需给出定义
	code := sub.Code
	if problem.IsFunction() {
		if problem.DriverFile == "" {
			return repo.StatusSystemError, 0, 0, 0, "函数题缺少驱动器（driver.cpp），请联系老师补配", nil
		}
		if sub.Language != "cpp" {
			return repo.StatusSystemError, 0, 0, 0, "函数题仅支持 C++", nil
		}
		driverSrc, derr := testdata.ReadAll(j.Provider, problem.ID, problem.DriverFile)
		if derr != nil {
			return repo.StatusSystemError, 0, 0, 0, "读取驱动器失败: " + derr.Error(), nil
		}
		code = string(driverSrc) + "\n" + sub.Code
	}
	if len(problem.Cases) == 0 {
		return repo.StatusSystemError, 0, 0, 0, "题目没有测试数据", nil
	}
	if cancelReq, lost := j.checkStop(ctx, sub); lost {
		return statusLeaseLost, 0, 0, 0, "", nil
	} else if cancelReq {
		return repo.StatusCancelled, 0, 0, 0, "已取消评测", nil
	}

	var fileID string
	if language.NeedCompile {
		id, compileStatus, compileInfo, err := j.compile(ctx, language, code)
		if err != nil {
			return repo.StatusSystemError, 0, 0, 0, "沙箱调用失败: " + err.Error(), nil
		}
		if compileStatus != "" {
			return compileStatus, 0, 0, 0, compileInfo, nil
		}
		fileID = id
		defer j.releaseBinary(fileID)
		// 编译可能耗时数十秒，编译完再查一次取消/租约
		if cancelReq, lost := j.checkStop(ctx, sub); lost {
			return statusLeaseLost, 0, 0, 0, "", nil
		} else if cancelReq {
			return repo.StatusCancelled, 0, 0, 0, "已取消评测", nil
		}
	}

	cmp := newOutputComparer(problem)
	chk := j.checkerRunnerFor(problem)

	// 交互题（v0.9.2）：interactor 与选手双进程管道判分（逐点等分，不走比对/子任务管道）
	if problem.IsInteractive() {
		return j.evaluateInteractive(ctx, sub, language, fileID, problem)
	}

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
		return j.evaluateSubtasks(ctx, sub, language, fileID, code, problem, cmp, chk, fio)
	}
	return j.evaluatePoints(ctx, sub, language, fileID, code, problem, cmp, chk, fio)
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
// input 为该点输入内容（runOne 已读，透传给 checker，省一次重复读盘——快赢批⑤）。
// 返回 caseStatus（明细行）/ finalStatus（整体终态，空 = 继续）/ reason；err 非 nil 为致命错误
// （答案读取/校验器异常），由调用方整体判 System Error。
func (j *Judger) judgeCase(ctx context.Context, tc repo.CaseMeta, res *sandbox.Result, input string, problem *repo.Problem, cmp outputComparer, chk *checkerRunner, fio fileIOParams) (caseStatus, finalStatus, reason string, err error) {
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
		ok, msg, serr := chk.run(ctx, tc, input, string(ans), progOut)
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

// evaluatePoints 单点等分模式：每点 100/n 分，按通过点数给分。全量评测——任何测试点
// 失败（WA/RE/MLE/TLE/OLE）都不终止，跑完全部点；整卷状态与提示取首个失败点。
func (j *Judger) evaluatePoints(ctx context.Context, sub *repo.Submission, language *lang.Language, fileID, code string, problem *repo.Problem, cmp outputComparer, chk *checkerRunner, fio fileIOParams) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	st := &evalState{}
	n := len(problem.Cases)
	// 点间检查 2s 节流（快赢批⑤）：心跳 goroutine 已独立保租约，点间查询只是取消与
	// 回收的尽力检测——原每点一次 SubmissionLease，60 点题一份提交 60+ 次 Mongo 往返；
	// 节流后取消感知最坏多 2s（取消语义本就受当前测试点时限支配）；首点即查不变
	lastCheck := time.Time{}
	checkGate := func() (cancelRequested, leaseLost bool) {
		if time.Since(lastCheck) < 2*time.Second {
			return false, false
		}
		lastCheck = time.Now()
		return j.checkStop(ctx, sub)
	}
	passed := 0
	failStatus, failInfo := "", "" // 首个失败点的状态与原因（整卷终态）
	for i, tc := range problem.Cases {
		// 每个测试点之间检查取消/租约：取消即停（已跑点如实入明细，得分 0）；
		// 放在点前而非点后——最后一个测点跑完的完整结果不被迟到取消回滚（2s 节流见 checkGate）
		if cancelReq, lost := checkGate(); lost {
			return statusLeaseLost, 0, 0, 0, "", nil
		} else if cancelReq {
			return cancelledResult(st)
		}
		res, input, err := j.runOne(ctx, language, fileID, code, tc, problem, fio)
		if err != nil {
			return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, partialScore(passed, n),
				"沙箱调用失败: " + err.Error(), st.detail
		}
		t, m := st.observe(res)
		caseStatus, finalStatus, reason, jerr := j.judgeCase(ctx, tc, res, input, problem, cmp, chk, fio)
		if jerr != nil {
			return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, partialScore(passed, n),
				jerr.Error(), st.detail
		}
		if finalStatus == repo.StatusWrongAnswer {
			reason += waFileIOHint(code, fio)
		}
		if caseStatus != "" {
			st.detail = append(st.detail, &repo.CaseResult{Index: tc.Index, Status: caseStatus, TimeMS: t, MemKB: m, Msg: reason})
		}
		if finalStatus == "" {
			passed++
			continue
		}
		// 失败点不终止：记录首个失败，继续评测后续点
		if failStatus == "" {
			failStatus = finalStatus
			failInfo = fmt.Sprintf("第 %d 个测试点: %s", i+1, reason)
		}
	}
	if failStatus != "" {
		return failStatus, st.sumTimeMS, st.sumMemKB, partialScore(passed, n), failInfo, st.detail
	}
	return repo.StatusAccepted, st.sumTimeMS, st.sumMemKB, 100, "", st.detail
}

// evaluateSubtasks 子任务模式：组内测试点全过才得该组分。全量评测——失败点不终止、
// 不跳过组内剩余点，全部测试点跑完。
// 总分 100 判 Accepted；否则以第一个失败点的状态作为整份提交状态（保留部分分）。
func (j *Judger) evaluateSubtasks(ctx context.Context, sub *repo.Submission, language *lang.Language, fileID, code string, problem *repo.Problem, cmp outputComparer, chk *checkerRunner, fio fileIOParams) (status string, timeMS, memKB, score int, info string, detail []*repo.CaseResult) {
	// 点间检查 2s 节流（同 evaluatePoints，快赢批⑤）
	lastCheck := time.Time{}
	checkGate := func() (cancelRequested, leaseLost bool) {
		if time.Since(lastCheck) < 2*time.Second {
			return false, false
		}
		lastCheck = time.Now()
		return j.checkStop(ctx, sub)
	}
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
			// 每个测试点之间检查取消/租约（同单点模式：点前检查，末点结果不回滚；2s 节流见 checkGate）
			if cancelReq, lost := checkGate(); lost {
				return statusLeaseLost, 0, 0, 0, "", nil
			} else if cancelReq {
				return cancelledResult(st)
			}
			res, input, err := j.runOne(ctx, language, fileID, code, tc, problem, fio)
			if err != nil {
				return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, total,
					"沙箱调用失败: " + err.Error(), st.detail
			}
			t, m := st.observe(res)
			caseStatus, finalStatus, reason, jerr := j.judgeCase(ctx, tc, res, input, problem, cmp, chk, fio)
			if jerr != nil {
				return repo.StatusSystemError, st.sumTimeMS, st.sumMemKB, total,
					jerr.Error(), st.detail
			}
			if finalStatus == repo.StatusWrongAnswer {
				reason += waFileIOHint(code, fio)
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
				// 失败点不跳过：全量评测，组内剩余点照常运行
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
		// 方案二：编译串行化。持闸段收进闭合并 defer 释放：Run 内部 panic 时外层
		// runOneSafely 的 recover 会救活 worker，闸若不释放将永久占死全部判题与自测
		// 编译（v0.27：原裸 <-compileGate 在 panic 路径上漏放）。
		results, err := func() (res []sandbox.Result, err error) {
			select {
			case compileGate <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			defer func() { <-compileGate }()
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
			return j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
		}()
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

// compileGate 编译串行化信号量：默认同一时刻仅允许一个编译任务进入沙箱（并发
// g++ 瞬时 CE 的治理口径）；容量可经 SetCompileConcurrency 按部署调大（快赢批⑤，
// main 启动时在 Pool 之前调用一次）。
var compileGate = make(chan struct{}, 1)

// SetCompileConcurrency 调整编译闸容量（n < 1 忽略）。必须在判题线程池启动前调用。
func SetCompileConcurrency(n int) {
	if n >= 1 {
		compileGate = make(chan struct{}, n)
	}
}

// runOne 执行单个测试点；input 一并返回（judgeCase 的 checker 需要输入文件，
// 复用本函数已读内容，省一次重复读盘——快赢批⑤）。
func (j *Judger) runOne(ctx context.Context, l *lang.Language, fileID, code string, tc repo.CaseMeta, p *repo.Problem, fio fileIOParams) (*sandbox.Result, string, error) {
	input, err := testdata.ReadAll(j.Provider, p.ID, tc.InName)
	if err != nil {
		return nil, "", fmt.Errorf("读取输入文件: %w", err)
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
		cmd.CopyIn = map[string]sandbox.CopyFile{l.SourceFile: sandbox.CopyContent(code)}
	}
	if fio.enabled {
		// 文件 IO：测试点输入按题目配置的文件名拷入工作目录，
		// 程序 freopen 读入、把答案写到输出文件名，判题收集该文件比对（同样走缓存形态）
		cmd.CopyIn[fio.inName] = sandbox.CopyContent(string(input))
		cmd.CopyOutCached = append(cmd.CopyOutCached, fio.outName)
	}
	results, err := j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
	if err != nil || len(results) == 0 {
		return nil, string(input), err
	}
	return &results[0], string(input), nil
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

// waFileIOHint stdio 判题 WA 时的针对性提示：仅当本题不启用文件 IO 且源码
// 检出文件操作痕迹（freopen/fopen/fstream 家族）时返回追加文案——freopen 会因
// 文件不存在而静默失败（读空输入/输出为空），学生常误判为算法错误。
// 普通输出不一致返回空串，不贴文件操作提示（v0.20：曾对所有 WA 一刀切误导）。
func waFileIOHint(code string, fio fileIOParams) string {
	if fio.enabled || fileIOKeyword(code) == "" {
		return ""
	}
	return "（检测到文件操作：本题不需要文件操作）"
}

// fileIOKeyword 扫描源码中的文件操作痕迹（freopen/fopen/fstream 家族，大小写不敏感）。
// 仅在 stdio 判题（题库/练习恒 stdio、场次未开启文件 IO）的 WA 提示里引用：
// freopen 打不开配置文件会静默失败，表现恰好是读空输入、输出为空。空串 = 未检出。
func fileIOKeyword(code string) string {
	lc := strings.ToLower(code)
	for _, kw := range []string{"freopen", "fopen", "ifstream", "ofstream", "fstream"} {
		if strings.Contains(lc, kw) {
			return kw
		}
	}
	return ""
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
