package server

// 用户公开主页（v0.17，plan §2.9）：GET /users/:id/profile，登录可见（未登录由路由
// 守卫弹登录）。本人与他人统一视图，无私有区块；聚合只读，口径与既有公开面一致
//（详见 repo/profile.go 注释）。隐藏题剔除按观察者权限位 problem.view.all 现判——
// 无权限者的计数卡与列表同步缩减，等级经验不受影响（等级聚合不剔隐藏）。

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// UserProfile 用户公开主页聚合数据。
func (h *Handlers) UserProfile(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	u, err := h.repo.GetUserByID(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取失败")
		return
	}

	// 资料区（v0.23 起按本人公开开关下发）：学校/年级/性别/QQ 逐字段判定——
	// 关掉的整字段省略（前端 v-if 渲染）；存量未配置沿用 v0.17 口径（性别/学校/年级公开、QQ 私密）
	vis := u.EffProfileVis()
	out := gin.H{
		"id":         u.ID,
		"username":   u.Username,
		"nickname":   u.Nickname,
		"created_at": u.CreatedAt,
	}
	if vis.School {
		out["school"] = u.School
	}
	if vis.Grade {
		out["grade"] = u.Grade
	}
	if vis.RegionPublic() {
		out["region"] = u.Region
	}
	if vis.Gender {
		out["gender"] = u.Gender
	}
	if vis.QQ {
		out["qq"] = u.QQ
	}
	// QQ 头像（v0.23）：绑定开关开且填了 QQ 号时下发头像直链（QQ 号本身可保持私密）；
	// 前端有 avatar_url 用图，否则回退首字头像
	if u.QQAvatar && u.QQ != "" {
		out["avatar_url"] = "https://q1.qlogo.cn/g?b=qq&nk=" + u.QQ + "&s=100"
	}

	// 等级（徽章 + 距下一级进度）与全站排名：聚合失败降级省略，不阻塞主页
	if lv := h.levelBadgeInfo(c, id); lv != nil {
		out["level"] = lv
	}
	if rank, solved, err := h.repo.RankMe(ctx, id); err == nil {
		out["rank"] = gin.H{"rank": rank, "solved": solved}
	}

	// 统计卡：提交总数（排除 Cancelled 作废件）/ AC 条数 / AC 率
	total, accepted, err := h.repo.UserSubmissionStats(ctx, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "统计失败")
		return
	}
	acRate := 0.0
	if total > 0 {
		acRate = float64(accepted) / float64(total)
	}
	out["stats"] = gin.H{"submissions": total, "accepted": accepted, "ac_rate": acRate}

	// 近期提交 20 条（published；投影不带代码/明细，点击进提交详情走既有权限规则）
	if recent, err := h.repo.UserRecentSubmissions(ctx, id, 20); err == nil {
		items := make([]gin.H, 0, len(recent))
		for _, s := range recent {
			items = append(items, gin.H{
				"id": s.ID, "pid": s.ProblemID, "title": s.ProblemTitle,
				"status": s.Status, "score": s.Score, "lang": s.Language,
				"created_at": s.CreatedAt,
			})
		}
		out["recent"] = items
	}

	// 首次 AC 清单（全量一次下发，时间窗前端本地切）；隐藏题对无全量可见权限的观察者整条剔除
	if first, err := h.repo.UserFirstAccepted(ctx, id); err == nil {
		seeHidden := hasPerm(c, repo.PermProblemViewAll)
		list := make([]gin.H, 0, len(first))
		for _, e := range first {
			if e.Hidden && !seeHidden {
				continue
			}
			list = append(list, gin.H{
				"pid": e.PID, "title": e.Title, "difficulty": e.Difficulty,
				"hidden": e.Hidden, "first_ac_at": e.FirstACAt,
			})
		}
		out["first_ac"] = list
	}

	// 比赛历史：只含比赛；状态随时段（进行中/成绩未公布/已公布），名次仅在场次
	// 发布后计算——赛时与隐藏期绝不从主页侧漏名次（口径沿用）。
	// 成绩表关闭（scoreboard=false）的场次只显示参与不出名次，尊重组织者设定。
	if contests, err := h.repo.UserContests(ctx, id); err == nil {
		now := time.Now()
		rounds := make([]gin.H, 0, len(contests))
		for _, ct := range contests {
			row := gin.H{
				"id": ct.ID, "title": ct.Title,
				"status": ct.Status(now),
				"start":  ct.Start, "end": ct.End,
			}
			switch {
			case ct.Status(now) != "ended":
				// 进行中（有提交必已开赛，scheduled 仅防御性保留）
			case !ct.Published:
				row["result"] = "hidden" // 已结束、隐藏期未过：成绩未公布
			default:
				row["result"] = "published"
				if ct.Scoreboard {
					if rows, err := h.repo.RoundLeaderboard(ctx, repo.KindContest, ct.ID, roundFullScores(ct)); err == nil {
						row["total_users"] = len(rows)
						for i, r := range rows {
							if r.UID == id {
								row["rank"] = i + 1
								row["score"] = r.Total
								break
							}
						}
					}
				}
			}
			rounds = append(rounds, row)
		}
		out["contests"] = rounds
	}

	ok(c, out)
}
