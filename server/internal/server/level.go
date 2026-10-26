package server

// 等级体系 API（v0.11）：公开读配置（徽章渲染要名字与特效开关）、root 改配置。
// 等级本身是读时聚合值（repo.LevelProgress），在这里统一算给 /me/profile 与排行榜。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// levelView 公开配置视图：徽章组件（名字/色轴/特效）与进度条（阈值）渲染所需。
func levelView(cfg *repo.LevelConfig) gin.H {
	return gin.H{
		"names":            cfg.Names,
		"difficulty_names": cfg.DifficultyNames,
		"difficulty_notes": cfg.DifficultyNotes,
		"thresholds":       cfg.Thresholds,
		"decay":            [3]float64{cfg.Decay1, cfg.Decay2, cfg.Decay3},
		"effects_enabled":  cfg.EffectsEnabled,
		"effects_tiers":    cfg.EffectsTiers,
	}
}

// GetLevels 等级配置公开读（游客也要在排行榜页渲染等级徽章）。
// 附带自动补全解锁等级（非敏感门槛值，前端设置页禁用态提示用）。
func (h *Handlers) GetLevels(c *gin.Context) {
	cfg, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取等级配置失败")
		return
	}
	view := levelView(cfg)
	if s, err := h.repo.GetSystemSettings(c.Request.Context()); err == nil {
		view["autocomplete_min_level"] = s.MinAutocompleteLevel()
	}
	ok(c, view)
}

// AdminGetLevelConfig 等级面板读（root）。
func (h *Handlers) AdminGetLevelConfig(c *gin.Context) {
	cfg, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取等级配置失败")
		return
	}
	ok(c, gin.H{"config": cfg})
}

