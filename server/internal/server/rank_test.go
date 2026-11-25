package server

// 排行榜快照（v0.100）单测：缓存策略（微缓存命中不重算 / 并发单飞 / 失败回退旧快照 /
// 失败静默期 / 冷启动失败 / 无数据源 / 命中不等在途重算 / 等待方可放弃）、默认序与
// 坚持折算、接口契约（整表 + me）、主页名次与榜单同链同源、game-rank 路由退役。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// ---- 取数面替身 ----

// fakeRankSource rankCache 取数面的替身：预设原始行/造错，统计重算次数，可阻塞模拟慢查询。
type fakeRankSource struct {
	mu      sync.Mutex
	rows    []repo.RankRow
	err     error
	cfgErr  error
	adjErr  error
	calls   int
	limit   int // 最近一次重算收到的全榜上限
	gate    chan struct{} // 非 nil 时重算阻塞到它关闭
	started chan struct{} // 非 nil 时每次重算进入后发一个信号（带缓冲）
}

func (f *fakeRankSource) RankBoard(ctx context.Context, limit int) ([]repo.RankRow, error) {
	f.mu.Lock()
	f.calls++
	f.limit = limit
	rows, err, gate, started := f.rows, f.err, f.gate, f.started
	f.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	return rows, err
}

func (f *fakeRankSource) GetLevelConfig(context.Context) (*repo.LevelConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return repo.DefaultLevelConfig(), f.cfgErr
}

func (f *fakeRankSource) SumXpAdjust(context.Context, []int64) (map[int64]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[int64]int64{}, f.adjErr
}

func (f *fakeRankSource) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeRankSource) fail(err, cfgErr error) {
	f.mu.Lock()
	f.err, f.cfgErr = err, cfgErr
	f.mu.Unlock()
}

// setRankTTL 缩短微缓存窗口（用完还原）。
func setRankTTL(t *testing.T, d time.Duration) {
	t.Helper()
	old := rankFreshTTL
	rankFreshTTL = d
	t.Cleanup(func() { rankFreshTTL = old })
}

// rankRowsForOrder 默认序夹具（默认阈值 200/600/…；XP = 2^(难度−1)）：
// B 600×D1 = 600 Lv3；Lv2：E 50×D1+1×D10 = 562、J 400×D1 = 400、K 200×D2 = 400、
// A 200×D1 = 200、C 100×D2 = 200；Lv1：D、I 各 5×D3 = 20。
// 期望序 B, E, J, K, A, C, D, I——每条决胜规则都有一对只靠它分先后的行：
// 同级比经验（E 在 J/K 前）；同级同经验比解题数（J 400 > K 200，且解题数多者 uid 更大）；
// 三项全同比 uid（D 5 < I 9，A 1 < C 4）。
func rankRowsForOrder() []repo.RankRow {
	counts := func(idx int, n int64) [10]int64 {
		var c [10]int64
		c[idx] = n
		return c
	}
	eCounts := [10]int64{}
	eCounts[0], eCounts[9] = 50, 1
	return []repo.RankRow{
		{UID: 4, Solved: 100, Counts: counts(1, 100),
			Username: "c", Nickname: "丙", ComboCur: 0, ComboBest: 4, StreakDays: 6, StreakBest: 6,
			LastAcDay: beijingDayStr(time.Now().AddDate(0, 0, -1))},
		{UID: 1, Solved: 200, Counts: counts(0, 200),
			Username: "a", ComboCur: 3, ComboBest: 9, StreakDays: 10, StreakBest: 12,
			LastAcDay: beijingDayStr(time.Now().AddDate(0, 0, -3))},
		{UID: 5, Solved: 5, Counts: counts(2, 5)},
		{UID: 2, Solved: 600, Counts: counts(0, 600),
			Username: "b", Nickname: "乙", QQ: "12345", QQAvatar: true},
		{UID: 3, Solved: 51, Counts: eCounts,
			Username: "e", Nickname: "丁", ComboBest: 2, QQ: "23456"},
		{UID: 9, Solved: 5, Counts: counts(2, 5), Username: "i", Nickname: "戊"},
		{UID: 8, Solved: 400, Counts: counts(0, 400), Username: "j", Nickname: "己"},
		{UID: 6, Solved: 200, Counts: counts(1, 200), Username: "k", Nickname: "庚"},
	}
}

