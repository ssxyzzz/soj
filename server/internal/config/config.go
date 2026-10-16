// Package config 统一读取环境变量配置，所有配置项都有适合本地开发的默认值，
// 容器部署时通过环境变量覆盖（见 docker-compose.yml）。
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr         string // HTTP 监听地址（同时托管前端页面与 API）
	MongoURI     string
	MongoDB      string
	DataDir      string // 测试数据根目录 /data/testdata
	BackupDir    string // 备份密文归档目录（服务器留存） /data/backups
	KeysDir      string // 备份密钥卷目录（与 testdata 卷分离） /data/keys
	SeedDir      string // 启动时自动导入的种子题目目录（可为空）
	SandboxAddr  string // go-judge 沙箱地址
	JWTSecret    string
	JudgeWorkers int // 判题并发数
}

func Load() *Config {
	return &Config{
		Addr:         getEnv("SOJ_ADDR", ":8080"),
		MongoURI:     getEnv("SOJ_MONGO_URI", "mongodb://127.0.0.1:27017"),
		MongoDB:      getEnv("SOJ_MONGO_DB", "soj"),
		DataDir:      getEnv("SOJ_DATA_DIR", "./data/testdata"),
		BackupDir:    getEnv("SOJ_BACKUP_DIR", "./data/backups"),
		KeysDir:      getEnv("SOJ_KEYS_DIR", "./data/keys"),
		SeedDir:      getEnv("SOJ_SEED_DIR", ""),
		SandboxAddr:  getEnv("SOJ_SANDBOX_ADDR", "http://127.0.0.1:5050"),
		JWTSecret:    getEnv("SOJ_JWT_SECRET", "soj-dev-secret"),
		JudgeWorkers: getEnvInt("SOJ_JUDGE_WORKERS", 2),
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
