package server

// 挑战模式 + 自测 + 题内三榜（v0.23）。
//
//   - 挑战：start/status/quit 三接口；会话状态机在 repo（计时权威在服务端）。
//     提交联动走 CreateSubmission 的 challenge_id 回填，AC 终判挂在 judge 管道。
//   - 自测：POST /problems/:id/run 直跑沙箱（编译串行闸共用，运行独立信号量，
//     每人限流；无提交记录、不污染统计）。仅 stdio 传统题。
//   - 三榜：单一接口一次下发挑战榜（登录可见）+ 用时/内存榜（AC 门槛），30s 进程缓存。

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/lang"
	"soj/internal/repo"
)

// 自测资源口径：每人每分钟 10 次、全站并发 2、输入 1MB。
const (
	selfRunRatePerMin  = 10
	selfRunConcurrency = 2
	selfRunInputMax    = 1 << 20
)

// ---------- 挑战模式 ----------

// challengeEligibleFail 统一的可挑战性校验：题目资格 + 赛事锁题。通过返回 nil。
func (h *Handlers) challengeEligibleFail(c *gin.Context, p *repo.Problem) *string {
	if !p.ChallengeEligible() {
		s := "本题暂不支持挑战模式（仅限标准输入输出的传统题）"
		return &s
	}
	if len(p.Cases) == 0 {
		s := "本题暂无数据，无法挑战"
		return &s
	}
	uid, _, role, logged := currentUser(c)
	if logged {
		lockMap, err := h.loadLockMap(c.Request.Context(), uid, role)
		if err == nil && lockMap[p.ID] != nil {
			lk := lockMap[p.ID]
			what := "比赛"
			if lk.Kind == repo.KindExam {
				what = "考试"
			}
			s := "该题正在" + what + "中，暂不可挑战"
			return &s
		}
	}
	return nil
}

// StartChallenge 开始挑战：上一场 running 会话自动顶为退出，落新会话返回服务端时限。
func (h *Handlers) StartChallenge(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	p, err := h.repo.GetProblem(ctx, pid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	if msg := h.challengeEligibleFail(c, p); msg != nil {
		fail(c, http.StatusBadRequest, *msg)
		return
	}
	ch, err := h.repo.StartChallenge(ctx, uid, p)
	if err != nil {
		fail(c, http.StatusInternalServerError, "开始挑战失败")
		return
	}
	ok(c, gin.H{
		"id":          ch.ID,
		"started_at":  ch.StartedAt.UnixMilli(),
		"deadline_at": ch.DeadlineAt.UnixMilli(),
		"sec":         p.EffChallengeSec(),
	})
}

// challengeJSON 会话/记录的对外视图。running 态附 remaining_ms。
func challengeJSON(ch *repo.Challenge) gin.H {
	d := gin.H{
		"id":         ch.ID,
		"status":     ch.Status,
		"started_at": ch.StartedAt.UnixMilli(),
	}
	if ch.Status == repo.ChallengeRunning {
		d["deadline_at"] = ch.DeadlineAt.UnixMilli()
		d["remaining_ms"] = ch.DeadlineAt.UnixMilli() - time.Now().UnixMilli()
	} else {
		d["finished_at"] = ch.FinishedAt.UnixMilli()
		if ch.Status == repo.ChallengeAC {
			d["duration_ms"] = ch.DurationMS
			d["chars_per_sec"] = ch.CharsPerSec
		}
	}
	return d
}

// ChallengeStatus 挑战页刷新恢复：当前 running 会话（含剩余毫秒）+ 最近一份终态记录。
func (h *Handlers) ChallengeStatus(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	active, err := h.repo.ActiveChallenge(ctx, uid, pid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取挑战状态失败")
		return
	}
	last, err := h.repo.LastChallenge(ctx, uid, pid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取挑战状态失败")
		return
	}
	resp := gin.H{"active": gin.H{}}
	if active != nil {
		resp["active"] = challengeJSON(active)
	}
	if last != nil {
		resp["last"] = challengeJSON(last)
	}
	ok(c, resp)
}

// QuitChallenge 主动退出挑战。
func (h *Handlers) QuitChallenge(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	quit, err := h.repo.QuitChallenge(c.Request.Context(), uid, pid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "退出挑战失败")
		return
	}
	ok(c, gin.H{"quit": quit})
}

// ---------- 自测直跑 ----------

var selfTestGate = make(chan struct{}, selfRunConcurrency)

