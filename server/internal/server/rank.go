package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"soj/internal/model"
)

// Rank 从 Redis zset 读取解题排行榜（前 50 名）。
func (h *Handlers) Rank(c *gin.Context) {
	ctx := c.Request.Context()
	zs, err := h.rdb.ZRevRangeWithScores(ctx, model.RankKey, 0, 49).Result()
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取排行榜失败")
		return
	}

	type row struct {
		id     int64
		solved int
	}
	rows := make([]row, 0, len(zs))
	ids := make([]int64, 0, len(zs))
	for _, z := range zs {
		id, err := strconv.ParseInt(z.Member.(string), 10, 64)
		if err != nil {
			continue
		}
		rows = append(rows, row{id: id, solved: int(z.Score)})
		ids = append(ids, id)
	}

	users, err := model.GetUsersByIDs(ctx, ids)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取用户信息失败")
		return
	}

	items := make([]gin.H, 0, len(rows))
	for i, r := range rows {
		item := gin.H{"rank": i + 1, "user_id": r.id, "solved": r.solved}
		if u := users[r.id]; u != nil {
			item["username"] = u.Username
			item["nickname"] = u.Nickname
		} else {
			item["username"] = "-"
			item["nickname"] = "-"
		}
		items = append(items, item)
	}
	ok(c, items)
}
