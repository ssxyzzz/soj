package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"soj/internal/config"
	"soj/internal/model"
	"soj/internal/mq"
)

type Handlers struct {
	cfg *config.Config
	rdb *redis.Client
	pub *mq.Publisher
}

// 统一响应格式: 成功 {"code":0,"data":...}，失败 {"code":1,"message":"..."}
func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": 1, "message": message})
}

func (h *Handlers) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	ok(c, gin.H{
		"db":    model.Ping(ctx) == nil,
		"redis": h.rdb.Ping(ctx).Err() == nil,
	})
}
