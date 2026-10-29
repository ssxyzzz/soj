package server

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const (
	rankTTL      = 30 * time.Second
	rankBoardCap = 10000 // 全榜缓存上限（防病态规模；班级/社团部署实际远小于此）
	rankPageSize = 100   // 每页人数：第一页即前 100 名，后续整页翻看
)

// rankCache 排行榜进程内缓存：全榜（有已发布 AC 的用户）一次聚合，按页切片下发。
// 几百人规模聚合本身毫秒级，缓存只为避免榜单页被频繁刷新时重复打库。进程重启缓存自动重建。
type rankCache struct {
	mu    sync.Mutex
	repo  *repo.Repo
	at    time.Time
	items []gin.H
}

func newRankCache(r *repo.Repo) *rankCache { return &rankCache{repo: r} }

// get 全榜条目（rank 为全榜绝对名次，切片后无需重算）；出错返回 nil 由调用方降级
func (rc *rankCache) get(ctx context.Context) []gin.H {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.items != nil && time.Since(rc.at) < rankTTL {
		return rc.items
	}
	rows, err := rc.repo.RankBoard(ctx, rankBoardCap)
	if err != nil {
		return nil // 出错时保留旧缓存不动，由调用方决定降级行为
	}
	// 等级（v0.11）：一次聚合批算全榜各难度 AC 数再定级；失败降级为不带 level 字段
	levels := map[int64]int{}
	uids := make([]int64, 0, len(rows))
	for _, r := range rows {
		uids = append(uids, r.UID)
	}
	if cfg, counts, adjs, ok := levelAgg(ctx, rc.repo, uids); ok {
		for uid, c := range counts {
			lv, _, _, _, _ := repo.LevelProgress(cfg, c, adjs[uid])
			levels[uid] = lv
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

// Rank 解题排行榜（分页榜单：每页 rankPageSize 名 + 观察者位次，榜单 30s 缓存）。
// me = 观察者自己的条目（含榜外名次），前端置顶展示——「方便查看自己」。
func (h *Handlers) Rank(c *gin.Context) {
	all := h.rank.get(c.Request.Context())
	if all == nil {
		fail(c, http.StatusInternalServerError, "读取排行榜失败")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	start := (page - 1) * rankPageSize
	if start > len(all) {
		start = len(all) // 越界页回空列表（前端受 total 约束，正常到不了）
	}
	end := start + rankPageSize
	if end > len(all) {
		end = len(all)
	}
	uid, _, _, logged := currentUser(c)
	var me gin.H
	if logged {
		// 在榜内直接取缓存条目（名次与榜单严格一致）；榜外走 RankMe 补算
		for _, it := range all {
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
				} else {
					slog.Warn("榜单：榜外用户信息读取失败，省略用户名字段", "component", "web", "uid", uid, "error", err)
				}
				// 等级与榜单同口径（解题难度分布 + 经验调整）；0 解题不出等级
				if solved > 0 {
					if cfg, counts, adjs, ok := levelAgg(c.Request.Context(), h.repo, []int64{uid}); ok {
						if lv, _, _, _, _ := repo.LevelProgress(cfg, counts[uid], adjs[uid]); lv > 0 {
							me["level"] = lv
						}
					}
				}
			} else {
				// RankMe 失败：me 缺省为空，前端退化为纯榜单
				slog.Warn("榜单：榜外排名补算失败，me 省略", "component", "web", "uid", uid, "error", err)
			}
		}
	}
	ok(c, gin.H{"items": all[start:end], "me": me, "total": len(all)})
}
