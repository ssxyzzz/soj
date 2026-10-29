package server

// 题面资产公开路由（v0.31）：GET /api/problems/:id/assets/:file——文件名按生成形状
// 强校验（repo.ValidAssetName，纵深防御同 ServeUpload）；可见性跟随题面（隐藏题对
// 无「题库全量可见」者 404、未发布赛事隐藏期 404，与 GetProblem 同口径——不泄露
// 存在性）；内容寻址名恒定 → immutable 缓存。

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// assetViewDenied 资产可见性判定（与 GetProblem 的 404 条款同口径，纯函数供单测）：
// 隐藏题 ∨ 未发布赛事隐藏期，对非特权视角一律拒绝。
func assetViewDenied(hidden, privileged bool, lk *contestLock) bool {
	if privileged {
		return false
	}
	if hidden {
		return true
	}
	return lk != nil && lk.Phase != "running"
}

// ServeProblemAsset GET /problems/:id/assets/:file：题面图随题面公开（游客可看公开题）。
func (h *Handlers) ServeProblemAsset(c *gin.Context) {
	id, okID := parseID(c, "id")
	name := c.Param("file")
	if !okID || !repo.ValidAssetName(name) {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	ctx := c.Request.Context()
	// 瘦读：资产判定只要 hidden 标记（GetProblemMeta 已排除题面两大字段）
	p, err := h.repo.GetProblemMeta(ctx, id)
	if repo.IsNotFound(err) {
		c.String(http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		c.String(http.StatusInternalServerError, "读取题目失败")
		return
	}
	uid, _, role, logged := currentUser(c)
	privileged := logged && hasPerm(c, repo.PermProblemViewAll)
	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		c.String(http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	if assetViewDenied(p.Hidden, privileged, lockMap[id]) {
		c.String(http.StatusNotFound, "题目不存在")
		return
	}
	f, err := h.provider().Open(id, "assets/"+name)
	if err != nil {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	defer f.Close()
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, f)
}
