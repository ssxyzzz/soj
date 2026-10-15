// Package config 统一读取环境变量配置，所有配置项都有适合本地开发的默认值，
// 容器部署时通过环境变量覆盖（见 docker-compose.yml）。
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr          string // HTTP 监听地址
	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RabbitURL     string
	RabbitQueue   string // 判题队列名
	SandboxAddr   string // go-judge 沙箱地址
	JWTSecret     string
	JudgeWorkers  int // 判题并发数
}

func Load() *Config {
	return &Config{
		Addr:          getEnv("SOJ_ADDR", ":8080"),
		MySQLDSN:      getEnv("SOJ_MYSQL_DSN", "soj:soj123@tcp(127.0.0.1:3306)/soj?charset=utf8mb4&parseTime=true&loc=Local"),
		RedisAddr:     getEnv("SOJ_REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("SOJ_REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("SOJ_REDIS_DB", 0),
		RabbitURL:     getEnv("SOJ_RABBIT_URL", "amqp://soj:soj123@127.0.0.1:5672/"),
		RabbitQueue:   getEnv("SOJ_QUEUE", "soj.submissions"),
		SandboxAddr:   getEnv("SOJ_SANDBOX_ADDR", "http://127.0.0.1:5050"),
		JWTSecret:     getEnv("SOJ_JWT_SECRET", "soj-dev-secret"),
		JudgeWorkers:  getEnvInt("SOJ_JUDGE_WORKERS", 4),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
