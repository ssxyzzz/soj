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
	Bank  bool          // 主题库比赛（锁优先于班级考试、全局指引）
	Kind  string        // contest | exam
	RefID int64         // 比赛/考试 ID（前端指引跳转用）
}

// loadLockMap 当前视角下各题目的锁（生效语义：决定锁题指引与提交限制）。
// 未发布赛事数量小，每次请求全量评估。主题库比赛不锁 root；班级考试只锁本班成员。
func (h *Handlers) loadLockMap(ctx context.Context, uid int64, role string) (map[int64]*contestLock, error) {
	all, err := h.loadAllLocks(ctx)
	if err != nil {
		return nil, err
	}
	// 同一个班的多个锁题只查一次成员关系（v0.25 万题体检收敛 N+1：原逐 pid FindOne，
	// 每场考试的每道题都各查一次库，题库列表每请求都跑此段）
	memberOf := map[int64]bool{}
	if uid > 0 {
		for _, lk := range all {
			if !lk.Bank && !memberOf[lk.Round.ClassID] {
				member, _ := h.repo.IsMember(ctx, lk.Round.ClassID, uid)
				memberOf[lk.Round.ClassID] = member
			}
		}
	}
	out := make(map[int64]*contestLock, len(all))
	for pid, lk := range all {
		if lk.Bank {
			if role == repo.RoleRoot {
				continue // 主题库比赛对 root 不锁
			}
		} else {
			if uid <= 0 || !memberOf[lk.Round.ClassID] {
				continue
			}
		}
		out[pid] = lk
	}
	return out, nil
}

// loadLockMapView 锁信息的「展示视角」（题库列表/详情的锁定标记）：root 与内置 admin
// 不受锁题约束（生效语义见 loadLockMap），但要能看到每道题当前的比赛中/考试中状态，
// 因此这两类视角返回全量锁；其余视角与生效语义一致。
func (h *Handlers) loadLockMapView(ctx context.Context, uid int64, role string) (map[int64]*contestLock, error) {
	if role == repo.RoleRoot || role == repo.RoleAdmin {
		return h.loadAllLocks(ctx)
	}
	return h.loadLockMap(ctx, uid, role)
}

func (h *Handlers) loadAllLocks(ctx context.Context) (map[int64]*contestLock, error) {
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
		for _, cp := range ct.Problems {
			apply(cp.PID, &contestLock{Round: ct, Phase: phase, Bank: true, Kind: repo.KindContest, RefID: ct.ID})
		}
	}
	for _, e := range exams {
		phase := phaseOf(e.Start, e.End)
		for _, ep := range e.Problems {
			apply(ep.PID, &contestLock{Round: e, Phase: phase, Bank: false, Kind: repo.KindExam, RefID: e.ID})
		}
	}
	return out, nil
}

// bankContestOf 返回收录该题的主题库比赛（未发布即处于隐藏期，无则 nil）。
// 比赛隐藏期覆盖赛前/赛中/赛后：期间该题在各处入口统一引导进比赛上下文。
func (h *Handlers) bankContestOf(ctx context.Context, pid int64) *repo.Contest {
	contests, err := h.repo.ListUnpublishedContests(ctx)
	if err != nil {
		return nil
	}
	for _, ct := range contests {
		if _, ok := ct.ProblemScore(pid); ok {
			return ct
		}
	}
	return nil
}

// bankRunningContestOf 返回收录该题且正在进行中的主题库比赛（无则 nil）。
// 比赛优先级最高：比赛进行中时该题的提交通道收归比赛，考试通道关闭。
func (h *Handlers) bankRunningContestOf(ctx context.Context, pid int64) *repo.Contest {
	contests, err := h.repo.ListUnpublishedContests(ctx)
	if err != nil {
		return nil
	}
	now := time.Now()
	for _, ct := range contests {
		if ct.Status(now) != "running" {
			continue
		}
		if _, ok := ct.ProblemScore(pid); ok {
			return ct
		}
	}
	return nil
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
// 需登录（游客仅首页/题库/题目详情）；未开始（隐藏期赛前段）的比赛对
// 无「题库全量可见」且非组织者的视角不出现。
func (h *Handlers) ListContests(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	privileged := hasPerm(c, repo.PermProblemViewAll)
	list, err := h.repo.ListContests(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return
	}
	now := time.Now()
	items := make([]gin.H, 0, len(list))
	for _, ct := range list {
		if !privileged && ct.Status(now) == "scheduled" && (!logged || uid != ct.CreatedBy) && role != repo.RoleRoot {
			continue
		}
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
