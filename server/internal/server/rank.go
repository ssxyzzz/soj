package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const rankTTL = 30 * time.Second

// rankCache 排行榜进程内缓存：几百人规模聚合本身毫秒级，
// 缓存只为避免榜单页被频繁刷新时重复打库。进程重启缓存自动重建。
type rankCache struct {
	mu    sync.Mutex
	repo  *repo.Repo
	at    time.Time
	items []gin.H
}

func newRankCache(r *repo.Repo) *rankCache { return &rankCache{repo: r} }

func (rc *rankCache) get(ctx context.Context) []gin.H {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.items != nil && time.Since(rc.at) < rankTTL {
		return rc.items
	}
	rows, err := rc.repo.RankTop(ctx, 50)
	if err != nil {
		return nil // 出错时保留旧缓存不动，由调用方决定降级行为
	}
	// 等级（v0.11）：一次聚合批算上榜 50 人的各难度 AC 数再定级；失败降级为不带 level 字段
	levels := map[int64]int{}
	if cfg, err := rc.repo.GetLevelConfig(ctx); err == nil {
		uids := make([]int64, 0, len(rows))
		for _, r := range rows {
			uids = append(uids, r.UID)
		}
		if counts, err := rc.repo.SolvedDifficultyCounts(ctx, uids); err == nil {
			// 经验调整合计（管理端加/减）并入定级；失败按 0 处理
			adjs, err := rc.repo.SumXpAdjust(ctx, uids)
			if err != nil {
				adjs = map[int64]int64{}
			}
			for uid, c := range counts {
				lv, _, _, _, _ := repo.LevelProgress(cfg, c, adjs[uid])
				levels[uid] = lv
			}
		}
	}
	items := make([]gin.H, 0, len(rows))
	for i, r := range rows {
		username, nickname := r.Username, r.Nickname
		if username == "" {
			username, nickname = "-", "-"
		}
		item := gin.H{
			"rank":     i + 1,
			"user_id":  r.UID,
			"username": username,
			"nickname": nickname,
			"solved":   r.Solved,
		}
		if lv := levels[r.UID]; lv > 0 {
			item["level"] = lv
		}
		items = append(items, item)
	}
	rc.items, rc.at = items, time.Now()
	return items
}

// Rank 解题排行榜（前 50 名 + 观察者位次，榜单 30s 缓存）。
// me = 观察者自己的条目（含榜外名次），前端置顶展示——「方便查看自己」。
func (h *Handlers) Rank(c *gin.Context) {
	items := h.rank.get(c.Request.Context())
	if items == nil {
		fail(c, http.StatusInternalServerError, "读取排行榜失败")
		return
	}
	uid, _, _, logged := currentUser(c)
	var me gin.H
	if logged {
		// 在榜内直接取缓存条目（名次与榜单严格一致）；榜外走 RankMe 补算
		for _, it := range items {
			if it["user_id"] == uid {
				me = it
				break
			}
		}
		if me == nil {
			rank, solved, err := h.repo.RankMe(c.Request.Context(), uid)
			if err == nil {
				me = gin.H{"rank": rank, "user_id": uid, "solved": solved}
				if u, err := h.repo.GetUserByID(c.Request.Context(), uid); err == nil {
					uname, nick := u.Username, u.Nickname
					if uname == "" {
						uname, nick = "-", "-"
					}
					me["username"], me["nickname"] = uname, nick
				}
				// 等级与榜单同口径（解题难度分布 + 经验调整）；0 解题不出等级
				if solved > 0 {
					if cfg, err := h.repo.GetLevelConfig(c.Request.Context()); err == nil {
						if counts, err := h.repo.SolvedDifficultyCounts(c.Request.Context(), []int64{uid}); err == nil {
							adjs, err := h.repo.SumXpAdjust(c.Request.Context(), []int64{uid})
							if err != nil {
								adjs = map[int64]int64{}
							}
							if lv, _, _, _, _ := repo.LevelProgress(cfg, counts[uid], adjs[uid]); lv > 0 {
								me["level"] = lv
							}
						}
					}
				}
			}
			// RankMe 失败：me 缺省为空，前端退化为纯榜单
		}
	}
	ok(c, gin.H{"items": items, "me": me})
}
