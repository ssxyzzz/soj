package server

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// 慢请求观测（v0.66 批①）：请求总耗时超阈值打 Warn（路由模板 + 实际路径 + 状态码）。
// 多题导出/备份下载等合法长响应同样会触发——那是想要的可观测性（谁在跑长任务
// 看得见），噪音量级 = 长任务频次，可接受。阈值 1s：正常接口毫秒级，超秒即值得点名。
const slowReqWarnThreshold = time.Second

// slowLog 注册在最外层（先于 gin.Logger），度量含中间件与响应写出的完整耗时。
func slowLog() gin.HandlerFunc { return slowLogWith(slowReqWarnThreshold) }

// slowLogWith 阈值可注入（单测用毫秒级阈值，避免真等 1 秒）。
// SSE 事件流（v0.98）按寿命上限主动轮转（8min 一关一开），若不豁免每条连接
// 断开都会打一条 Warn——频次 = 在线标签页数 × 每小时 7.5 次重连，纯噪音，跳过。
func slowLogWith(threshold time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/api/events" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		if d := time.Since(start); d >= threshold {
			slog.Warn("慢 HTTP 请求", "component", "web",
				"route", c.FullPath(), "method", c.Request.Method,
				"path", c.Request.URL.Path, "status", c.Writer.Status(),
				"ms", d.Milliseconds())
		}
	}
}
