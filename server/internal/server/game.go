package server

// 游戏感批端点（v0.41）：每日一题 / 每日复仇与错题本 / 成就墙 / 知识星图 / 降临
// 横幅 / 连击与坚持双榜。挂在 AuthOptional 或登录，均只读 + 本人数据（无副作用）。

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// GetDaily GET /api/daily —— 今日挑战（公开：未登录可见题卡，登录附带完成态）。
func (h *Handlers) GetDaily(c *gin.Context) {
	dp, p, err := h.repo.GetDailyProblemMeta(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取每日一题失败")
		return
	}
	if dp == nil || p == nil {
		ok(c, gin.H{"enabled": false}) // 全站无可用题（空库）
		return
	}
	resp := gin.H{
		"enabled":    true,
		"pid":        dp.PID,
		"title":      p.Title,
		"difficulty": p.EffDifficulty(),
	}
	if uid, _, _, logged := currentUser(c); logged {
		resp["done"] = func() bool {
			a, err := h.repo.GetJadeAward(c.Request.Context(), uid, dp.PID)
			return err == nil && a != nil
		}()
		// 全站完成计数（今日 00:00 后该题的 jadeAwards 数——从众软钩）
		dayStart := time.Now().In(repo.BeijingZone()).Truncate(24 * time.Hour)
		// Truncate 按 UTC 截断，手动对齐北京零点
		bt := time.Now().In(repo.BeijingZone())
		dayStart = time.Date(bt.Year(), bt.Month(), bt.Day(), 0, 0, 0, 0, repo.BeijingZone())
		resp["solved_today"] = h.repo.CountDailySolved(c.Request.Context(), dp.PID, dayStart)
	}
	ok(c, resp)
}

// MyRevenge GET /api/me/revenge —— 每日复仇推荐（错题 ≤3 道，最近未战优先）。
func (h *Handlers) MyRevenge(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	rows, err := h.repo.Wrongbook(c.Request.Context(), uid, 3)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取错题失败")
		return
	}
	ok(c, gin.H{"list": rows})
}

// MyWrongbook GET /api/me/wrongbook?page= —— 错题本全量（个人中心）。
func (h *Handlers) MyWrongbook(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	rows, err := h.repo.Wrongbook(c.Request.Context(), uid, page*20)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取错题本失败")
		return
	}
	// 简易分页：Wrongbook 返回前 page*20 条，切片出本页
	start := (page - 1) * 20
	end := start + 20
	if start > len(rows) {
		start = len(rows)
	}
	if end > len(rows) {
		end = len(rows)
	}
	ok(c, gin.H{"list": rows[start:end], "total_estimate": len(rows)})
}

// MyAchievements GET /api/me/achievements —— 成就墙（定义 + 解锁态）。
func (h *Handlers) MyAchievements(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	ctx := c.Request.Context()
	cur, err := h.repo.RawAchievements(ctx, uid)
	if err != nil {
	 fail(c, http.StatusInternalServerError, "读取成就失败")
	 return
	}
	unlocked := map[string]time.Time{}
	for _, a := range cur {
		unlocked[a.Code] = a.At
	}
	defs := make([]gin.H, 0, len(repo.AchievementDefs))
	for _, d := range repo.AchievementDefs {
		item := gin.H{"code": d.Code, "name": d.Name, "desc": d.Desc, "tier": d.Tier, "icon": d.Icon}
		if at, has := unlocked[d.Code]; has {
			item["unlocked_at"] = at.Format(time.RFC3339)
		}
		defs = append(defs, item)
	}
	ok(c, gin.H{"list": defs, "unlocked": len(unlocked)})
}

// MyStarMap GET /api/me/starmap —— 知识星图。
func (h *Handlers) MyStarMap(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	nodes, err := h.repo.StarMap(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取星图失败")
		return
	}
	lit := 0
	for _, n := range nodes {
		if n.Lit {
			lit++
		}
	}
	ok(c, gin.H{"list": nodes, "lit": lit})
}

// GetHeralds GET /api/heralds —— 降临横幅（最近 Lv≥7 升级记录，前端轮询）。
func (h *Handlers) GetHeralds(c *gin.Context) {
	rows, err := h.repo.RecentHeralds(c.Request.Context(), 5)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取降临记录失败")
		return
	}
	ok(c, gin.H{"list": rows})
}

// GameRank GET /api/game-rank?type=combo|streak —— 连击/坚持双榜（等级/刷题榜走 /api/rank）。
func (h *Handlers) GameRank(c *gin.Context) {
	kind := c.Query("type")
	if kind != "combo" && kind != "streak" {
		fail(c, http.StatusBadRequest, "榜单类型仅支持 combo / streak")
		return
	}
	rows, err := h.repo.GameBoard(c.Request.Context(), kind, 20)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取榜单失败")
		return
	}
	resp := gin.H{"list": rows}
	if uid, _, _, logged := currentUser(c); logged {
		resp["my_rank"], resp["my_value"] = h.repo.GameBoardRank(c.Request.Context(), kind, uid)
	}
	ok(c, resp)
}
