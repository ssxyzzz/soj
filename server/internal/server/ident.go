package server

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// 身份缓存：uid → 用户名/角色/生效权限位。角色不再烧进 JWT 后，每个登录请求
// 都要拿权威身份；库 + 角色两份文档读进来后缓存 TTL 60s，用户或角色变更时整体失效
// （AdminUpdateUser / 角色管理各接口调用 flush），降权即时生效。
type identEntry struct {
	uname  string
	role   string
	perms  []string
	pwdVer int // 密码版本（v0.14）：token 携带值与库不一致 → 改密后的旧 token，拒绝
	exp    time.Time
}

type identCache struct {
	mu  sync.RWMutex
	m   map[int64]identEntry
	ttl time.Duration
}

func newIdentCache() *identCache {
	return &identCache{m: map[int64]identEntry{}, ttl: 60 * time.Second}
}

// flush 整体失效（角色或用户变更后调用）。
func (ic *identCache) flush() {
	ic.mu.Lock()
	ic.m = map[int64]identEntry{}
	ic.mu.Unlock()
}

// loadIdent 取用户身份（带缓存）。用户或其角色不存在时 ok=false（按未登录处理，
// 顺带堵住「删号后旧 token 仍有效」）。
func (h *Handlers) loadIdent(ctx context.Context, uid int64) (identEntry, bool) {
	now := time.Now()
	h.idents.mu.RLock()
	e, hit := h.idents.m[uid]
	h.idents.mu.RUnlock()
	if hit && now.Before(e.exp) {
		return e, true
	}
	u, err := h.repo.GetUserByID(ctx, uid)
	if err != nil {
		return identEntry{}, false
	}
	role, err := h.repo.GetRole(ctx, u.Role)
	if err != nil {
		return identEntry{}, false // 用户挂着已删除的角色：拒绝，直到管理员修正
	}
	e = identEntry{uname: u.Username, role: u.Role, perms: repo.EffectivePerms(role), pwdVer: u.PwdVer, exp: now.Add(h.idents.ttl)}
	h.idents.mu.Lock()
	h.idents.m[uid] = e
	h.idents.mu.Unlock()
	return e, true
}

// hasPerm 权限判定：root 恒通过（代码级超角色，不依赖数据库内容），其余查生效权限位。
func hasPerm(c *gin.Context, perm string) bool {
	if role, exists := c.Get("role"); exists {
		if r, _ := role.(string); r == repo.RoleRoot {
			return true
		}
	}
	perms, _ := c.Get("perms")
	list, _ := perms.([]string)
	for _, p := range list {
		if p == perm {
			return true
		}
	}
	return false
}

// hasPermAny 任一权限位命中即可（如题面编辑 = edit.all 或 edit.own，属主校验在 handler）。
func hasPermAny(c *gin.Context, perms ...string) bool {
	for _, p := range perms {
		if hasPerm(c, p) {
			return true
		}
	}
	return false
}
