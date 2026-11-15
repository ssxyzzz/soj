package server

// 站点公开统计（v0.19 首页对齐 v3 预览）：收录题目 / 注册选手 / 今日提交 / 平均判题
// 时长。只读聚合、60s 内存缓存。
// v0.97.12：今日一题（featured 全局随机 + 登录按等级个性化推荐）随首页降级卡退役
// 整链拆除——登录用户的个性化选题迁往 /api/daily 今日挑战（repo.PickDailyProblem
// 同源），本接口只剩四个统计数。

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

type siteStats struct {
	ProblemCount     int64   `json:"problem_count"`
	UserCount        int64   `json:"user_count"`
	TodaySubmissions int64   `json:"today_submissions"`
	AvgJudgeMS       float64 `json:"avg_judge_ms"`
}

var (
	statsMu      sync.Mutex
	statsCache   *siteStats
	statsCacheAt time.Time
)

// GetSiteStats 站点统计（免登录可访）。60s 全局缓存防聚合压库。
// v0.26 缩界：聚合计算移出全局互斥锁（原持锁查库，Mongo 慢时统计请求串行排队）——
// 锁只护缓存字段读写，缓存失效的并发请求各自计算、后写覆盖，代价可忽略。
func (h *Handlers) GetSiteStats(c *gin.Context) {
	statsMu.Lock()
	st := statsCache
	fresh := st != nil && time.Since(statsCacheAt) < time.Minute
	statsMu.Unlock()
	if !fresh {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		pc, err := h.repo.CountVisibleProblems(ctx, 0)
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		uc, err := h.repo.CountAllUsers(ctx)
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		y, m, d := time.Now().Date()
		dayStart := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		ts, err := h.repo.CountSubmissionsSince(ctx, dayStart)
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}

		// 平均判题时长：最近 1000 条已完成窗口平均（查询契约在 repo.AvgJudgeMSRecent），
		// 失败降级按 0 展示（v0.32 起留 Warn 观测）
		avg, avgErr := h.repo.AvgJudgeMSRecent(ctx)
		if avgErr != nil {
			slog.Warn("首页统计：平均判题时长聚合失败，均值按 0 展示", "component", "web", "error", avgErr)
		}

		st = &siteStats{
			ProblemCount:     pc,
			UserCount:        uc,
			TodaySubmissions: ts,
			AvgJudgeMS:       avg,
		}
		statsMu.Lock()
		statsCache, statsCacheAt = st, time.Now()
		statsMu.Unlock()
	}
	ok(c, st)
}

// GetMyProblemStats 我的做题统计（v0.19 题库副列 v3 卡）：AC 题数 / 可见题总数 /
// 提交数 / AC 率 / 最近 7 天提交分布。登录即可看自己的（无敏感面）。
// 分母按观看者视角（2026-10-27）：持「题库全量可见」者含隐藏题，与题库页头「共 N 题」一致。
func (h *Handlers) GetMyProblemStats(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "请先登录")
		return
	}
	st, err := h.repo.MyProblemStats(c.Request.Context(), uid, hasPerm(c, repo.PermProblemViewAll))
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, st)
}
