package push

import (
	"reflect"
	"testing"
)

func TestHubUserFanout(t *testing.T) {
	h := Init()
	a := h.Subscribe(SubInfo{UID: 1})
	b := h.Subscribe(SubInfo{UID: 1})
	c := h.Subscribe(SubInfo{UID: 2})
	defer func() { h.Unsubscribe(a); h.Unsubscribe(b); h.Unsubscribe(c) }()

	ToUser(1, TopicNotify, "x")

	// 同 uid 两个连接各得一份，别的 uid 不收
	for _, s := range []*subscriber{a, b} {
		select {
		case ev := <-s.Events():
			if ev.Topic != TopicNotify || ev.Data != "x" {
				t.Fatalf("载荷不符: %+v", ev)
			}
		default:
			t.Fatal("同 uid 连接未收到事件")
		}
	}
	select {
	case <-c.Events():
		t.Fatal("别的 uid 不应收到")
	default:
	}
}

func TestHubUnsubscribeIdempotent(t *testing.T) {
	h := Init()
	s := h.Subscribe(SubInfo{UID: 7})
	h.Unsubscribe(s)
	h.Unsubscribe(s) // 二次注销不 panic

	ToUser(7, TopicNotify, 1)
	select {
	case <-s.Events():
		t.Fatal("注销后不应再收事件")
	default:
	}
}

func TestHubBroadcastPermFilter(t *testing.T) {
	h := Init()
	plain := h.Subscribe(SubInfo{UID: 1, Perms: map[string]bool{}})
	priv := h.Subscribe(SubInfo{UID: 2, Perms: map[string]bool{"submission.view.all": true}})
	defer func() { h.Unsubscribe(plain); h.Unsubscribe(priv) }()

	ToAll(TopicSubAll, "e1", "submission.view.all")
	ToAll(TopicRace, "e2", "")

	// 无权限连接只应收到全量广播（race），收不到权限过滤广播（suball）
	if ev := <-plain.Events(); ev.Topic != TopicRace {
		t.Fatalf("无权限连接收到的应是全量广播: %+v", ev)
	}
	if ev := <-priv.Events(); ev.Topic != TopicSubAll {
		t.Fatalf("特权连接应先收到过滤广播: %+v", ev)
	}
	// 无 perm 的广播人人有份
	if ev := <-priv.Events(); ev.Topic != TopicRace {
		t.Fatalf("全量广播漏发: %+v", ev)
	}
}

func TestHubSlowClientDropsAndResync(t *testing.T) {
	h := Init()
	s := h.Subscribe(SubInfo{UID: 1})
	defer h.Unsubscribe(s)

	// 通道容量 64：灌 100 条，只有前 64 条落袋，后续丢帧
	for i := 0; i < 100; i++ {
		ToUser(1, TopicSub, i)
	}
	if len(s.ch) != 64 {
		t.Fatalf("应满容丢弃，通道存量 = %d", len(s.ch))
	}
	if !s.Dropped() {
		t.Fatal("丢帧后应置 resync 标记")
	}
	if s.Dropped() {
		t.Fatal("Dropped 消费即复位")
	}
	// 丢帧期间发布方不阻塞（上面 100 次调用全量返回即为证明）；清空后恢复投递
	for n := len(s.ch); n > 0; n-- { // 非阻塞清空（通道不关闭，不能用 for range）
		<-s.ch
	}
	ToUser(1, TopicSub, "after")
	select {
	case ev := <-s.Events():
		if ev.Data != "after" {
			t.Fatalf("恢复投递载荷不符: %+v", ev)
		}
	default:
		t.Fatal("恢复投递失败")
	}
}

func TestNoInitNoOp(t *testing.T) {
	// std 未指向时包级函数静默 no-op（judge-only 进程路径）
	old := std.Swap(nil)
	defer std.Store(old)
	ToUser(1, TopicNotify, "x")
	ToAll(TopicRace, "x", "")
}

func TestTopicContract(t *testing.T) {
	// 契约：主题名与前端 useEventStream 订阅名逐字对齐，改名必须两端同步
	got := []string{TopicSub, TopicSubAll, TopicNotify, TopicRace, TopicTask, TopicResync}
	want := []string{"sub", "suball", "notify", "race", "task", "resync"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("主题契约漂移: %v", got)
	}
}