func beijingDayStr(t time.Time) string { return t.In(repo.BeijingZone()).Format("2006-01-02") }

// 快照默认序：等级 → 经验 → 解题数 → uid；坚持天数读时折算；账号缺失用户名兜底「-」；
// 行不带 rank（名次由前端按当前排序列算）；lookup 榜外 = 榜人数 + 1。
func TestRankSnapshotDefaultOrder(t *testing.T) {
	src := &fakeRankSource{}
	src.mu.Lock()
	src.rows = rankRowsForOrder()
	src.mu.Unlock()
	snap, err := newRankCache(src).get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got []int64
	for _, it := range snap.items {
		got = append(got, it.UserID)
	}
	if want := []int64{2, 3, 8, 6, 1, 4, 5, 9}; !equalInt64(got, want) {
		t.Fatalf("默认序 uid = %v, want %v", got, want)
	}
	if src.limit != rankBoardCap {
		t.Fatalf("重算传入的全榜上限 = %d，期望 %d", src.limit, rankBoardCap)
	}
	byUID := map[int64]rankItem{}
	for _, it := range snap.items {
		byUID[it.UserID] = it
	}
	if it := byUID[1]; it.Level != 2 || it.Xp != 200 || it.StreakDays != 0 || it.StreakBest != 12 || it.ComboCur != 3 {
		t.Fatalf("uid1 = %+v，期望 Lv2/200，断更折算 streak 0、历史坚持 12、连击 3", it)
	}
	if it := byUID[4]; it.StreakDays != 6 {
		t.Fatalf("uid4 streak = %d，期望 6（昨天有 AC 不折算）", it.StreakDays)
	}
	if it := byUID[5]; it.Username != "-" || it.Nickname != "-" || it.Level != 1 {
		t.Fatalf("账号缺失行 = %+v，期望用户名兜底 - 与 Lv1", it)
	}
	if it := byUID[3]; it.ComboCur != 0 || it.ComboBest != 2 {
		t.Fatalf("uid3 连击 = %+v，期望 0 / 2", it)
	}
	if r, s := snap.lookup(2); r != 1 || s != 600 {
		t.Fatalf("lookup(2) = (%d, %d)，期望 (1, 600)", r, s)
	}
	if r, s := snap.lookup(5); r != 7 || s != 5 {
		t.Fatalf("lookup(5) = (%d, %d)，期望 (7, 5)", r, s)
	}
	if r, s := snap.lookup(8); r != 3 || s != 400 {
		t.Fatalf("lookup(8) = (%d, %d)，期望 (3, 400)", r, s)
	}
	if r, s := snap.lookup(9); r != 8 || s != 5 {
		t.Fatalf("lookup(9) = (%d, %d)，期望 (8, 5)", r, s)
	}
	if r, s := snap.lookup(999); r != 9 || s != 0 {
		t.Fatalf("榜外 lookup = (%d, %d)，期望 (9, 0)", r, s)
	}
	raw, _ := json.Marshal(snap.items[0])
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"user_id", "username", "nickname", "level", "xp", "solved",
		"combo_cur", "combo_best", "streak_days", "streak_best"} {
		if _, ok := m[k]; !ok {
			t.Errorf("行 JSON 缺字段 %q：%s", k, raw)
		}
	}
	if _, ok := m["rank"]; ok {
		t.Errorf("行不应带 rank（名次由前端算）：%s", raw)
	}
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 榜单行 QQ 头像直链（v0.100.2）：设置页「QQ头像」开关开且填了 QQ 号才有，值与顶栏 /
// 公开主页同一直链单点；只填号没开开关、只开开关没填号、两者皆无都不带这个键（omitempty，
// 不给没开的人白搭字节）；QQ 号与开关的原始字段不随榜单行出仓。
func TestRankSnapshotAvatarURL(t *testing.T) {
	src := &fakeRankSource{}
	rows := []repo.RankRow{
		{UID: 1, Solved: 3, Username: "on", Nickname: "开且填号", QQ: "12345", QQAvatar: true},
		{UID: 2, Solved: 3, Username: "qqonly", Nickname: "只填号", QQ: "23456"},
		{UID: 3, Solved: 3, Username: "swonly", Nickname: "只开开关", QQAvatar: true},
		{UID: 4, Solved: 3, Username: "none", Nickname: "都没有"},
	}
	src.mu.Lock()
	src.rows = rows
	src.mu.Unlock()
	snap, err := newRankCache(src).get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(snap.items) != len(rows) {
		t.Fatalf("快照 %d 行，期望 %d", len(snap.items), len(rows))
	}
	for _, row := range rows {
		it := snap.items[snap.index[row.UID]]
		raw, _ := json.Marshal(it)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		want := row.QQAvatar && row.QQ != ""
		if (it.AvatarURL != "") != want {
			t.Errorf("uid %d 带头像 = %v，期望 %v：%s", row.UID, it.AvatarURL != "", want, raw)
		}
		if _, ok := m["avatar_url"]; ok != want {
			t.Errorf("uid %d JSON 带 avatar_url 键 = %v，期望 %v：%s", row.UID, ok, want, raw)
		}
		if want && it.AvatarURL != "https://q1.qlogo.cn/g?b=qq&nk=12345&s=100" {
			t.Errorf("uid %d 头像直链 = %q，期望与顶栏/公开主页同一条：%s", row.UID, it.AvatarURL, raw)
		}
		for _, k := range []string{"qq", "qq_avatar", "qqAvatar", "QQ", "QQAvatar"} {
			if _, ok := m[k]; ok {
				t.Errorf("uid %d 行 JSON 外泄原始字段 %q：%s", row.UID, k, raw)
			}
		}
	}
}

