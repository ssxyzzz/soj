package server

// 站点公开统计（v0.19 首页对齐 v3 预览）：收录题目 / 注册选手 / 今日提交 / 平均判题时长
// + 今日一题（按日随机一道可见题，同一天恒定；2026-10-25 起登录用户按账号等级推荐
// 相应难度——同档无题逐环外扩 ±1 档，最后兜底全池；匿名保持全局每日一题）。
// 只读聚合、60s 内存缓存（缓存存全局口径，个性化在命中后按请求覆盖）。

import (
	"context"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/repo"
)

type featuredProblem struct {
	ID         int64    `json:"id"`
	Title      string   `json:"title"`
	Difficulty int      `json:"difficulty"`
	AcceptRate float64  `json:"accept_rate"` // 0–100；无提交为 0
	Tags       []string `json:"tags"`        // 标签树节点名（首页今日一题 meta 展示，最多 4 个）
}

type siteStats struct {
	ProblemCount     int64            `json:"problem_count"`
	UserCount        int64            `json:"user_count"`
	TodaySubmissions int64            `json:"today_submissions"`
	AvgJudgeMS       float64          `json:"avg_judge_ms"`
	Featured         *featuredProblem `json:"featured"`
}

var (
	statsMu      sync.Mutex
	statsCache   *siteStats
	statsCacheAt time.Time
)

// pickFeatured 按种子从可见题池挑一道（difficulty>0 时限同难度档），装配标签名与通过率。
// 池为空（或查询失败）返回 nil，由调用方扩大范围/兜底。种子 = 日期（全局）或 日期|uid（个性化）。
func (h *Handlers) pickFeatured(ctx context.Context, seed string, difficulty int) *featuredProblem {
	q := bson.M{"hidden": bson.M{"$ne": true}}
	if difficulty > 0 {
		q["difficulty"] = difficulty
	}
	n, err := h.repo.Problems.CountDocuments(ctx, q)
	if err != nil || n == 0 {
		return nil
	}
	hseed := fnv.New32a()
	_, _ = hseed.Write([]byte(seed))
	skip := int64(hseed.Sum32()) % n
	fcur, err := h.repo.Problems.Find(ctx, q,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetSkip(skip).SetLimit(1))
	if err != nil {
		return nil
	}
	defer fcur.Close(ctx)
	var rows []struct {
		ID         int64   `bson:"_id"`
		Title      string  `bson:"title"`
		Difficulty int     `bson:"difficulty,omitempty"`
		Tags       []int64 `bson:"tags"`
	}
	if err := fcur.All(ctx, &rows); err != nil || len(rows) == 0 {
		return nil
	}
	fp := &featuredProblem{
		ID:         rows[0].ID,
		Title:      rows[0].Title,
		Difficulty: rows[0].Difficulty,
	}
	// 标签树节点名（首页今日一题 meta 展示，最多 4 个；树读取失败静默跳过）
	if len(rows[0].Tags) > 0 {
		if nodes, terr := h.repo.GetTagTree(ctx); terr == nil {
			names := repo.TagIDNames(nodes)
			for _, id := range rows[0].Tags {
				if n, found := names[id]; found {
					fp.Tags = append(fp.Tags, n)
					if len(fp.Tags) >= 4 {
						break
					}
				}
			}
		}
	}
	// 通过率（AC / 总提交，题库同口径）；两次 count 开销可忽略
	total, _ := h.repo.Submissions.CountDocuments(ctx, bson.M{"pid": fp.ID})
	if total > 0 {
		ac, _ := h.repo.Submissions.CountDocuments(ctx, bson.M{"pid": fp.ID, "status": "Accepted"})
		fp.AcceptRate = float64(ac) / float64(total) * 100
	}
	return fp
}

// userTier 账号当前等级（1–10，聚合口径与排行榜一致：难度分布 + 经验调整）；
// 读取失败回 1（青铜）。
func (h *Handlers) userTier(ctx context.Context, uid int64) int {
	cfg, counts, adjs, ok := levelAgg(ctx, h.repo, []int64{uid})
	if !ok {
		return 1
	}
	lv, _, _, _, _ := repo.LevelProgress(cfg, counts[uid], adjs[uid])
	if lv < 1 || lv > 10 {
		return 1
	}
	return lv
}

