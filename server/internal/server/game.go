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

// MyRevenge GET /api/me/revenge —— 每日复仇推荐（v0.45：每日 1 道候选，最久未战
// 优先、最早错误满复仇窗口；used = 当日名额已用，前端展示完成态。入口纯引流——
// 推荐题外自行订正同规则触发 ×1.5）。
func (h *Handlers) MyRevenge(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	hours := 24
	if set, err := h.repo.GetSystemSettings(c.Request.Context()); err == nil && set != nil {
		hours = set.JadeRevengeHoursVal()
	}
	rows, err := h.repo.Wrongbook(c.Request.Context(), uid, 1, time.Duration(hours)*time.Hour)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取错题失败")
		return
	}
	ok(c, gin.H{"list": rows, "used": h.repo.RevengeSlotUsed(c.Request.Context(), uid)})
}

// UserWrongbook GET /api/users/:id/wrongbook?page= —— 错题本全量（不过滤复仇窗口）。
// v0.85 随公开主页公开（原 /api/me/wrongbook 退役）；隐藏题条目对无全量可见权限的
// 观察者整条剔除（口径同公开主页 first_ac）。
func (h *Handlers) UserWrongbook(c *gin.Context) {
	uid, okUID := h.targetUID(c)
	if !okUID {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	rows, err := h.repo.Wrongbook(c.Request.Context(), uid, page*20, 0)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取错题本失败")
		return
	}
	// 隐藏题剔除（观察者权限现判）后再切本页——无权限者的页条目与 total 同步缩减
	if !hasPerm(c, repo.PermProblemViewAll) {
		kept := rows[:0]
		for _, r := range rows {
			if !r.Hidden {
				kept = append(kept, r)
			}
		}
		rows = kept
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

// targetUID 公开主页族接口（game 三卡 v0.85 公开化）共用：解析路由 :id 并校验
// 用户存在（不存在回 404；parseID 自带非法 ID 400）。
func (h *Handlers) targetUID(c *gin.Context) (int64, bool) {
	id, okID := parseID(c, "id")
	if !okID {
		return 0, false
	}
	if _, err := h.repo.GetUserByID(c.Request.Context(), id); repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "用户不存在")
		return 0, false
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取失败")
		return 0, false
	}
	return id, true
}

// UserAchievements GET /api/users/:id/achievements —— 成就墙（定义 + 解锁态）。
// v0.85 随公开主页公开（原 /api/me/achievements 退役）；解锁记录无隐私面，直接下发。
func (h *Handlers) UserAchievements(c *gin.Context) {
	uid, okUID := h.targetUID(c)
	if !okUID {
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

// UserStarMap GET /api/users/:id/starmap —— 知识星图。
// v0.85 随公开主页公开；隐藏题的知识点计数按观察者权限剔除（全站隐藏题口径）。
func (h *Handlers) UserStarMap(c *gin.Context) {
	uid, okUID := h.targetUID(c)
	if !okUID {
		return
	}
	nodes, err := h.repo.StarMap(c.Request.Context(), uid, hasPerm(c, repo.PermProblemViewAll))
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

// GameRank GET /api/game-rank?type=combo|streak&by=best|cur —— 连击/坚持双榜
//（等级/刷题榜走 /api/rank）；连击榜 by 切排序维度（默认 best 历史连击，cur = 当前）。
func (h *Handlers) GameRank(c *gin.Context) {
	kind := c.Query("type")
	if kind != "combo" && kind != "streak" {
		fail(c, http.StatusBadRequest, "榜单类型仅支持 combo / streak")
		return
	}
	by := c.Query("by")
	if by != "best" && by != "cur" {
		by = "best"
	}
	rows, err := h.repo.GameBoard(c.Request.Context(), kind, by, 20)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取榜单失败")
		return
	}
	resp := gin.H{"list": rows, "by": by}
	if uid, _, _, logged := currentUser(c); logged {
		resp["my_rank"], resp["my_value"] = h.repo.GameBoardRank(c.Request.Context(), kind, by, uid)
	}
	ok(c, resp)
}
