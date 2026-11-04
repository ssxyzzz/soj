// Package config 统一读取环境变量配置，所有配置项都有适合本地开发的默认值，
// 容器部署时通过环境变量覆盖（见 docker-compose.yml）。
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string // HTTP 监听地址（同时托管前端页面与 API）
	MongoURI     string
	MongoDB      string
	DataDir      string // 测试数据根目录 /data/testdata
	UploadsDir   string // 题面图片目录 /data/uploads
	BackupDir    string // 备份密文归档目录（服务器留存） /data/backups
	KeysDir      string // 备份密钥卷目录（与 testdata 卷分离） /data/keys
	// 备份归档在线下载体积上限（字节）：media/all 档动辄数百 MB，走在线下载会
	// 长时间占满站点唯一的公网管道（学生侧全站变卡，2026-10-30 事故根因）——
	// 超限归档一律 403 并附服务器取包命令，db 档（KB 级）不受影响。
	BackupDownloadLimit int64
	SeedDir      string // 启动时自动导入的种子题目目录（可为空）
	SandboxAddr  string // go-judge 沙箱地址
	JWTSecret    string
	// 下载直链签名密钥（v0.64 直链化）：与 JWT 用途隔离的独立一把，同样走
	// 「显式 env > 密钥卷 > 自动生成持久化」（见 resolveDownloadSecret）。
	DownloadSecret string
	JudgeWorkers   int // 判题并发数
	// 编译并发闸容量（快赢批⑤）：默认 1 = 编译串行化（并发 g++ 瞬时 CE 治理的既定
	// 口径）；考试 + 自测高峰期编译吞吐吃紧时可在 override 调大（受沙箱核数约束）。
	// 判题 / 自测 / checker / interactor 编译共闸。
	CompileConcurrency int
	// 可信代理（v0.13）：ClientIP() 仅从可信直连方解析 X-Forwarded-For。
	// 反代拓扑下用于还原学生真实 IP；nil = 不信任任何代理。
	TrustedProxies []string
}

func Load() *Config {
	c := &Config{
		Addr:               getEnv("SOJ_ADDR", ":8080"),
		MongoURI:           getEnv("SOJ_MONGO_URI", "mongodb://127.0.0.1:27017"),
		MongoDB:            getEnv("SOJ_MONGO_DB", "soj"),
		DataDir:            getEnv("SOJ_DATA_DIR", "./data/testdata"),
		UploadsDir:         getEnv("SOJ_UPLOADS_DIR", "./data/uploads"),
		BackupDir:          getEnv("SOJ_BACKUP_DIR", "./data/backups"),
		KeysDir:            getEnv("SOJ_KEYS_DIR", "./data/keys"),
		BackupDownloadLimit: getEnvInt64("SOJ_BACKUP_DOWNLOAD_LIMIT", 32<<20),
		SeedDir:            getEnv("SOJ_SEED_DIR", ""),
		SandboxAddr:        getEnv("SOJ_SANDBOX_ADDR", "http://127.0.0.1:5050"),
		JWTSecret:          os.Getenv("SOJ_JWT_SECRET"), // 留空 = server.Run 从密钥卷载入或自动生成随机密钥（见 resolveJWTSecret）
		DownloadSecret:     os.Getenv("SOJ_DOWNLOAD_SECRET"), // 留空 = server.Run 从密钥卷载入或自动生成（见 resolveDownloadSecret）
		JudgeWorkers:       getEnvInt("SOJ_JUDGE_WORKERS", 2),
		CompileConcurrency: getEnvInt("SOJ_COMPILE_CONCURRENCY", 1),
	}
	c.TrustedProxies = parseTrustedProxies(getEnv("SOJ_TRUSTED_PROXIES", "127.0.0.1,::1"))
	if len(c.TrustedProxies) == 0 {
		slog.Info("可信代理: 无（不解析 X-Forwarded-For，ClientIP 取直连地址）", "component", "web")
	} else {
		slog.Info("可信代理", "component", "web", "proxies", strings.Join(c.TrustedProxies, ","))
	}
	return c
}

// parseTrustedProxies 解析 SOJ_TRUSTED_PROXIES（逗号分隔的 IP/CIDR）：
// "none" = 不信任任何代理；"*" = 信任所有（等价 0.0.0.0/0 + ::/0，仅直连无代理时用）。
func parseTrustedProxies(v string) []string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "none":
		return nil
	case "*":
		return []string{"0.0.0.0/0", "::/0"}
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
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

func getEnvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
