package server

import (
	"context"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

// API 默认超时（v0.66 批②隔离）：/api 请求统一挂 30s deadline——兜住 Mongo 抖动、
// 下游悬挂等「无上限等待」类故障（原绝大多数 handler 直接用无 deadline 的
// c.Request.Context()），慢查询在 30s 处被切断而非占满连接池拖垮全站。
// handler 内 mongo/HTTP 调用均尊重 ctx，超时自然以错误返回；不做响应缓冲
//（TimeoutHandler 会破坏流式下载，故用请求级 deadline 形态）。
const apiDefaultTimeout = 30 * time.Second

// longRouteRe 长路由白名单：合法的长耗时端点不挂默认 deadline（各自已有专属
// 超时或本就是流式长响应）。新增长路由时同步维护这里——lint 守卫兜不住正则清单，
// 改动即复核。
var longRouteRe = []*regexp.Regexp{
	// 自测直跑（等闸无超时 + 运行 60s tctx）
	regexp.MustCompile(`^/api/problems/\d+/run$`),
	// AI 助教/找 bug（上游调用自带 60s ctx）
	regexp.MustCompile(`^/api/problems/\d+/ai(/hint/[^/]+)?$`),
	regexp.MustCompile(`^/api/submissions/\d+/ai-debug$`),
	// 长流式下载（dlAuth 直链：多题导出 zip、testdata 包、成绩 CSV、备份归档）
	regexp.MustCompile(`^/api/problems/export$`),
	regexp.MustCompile(`^/api/problems/\d+/testdata/download$`),
	regexp.MustCompile(`^/api/(assignments|contests|exams)/\d+/export$`),
	regexp.MustCompile(`^/api/admin/backup/download$`),
	regexp.MustCompile(`^/api/admin/users/import-template$`),
	// 批量建号 commit：千行 bcrypt 同步跑可至分钟级（A4，批④ job 化后移出白名单）
	regexp.MustCompile(`^/api/admin/users/import-commit$`),
}

func isLongRoute(path string) bool {
	for _, re := range longRouteRe {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// apiTimeout 挂 /api 组：非白名单路由把请求 ctx 换成带 deadline 的版本。
func apiTimeout() gin.HandlerFunc { return apiTimeoutWith(apiDefaultTimeout) }

// apiTimeoutWith 阈值可注入（单测用）。
func apiTimeoutWith(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isLongRoute(c.Request.URL.Path) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), d)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			cancel()
			return
		}
		c.Next()
	}
}
