package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const (
	rankBoardCap    = 10000           // 全榜上限（防病态规模；班级/社团部署实际远小于此）
	rankLoadTimeout = 3 * time.Second // 单次重算的时间上限
)

// rankFreshTTL 快照微缓存窗口：窗口内的请求不重算（重算失败后的静默期同此）。
// var 而非 const：缓存策略单测要缩短窗口。
var rankFreshTTL = 2 * time.Second

// 排行榜快照（v0.100 四榜合一）：全站一张表，打开排名页时整表下发，排序、过滤、名次
// 全在前端内存做——后端只负责让「这一次拉取」打开即新、花费有界。
//
// 缓存策略 = 同步重算 + 2 秒微缓存 + 单飞合并：快照超出微缓存窗口的请求触发一次重算
// 并等它（今日量级约 20ms）；并发只算一次，其余等这一次——同一时刻打开的人看到同一份。
// 原先的 30s SWR（过期先回旧榜、后台再刷）对「打开即走」的页面不合适：没有下一个请求
// 来接新榜，空闲后的首个访问者必看旧榜。锁只在读写快照指针与在途重算句柄时持有，重算
// 在锁外——不重演持锁聚合致榜单页串行卡。重算失败或超时回退上一份快照，没有旧快照才
// 报错；失败后同样隔一个微缓存窗口才再试，库不可用时请求不会个个卡满重算超时。
type rankCache struct {
	mu         sync.Mutex
	src        rankSource
	snap       *rankSnapshot // 最近一份成功快照（微缓存命中源；重算失败时的回退）
	flight     *rankFlight   // 在途重算（单飞）
	retryAfter time.Time     // 重算失败后的静默期：此前不再发起重算，直接回退
	lastErr    error         // 静默期内无快照可回时上报的错因
}

// rankSource 快照重算所需的 repo 面（生产 = *repo.Repo；单测注入替身计数/造错）。
type rankSource interface {
	RankBoard(ctx context.Context, limit int) ([]repo.RankRow, error)
	GetLevelConfig(ctx context.Context) (*repo.LevelConfig, error)
	SumXpAdjust(ctx context.Context, uids []int64) (map[int64]int64, error)
}

var errRankNoSource = errors.New("排行榜数据源未就绪")

// rankFlight 一次在途重算：首个请求发起，并发请求等 done 后取同一结果。
type rankFlight struct {
	done chan struct{}
	snap *rankSnapshot
	err  error
}

func newRankCache(src rankSource) *rankCache { return &rankCache{src: src} }

