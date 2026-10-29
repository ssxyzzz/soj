package server

// 题面资产公开路由（v0.31）：GET /problems/:id/assets/:file——文件名按生成形状
// 强校验（纵深防御同 ServeUpload：同函数正则 + fs.ReadFile 的 os.DirFS 根内读取，
// io/fs 语义天然拒绝 .. 与绝对路径）；可见性跟随题面（隐藏题对无「题库全量可见」
// 者 404、未发布赛事隐藏期 404，与 GetProblem 同口径——不泄露存在性）；
// 内容寻址名恒定 → immutable 缓存。

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// assetFileRE 资产文件名形状全匹配：内容寻址 hex32 + 白名单后缀（svg 明确不收——
// XSS 载体）。与 repo.ValidAssetName 同形；读取侧用本文件正则保证校验与使用同函数可见。
var assetFileRE = regexp.MustCompile(`^([0-9a-f]{32})\.(png|jpe?g|gif|webp)$`)

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
	if !okID || !assetFileRE.MatchString(name) {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	ext := filepath.Ext(name) // 后缀已被正则钉为小写白名单
	ctype, okExt := imageExts[ext]
	if !okExt {
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
	// 与 ServeUpload 同款读取形态：os.DirFS 钉死在 <卷>/<题号>/assets 内，文件名已过
	// 形状正则（无分隔符/../的表达空间），io/fs.ReadFile 二次拒绝非法路径
	root := os.DirFS(filepath.Join(h.cfg.DataDir, strconv.FormatInt(id, 10), "assets"))
	data, err := fs.ReadFile(root, name)
	if err != nil {
		c.String(http.StatusNotFound, "image not found")
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, ctype, data)
}
