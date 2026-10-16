package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/auth"
	"soj/internal/repo"
)

// CORS 允许跨域（前后端分离本地开发时 vite 代理已同源，这里兜底）。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AuthRequired 校验 Bearer Token，并从数据库加载权威身份（用户名/角色/生效权限位，
// 带缓存）。角色不信任 token 内容，降权即时生效。
func AuthRequired(h *Handlers) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		uid, _, err := auth.ParseToken(token, h.cfg.JWTSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录或登录已过期"})
			return
		}
		e, ok := h.loadIdent(c.Request.Context(), uid)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "账号状态异常，请重新登录"})
			return
		}
		c.Set("uid", uid)
		c.Set("uname", e.uname)
		c.Set("role", e.role)
		c.Set("perms", e.perms)
		c.Next()
	}
}

// AuthOptional 尝试解析登录态，无论成功与否都放行（用于可选登录的公开接口）。
func AuthOptional(h *Handlers) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if uid, _, err := auth.ParseToken(token, h.cfg.JWTSecret); err == nil {
			if e, ok := h.loadIdent(c.Request.Context(), uid); ok {
				c.Set("uid", uid)
				c.Set("uname", e.uname)
				c.Set("role", e.role)
				c.Set("perms", e.perms)
			}
		}
		c.Next()
	}
}

// RequirePerm 校验当前用户持有某权限位（root 恒通过）。挂在 AuthRequired/AuthOptional 之后。
func RequirePerm(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _, _, logged := currentUser(c)
		if !logged {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录"})
			return
		}
		if !hasPerm(c, perm) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "message": "权限不足"})
			return
		}
		c.Next()
	}
}

// RequirePermAny 任一权限位命中即可（属主级校验由 handler 补充）。
func RequirePermAny(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _, _, logged := currentUser(c)
		if !logged {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录"})
			return
		}
		if !hasPermAny(c, perms...) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "message": "权限不足"})
			return
		}
		c.Next()
	}
}

// RequireRoot 硬编码 root：用于角色管理等不可下放的操作。
func RequireRoot() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _, role, logged := currentUser(c)
		if !logged {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录"})
			return
		}
		if role != repo.RoleRoot {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "message": "仅 root 可执行该操作"})
			return
		}
		c.Next()
	}
}

// currentUser 取出 AuthRequired/AuthOptional 写入的用户信息；未登录时第四个返回值为 false。
func currentUser(c *gin.Context) (uid int64, username, role string, ok bool) {
	v, exists := c.Get("uid")
	if !exists {
		return 0, "", "", false
	}
	uid, ok = v.(int64)
	if !ok {
		return 0, "", "", false
	}
	if u, exists := c.Get("uname"); exists {
		username, _ = u.(string)
	}
	if r, exists := c.Get("role"); exists {
		role, _ = r.(string)
	}
	return uid, username, role, true
}
