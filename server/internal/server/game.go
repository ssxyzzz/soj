package server

// 游戏感批端点（v0.41）：今日挑战 / 每日复仇与错题本 / 成就墙 / 知识星图 / 降临
// 横幅 / 连击与坚持双榜。挂在 AuthOptional 或登录，均只读 + 本人数据（无副作用）。

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// 今日挑战选题缓存（v0.97.12 随个性化从 stats 迁来）：按 (uid, 日期) 缓存选题结果
// 30s——tier 三件套 + 最多 11 档池计数/取样不便宜（v0.66 压测 p50 曾 279ms）；
// 条目量 = 登录用户数（与 identCache 同量级），按日键过期、常驻进程内存。
var (
	dailyMu    sync.Mutex
	dailyCache = map[int64]dailyEntry{}
)

type dailyEntry struct {
	day string
	pid int64
	at  time.Time
}

const dailyCacheTTL = 30 * time.Second

// GetDaily GET /api/daily —— 今日挑战（v0.97.12 起登录专属：未登录 enabled:false
// 前端整卡隐藏；选题按账号等级个性化——种子 日期|uid 同日恒定、同档无题逐环外扩
// ±1 档、兜底全池，人人各异的今日挑战；v0.41 全站同题与 solved_today 从众计数退役）。
func (h *Handlers) GetDaily(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		ok(c, gin.H{"enabled": false})
		return
	}
	day := time.Now().In(repo.BeijingZone()).Format("2006-01-02")
	dailyMu.Lock()
	pe, hit := dailyCache[uid]
	dailyMu.Unlock()
	var pid int64
	if !hit || pe.day != day || time.Since(pe.at) >= dailyCacheTTL {
		p, err := h.repo.PickDailyProblem(c.Request.Context(), uid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取今日挑战失败")
			return
		}
		if p == nil {
			ok(c, gin.H{"enabled": false}) // 全站无可用题（空库）
			return
		}
		pid = p.ID
		dailyMu.Lock()
		dailyCache[uid] = dailyEntry{day: day, pid: pid, at: time.Now()}
		dailyMu.Unlock()
	} else {
		pid = pe.pid
	}
	p, err := h.repo.GetProblemMeta(c.Request.Context(), pid)
	if err != nil || p == nil {
		ok(c, gin.H{"enabled": false}) // 缓存命中但题目已被删/下架：降级隐藏
		return
	}
	resp := gin.H{
		"enabled":    true,
		"pid":        pid,
		"title":      p.Title,
		"difficulty": p.EffDifficulty(),
		// done 现查不走缓存：首过后立即变 true（30s 滞后不可接受），单条 FindOne 便宜
		"done": func() bool {
			a, err := h.repo.GetJadeAward(c.Request.Context(), uid, pid)
			return err == nil && a != nil
		}(),
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

// UserFavorites GET /api/users/:id/favorites —— 收藏卡（v0.89 收藏铺开批）：
// 按收藏时间倒序前 20 条，本人/他人同口径（v0.85 公开主页拍板「默认全部可见」）；
// 隐藏题按观察者权限剔除（无「题库全量可见」不见）。
func (h *Handlers) UserFavorites(c *gin.Context) {
	uid, okUID := h.targetUID(c)
	if !okUID {
		return
	}
	rows, err := h.repo.FavoriteRows(c.Request.Context(), uid, 20)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取收藏失败")
		return
	}
	if !hasPerm(c, repo.PermProblemViewAll) {
		kept := rows[:0]
		for _, r := range rows {
			if !r.Hidden {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	ok(c, gin.H{"list": rows})
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
