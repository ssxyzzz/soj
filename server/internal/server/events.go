package server

// 全站后端推送（v0.98）：SSE 端点 + submissions change stream 消费。
//
// 事件只是信号（notify-then-fetch）：载荷极轻（sid/kind/进度计数），前端收到后
// 走既有 GET 取数——业务数据口径仍单源，轮询降级为兜底。鉴权复用 AuthRequired
// （fetch 流式读取可带 Authorization 头；滑动续期随重连自然发生）。
//
// 一切有界：25s 心跳注释帧（< http.Server IdleTimeout 2m / Caddy idle 3m）；
// 连接 8min 寿命上限（< Caddy write 10m）到期 event: bye 主动关闭、客户端立即
// 重连——长连接寿命有界且续期窗口前移。慢消费者丢帧由 push.Hub 置 resync 标记，
// 写出侧补发一条 resync 帧提示客户端整页重拉。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/push"
	"soj/internal/repo"
)

// SSE 时序参数（包级 var：单测可注入毫秒级）。
var (
	sseHeartbeat = 25 * time.Second
	sseMaxAge    = 8 * time.Minute
)

// Events GET /api/events —— SSE 事件流（登录态）：单连接推该用户有权看到的全部
// 主题，服务端按 uid/权限过滤，前端按载荷自行认领（无需按 sid 动态订阅）。
func (h *Handlers) Events(c *gin.Context) {
	uid, uname, _, ok := currentUser(c)
	if !ok {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	perms, _ := c.Get("perms")
	permList, _ := perms.([]string)
	permSet := make(map[string]bool, len(permList))
	for _, p := range permList {
		permSet[p] = true
	}
	if role, exists := c.Get("role"); exists {
		if r, _ := role.(string); r == repo.RoleRoot {
			permSet["*"] = true // root 快照口径与 hasPerm 对齐（代码级超角色）
		}
	}

	// 先订阅再写首帧：hello 帧到达客户端时订阅已生效——首帧与事件之间无丢帧窗口
	sub := h.hub.Subscribe(push.SubInfo{UID: uid, Perms: permSet})
	defer h.hub.Unsubscribe(sub)

	w := c.Writer
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no") // 反代（nginx 语义）禁缓冲；Caddy 侧配置文件已排除
	w.WriteHeader(http.StatusOK)
	writeSSE(w, "", push.Event{Topic: "hello", Data: gin.H{"uid": uid, "uname": uname}})
	w.Flush() // 头 + 首帧立即上线（fetch 的 Do() 等首字节，不 Flush 客户端挂起）

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	ageLimit := time.NewTimer(sseMaxAge)
	defer ageLimit.Stop()
	for {
		select {
		case <-c.Request.Context().Done(): // 客户端断开（http server 取消请求 ctx）
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ":ping\n\n") // 注释帧：保活 + 探活，客户端无感
			w.Flush()
		case <-ageLimit.C:
			writeSSE(w, "bye", push.Event{Topic: "bye", Data: gin.H{}}) // 寿命上限：主动关闭换新连接（续期窗口前移）
			w.Flush()
			return
		case ev := <-sub.Events():
			if sub.Dropped() {
				writeSSE(w, push.TopicResync, push.Event{Topic: push.TopicResync, Data: gin.H{}})
			}
			writeSSE(w, ev.Topic, ev)
			w.Flush()
		}
	}
}

// writeSSE 写一帧：event 行（缺省 topic 时省略——首帧 hello 带 retry 提示走 data）+ data 行。
func writeSSE(w gin.ResponseWriter, event string, ev push.Event) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return // 载荷序列化失败：丢帧保连接（信号有兜底轮询）
	}
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// ---- 提交变更消费（change stream → push） ----

// subEvent 提交事件载荷（契约：与前端 types.ts SSE 事件类型逐字对齐，契约测试钉死）。
type subEvent struct {
	SID       int64  `json:"sid"`
	Kind      string `json:"kind"` // status（状态/揭榜/重测/取消）| progress（逐点进度）
	Status    string `json:"status,omitempty"`
	RaceID    int64  `json:"race_id,omitempty"`
	Challenge int64  `json:"challenge,omitempty"`
	CaseDone  int    `json:"case_done,omitempty"`
	CaseTotal int    `json:"case_total,omitempty"`
	Stage     string `json:"stage,omitempty"` // compile | run
}

