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
			for uid, c := range counts {
				lv, _, _, _, _ := repo.LevelProgress(cfg, c)
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

// Rank 解题排行榜（前 50 名，30s 缓存）。
func (h *Handlers) Rank(c *gin.Context) {
	items := h.rank.get(c.Request.Context())
	if items == nil {
		fail(c, http.StatusInternalServerError, "读取排行榜失败")
		return
	}
	ok(c, items)
}
