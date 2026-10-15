// Package server 组装 SOJ Web API：路由、中间件与各业务 handler。
package server

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
	"soj/internal/model"
	"soj/internal/mq"
	"soj/internal/storage"
)

func Run() {
	cfg := config.Load()
	if os.Getenv("SOJ_DEBUG") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	model.SetDB(storage.MustOpenMySQL(cfg.MySQLDSN))
	rdb := storage.MustOpenRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	pub, err := mq.NewPublisher(cfg.RabbitURL, cfg.RabbitQueue)
	if err != nil {
		log.Fatalf("初始化 RabbitMQ 发布者失败: %v", err)
	}

	h := &Handlers{cfg: cfg, rdb: rdb, pub: pub}

	r := gin.Default()
	r.Use(CORS())

	api := r.Group("/api")
	{
		api.GET("/health", h.Health)

		api.POST("/auth/register", h.Register)
		api.POST("/auth/login", h.Login)
		api.GET("/me", AuthRequired(cfg), h.Me)

		api.GET("/problems", h.ListProblems)
		api.GET("/problems/:id", h.GetProblem)

		api.POST("/submissions", AuthRequired(cfg), h.CreateSubmission)
		api.GET("/submissions", h.ListSubmissions)
		api.GET("/submissions/:id", AuthOptional(cfg), h.GetSubmission)

		api.GET("/rank", h.Rank)
	}

	log.Printf("[web] SOJ API 启动: %s (queue=%s)", cfg.Addr, cfg.RabbitQueue)
	if err := r.Run(cfg.Addr); err != nil {
		log.Fatalf("[web] HTTP 服务退出: %v", err)
	}
}
