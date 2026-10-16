package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/auth"
	"soj/internal/repo"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

func userJSON(u *repo.User) gin.H {
	return gin.H{"id": u.ID, "username": u.Username, "nickname": u.Nickname, "role": u.Role}
}

// userWithPermsJSON 登录态响应用：附生效权限位（root 展开为全目录，前端据此显隐入口）。
func (h *Handlers) userWithPermsJSON(ctx context.Context, u *repo.User) gin.H {
	perms := []string{}
	if role, err := h.repo.GetRole(ctx, u.Role); err == nil {
		perms = repo.EffectivePerms(role)
	}
	j := userJSON(u)
	j["perms"] = perms
	return j
}

func (h *Handlers) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if !usernameRE.MatchString(req.Username) {
		fail(c, http.StatusBadRequest, "用户名需为 3-32 位字母/数字/下划线")
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 64 {
		fail(c, http.StatusBadRequest, "密码长度需在 6-64 之间")
		return
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = req.Username
	}

	ctx := c.Request.Context()
	if _, _, err := h.repo.GetUserByUsername(ctx, req.Username); err == nil {
		fail(c, http.StatusBadRequest, "用户名已存在")
		return
	} else if !repo.IsNotFound(err) {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	uid, err := h.repo.CreateUserWithPassword(ctx, req.Username, req.Password, nickname, repo.RoleDefault)
	if err != nil {
		if isDupKey(err) {
			fail(c, http.StatusBadRequest, "用户名已存在")
			return
		}
		fail(c, http.StatusInternalServerError, "创建用户失败")
		return
	}
	token, err := auth.GenerateToken(uid, req.Username, h.cfg.JWTSecret)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	u := &repo.User{ID: uid, Username: req.Username, Nickname: nickname, Role: repo.RoleDefault}
	ok(c, gin.H{"token": token, "user": h.userWithPermsJSON(c.Request.Context(), u)})
}

func (h *Handlers) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	ctx := c.Request.Context()
	u, hash, err := h.repo.GetUserByUsername(ctx, req.Username)
	if repo.IsNotFound(err) || (err == nil && !auth.CheckPassword(hash, req.Password)) {
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	token, err := auth.GenerateToken(u.ID, u.Username, h.cfg.JWTSecret)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	h.idents.flush() // 登录时失效缓存，确保拿到最新角色/昵称
	ok(c, gin.H{"token": token, "user": h.userWithPermsJSON(c.Request.Context(), u)})
}

func (h *Handlers) Me(c *gin.Context) {
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	u, err := h.repo.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	ok(c, h.userWithPermsJSON(c.Request.Context(), u))
}

// parseID 解析路径参数中的数字 ID。
func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "非法的 ID")
		return 0, false
	}
	return id, true
}

// isDupKey MongoDB 唯一索引冲突（并发注册同名用户的兜底）。
func isDupKey(err error) bool {
	var we mongo.WriteException
	if errors.As(err, &we) {
		return we.HasErrorCode(11000)
	}
	return false
}
