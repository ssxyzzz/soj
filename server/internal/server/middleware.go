package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/auth"
	"soj/internal/config"
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

// AuthRequired 校验 Bearer Token，通过后把用户信息放入 gin.Context。
func AuthRequired(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		uid, uname, role, err := auth.ParseToken(token, cfg.JWTSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录或登录已过期"})
			return
		}
		c.Set("uid", uid)
		c.Set("uname", uname)
		c.Set("role", role)
		c.Next()
	}
}

// AuthOptional 尝试解析登录态，无论成功与否都放行（用于可选登录的公开接口）。
func AuthOptional(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if uid, uname, role, err := auth.ParseToken(token, cfg.JWTSecret); err == nil {
			c.Set("uid", uid)
			c.Set("uname", uname)
			c.Set("role", role)
		}
		c.Next()
	}
}

// RequireRole 校验当前登录用户的全局角色（操作类权限查全局角色，见 DEVELOPMENT.md）。
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _, role, ok := currentUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 1, "message": "未登录"})
			return
		}
		for _, r := range roles {
			if role == r {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "message": "权限不足"})
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
