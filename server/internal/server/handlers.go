package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
	"soj/internal/judge"
	"soj/internal/repo"
	"soj/internal/sandbox"
	"soj/internal/testdata"
)

// leaderEntry 比赛榜单缓存条目。
type leaderEntry struct {
	at        time.Time
	published bool
	rows      any
}

type Handlers struct {
	cfg      *config.Config
	repo     *repo.Repo
	sandbox  *sandbox.Client
	store    testdata.Provider // 测试数据读取（题目样例展示用）
	waker    *judge.Waker      // 提交即时唤醒（v0.7.1）：判题同进程时空闲 worker 近零延迟领取
	judger   *judge.Judger     // 自测直跑（v0.23）：复用判题器的编译串行闸与沙箱客户端
	rank     *rankCache
	limiter  *rateLimiter
	runLimit *rateLimiter // 自测每 人限流（v0.23，独立于提交限流窗口）
	boards   *boardsCache // 题内三榜缓存（v0.23，TTL 30s）
	idents   *identCache
	// IP 限流（v0.14）：global 全 /api、auth 登录注册等，独立滑动窗口；内存态，单进程
	ipLimitGlobal *ipLimiter
	ipLimitAuth   *ipLimiter

	leaderMu    sync.Mutex
	leaderCache map[string]leaderEntry

	// 批量导入（v0.15）：全站单任务（同一时刻仅一个导入任务）；job 在锁下创建/替换
	batchMu sync.Mutex
	batch   *batchImportJob

	// 备份（v0.26 任务化）：与导入同款单任务骨架，后台打包不再占住 HTTP 请求
	backupMu   sync.Mutex
	backupTask *backupJob
}

// wakeJudge 提交/重测入队后唤醒判题 worker（多进程部署收不到，靠 1s 轮询兜底）。
func (h *Handlers) wakeJudge() {
	if h.waker != nil {
		h.waker.Notify()
	}
}

// 统一响应格式: 成功 {"code":0,"data":...}，失败 {"code":1,"message":"..."}
func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": 1, "message": message})
}

// failData 失败响应附加结构化数据（{"code":1,"message":"...","data":...}），
// 供前端在 toast 之外展示明细——首个使用方是标签树保存被拒时的引用题目清单；
// api 拦截器会把 data 挂到 rejected Error 上（error.data）。
func failData(c *gin.Context, status int, message string, data any) {
	c.JSON(status, gin.H{"code": 1, "message": message, "data": data})
}

// Health 返回数据库与沙箱的连通状态。
func (h *Handlers) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	ok(c, gin.H{
		"db":      h.repo.Ping(ctx) == nil,
		"sandbox": h.sandbox.Available(ctx),
	})
}