// raceEvent 对局事件载荷：见 push.RaceEvent（跨包双发布方共用唯一实现）。

// watchSubmissions web 进程生命周期内消费 submissions 变更流并扇出（server.Start 挂启）。
func (h *Handlers) watchSubmissions(ctx context.Context) {
	err := h.repo.WatchSubmissionChanges(ctx, h.onSubSignal)
	if err != nil && ctx.Err() == nil {
		slog.Warn("提交变更监听退出", "component", "push", "error", err)
	}
}

// onSubSignal 变更信号分类扇出。kind 由快照推断：Judging 且带进度/阶段字段为
// progress（逐点回写/SetCaseStage），其余（含 insert/终态/重测/取消/揭榜翻 published）
// 为 status——两类载荷前端都按「信号 → 复用 GET 取数」消费。
func (h *Handlers) onSubSignal(s repo.SubSignal) {
	// 隐藏期抑制（v0.97.29 揭榜语义对齐）：只有「赛期内隐藏（HideDuring）且未发布」
	// 的场次提交不发任何事件（防 owner 提前窥探终态）；非隐藏赛榜实时可见状态本就
	// 公开、揭榜翻转后（published=true 或已不在未发布底表）一律放行——判定走锁题
	// 底表 30s 进程缓存（unpublishedRounds），未命中一次列表读
	if h.roundHidesSubmissions(s) {
		return
	}
	ev := subEvent{SID: s.SID, Status: s.Status, RaceID: s.RaceID, Challenge: s.ChallengeID}
	if s.Status == repo.StatusJudging && (s.CaseDone > 0 || s.Stage != "") {
		ev.Kind = "progress"
		ev.CaseDone, ev.CaseTotal, ev.Stage = s.CaseDone, s.CaseTotal, s.Stage
	} else {
		ev.Kind = "status"
	}
	push.ToUser(s.UID, push.TopicSub, ev)
	push.ToAll(push.TopicSubAll, ev, repo.PermSubmissionViewAll)
	if s.RaceID != 0 {
		// 对局提交（含 judge 回写 raceMs 触发的榜单变化）：课堂 PK 看板 watchers 重拉
		push.ToAll(push.TopicRace, push.RaceEvent{RaceID: s.RaceID, Kind: "update", SID: s.SID}, "")
	}
}

// roundHidesSubmissions 场次提交的推送抑制判定：true = 不发任何事件。
// 练习/挑战恒 false；场次提交在 published 后恒 false；未发布场次查 HideDuring。
func (h *Handlers) roundHidesSubmissions(s repo.SubSignal) bool {
	if s.Published || (s.ContestID == 0 && s.ExamID == 0) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	contests, exams, err := h.unpublishedRounds(ctx)
	if err != nil {
		return true // 底表读不到时保守抑制（信号有兜底轮询，不丢正确性只丢即时性）
	}
	for _, c := range contests {
		if c.ID == s.ContestID {
			return c.HideDuring
		}
	}
	for _, e := range exams {
		if e.ID == s.ExamID {
			return e.HideDuring
		}
	}
	return false // 不在未发布底表 = 已发布
}

// taskEvent 后台任务进度事件（import/backup/tidy/organize 单任务骨架共用出口）。
type taskEvent struct {
	Job   string `json:"job"`   // import | backup | tidy | organize
	State string `json:"state"` // running | done | failed | cancelled
	Done  int    `json:"done,omitempty"`
	Total int    `json:"total,omitempty"`
}

// pushTask 后台任务进度扇出（perm 与各任务面板准入同口径；空 = 全员——organize
// 无权限位门（属主校验在 handler），载荷不含 listId，非属主客户端无从认领）。
func pushTask(job, state, perm string, done, total int) {
	push.ToAll(push.TopicTask, taskEvent{Job: job, State: state, Done: done, Total: total}, perm)
}
