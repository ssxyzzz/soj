package server

// 站点公开统计（v0.19 首页对齐 v3 预览）：收录题目 / 注册选手 / 今日提交 / 平均判题时长
// + 今日一题（按日随机一道可见题，同一天恒定）。只读聚合、60s 内存缓存，首页访客共享。

import (
	"context"
	"hash/fnv"
	"net/http"
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
	ProblemCount     int64           `json:"problem_count"`
	UserCount        int64           `json:"user_count"`
	TodaySubmissions int64           `json:"today_submissions"`
	AvgJudgeMS       float64         `json:"avg_judge_ms"`
	Featured         *featuredProblem `json:"featured"`
}

var (
	statsMu      sync.Mutex
	statsCache   *siteStats
	statsCacheAt time.Time
)

// GetSiteStats 公开站点统计（免登录，全局 IP 限流已覆盖）；60 秒缓存防聚合压库。
func (h *Handlers) GetSiteStats(c *gin.Context) {
	statsMu.Lock()
	defer statsMu.Unlock()
	if statsCache != nil && time.Since(statsCacheAt) < time.Minute {
		ok(c, statsCache)
		return
	}

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
		if err := cur.All(ctx, &rows); err == nil && len(rows) > 0 {
			avg = rows[0].Avg
		}
		cur.Close(ctx)
	}

	// 今日一题：日期字符串 FNV 哈希对可见题数取模，按 _id 稳定排序跳取——同一天恒定同一题
	st := &siteStats{
		ProblemCount:     pc,
		UserCount:        uc,
		TodaySubmissions: ts,
		AvgJudgeMS:       avg,
	}
	if pc > 0 {
		hseed := fnv.New32a()
		_, _ = hseed.Write([]byte(time.Now().Format("2006-01-02")))
		skip := int64(hseed.Sum32()) % pc
		fcur, err := h.repo.Problems.Find(ctx, visible,
			options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetSkip(skip).SetLimit(1))
		if err == nil {
			var rows []struct {
				ID         int64   `bson:"_id"`
				Title      string  `bson:"title"`
				Difficulty int     `bson:"difficulty,omitempty"`
				Tags       []int64 `bson:"tags"`
			}
			if err := fcur.All(ctx, &rows); err == nil && len(rows) > 0 {
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
				// 通过率（AC / 总提交，题库同口径）；两次 count 在 60s 缓存内可忽略
				total, _ := h.repo.Submissions.CountDocuments(ctx, bson.M{"pid": fp.ID})
				if total > 0 {
					ac, _ := h.repo.Submissions.CountDocuments(ctx, bson.M{"pid": fp.ID, "status": "Accepted"})
					fp.AcceptRate = float64(ac) / float64(total) * 100
				}
				st.Featured = fp
			}
			fcur.Close(ctx)
		}
	}

	statsCache = st
	statsCacheAt = time.Now()
	ok(c, st)
}

// GetMyProblemStats 我的做题统计（v0.19 题库副列 v3 卡）：AC 题数 / 可见题总数 /
// 提交数 / AC 率 / 最近 7 天提交分布。登录即可看自己的（无敏感面）。
func (h *Handlers) GetMyProblemStats(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "请先登录")
		return
	}
	st, err := h.repo.MyProblemStats(c.Request.Context(), uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	ok(c, st)
}