// 微缓存：窗口内不重算且同一份；窗口外重算换新。
func TestRankCacheMicroCache(t *testing.T) {
	setRankTTL(t, 40*time.Millisecond)
	src := &fakeRankSource{}
	rc := newRankCache(src)
	a, err := rc.get(context.Background())
	if err != nil || a == nil || src.count() != 1 {
		t.Fatalf("首次 get = %v err=%v calls=%d", a, err, src.count())
	}
	b, _ := rc.get(context.Background())
	if a != b || src.count() != 1 {
		t.Fatalf("窗口内应同一份且不重算：a==b=%v calls=%d", a == b, src.count())
	}
	time.Sleep(90 * time.Millisecond)
	c, _ := rc.get(context.Background())
	if c == a || src.count() != 2 {
		t.Fatalf("窗口外应重算：c==a=%v calls=%d", c == a, src.count())
	}
}

// 并发单飞：在途重算期间到达的请求只算一次，全部拿到同一份。
func TestRankCacheSingleFlight(t *testing.T) {
	src := &fakeRankSource{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	src.mu.Lock()
	src.rows = rankRowsForOrder()
	src.mu.Unlock()
	rc := newRankCache(src)
	const n = 20
	var entered, done sync.WaitGroup
	entered.Add(n)
	done.Add(n)
	results := make([]*rankSnapshot, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			entered.Done()
			results[i], errs[i] = rc.get(context.Background())
			done.Done()
		}(i)
	}
	entered.Wait()
	select {
	case <-src.started:
	case <-time.After(2 * time.Second):
		t.Fatal("重算未进入")
	}
	// 给其余请求时间撞上在途重算（它们只会等，不会再发起）
	time.Sleep(50 * time.Millisecond)
	close(src.gate)
	done.Wait()
	if src.count() != 1 {
		t.Fatalf("并发 %d 人只应重算 1 次，实际 %d", n, src.count())
	}
	for i := range results {
		if errs[i] != nil || results[i] != results[0] {
			t.Fatalf("请求 %d 结果与其他不同：err=%v same=%v", i, errs[i], results[i] == results[0])
		}
	}
}

// 重算失败：回退上一份快照（发起者与等待者同一份）；失败静默期内不再重算；静默期过后再试。
func TestRankCacheFallbackToStale(t *testing.T) {
	setRankTTL(t, 40*time.Millisecond)
	src := &fakeRankSource{}
	rc := newRankCache(src)
	first, err := rc.get(context.Background())
	if err != nil {
		t.Fatalf("首次 get: %v", err)
	}
	src.fail(errors.New("库不可用"), nil)
	time.Sleep(90 * time.Millisecond)
	// 静默期外第一次请求：发起重算失败 → 回退旧快照
	got, err := rc.get(context.Background())
	if err != nil || got != first || src.count() != 2 {
		t.Fatalf("失败应回退旧快照：same=%v err=%v calls=%d", got == first, err, src.count())
	}
	// 失败静默期内：直接回旧快照，不再重算
	if got, err = rc.get(context.Background()); err != nil || got != first || src.count() != 2 {
		t.Fatalf("静默期内不应再重算：same=%v err=%v calls=%d", got == first, err, src.count())
	}
	time.Sleep(90 * time.Millisecond)
	src.fail(nil, nil)
	if got, err = rc.get(context.Background()); err != nil || got == first || src.count() != 3 {
		t.Fatalf("静默期后应重算成功换新：same=%v err=%v calls=%d", got == first, err, src.count())
	}
}