// GetSiteStats 站点统计（免登录可访；AuthOptional 供个性化今日一题读 uid）。
// 60s 全局缓存防聚合压库；登录用户在缓存命中后按请求覆盖 Featured（不写回共享缓存）。
// v0.26 缩界：聚合计算移出全局互斥锁（原持锁查库，Mongo 慢时统计请求串行排队）——
// 锁只护缓存字段读写，缓存失效的并发请求各自计算、后写覆盖，代价可忽略。
func (h *Handlers) GetSiteStats(c *gin.Context) {
	uid, _, _, logged := currentUser(c)

	statsMu.Lock()
	st := statsCache
	fresh := st != nil && time.Since(statsCacheAt) < time.Minute
	statsMu.Unlock()
	if !fresh {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		visible := bson.M{"hidden": bson.M{"$ne": true}}
		pc, err := h.repo.Problems.CountDocuments(ctx, visible)
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		uc, err := h.repo.Users.CountDocuments(ctx, bson.M{})
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		y, m, d := time.Now().Date()
		dayStart := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		ts, err := h.repo.Submissions.CountDocuments(ctx, bson.M{"createdAt": bson.M{"$gte": dayStart}})
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器内部错误")
			return
		}

		// 平均判题时长：最近 1000 条已完成（timeMs>0）的窗口平均，避免全表聚合
		avg := 0.0
		cur, err := h.repo.Submissions.Aggregate(ctx, mongo.Pipeline{
			{{Key: "$match", Value: bson.M{"timeMs": bson.M{"$gt": 0}}}},
			{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: -1}}}},
			{{Key: "$limit", Value: 1000}},
			{{Key: "$group", Value: bson.M{"_id": nil, "avg": bson.M{"$avg": "$timeMs"}}}},
		})
		if err == nil {
			var rows []struct {
				Avg float64 `bson:"avg"`
			}
			if err := cur.All(ctx, &rows); err != nil {
				slog.Warn("首页统计：判题时长窗口读取失败，均值按 0 展示", "component", "web", "error", err)
			} else if len(rows) > 0 {
				avg = rows[0].Avg
			}
			cur.Close(ctx)
		} else {
			slog.Warn("首页统计：平均判题时长聚合失败，均值按 0 展示", "component", "web", "error", err)
		}

		st = &siteStats{
			ProblemCount:     pc,
			UserCount:        uc,
			TodaySubmissions: ts,
			AvgJudgeMS:       avg,
		}
		if pc > 0 {
			st.Featured = h.pickFeatured(ctx, time.Now().Format("2006-01-02"), 0)
		}
		statsMu.Lock()
		statsCache, statsCacheAt = st, time.Now()
		statsMu.Unlock()
	}

	// 登录用户：按账号等级档推荐今日一题——种子 = 日期|uid（当天恒定），同档无题逐环外扩
	if logged {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		day := time.Now().Format("2006-01-02")
		seed := day + "|" + strconv.FormatInt(uid, 10)
		tier := h.userTier(ctx, uid)
		var personal *featuredProblem // 必须从 nil 起跑：初值若取 st.Featured 会因非 nil 跳过整个外扩循环
		tried := [11]bool{}
		for ring := 0; ring < 10 && personal == nil; ring++ {
			for _, d := range [2]int{tier - ring, tier + ring} {
				if d < 1 || d > 10 || tried[d] {
					continue
				}
				tried[d] = true
				if fp := h.pickFeatured(ctx, seed, d); fp != nil {
					personal = fp
					break
				}
			}
		}
		if personal == nil {
			personal = h.pickFeatured(ctx, seed, 0) // 全档皆空兜底全池
		}
		copied := *st
		copied.Featured = personal
		st = &copied
	}
	ok(c, st)
}

// GetMyProblemStats 我的做题统计（v0.19 题库副列 v3 卡）：AC 题数 / 可见题总数 /
// 提交数 / AC 率 / 最近 7 天提交分布。登录即可看自己的（无敏感面）。
// 分母按观看者视角（2026-10-27）：持「题库全量可见」者含隐藏题，与题库页头「共 N 题」一致。
func (h *Handlers) GetMyProblemStats(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "请先登录")
		return
	}
	st, err := h.repo.MyProblemStats(c.Request.Context(), uid, hasPerm(c, repo.PermProblemViewAll))
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, st)
}
