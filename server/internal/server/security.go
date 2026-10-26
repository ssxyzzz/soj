package server

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ---- 安全审计（v0.14）：登录/改密/重置/锁定/限流事件落 securityLogs（TTL 90 天） ----

// securityLog 写一条审计（附请求方 IP/UA）；失败只打日志，不阻断业务。
func (h *Handlers) securityLog(c *gin.Context, typ string, uid int64, uname, detail string) {
	if err := h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
		Type: typ, UID: uid, Username: uname,
		IP: c.ClientIP(), UA: c.Request.UserAgent(), Detail: detail,
	}); err != nil {
		log.Printf("[security] 审计写入失败: %v", err)
	}
}

// ---- IP 限流（v0.14）：进程内存滑动窗。单后端进程部署够用；多实例需换 Redis（局限记录在案） ----

const (
	ipRateGlobalPerMin = 600 // 全局：单 IP 每分钟请求上限（只拦脚本，正常浏览远达不到）
	ipRateAuthPerMin   = 30  // 认证类：单 IP 每分钟登录/注册尝试上限
)

type ipLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, limit: limit, window: window}
}

// allow 滑动窗口判定：窗口内已达上限返回 false，否则记一笔返回 true。
func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	arr := l.hits[ip]
	kept := arr[:0]
	for _, t := range arr {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}

// isPrivateIP 私网/回环/链路本地不计数：共享出口（反代/隧道）场景下全体学生
// 同源 IP（docker 网桥 172.x），按该 IP 限流会误伤整校；公网直连才是真实攻击面。
// 反代上线后学生真实公网 IP 经 XFF 还原，限流自动生效。
func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return true // 解析不出（异常场景）不限流，宁可放过不可误伤
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// rateLimit IP 限流中间件（v0.14）：class = global（全部 /api）| auth（登录/注册等），
// 两类独立计数。超限 429 + Retry-After；429 审计同 IP 同类别 60s 去重一条，防刷库。
func (h *Handlers) rateLimit(class string) gin.HandlerFunc {
	var (
		mu        sync.Mutex
		lastAudit = map[string]time.Time{}
	)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if isPrivateIP(ip) {
			c.Next()
			return
		}
		lim := h.ipLimitGlobal
		if class != "global" {
			lim = h.ipLimitAuth
		}
		if lim.allow(ip, time.Now()) {
			c.Next()
			return
		}
		key := class + "|" + ip
		mu.Lock()
		last, seen := lastAudit[key]
		shouldLog := !seen || time.Since(last) > time.Minute
		if shouldLog {
			lastAudit[key] = time.Now()
		}
		mu.Unlock()
		if shouldLog {
			h.securityLog(c, repo.SecRateLimited, 0, "", "IP 限流触发（"+class+"）")
		}
		c.Header("Retry-After", "60")
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"code": 1, "message": "请求过于频繁，请稍后再试"})
	}
}

// ---- 后台安全日志查询（root 硬编码，同标签树/等级面板先例不下放） ----

// AdminListSecurityLogs GET /api/admin/security-logs?page&page_size&type&q（root）。
func (h *Handlers) AdminListSecurityLogs(c *gin.Context) {
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListSecurityLogs(c.Request.Context(), page, pageSize, c.Query("type"), c.Query("q"))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取安全日志失败")
		return
	}
	rows := make([]gin.H, 0, len(list))
	for _, e := range list {
		rows = append(rows, gin.H{
			"at": e.At, "type": e.Type, "uid": e.UID, "username": e.Username,
			"ip": e.IP, "ua": e.UA, "detail": e.Detail,
		})
	}
	ok(c, gin.H{"list": rows, "total": total, "types": repo.SecurityLogTypes})
}
