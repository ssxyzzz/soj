// Package judge 实现判题流程：编译 -> 逐测试点运行 -> 比较输出 -> 回写结果。
//
// 与沙箱的交互分两步：
//  1. 编译请求：源码经 stdin 交给编译器，产物通过 copyOutCached 缓存为 fileId；
//  2. 运行请求：每个测试点一次，通过 copyIn(fileId) 复用编译产物，测完删除。
package judge

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"soj/internal/lang"
	"soj/internal/model"
	"soj/internal/sandbox"
)

const (
	compileCPULimit    = 30 * int64(time.Second)
	compileMemoryLimit = 1 << 30 // 1GiB
	maxStdout          = 64 << 20
	maxStderr          = 1 << 20
)

type Judger struct {
	Sandbox *sandbox.Client
	Redis   *redis.Client
}

type verdict struct {
	status   string
	timeMS   int
	memoryKB int
	score    int
	info     string
}

// Judge 处理一条提交（作为 RabbitMQ 消费回调被调用）。
func (j *Judger) Judge(ctx context.Context, submissionID int64) {
	sub, err := model.GetSubmission(ctx, submissionID)
	if err != nil {
		log.Printf("[judge] 获取提交 #%d 失败: %v", submissionID, err)
		return
	}
	// 只判 Pending 状态，消息重复投递时直接跳过
	if sub.Status != model.StatusPending {
		return
	}
	if err := model.UpdateSubmissionStatus(ctx, sub.ID, model.StatusJudging); err != nil {
		log.Printf("[judge] 更新提交 #%d 状态失败: %v", sub.ID, err)
		return
	}

	v := j.judge(ctx, sub)

	if err := model.FinishSubmission(ctx, sub.ID, v.status, v.timeMS, v.memoryKB, v.score, v.info); err != nil {
		log.Printf("[judge] 回写提交 #%d 结果失败: %v", sub.ID, err)
	}
	log.Printf("[judge] 提交 #%d uid=%d pid=%d lang=%s => %s (%dms / %dKB)",
		sub.ID, sub.UserID, sub.ProblemID, sub.Language, v.status, v.timeMS, v.memoryKB)

	if v.status == model.StatusAccepted {
		j.updateRank(ctx, sub.UserID, sub.ProblemID)
	}
}

// updateRank 用 Redis zset 维护解题排行榜；set 记录用户已过的题，避免重复计分。
func (j *Judger) updateRank(ctx context.Context, userID, problemID int64) {
	acKey := fmt.Sprintf("soj:ac:%d", userID)
	added, err := j.Redis.SAdd(ctx, acKey, problemID).Result()
	if err != nil {
		log.Printf("[judge] 更新解题集合失败 uid=%d: %v", userID, err)
		return
	}
	if added > 0 {
		if _, err := j.Redis.ZIncrBy(ctx, model.RankKey, 1, strconv.FormatInt(userID, 10)).Result(); err != nil {
			log.Printf("[judge] 更新排行榜失败 uid=%d: %v", userID, err)
		}
	}
}

