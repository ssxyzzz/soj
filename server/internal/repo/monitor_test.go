package repo

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// captureLogs 默认 logger 换成内存 handler（测试结束还原），回传缓冲区。
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

// 慢命令观测阈值行为：低于阈值静默、达到阈值打 Warn（v0.66 批①单测锁口径）。
func TestSlowOpMonitorThreshold(t *testing.T) {
	buf := captureLogs(t)

	m := slowOpMonitor()
	m.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: slowOpWarnThreshold - time.Millisecond,
			CommandName: "find", DatabaseName: "soj",
		},
	})
	if strings.Contains(buf.String(), "慢 Mongo 命令") {
		t.Fatalf("低于阈值不应告警：%s", buf.String())
	}

	m.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: slowOpWarnThreshold + time.Millisecond,
			CommandName: "aggregate", DatabaseName: "soj",
		},
	})
	out := buf.String()
	if !strings.Contains(out, "慢 Mongo 命令") || !strings.Contains(out, "aggregate") {
		t.Fatalf("达到阈值应告警并带命令名：%s", out)
	}
}

// 等待型 getMore（请求自带 maxTimeMS）：申报预算内返回不算慢；超出预算达常规阈值才告警
//（带 awaitMS 标出预算）。不带 maxTimeMS 的 getMore 照常规阈值判（v0.98.1 修生产日志刷屏）。
func TestSlowOpMonitorAwaitBudget(t *testing.T) {
	buf := captureLogs(t)
	m := slowOpMonitor()
	ctx := context.Background()
	// start 发一条 getMore（maxTimeMS ≤ 0 = 不带预算）；done 让它以指定耗时成功返回
	start := func(reqID, maxTimeMS int64) {
		t.Helper()
		doc := bson.M{"getMore": int64(7), "collection": "submissions"}
		if maxTimeMS > 0 {
			doc["maxTimeMS"] = maxTimeMS
		}
		cmd, err := bson.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal getMore: %v", err)
		}
		m.Started(ctx, &event.CommandStartedEvent{
			Command: cmd, DatabaseName: "soj", CommandName: "getMore", RequestID: reqID})
	}
	done := func(reqID int64, d time.Duration) {
		t.Helper()
		m.Succeeded(ctx, &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: d, CommandName: "getMore", DatabaseName: "soj", RequestID: reqID}})
	}
	expect := func(wantWarn bool, stage string) {
		t.Helper()
		if got := strings.Contains(buf.String(), "慢 Mongo 命令"); got != wantWarn {
			t.Fatalf("%s: 告警=%v, want %v\n%s", stage, got, wantWarn, buf.String())
		}
		buf.Reset()
	}

	start(1, 1000)
	done(1, 1004*time.Millisecond)
	expect(false, "申报 1000ms 预算、1004ms 返回")
	start(2, 1000)
	done(2, (time.Second + slowOpWarnThreshold - time.Millisecond))
	expect(false, "预算 + 常规阈值的临界内")
	start(3, 1000)
	done(3, time.Second+slowOpWarnThreshold)
	if out := buf.String(); !strings.Contains(out, "cmd=getMore") || !strings.Contains(out, "awaitMS=1000") {
		t.Fatalf("超出预算达常规阈值应告警并带预算：%s", out)
	}
	buf.Reset()
	start(4, 50)
	done(4, 50*time.Millisecond+slowOpWarnThreshold)
	expect(true, "小预算同样按「预算 + 常规阈值」判")
	start(5, 0)
	done(5, slowOpWarnThreshold+time.Millisecond)
	expect(true, "不带预算的 getMore 照常规阈值判")
	// Failed 清预算：同一 RequestID 之后不会再被当成等待型
	start(6, 1000)
	m.Failed(ctx, &event.CommandFailedEvent{CommandFinishedEvent: event.CommandFinishedEvent{
		CommandName: "getMore", DatabaseName: "soj", RequestID: 6}})
	done(6, slowOpWarnThreshold+time.Millisecond)
	expect(true, "失败的等待不残留预算")
}

// TestChangeStreamIdleAwaitNotSlow 真实变更流的空闲等待不出慢命令告警（v0.98.1 修生产
// 日志刷屏）。带计数的探针替身钉死两件事：等待型 getMore 确实在发生（防空转断言），
// 且生产调用点给 getMore 带上了申报预算 maxTimeMS——不带则条条告警，本用例即红。
func TestChangeStreamIdleAwaitNotSlow(t *testing.T) {
	buf := captureLogs(t)
	slow := slowOpMonitor()
	var idle atomic.Int64 // 完成且耗时接近等待预算的 getMore（= 一次空闲等待）
	counting := &event.CommandMonitor{
		Started: slow.Started,
		Succeeded: func(ctx context.Context, ev *event.CommandSucceededEvent) {
			if ev.CommandName == "getMore" && ev.Duration >= subWatchAwait*9/10 {
				idle.Add(1)
			}
			slow.Succeeded(ctx, ev)
		},
		Failed: slow.Failed,
	}
	cli := dialTestMongo(t, func(o *options.ClientOptions) { o.SetMonitor(counting) })
	db := cli.Database(fmt.Sprintf("soj_monitor_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		_ = db.Drop(dctx)
		_ = cli.Disconnect(dctx)
	})
	// 只接上监听用到的集合（WatchSubmissionChanges 仅依赖 submissions）
	r := &Repo{cli: cli, db: db, submissions: db.Collection("submissions")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan SubSignal, 8)
	watchDone := make(chan error, 1)
	go func() { watchDone <- r.WatchSubmissionChanges(ctx, func(s SubSignal) { seen <- s }) }()
	// 流就绪：写一条文档并等到它的事件（游标已建立并在迭代）
	insert(t, r, bson.M{"_id": int64(80), "uid": int64(8), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now()})
	waitFor(t, seen, func(s SubSignal) bool { return s.SID == 80 })
	// 空闲等数个预算周期：期间只会有等待型 getMore
	time.Sleep(3*subWatchAwait + 300*time.Millisecond)
	cancel()
	select {
	case <-watchDone:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后监听应退出")
	}
	if n := idle.Load(); n < 2 {
		t.Fatalf("空闲等待型 getMore 应完成 ≥ 2 次, got %d", n)
	}
	if out := buf.String(); strings.Contains(out, "cmd=getMore") {
		t.Fatalf("预算内的空闲等待不应报慢命令：%s", out)
	}
	// 对照：捕获链路有效——不带预算的 getMore 达阈值照常告警
	slow.Succeeded(context.Background(), &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{
		Duration: slowOpWarnThreshold + time.Millisecond, CommandName: "getMore", DatabaseName: "soj", RequestID: 1 << 40}})
	if out := buf.String(); !strings.Contains(out, "cmd=getMore") {
		t.Fatalf("普通慢 getMore 应告警：%s", out)
	}
}

// 探针回调挂在每条 Mongo 命令上（热点路径）：普通命令（find，非等待型）的单次回调开销。
func BenchmarkSlowOpStarted(b *testing.B) {
	m := slowOpMonitor()
	cmd, _ := bson.Marshal(bson.M{"find": "users", "filter": bson.M{"_id": 1}})
	ev := &event.CommandStartedEvent{Command: cmd, DatabaseName: "soj", CommandName: "find", RequestID: 1}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Started(context.Background(), ev)
	}
}

func BenchmarkSlowOpSucceeded(b *testing.B) {
	m := slowOpMonitor()
	ev := &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{
		Duration: time.Millisecond, CommandName: "find", DatabaseName: "soj", RequestID: 1}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Succeeded(context.Background(), ev)
	}
}