// 冷启动即失败：没有旧快照可回，报错；静默期内不再重算。
func TestRankCacheColdStartFailure(t *testing.T) {
	setRankTTL(t, 40*time.Millisecond)
	src := &fakeRankSource{}
	src.fail(errors.New("库不可用"), nil)
	rc := newRankCache(src)
	if snap, err := rc.get(context.Background()); err == nil || snap != nil || src.count() != 1 {
		t.Fatalf("冷启动失败应报错：snap=%v err=%v calls=%d", snap, err, src.count())
	}
	if snap, err := rc.get(context.Background()); err == nil || snap != nil || src.count() != 1 {
		t.Fatalf("静默期内应直接报错不重算：snap=%v err=%v calls=%d", snap, err, src.count())
	}
	time.Sleep(90 * time.Millisecond)
	src.fail(nil, nil)
	if _, err := rc.get(context.Background()); err != nil || src.count() != 2 {
		t.Fatalf("静默期后应重算成功：err=%v calls=%d", err, src.count())
	}
}

// 等级配置读不到 = 整次重算失败回退旧快照（不出无等级的榜）。
func TestRankCacheConfigErrorFallsBack(t *testing.T) {
	setRankTTL(t, 40*time.Millisecond)
	src := &fakeRankSource{}
	rc := newRankCache(src)
	first, _ := rc.get(context.Background())
	src.fail(nil, errors.New("配置读失败"))
	time.Sleep(90 * time.Millisecond)
	if got, err := rc.get(context.Background()); err != nil || got != first {
		t.Fatalf("配置失败应回退：same=%v err=%v", got == first, err)
	}
}

// 经验调整读取失败不拖垮榜单：按全 0 出榜（xpAdjust 是软增强，与 levelAgg 同口径）。
func TestRankCacheXpAdjustFailureTolerated(t *testing.T) {
	src := &fakeRankSource{}
	src.mu.Lock()
	src.rows = rankRowsForOrder()
	src.adjErr = errors.New("xpAdjust 读失败")
	src.mu.Unlock()
	snap, err := newRankCache(src).get(context.Background())
	if err != nil || len(snap.items) != 8 {
		t.Fatalf("经验调整失败不应拖垮榜单：err=%v rows=%d", err, len(snap.items))
	}
}

// 无数据源（httptest 装配 / nil 缓存）：报错不 panic。
func TestRankCacheNoSource(t *testing.T) {
	if _, err := newRankCache(nil).get(context.Background()); !errors.Is(err, errRankNoSource) {
		t.Fatalf("nil 数据源应报 errRankNoSource，实际 %v", err)
	}
	var rc *rankCache
	if _, err := rc.get(context.Background()); !errors.Is(err, errRankNoSource) {
		t.Fatalf("nil 缓存应报 errRankNoSource，实际 %v", err)
	}
}

// 微缓存命中不等在途重算：锁只在读写指针时持有，重算在锁外。
func TestRankCacheHitNotBlockedByInflight(t *testing.T) {
	rc := newRankCache(&fakeRankSource{})
	rc.snap = &rankSnapshot{at: time.Now(), index: map[int64]int{}}
	rc.flight = &rankFlight{done: make(chan struct{})} // 永不关闭的在途重算
	done := make(chan *rankSnapshot, 1)
	go func() {
		s, _ := rc.get(context.Background())
		done <- s
	}()
	select {
	case s := <-done:
		if s != rc.snap {
			t.Fatalf("命中应直接回当前快照")
		}
	case <-time.After(time.Second):
		t.Fatal("微缓存命中被在途重算卡住")
	}
}

