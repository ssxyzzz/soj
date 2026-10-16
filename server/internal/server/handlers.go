package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
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
	cfg     *config.Config
	repo    *repo.Repo
	sandbox *sandbox.Client
	store   testdata.Provider // 测试数据读取（题目样例展示用）
	rank    *rankCache
	limiter *rateLimiter
	idents  *identCache

	leaderMu    sync.Mutex
	leaderCache map[string]leaderEntry
}

// 统一响应格式: 成功 {"code":0,"data":...}，失败 {"code":1,"message":"..."}
func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": 1, "message": message})
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
