package server

// AI 底座 HTTP 面（v0.33-③）：供应商配置（key 只写不回显）+ 调用审计（root）。
// 提示/找 bug 两能的生成端点在 v0.33-④（ai_tutor.go）复用本文件的价格/守卫助手。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/ai"
)

// maskedProvider 回显档位（key 打码：有 key 显示已配置、空显示未配置；不回明文）。
func maskedProvider(p ai.Provider) gin.H {
	return gin.H{
		"name": p.Name, "base_url": p.BaseURL, "model": p.Model,
		"has_key": p.APIKey != "",
	}
}

// AdminGetAIConfig GET /api/admin/ai/config —— 供应商配置（root；key 打码）。
func (h *Handlers) AdminGetAIConfig(c *gin.Context) {
	cfg, err := h.repo.GetAIConfig(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取 AI 配置失败")
		return
	}
	providers := make([]gin.H, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		providers = append(providers, maskedProvider(p))
	}
	s, _ := h.repo.GetSystemSettings(c.Request.Context())
	ok(c, gin.H{
		"providers": providers, "active": cfg.Active,
		"daily_limit":       s.AiDailyLimitVal(),
		"staff_daily_limit": s.AiStaffDailyLimitVal(),
		"tutor_name":        s.AiTutorNameVal(),
		"discount": gin.H{
			"lv1": *s.AiDiscount1Lv, "pct1": *s.AiDiscount1Pct,
			"lv2": *s.AiDiscount2Lv, "pct2": *s.AiDiscount2Pct,
		},
	})
}

// AdminSaveAIConfig POST /api/admin/ai/config —— 覆盖保存（root）。
// key 留空的档位保留旧 key（「不改」语义）；active 必须指向存在的档。
func (h *Handlers) AdminSaveAIConfig(c *gin.Context) {
	var req struct {
		Providers []struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			APIKey  string `json:"api_key"`
			Model   string `json:"model"`
		} `json:"providers"`
		Active          string  `json:"active"`
		DailyLimit      *int    `json:"daily_limit"`
		StaffDailyLimit *int    `json:"staff_daily_limit"` // 特权层（教师/管理员）每日上限，0 = 不限
		TutorName       *string `json:"tutor_name"`
		Discount        *struct {
			Lv1  *int `json:"lv1"`
			Pct1 *int `json:"pct1"`
			Lv2  *int `json:"lv2"`
			Pct2 *int `json:"pct2"`
		} `json:"discount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ctx := c.Request.Context()
	old, err := h.repo.GetAIConfig(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取 AI 配置失败")
		return
	}
	oldKeys := map[string]string{}
	for _, p := range old.Providers {
		oldKeys[p.Name] = p.APIKey
	}
	cfg := &ai.Config{Active: strings.TrimSpace(req.Active)}
	names := map[string]bool{}
	for _, p := range req.Providers {
		name := strings.TrimSpace(p.Name)
		if name == "" || len(name) > 32 {
			fail(c, http.StatusBadRequest, "档名需为 1–32 字符")
			return
		}
		if names[name] {
			fail(c, http.StatusBadRequest, "档名重复："+name)
			return
		}
		names[name] = true
		key := strings.TrimSpace(p.APIKey)
		if key == "" {
			key = oldKeys[name] // 留空 = 不改旧 key
		}
		if p.BaseURL != "" && !strings.HasPrefix(p.BaseURL, "https://") && !strings.HasPrefix(p.BaseURL, "http://") {
			fail(c, http.StatusBadRequest, "Base URL 需以 http(s):// 开头")
			return
		}
		cfg.Providers = append(cfg.Providers, ai.Provider{
			Name: name, BaseURL: strings.TrimRight(strings.TrimSpace(p.BaseURL), "/"),
			APIKey: key, Model: strings.TrimSpace(p.Model),
		})
	}
	if cfg.Active != "" && !names[cfg.Active] {
		fail(c, http.StatusBadRequest, "启用档不存在："+cfg.Active)
		return
	}
	if err := h.repo.SaveAIConfig(ctx, cfg); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	// 计费参数走系统设置（同一表单一并保存）；提示/找bug 单价已写死推导（2026-10-30），不再可配
	if req.DailyLimit != nil || req.StaffDailyLimit != nil || req.Discount != nil || req.TutorName != nil {
		if s, err := h.repo.GetSystemSettings(ctx); err == nil {
			if req.DailyLimit != nil {
				s.AiDailyLimit = req.DailyLimit
			}
			if req.StaffDailyLimit != nil {
				if v := *req.StaffDailyLimit; v < 0 || v > 1000 {
					fail(c, http.StatusBadRequest, "特权层每日上限需在 0–1000（0 = 不限）")
					return
				}
				s.AiStaffDailyLimit = req.StaffDailyLimit
			}
			if req.TutorName != nil {
				name := strings.TrimSpace(*req.TutorName)
				if n := len([]rune(name)); n < 1 || n > 12 {
					fail(c, http.StatusBadRequest, "AI 名字需为 1–12 个字（默认：小玉）")
					return
				}
				s.AiTutorName = name
			}
			if req.Discount != nil {
				s.AiDiscount1Lv, s.AiDiscount1Pct = req.Discount.Lv1, req.Discount.Pct1
				s.AiDiscount2Lv, s.AiDiscount2Pct = req.Discount.Lv2, req.Discount.Pct2
			}
			_ = h.repo.UpdateSystemSettings(ctx, s)
		}
	}
	ok(c, gin.H{"saved": true})
}

// AdminAiCalls GET /api/admin/ai/calls?uid=&page= —— 调用审计（root，成本监控）。
func (h *Handlers) AdminAiCalls(c *gin.Context) {
	uid, _ := strconv.ParseInt(c.Query("uid"), 10, 64)
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListAiCalls(c.Request.Context(), uid, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取审计失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, a := range list {
		items = append(items, gin.H{
			"id": a.ID, "uid": a.UID, "pid": a.PID, "kind": a.Kind,
			"tokens": a.TokensTotal, "prompt_chars": a.PromptChars,
			"cost": a.Cost, "provider": a.Provider, "model": a.Model,
			"ok": a.OK, "err": a.Err, "at": a.At.Format("2006-01-02 15:04"),
		})
	}
	ok(c, gin.H{"list": items, "total": total})
}