// 等待方可放弃：请求 ctx 结束时不再等在途重算，有旧快照回旧快照。
func TestRankCacheWaiterAbandonsOnCtxDone(t *testing.T) {
	setRankTTL(t, 40*time.Millisecond)
	src := &fakeRankSource{}
	rc := newRankCache(src)
	first, _ := rc.get(context.Background())
	time.Sleep(90 * time.Millisecond)
	// 首份快照到手后才上闸门：接下来发起者的重算会卡在闸门上
	gate := make(chan struct{})
	src.mu.Lock()
	src.gate, src.started = gate, make(chan struct{}, 1)
	src.mu.Unlock()
	ownerDone := make(chan struct{})
	go func() {
		rc.get(context.Background())
		close(ownerDone)
	}()
	select {
	case <-src.started:
	case <-time.After(2 * time.Second):
		t.Fatal("重算未进入")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := rc.get(ctx)
	if err != nil || got != first {
		t.Fatalf("放弃等待应回旧快照：same=%v err=%v", got == first, err)
	}
	close(gate)
	<-ownerDone
}

// ---- 接口契约 ----

func rankTestEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	authed := api.Group("", AuthRequired(h))
	authed.GET("/rank", h.Rank)
	authed.GET("/users/:id/profile", h.UserProfile)
	return r
}

// GET /api/rank：整表 + total + me；去参数（type/page 给了也无效）；未登录 401。
func TestRankHandlerContract(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	src := &fakeRankSource{}
	src.mu.Lock()
	src.rows = rankRowsForOrder()
	src.mu.Unlock()
	h.rank = newRankCache(src)
	r := rankTestEngine(h)

	tokIn := f.addUser(h, 4, "c", repo.RoleDefault, 0)
	tokOut := f.addUser(h, 77, "out", repo.RoleDefault, 0)
	if code, _ := doJSON(r, "GET", "/api/rank", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，期望 401", code)
	}
	code, body := doJSON(r, "GET", "/api/rank?type=level&page=3", tokIn, "")
	data, _ := body["data"].(map[string]any)
	if code != http.StatusOK || body["code"] != float64(0) || data == nil {
		t.Fatalf("已登录 = %d %v", code, body)
	}
	items, _ := data["items"].([]any)
	if len(items) != 8 || data["total"] != float64(8) {
		t.Fatalf("整表 = %d 行 total=%v，期望 8 / 8", len(items), data["total"])
	}
	first, _ := items[0].(map[string]any)
	if first["user_id"] != float64(2) || first["level"] != float64(3) {
		t.Fatalf("首行 = %v，期望默认序第一 uid 2（Lv3）", first)
	}
	// 头像直链（v0.100.2）：开了「QQ头像」的 uid 2 带，没开的 uid 3 不带键；原始字段不出仓
	if first["avatar_url"] != "https://q1.qlogo.cn/g?b=qq&nk=12345&s=100" {
		t.Fatalf("uid 2 行应带 QQ 头像直链：%v", first)
	}
	for _, it := range items {
		m, _ := it.(map[string]any)
		if _, has := m["avatar_url"]; has != (m["user_id"] == float64(2)) {
			t.Errorf("行 avatar_url 有无 = %v，期望只有 uid 2 带：%v", has, m)
		}
		for _, k := range []string{"qq", "qq_avatar", "qqAvatar"} {
			if _, ok := m[k]; ok {
				t.Errorf("榜单行不应外泄原始字段 %q：%v", k, m)
			}
		}
	}
	me, _ := data["me"].(map[string]any)
	if me["rank"] != float64(6) || me["solved"] != float64(100) {
		t.Fatalf("me = %v，期望名次 6 解题 100", me)
	}
	_, body = doJSON(r, "GET", "/api/rank", tokOut, "")
	data, _ = body["data"].(map[string]any)
	me, _ = data["me"].(map[string]any)
	if me["rank"] != float64(9) || me["solved"] != float64(0) {
		t.Fatalf("榜外 me = %v，期望名次 9 解题 0", me)
	}
}

