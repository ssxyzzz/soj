package server

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"soj/internal/auth"
	"soj/internal/model"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

func userJSON(u *model.User) gin.H {
	return gin.H{"id": u.ID, "username": u.Username, "nickname": u.Nickname, "role": u.Role}
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
	if _, _, err := model.GetUserByUsername(ctx, req.Username); err == nil {
		fail(c, http.StatusBadRequest, "用户名已存在")
		return
	} else if !errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	id, err := model.CreateUser(ctx, req.Username, hash, nickname)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建用户失败")
		return
	}
	token, err := auth.GenerateToken(id, req.Username, "user", h.cfg.JWTSecret)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, gin.H{"token": token, "user": userJSON(&model.User{ID: id, Username: req.Username, Nickname: nickname, Role: "user"})})
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
	u, hash, err := model.GetUserByUsername(ctx, req.Username)
	if errors.Is(err, model.ErrNotFound) || (err == nil && !auth.CheckPassword(hash, req.Password)) {
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	token, err := auth.GenerateToken(u.ID, u.Username, u.Role, h.cfg.JWTSecret)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, gin.H{"token": token, "user": userJSON(u)})
}

func (h *Handlers) Me(c *gin.Context) {
	uid, _, _, okAuth := currentUser(c)
	if !okAuth {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	u, err := model.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	ok(c, userJSON(u))
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