func (j *Judger) judge(ctx context.Context, sub *model.Submission) (v verdict) {
	language, ok := lang.Languages[sub.Language]
	if !ok {
		return verdict{status: model.StatusSystemError, info: "不支持的语言: " + sub.Language}
	}
	problem, err := model.GetProblem(ctx, sub.ProblemID)
	if err != nil {
		return verdict{status: model.StatusSystemError, info: "题目不存在或读取失败"}
	}
	cases, err := model.ListAllCases(ctx, sub.ProblemID)
	if err != nil || len(cases) == 0 {
		return verdict{status: model.StatusSystemError, info: "题目没有测试数据"}
	}

	var fileID string
	if language.NeedCompile {
		id, cv, err := j.compile(ctx, language, sub.Code)
		if err != nil {
			return verdict{status: model.StatusSystemError, info: "沙箱调用失败: " + err.Error()}
		}
		if cv.status != "" {
			return cv
		}
		fileID = id
		defer j.releaseBinary(fileID)
	}

	maxTimeMS, maxMemKB := 0, 0
	for i, tc := range cases {
		res, err := j.runOne(ctx, language, fileID, sub.Code, tc.Input, problem)
		if err != nil {
			return verdict{status: model.StatusSystemError, info: "沙箱调用失败: " + err.Error()}
		}
		if t := int(res.Time / int64(time.Millisecond)); t > maxTimeMS {
			maxTimeMS = t
		}
		if m := int(res.Memory / 1024); m > maxMemKB {
			maxMemKB = m
		}

		fail := func(status, reason string) verdict {
			return verdict{
				status:   status,
				timeMS:   maxTimeMS,
				memoryKB: maxMemKB,
				score:    i * 100 / len(cases),
				info:     fmt.Sprintf("第 %d 个测试点: %s", i+1, reason),
			}
		}

		switch res.Status {
		case "Accepted":
			if normalizeOutput(res.Stdout()) != normalizeOutput(tc.Output) {
				return fail(model.StatusWrongAnswer, "输出与答案不一致")
			}
		case "Time Limit Exceeded":
			return fail(model.StatusTLE, "超出时间限制")
		case "Memory Limit Exceeded":
			return fail(model.StatusMLE, "超出内存限制")
		case "Non Zero Exit Status", "Signalled":
			return fail(model.StatusRE, fmt.Sprintf("退出状态 %d, %s", res.ExitStatus, truncate(res.Stderr(), 512)))
		case "Output Limit Exceeded":
			return fail(model.StatusRE, "输出超过限制")
		default:
			return fail(model.StatusSystemError, "沙箱状态异常: "+res.Status)
		}
	}
	return verdict{status: model.StatusAccepted, timeMS: maxTimeMS, memoryKB: maxMemKB, score: 100}
}

// compile 编译并缓存产物；返回的 verdict.status 非空表示编译失败（编译错误）。
func (j *Judger) compile(ctx context.Context, l *lang.Language, code string) (string, verdict, error) {
	cmd := sandbox.Cmd{
		Args: l.CompileArgs,
		Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Files: []sandbox.File{
			{Content: code},                 // stdin：源码
			{Name: "stdout", Max: 64 << 10}, // 管道收集
			{Name: "stderr", Max: 256 << 10},
		},
		CPULimit:      compileCPULimit,
		MemoryLimit:   compileMemoryLimit,
		ProcLimit:     64,
		CopyOut:       []string{"stdout", "stderr"},
		CopyOutCached: []string{"main"},
	}
	results, err := j.Sandbox.Run(ctx, []sandbox.Cmd{cmd})
	if err != nil || len(results) == 0 {
		return "", verdict{}, err
	}
	r := results[0]
	if r.Status != "Accepted" {
		return "", verdict{status: model.StatusCompileError, info: "编译进程异常终止: " + r.Status}, nil
	}
	if id := r.FileIDs["main"]; id != "" {
		return id, verdict{}, nil
	}
	return "", verdict{status: model.StatusCompileError, info: truncate(r.Stderr(), 8192)}, nil
}

func (j *Judger) runOne(ctx context.Context, l *lang.Language, fileID, code, input string, p *model.Problem) (*sandbox.Result, error) {
	cpuLimit := int64(p.TimeLimitMS) * int64(time.Millisecond)
	cmd := sandbox.Cmd{
		Args: l.RunArgs,
		Env:  []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
		Files: []sandbox.File{
			{Content: input},
			{Name: "stdout", Max: maxStdout},
			{Name: "stderr", Max: maxStderr},
		},
		CPULimit:    cpuLimit,
		ClockLimit:  cpuLimit * 3, // 墙钟上限，防止 sleep 绕过 CPU 限制
		MemoryLimit: int64(p.MemoryLimitKB) << 10,
		ProcLimit:   64,
		CopyOut:     []string{"stdout", "stderr"},
	}
	if l.NeedCompile {
		cmd.CopyIn = map[string]sandbox.File{"main": {FileID: fileID}}
	} else {
		cmd.CopyIn = map[string]sandbox.File{l.SourceFile: {Content: code}}
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
