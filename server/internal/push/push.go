// Package push 提供 SSE 事件扇出（v0.98 全站后端推送）。
//
// 定位：进程内 Hub——web 进程持有，SSE 连接（/api/events）注册订阅者，事件源
// （change stream watcher、notify/race/task 钩子）调 ToUser/ToAll 扇出。
// judge-only 进程不 Init，包级函数自动 no-op，repo/judge 层钩子零负担。
// 事件只是信号（notify-then-fetch）：载荷极轻，客户端收到后走既有 GET 取数。
//
// 一切有界：每订阅者 64 容量缓冲通道，发送方非阻塞投递——慢消费者丢帧只置
// resync 标记（SSE 写出侧补发一条 resync 帧提示客户端整页重拉），绝不阻塞
// 发布方；连接寿命/心跳由 SSE handler 负责，本包只管扇出。
//
// 单副本边界（工程备忘口径）：订阅表是进程内状态，多副本部署时每个 web 副本
// 各自 Init + 各自接 change stream，各自服务自己的 SSE 连接——语义仍正确
// （每客户端恰好收到一份），无需共享存储。
package push

import (
	"sync"
	"sync/atomic"
)

// 事件主题（契约：与前端 useEventStream 订阅名逐字对齐，契约测试钉死）。
const (
	TopicSub    = "sub"    // 提交生命周期（per-uid：本人的；suball 见下）
	TopicSubAll = "suball" // 提交生命周期广播（特权者看全站列表用，发送期按权限过滤）
	TopicNotify = "notify" // 通知到达（per-uid：徽标/降临横幅）
	TopicRace   = "race"   // 课堂 PK / 挑战对局状态（广播，载荷带 raceId 供前端过滤）
	TopicTask   = "task"   // 后台任务进度（广播，发送期按 root 过滤）
	TopicResync = "resync" // 慢消费者丢帧后的补偿提示（客户端整页重拉）
)

// SubInfo 订阅者身份快照（连接建立时采集，发送期权限过滤用）。
type SubInfo struct {
	UID   int64
	Perms map[string]bool
}

// HasPerm 权限位查询（快照口径，连接期内不追新——8min 连接寿命自然刷新）。
// "*" 为 root 通配（SSE 订阅时写入，与 hasPerm 的代码级超角色语义对齐）。
func (s SubInfo) HasPerm(perm string) bool { return s.Perms[perm] || s.Perms["*"] }

// Event SSE 事件信封：topic 路由 + data 载荷（由 SSE 写出侧统一 JSON 序列化）。
type Event struct {
	Topic string
	Data  any
}

// RaceEvent 对局事件载荷（跨包双发布方共用唯一实现：server 的开播/取消 +
// repo 的惰性结算 + watcher 的榜单变化转发），键与前端 types.ts 逐字对齐。
type RaceEvent struct {
	RaceID int64  `json:"race_id"`
	Kind   string `json:"kind"` // start | settle | cancel | update（watcher 转发的榜单变化）
	SID    int64  `json:"sid,omitempty"`
}

// subscriber 单条 SSE 连接的订阅句柄：ch 由 SSE 写出 goroutine 独占消费，
// dropped 由发布方置位（丢帧）、写出侧发现后补发 resync 并复位。
type subscriber struct {
	info    SubInfo
	ch      chan Event
	dropped atomic.Bool
}

// Events 事件通道（仅 SSE 写出 goroutine 消费）。
func (s *subscriber) Events() <-chan Event { return s.ch }

// Dropped 消费丢帧标记并复位（true = 写出侧应先补发一条 resync 帧）。
func (s *subscriber) Dropped() bool { return s.dropped.Swap(false) }

// Hub 进程内订阅表。零值不可用，经 Init 创建。
type Hub struct {
	mu    sync.RWMutex
	byUID map[int64]map[*subscriber]struct{}
	all   map[*subscriber]struct{}
}

var std atomic.Pointer[Hub]

// Init 建全局 Hub（web 进程启动时调用一次；重复调用以最后一次为准）。
func Init() *Hub {
	h := &Hub{byUID: map[int64]map[*subscriber]struct{}{}, all: map[*subscriber]struct{}{}}
	std.Store(h)
	return h
}

// Subscribe 注册订阅者，返回句柄（SSE 连接建立时调用）。
func (h *Hub) Subscribe(info SubInfo) *subscriber {
	s := &subscriber{info: info, ch: make(chan Event, 64)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.all[s] = struct{}{}
	if h.byUID[info.UID] == nil {
		h.byUID[info.UID] = map[*subscriber]struct{}{}
	}
	h.byUID[info.UID][s] = struct{}{}
	return s
}

// Unsubscribe 注销（SSE 连接关闭时调用；幂等）。
func (h *Hub) Unsubscribe(s *subscriber) {
	if s == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.all[s]; !ok {
		return
	}
	delete(h.all, s)
	if m := h.byUID[s.info.UID]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(h.byUID, s.info.UID)
		}
	}
}

// deliver 非阻塞投递：通道满即丢帧置 resync 标记（发布方永不等待）。
func deliver(s *subscriber, ev Event) {
	select {
	case s.ch <- ev:
	default:
		s.dropped.Store(true)
	}
}

// ToUser 向指定用户的全部在线连接扇出（未 Init 时 no-op）。
func ToUser(uid int64, topic string, data any) {
	if h := std.Load(); h != nil {
		h.user(uid, Event{Topic: topic, Data: data})
	}
}

// ToAll 向全部在线连接广播；perm 非空时发送期按权限快照过滤。
func ToAll(topic string, data any, perm string) {
	if h := std.Load(); h != nil {
		h.broadcast(Event{Topic: topic, Data: data}, perm)
	}
}

func (h *Hub) user(uid int64, ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.byUID[uid] {
		deliver(s, ev)
	}
}

func (h *Hub) broadcast(ev Event, perm string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.all {
		if perm != "" && !s.info.HasPerm(perm) {
			continue
		}
		deliver(s, ev)
	}
}
