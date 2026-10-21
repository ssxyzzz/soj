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
		"names":           cfg.Names,
		"thresholds":      cfg.Thresholds,
		"decay":           [3]float64{cfg.Decay1, cfg.Decay2, cfg.Decay3},
		"effects_enabled": cfg.EffectsEnabled,
		"effects_tiers":   cfg.EffectsTiers,
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
		Names      [10]string `json:"names"`
		Thresholds [9]int64   `json:"thresholds"`
		Decay1     *float64   `json:"decay1"`
		Decay2     *float64   `json:"decay2"`
		Decay3     *float64   `json:"decay3"`
		EffectsEnabled *bool  `json:"effects_enabled"`
		EffectsTiers   *[10]bool `json:"effects_tiers"`
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

// levelBadgeInfo 单用户等级展示信息（聚合失败时返回 nil，由调用方降级省略字段）。
func (h *Handlers) levelBadgeInfo(c *gin.Context, uid int64) gin.H {
	cfg, err := h.repo.GetLevelConfig(c.Request.Context())
	if err != nil {
		return nil
	}
	counts, err := h.repo.SolvedDifficultyCounts(c.Request.Context(), []int64{uid})
	if err != nil {
		return nil
	}
	level, xp, _, nextThr, hasNext := repo.LevelProgress(cfg, counts[uid])
	info := gin.H{"level": level, "xp": xp}
	if hasNext {
		info["next_xp"] = nextThr
	}
	return info
}

// autocompleteAllowed 自动补全门槛：当前等级 ≥ 系统设置阈值（默认 Lv.5）。
func (h *Handlers) autocompleteAllowed(c *gin.Context, uid int64) bool {
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
	lv, _, _, _, _ := repo.LevelProgress(cfg, counts[uid])
	return lv >= s.MinAutocompleteLevel()
}
