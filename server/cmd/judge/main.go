// judge 是 SOJ 的判题 worker：消费 RabbitMQ 中的提交消息，
// 调用 go-judge 沙箱编译运行，并把结果写回 MySQL / Redis。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"soj/internal/config"
	"soj/internal/judge"
	"soj/internal/model"
	"soj/internal/mq"
	"soj/internal/sandbox"
	"soj/internal/storage"
)

func main() {
	cfg := config.Load()

	model.SetDB(storage.MustOpenMySQL(cfg.MySQLDSN))
	rdb := storage.MustOpenRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)

	judger := &judge.Judger{
		Sandbox: sandbox.New(cfg.SandboxAddr),
		Redis:   rdb,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("[judge] 判题 worker 启动: queue=%s workers=%d sandbox=%s",
		cfg.RabbitQueue, cfg.JudgeWorkers, cfg.SandboxAddr)
	if err := mq.Consume(ctx, cfg.RabbitURL, cfg.RabbitQueue, cfg.JudgeWorkers, judger.Judge); err != nil {
		log.Fatalf("[judge] 消费者退出: %v", err)
	}
}