// SelfTestRun 自测：编译 + 按用户输入单次运行。不走判题队列、不留提交记录。
func (h *Handlers) SelfTestRun(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Input    string `json:"input"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if !lang.Exists(req.Language) {
		fail(c, http.StatusBadRequest, "不支持的语言")
		return
	}
	if len(req.Code) > maxCodeSize {
		fail(c, http.StatusBadRequest, "代码过长（上限 64KB）")
		return
	}
	if len(req.Input) > selfRunInputMax {
		fail(c, http.StatusBadRequest, "测试输入过长（上限 1MB）")
		return
	}
	// 每人限流（独立于提交限流的滑动窗口）
	if !h.runLimit.allow(uid, selfRunRatePerMin, time.Now()) {
		fail(c, http.StatusTooManyRequests, "自测过于频繁：每分钟最多 10 次")
		return
	}
	p, err := h.repo.GetProblem(c.Request.Context(), pid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	if !p.ChallengeEligible() {
		fail(c, http.StatusBadRequest, "本题暂不支持自测（仅限标准输入输出的传统题）")
		return
	}
	// 独立信号量：自测排队，不与真实判题抢 worker 名额（编译段仍受 compileGate 串行约束）
	select {
	case selfTestGate <- struct{}{}:
		defer func() { <-selfTestGate }()
	case <-c.Request.Context().Done():
		return
	}
	tctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	res, err := h.judger.SelfTestRun(tctx, req.Language, req.Code, req.Input, p.TimeLimitMS, p.MemoryLimitKB)
	if err != nil {
		if tctx.Err() != nil {
			fail(c, http.StatusGatewayTimeout, "自测超时，请检查程序是否死循环")
			return
		}
		fail(c, http.StatusInternalServerError, "自测运行失败")
		return
	}
	ok(c, gin.H{
		"status":    res.Status,
		"stdout":    res.Stdout,
		"stderr":    res.Stderr,
		"time_ms":   res.TimeMS,
		"mem_kb":    res.MemKB,
		"truncated": res.Truncated,
	})
}

// ---------- 题内三榜 ----------

// boardsCache 题内三榜进程缓存：TTL 30s，键为 pid。
type boardsCache struct {
	mu    sync.Mutex
	repo  *repo.Repo
	ttl   time.Duration
	items map[int64]boardsEntry
}

type boardsEntry struct {
	at   time.Time
	data gin.H
}

func newBoardsCache(r *repo.Repo) *boardsCache {
	return &boardsCache{repo: r, ttl: 30 * time.Second, items: map[int64]boardsEntry{}}
}

func (bc *boardsCache) get(ctx context.Context, pid int64) (gin.H, error) {
	bc.mu.Lock()
	if e, hit := bc.items[pid]; hit && time.Since(e.at) < bc.ttl {
		bc.mu.Unlock()
		return e.data, nil
	}
	bc.mu.Unlock()
	data, err := bc.load(ctx, pid)
	if err != nil {
		return nil, err
	}
	bc.mu.Lock()
	// 简单防膨胀：条目超 256 清一轮过期
	if len(bc.items) > 256 {
		for k, e := range bc.items {
			if time.Since(e.at) >= bc.ttl {
				delete(bc.items, k)
			}
		}
	}
	bc.items[pid] = boardsEntry{at: time.Now(), data: data}
	bc.mu.Unlock()
	return data, nil
}

func (bc *boardsCache) load(ctx context.Context, pid int64) (gin.H, error) {
	const limit = 50
	timeRows, err := bc.repo.ProblemEfficiencyBoard(ctx, pid, false, limit)
	if err != nil {
		return nil, err
	}
	memRows, err := bc.repo.ProblemEfficiencyBoard(ctx, pid, true, limit)
	if err != nil {
		return nil, err
	}
	chRows, err := bc.repo.ChallengeRank(ctx, pid, limit)
	if err != nil {
		return nil, err
	}
	// 三榜合并取昵称（一次批量查询）
	uidSet := map[int64]bool{}
	for _, r := range timeRows {
		uidSet[r.UID] = true
	}
	for _, r := range memRows {
		uidSet[r.UID] = true
	}
	for _, r := range chRows {
		uidSet[r.UID] = true
	}
	uids := make([]int64, 0, len(uidSet))
	for u := range uidSet {
		uids = append(uids, u)
	}
	users, err := bc.repo.GetUsersByIDs(ctx, uids)
	if err != nil {
		users = map[int64]*repo.User{}
	}
	nick := func(uid int64) string {
		if u, hit := users[uid]; hit {
			return u.Nickname
		}
		return ""
	}
	uname := func(uid int64) string {
		if u, hit := users[uid]; hit {
			return u.Username
		}
		return ""
	}
	timeItems := make([]gin.H, 0, len(timeRows))
	for i, r := range timeRows {
		timeItems = append(timeItems, gin.H{"rank": i + 1, "uid": r.UID, "nickname": nick(r.UID), "username": uname(r.UID), "value": r.Value, "lang": r.Lang, "submission_id": r.SubmissionID, "created_at": r.CreatedAt.UnixMilli()})
	}
	memItems := make([]gin.H, 0, len(memRows))
	for i, r := range memRows {
		memItems = append(memItems, gin.H{"rank": i + 1, "uid": r.UID, "nickname": nick(r.UID), "username": uname(r.UID), "value": r.Value, "lang": r.Lang, "submission_id": r.SubmissionID, "created_at": r.CreatedAt.UnixMilli()})
	}
	chItems := make([]gin.H, 0, len(chRows))
	for i, r := range chRows {
		chItems = append(chItems, gin.H{"rank": i + 1, "uid": r.UID, "nickname": nick(r.UID), "username": uname(r.UID), "duration_ms": r.DurationMS, "chars_per_sec": r.CharsPerSec, "lang": r.Lang, "created_at": r.CreatedAt.UnixMilli()})
	}
	return gin.H{"time": timeItems, "mem": memItems, "challenge": chItems}, nil
}

// ProblemBoards 三榜一次下发。时间/内存榜维持 AC 门槛（同所有代码）；挑战榜登录即可见。
func (h *Handlers) ProblemBoards(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	data, err := h.boards.get(ctx, pid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取榜单失败")
		return
	}
	canViewBoards := hasPerm(c, repo.PermSubmissionViewAll)
	if !canViewBoards {
		solved, err := h.repo.HasSolved(ctx, uid, pid)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取榜单失败")
			return
		}
		canViewBoards = solved
	}
	resp := gin.H{"can_view_boards": canViewBoards, "challenge": data["challenge"]}
	if canViewBoards {
		resp["time"] = data["time"]
		resp["mem"] = data["mem"]
	} else {
		resp["time"] = nil
		resp["mem"] = nil
	}
	ok(c, resp)
}