// AdminUpdateLevelConfig 等级面板保存（root）：名称/阈值/衰减系数/特效开关整体覆盖。
func (h *Handlers) AdminUpdateLevelConfig(c *gin.Context) {
	var req struct {
		Names           [10]string `json:"names"`
		DifficultyNames [10]string `json:"difficulty_names"`
		DifficultyNotes [10]string `json:"difficulty_notes"`
		Thresholds      [9]int64   `json:"thresholds"`
		Decay1          *float64   `json:"decay1"`
		Decay2          *float64   `json:"decay2"`
		Decay3          *float64   `json:"decay3"`
		EffectsEnabled  *bool      `json:"effects_enabled"`
		EffectsTiers    *[10]bool  `json:"effects_tiers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	cur, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取等级配置失败")
		return
	}
	cfg := *cur
	cfg.Names = req.Names
	cfg.DifficultyNames = req.DifficultyNames
	for i := range req.DifficultyNotes {
		cfg.DifficultyNotes[i] = trimTo(req.DifficultyNotes[i], 100) // 每档说明 ≤100 字，空档留空不展示
	}
	cfg.Thresholds = req.Thresholds
	if req.Decay1 != nil {
		cfg.Decay1 = *req.Decay1
	}
	if req.Decay2 != nil {
		cfg.Decay2 = *req.Decay2
	}
	if req.Decay3 != nil {
		cfg.Decay3 = *req.Decay3
	}
	if req.EffectsEnabled != nil {
		cfg.EffectsEnabled = *req.EffectsEnabled
	}
	if req.EffectsTiers != nil {
		cfg.EffectsTiers = *req.EffectsTiers
	}
	if !cfg.Normalize() {
		fail(c, http.StatusBadRequest, "配置不合法：名称需为 1-12 字非空，阈值需为严格递增的正整数")
		return
	}
	if err := h.repo.UpdateLevelConfig(c.Request.Context(), &cfg); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"config": cfg})
}

// levelMapFor 批量算一组用户的等级（成绩表/榜单等列表场景用）。
// 任一步失败返回 nil，由调用方降级省略等级字段。
func (h *Handlers) levelMapFor(c *gin.Context, uids []int64) map[int64]int {
	if len(uids) == 0 {
		return map[int64]int{}
	}
	cfg, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		return nil
	}
	counts, err := h.repo.SolvedDifficultyCounts(c.Request.Context(), uids)
	if err != nil {
		return nil
	}
	adjs, err := h.repo.SumXpAdjust(c.Request.Context(), uids)
	if err != nil {
		return nil
	}
	out := make(map[int64]int, len(uids))
	// 全部请求用户都给等级（无 AC 的用户 counts 缺席，按空历史 = Lv.1 起步档）
	for _, uid := range uids {
		lv, _, _, _, _ := repo.LevelProgress(cfg, counts[uid], adjs[uid])
		out[uid] = lv
	}
	return out
}

// levelBadgeInfo 单用户等级展示信息（聚合失败时返回 nil，由调用方降级省略字段）。
// 含经验调整合计（管理端加/减，可负）。
func (h *Handlers) levelBadgeInfo(c *gin.Context, uid int64) gin.H {
	cfg, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		return nil
	}
	counts, err := h.repo.SolvedDifficultyCounts(c.Request.Context(), []int64{uid})
	if err != nil {
		return nil
	}
	adjs, err := h.repo.SumXpAdjust(c.Request.Context(), []int64{uid})
	if err != nil {
		return nil
	}
	adjust := adjs[uid]
	level, xp, _, nextThr, hasNext := repo.LevelProgress(cfg, counts[uid], adjust)
	info := gin.H{"level": level, "xp": xp, "adjust": adjust}
	if hasNext {
		info["next_xp"] = nextThr
	}
	return info
}

// autocompleteAllowed 自动补全门槛：当前等级 ≥ 系统设置阈值（默认 Lv.5）。
func (h *Handlers) autocompleteAllowed(c *gin.Context, uid int64) bool {
	// 老师角色（root/admin）不受限，门槛面向学生——与前端 autocompleteAvailable
	// 同口径；漏掉豁免时低等级 admin 保存偏好会被 400 拦死（开关在前端可点开）。
	if _, _, role, ok := currentUser(c); ok && (role == repo.RoleRoot || role == repo.RoleAdmin) {
		return true
	}
	s, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		return false
	}
	cfg, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		return false
	}
	counts, err := h.repo.SolvedDifficultyCounts(c.Request.Context(), []int64{uid})
	if err != nil {
		return false
	}
	adjs, err := h.repo.SumXpAdjust(c.Request.Context(), []int64{uid})
	if err != nil {
		return false
	}
	lv, _, _, _, _ := repo.LevelProgress(cfg, counts[uid], adjs[uid])
	return lv >= s.MinAutocompleteLevel()
}

// ---- 管理端经验调整（用户管理界面，admin.user.manage 权限位） ----

// AdminGetUserLevel 指定用户的当前等级信息（调整弹窗展示用）。
func (h *Handlers) AdminGetUserLevel(c *gin.Context) {
	uid, okID := parseID(c, "id")
	if !okID {
		return
	}
	if _, err := h.repo.GetUserByID(c.Request.Context(), uid); repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取用户失败")
		return
	}
	info := h.levelBadgeInfo(c, uid)
	if info == nil {
		fail(c, http.StatusInternalServerError, "统计等级失败")
		return
	}
	ok(c, info)
}

// AdminAdjustUserXp 给指定用户加/减经验：delta ≠ 0（正加负减，|delta| ≤ 1000000），
// reason 可选备注（≤100 字，随流水留审计）。
func (h *Handlers) AdminAdjustUserXp(c *gin.Context) {
	uid, okID := parseID(c, "id")
	if !okID {
		return
	}
	operatorUID, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Delta  *int64 `json:"delta"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Delta == nil || *req.Delta == 0 {
		fail(c, http.StatusBadRequest, "调整经验不能为 0（正数增加、负数减少）")
		return
	}
	if *req.Delta > 1000000 || *req.Delta < -1000000 {
		fail(c, http.StatusBadRequest, "单次调整幅度需在 ±1000000 以内")
		return
	}
	reason := trimTo(req.Reason, 100)
	if _, err := h.repo.GetUserByID(c.Request.Context(), uid); repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取用户失败")
		return
	}
	if err := h.repo.AddXpAdjust(c.Request.Context(), uid, *req.Delta, reason, operatorUID); err != nil {
		fail(c, http.StatusInternalServerError, "调整失败")
		return
	}
	info := h.levelBadgeInfo(c, uid)
	if info == nil {
		ok(c, gin.H{"adjusted": uid})
		return
	}
	ok(c, info)
}