// 同一个人的头像直链在榜单行与公开主页完全一致（同一个直链单点）：浏览器按同一条 URL
// 命中缓存，顶栏 / 主页 / 榜单看到的是同一张图；没开的人两边都不带键。
func TestRankAvatarSameAsProfile(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	src := &fakeRankSource{}
	src.mu.Lock()
	src.rows = []repo.RankRow{
		{UID: 1, Solved: 3, Username: "on", QQ: "12345", QQAvatar: true},
		{UID: 2, Solved: 3, Username: "qqonly", QQ: "23456"},
	}
	src.mu.Unlock()
	h.rank = newRankCache(src)
	r := rankTestEngine(h)
	f.addUser(h, 1, "on", repo.RoleDefault, 0)
	f.addUser(h, 2, "qqonly", repo.RoleDefault, 0)
	f.users[1].QQ, f.users[1].QQAvatar = "12345", true
	f.users[2].QQ = "23456"
	tok := f.addUser(h, 9, "viewer", repo.RoleDefault, 0)
	_, body := doJSON(r, "GET", "/api/rank", tok, "")
	data, _ := body["data"].(map[string]any)
	items, _ := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("榜单 %d 行，期望 2：%v", len(items), body)
	}
	for _, it := range items {
		m, _ := it.(map[string]any)
		uid := int64(m["user_id"].(float64))
		_, pbody := doJSON(r, "GET", "/api/users/"+strconv.FormatInt(uid, 10)+"/profile", tok, "")
		pdata, _ := pbody["data"].(map[string]any)
		if m["avatar_url"] != pdata["avatar_url"] {
			t.Fatalf("uid %d 头像直链榜单 %v ≠ 主页 %v", uid, m["avatar_url"], pdata["avatar_url"])
		}
	}
}

// 主页公开数据路径的替身（fakeRepo 未覆盖的只读聚合，主页 handler 需要）。
func (f *fakeRepo) UserSubmissionStats(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (f *fakeRepo) CountUserAcceptedAnswers(context.Context, int64) (int64, error) {
	return 0, nil
}
func (f *fakeRepo) UserRecentSubmissions(context.Context, int64, int) ([]*repo.Submission, error) {
	return nil, nil
}
func (f *fakeRepo) UserFirstAccepted(context.Context, int64) ([]repo.FirstACEntry, error) {
	return nil, nil
}
func (f *fakeRepo) UserContests(context.Context, int64) ([]*repo.Contest, error) { return nil, nil }

// 主页全站排名与榜单同链同源：榜内 = 默认序位置，榜外 = 榜人数 + 1；快照不可用省略字段。
func TestUserProfileRankSameChain(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	src := &fakeRankSource{}
	src.mu.Lock()
	src.rows = rankRowsForOrder()
	src.mu.Unlock()
	h.rank = newRankCache(src)
	r := rankTestEngine(h)
	for _, uid := range []int64{1, 2, 3, 4, 5, 6, 8, 9, 77} {
		f.addUser(h, uid, "u", repo.RoleDefault, 0)
	}
	tok := f.addUser(h, 1, "u", repo.RoleDefault, 0)
	cases := map[int64][2]int64{2: {1, 600}, 3: {2, 51}, 8: {3, 400}, 6: {4, 200}, 1: {5, 200},
		4: {6, 100}, 5: {7, 5}, 9: {8, 5}, 77: {9, 0}}
	for uid, want := range cases {
		_, body := doJSON(r, "GET", "/api/users/"+strconv.FormatInt(uid, 10)+"/profile", tok, "")
		data, _ := body["data"].(map[string]any)
		rk, _ := data["rank"].(map[string]any)
		if rk == nil || rk["rank"] != float64(want[0]) || rk["solved"] != float64(want[1]) {
			t.Fatalf("uid %d 主页 rank = %v，期望 (%d, %d)", uid, rk, want[0], want[1])
		}
	}
	// 快照不可用：字段省略，主页其余照常
	h.rank = newRankCache(nil)
	_, body := doJSON(r, "GET", "/api/users/2/profile", tok, "")
	data, _ := body["data"].(map[string]any)
	if _, has := data["rank"]; has || data["username"] == nil {
		t.Fatalf("快照不可用应省略 rank 且主页照常：%v", data)
	}
}

// /api/game-rank 随四榜合一退役（连击/坚持随 /api/rank 整表下发）。
func TestGameRankRouteRetired(t *testing.T) {
	raw, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("读 server.go: %v", err)
	}
	if strings.Contains(string(raw), "/game-rank") {
		t.Fatal("server.go 仍注册 /game-rank——四榜合一后该路由应退役")
	}
}