// rankItem 快照行：排序链要用的全部列。xp 仅作排序副键随行下发、前端不展示。
// avatar_url = QQ 头像直链（v0.100.2）：设置页「QQ头像」开关开且填了 QQ 号才带，
// 与顶栏、公开主页同一条规则、同一个直链单点；没开的人不带这个键，前端回退首字头像。
type rankItem struct {
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"`
	Nickname   string `json:"nickname"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	Level      int    `json:"level"`
	Xp         int64  `json:"xp"`
	Solved     int64  `json:"solved"`
	ComboCur   int    `json:"combo_cur"`
	ComboBest  int    `json:"combo_best"`
	StreakDays int    `json:"streak_days"`
	StreakBest int    `json:"streak_best"`
}

// rankSnapshot 一次重算的产物（不可变，读方共享）：items 按默认序排好，index 供按 uid
// 取名次。
type rankSnapshot struct {
	at    time.Time
	items []rankItem
	index map[int64]int
}

// lookup 观察者名次（默认序上的位置，从 1 起）与解题数；不在表内 → 名次 = 榜人数 + 1、
// 解题数 0（所有有 AC 的用户都排在前面）。
func (s *rankSnapshot) lookup(uid int64) (rank int, solved int64) {
	if i, ok := s.index[uid]; ok {
		return i + 1, s.items[i].Solved
	}
	return len(s.items) + 1, 0
}

// rankBefore 默认序（严格序无并列）：等级降序 → 经验降序 → 解题数降序 → uid 升序。
func rankBefore(a, b rankItem) bool {
	if a.Level != b.Level {
		return a.Level > b.Level
	}
	if a.Xp != b.Xp {
		return a.Xp > b.Xp
	}
	if a.Solved != b.Solved {
		return a.Solved > b.Solved
	}
	return a.UserID < b.UserID
}

// get 取快照：微缓存窗口内（或失败静默期内有旧快照）直接回；有在途重算则等它；否则由
// 本请求发起重算。等待受调用方 ctx 约束（请求断开/超时即放弃等待，回退旧快照）。
func (rc *rankCache) get(ctx context.Context) (*rankSnapshot, error) {
	if rc == nil || rc.src == nil {
		return nil, errRankNoSource
	}
	rc.mu.Lock()
	if snap := rc.snap; snap != nil && time.Since(snap.at) < rankFreshTTL {
		rc.mu.Unlock()
		return snap, nil
	}
	if time.Now().Before(rc.retryAfter) {
		snap, err := rc.snap, rc.lastErr
		if snap != nil {
			err = nil
		}
		rc.mu.Unlock()
		return snap, err
	}
	if f := rc.flight; f != nil {
		rc.mu.Unlock()
		select {
		case <-f.done:
			return f.snap, f.err
		case <-ctx.Done():
			return rc.fallback(ctx.Err())
		}
	}
	f := &rankFlight{done: make(chan struct{})}
	rc.flight = f
	rc.mu.Unlock()

	snap, err := rc.load()
	rc.mu.Lock()
	if err == nil {
		rc.snap, rc.retryAfter, rc.lastErr = snap, time.Time{}, nil
	} else {
		rc.retryAfter, rc.lastErr = time.Now().Add(rankFreshTTL), err
		if stale := rc.snap; stale != nil {
			snap, err = stale, nil // 回退旧快照：发起者与等待者拿到同一份
		}
	}
	rc.flight = nil
	f.snap, f.err = snap, err
	rc.mu.Unlock()
	close(f.done)
	if err != nil {
		slog.Warn("排行榜重算失败", "component", "rank", "error", err)
	}
	return snap, err
}

// fallback 放弃等待（请求断开/超时）时的回退：有旧快照回旧快照，没有才把错因报给调用方。
func (rc *rankCache) fallback(cause error) (*rankSnapshot, error) {
	rc.mu.Lock()
	snap := rc.snap
	rc.mu.Unlock()
	if snap != nil {
		return snap, nil
	}
	return nil, cause
}

// load 重算一份快照。用独立 ctx：结果服务所有等待者，不随发起请求断开而作废。
func (rc *rankCache) load() (*rankSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), rankLoadTimeout)
	defer cancel()
	rows, err := rc.src.RankBoard(ctx, rankBoardCap)
	if err != nil {
		return nil, err
	}
	// 等级是默认序主键：配置读不到宁可整次失败回退旧快照，不出无等级的榜
	cfg, err := rc.src.GetLevelConfig(ctx)
	if err != nil {
		return nil, err
	}
	uids := make([]int64, 0, len(rows))
	for _, r := range rows {
		uids = append(uids, r.UID)
	}
	// 经验调整失败按全 0（与 levelAgg 同口径：xpAdjust 是软增强，不可用不拖垮榜单）
	adjs, _ := rc.src.SumXpAdjust(ctx, uids)
	now := time.Now()
	items := make([]rankItem, 0, len(rows))
	for _, r := range rows {
		lv, xp, _, _, _ := repo.LevelProgress(cfg, r.Counts, adjs[r.UID])
		it := rankItem{
			UserID: r.UID, Username: r.Username, Nickname: r.Nickname,
			Level: lv, Xp: xp, Solved: r.Solved,
			ComboCur: r.ComboCur, ComboBest: r.ComboBest,
			StreakDays: repo.EffectiveStreakDays(r.StreakDays, r.LastAcDay, now),
			StreakBest: r.StreakBest,
		}
		if r.QQAvatar && r.QQ != "" {
			it.AvatarURL = qqAvatarURL(r.QQ)
		}
		if it.Username == "" {
			it.Username, it.Nickname = "-", "-" // 账号已不存在
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return rankBefore(items[i], items[j]) })
	snap := &rankSnapshot{at: time.Now(), items: items, index: make(map[int64]int, len(items))}
	for i := range items {
		snap.index[items[i].UserID] = i
	}
	return snap, nil
}

// Rank 排行榜：整表一次下发，排序、连击/坚持列的「只列大于 0」与名次全由前端按同一份
// 快照在内存算——规则只有一份，不会两边算得不一样。me = 观察者在默认序上的名次与
// 解题数，供只要一个数字的轻量消费方（题库页头卡）使用。
func (h *Handlers) Rank(c *gin.Context) {
	snap, err := h.rank.get(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取排行榜失败")
		return
	}
	data := gin.H{"items": snap.items, "total": len(snap.items), "me": nil}
	if uid, _, _, logged := currentUser(c); logged {
		rank, solved := snap.lookup(uid)
		data["me"] = gin.H{"rank": rank, "solved": solved}
	}
	ok(c, data)
}

// rankOf 观察者全站排名（默认序上的位置）与解题数；快照不可用返回 false（调用方降级
// 省略字段）。与排行榜走同一个 get——主页名次与榜单名次一定一致。
func (h *Handlers) rankOf(ctx context.Context, uid int64) (gin.H, bool) {
	snap, err := h.rank.get(ctx)
	if err != nil {
		return nil, false
	}
	rank, solved := snap.lookup(uid)
	return gin.H{"rank": rank, "solved": solved}, true
}
