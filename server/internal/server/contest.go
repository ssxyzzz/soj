package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ---------- 锁题评估 ----------
// 比赛/考试同构 handler 的合并实现见 round.go（v0.5.4 ①）；本文件保留
// 锁题评估、提交限速与主题库比赛列表。

// contestLock 对当前视角生效的题目锁。锁的传播范围跟赛事层级走：
// 主题库比赛对 root 以外的所有人生效；班级考试只对本班成员生效。
type contestLock struct {
	Round *repo.Contest // 比赛与考试同构，Kind 区分
	Phase string        // scheduled | running | hidden（已结束未发布）
	Bank  bool          // 主题库比赛（隐藏题赛中临时解锁、全局指引）
	Kind  string        // contest | exam
	RefID int64         // 比赛/考试 ID（前端指引跳转用）
}

// loadLockMap 当前视角下各题目的锁。未发布赛事数量小，每次请求全量评估。
func (h *Handlers) loadLockMap(ctx context.Context, uid int64, role string) (map[int64]*contestLock, error) {
	contests, err := h.repo.ListUnpublishedContests(ctx)
	if err != nil {
		return nil, err
	}
	exams, err := h.repo.ListUnpublishedExams(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := map[int64]*contestLock{}

	apply := func(pid int64, lk *contestLock) {
		old, ok := out[pid]
		// 主题库比赛优先于班级考试
		if !ok || (lk.Bank && !old.Bank) {
			out[pid] = lk
		}
	}
	phaseOf := func(start, end time.Time) string {
		p := ""
		switch {
		case now.Before(start):
			p = "scheduled"
		case now.Before(end):
			p = "running"
		default:
			p = "hidden"
		}
		return p
	}

	for _, ct := range contests {
		phase := phaseOf(ct.Start, ct.End)
		if role == repo.RoleRoot {
			continue // 主题库比赛对 root 不锁
		}
		for _, cp := range ct.Problems {
			apply(cp.PID, &contestLock{Round: ct, Phase: phase, Bank: true, Kind: repo.KindContest, RefID: ct.ID})
		}
	}
	for _, e := range exams {
		if uid <= 0 {
			continue
		}
		if member, _ := h.repo.IsMember(ctx, e.ClassID, uid); !member {
			continue
		}
		phase := phaseOf(e.Start, e.End)
		for _, ep := range e.Problems {
			apply(ep.PID, &contestLock{Round: e, Phase: phase, Bank: false, Kind: repo.KindExam, RefID: e.ID})
		}
	}
	return out, nil
}

// ---------- 提交限速（持 rate_exempt 权限位者豁免，上限 root 后台可调） ----------

type rateLimiter struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{hits: map[int64][]time.Time{}} }

func (l *rateLimiter) allow(uid int64, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	fresh := l.hits[uid][:0]
	for _, t := range l.hits[uid] {
		if now.Sub(t) < time.Minute {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= limit {
		l.hits[uid] = fresh
		return false
	}
	l.hits[uid] = append(fresh, now)
	return true
}

// ---------- 主题库比赛列表 ----------

// ListContests 主题库比赛列表（班级赛事见 /classes/:id/exams）。
func (h *Handlers) ListContests(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	list, err := h.repo.ListContests(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return
	}
	now := time.Now()
	items := make([]gin.H, 0, len(list))
	for _, ct := range list {
		item := gin.H{
			"id":            ct.ID,
			"title":         ct.Title,
			"status":        ct.Status(now),
			"start":         ct.Start,
			"end":           ct.End,
			"problem_count": len(ct.Problems),
			"is_organizer":  logged && uid == ct.CreatedBy,
		}
		if !ct.HiddenUntil.IsZero() {
			item["hidden_until"] = ct.HiddenUntil
		}
		items = append(items, item)
	}
	ok(c, items)
}
