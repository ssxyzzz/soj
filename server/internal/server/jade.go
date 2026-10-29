package server

// 玉经济体（v0.33-①）HTTP 面：本人余额与流水、root 查用户玉 + 人工调玉。
// 获取侧（首 AC / 掉落 / 比赛奖池）全在后端钩子自动结算，无 HTTP 入口；
// 消费侧（题解解锁 / 悬赏 / 商店 / AI）随第二三步接 JadeSpend。

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// jadeLedgerJSON 流水行下发视图（kind 前端做中文映射）。
func jadeLedgerJSON(e *repo.JadeLedgerEntry) gin.H {
	return gin.H{
		"id": e.ID, "delta": e.Delta, "kind": e.Kind,
		"reason": e.Reason, "ref": e.Ref, "at": e.At.Format(time.RFC3339),
	}
}

// MyJade GET /api/me/jade —— 导航栏余额（含货币名）。
func (h *Handlers) MyJade(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	bal, err := h.repo.JadeBalance(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取余额失败")
		return
	}
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	currency := repo.DefaultCurrencyName
	if s != nil {
		currency = s.CurrencyNameVal()
	}
	ok(c, gin.H{"balance": bal, "currency": currency})
}

// MyJadeLedger GET /api/me/jade/ledger?page= —— 个人资料页流水明细（仅本人）。
func (h *Handlers) MyJadeLedger(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.JadeLedgerPage(c.Request.Context(), uid, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取流水失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, e := range list {
		items = append(items, jadeLedgerJSON(e))
	}
	bal, _ := h.repo.JadeBalance(c.Request.Context(), uid)
	ok(c, gin.H{"list": items, "total": total, "balance": bal})
}

// AdminUserJade GET /api/admin/users/:id/jade?page= —— root 查指定用户余额与流水。
func (h *Handlers) AdminUserJade(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.JadeLedgerPage(c.Request.Context(), id, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取流水失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, e := range list {
		items = append(items, jadeLedgerJSON(e))
	}
	bal, _ := h.repo.JadeBalance(c.Request.Context(), id)
	ok(c, gin.H{"list": items, "total": total, "balance": bal})
}

// AdminJadeAdjust POST /api/admin/users/:id/jade-adjust {delta, reason} —— root 人工
// 加减玉（delta 正加负减；减超余额报错不透支）；reason 必填随流水留审计。
func (h *Handlers) AdminJadeAdjust(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Delta  int64  `json:"delta"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Delta == 0 {
		fail(c, http.StatusBadRequest, "调整值需为非零整数")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		fail(c, http.StatusBadRequest, "请填写调整原因")
		return
	}
	if len([]rune(reason)) > 200 {
		fail(c, http.StatusBadRequest, "原因过长（上限 200 字）")
		return
	}
	ctx := c.Request.Context()
	if _, err := h.repo.GetUserByID(ctx, id); err != nil {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	audit := "管理员 " + uname + " 调整：" + reason
	if req.Delta > 0 {
		if err := h.repo.JadeCredit(ctx, id, req.Delta, repo.JadeKindAdminAdjust, audit, uid); err != nil {
			fail(c, http.StatusInternalServerError, "调整失败")
			return
		}
	} else {
		if err := h.repo.JadeSpend(ctx, id, -req.Delta, repo.JadeKindAdminAdjust, audit, uid); err != nil {
			if err == repo.ErrInsufficientJade {
				fail(c, http.StatusBadRequest, "用户余额不足，无法扣减")
				return
			}
			fail(c, http.StatusInternalServerError, "调整失败")
			return
		}
	}
	bal, _ := h.repo.JadeBalance(ctx, id)
	ok(c, gin.H{"balance": bal})
}
