package server

// SSE 事件流测试（v0.98 全站后端推送）：真 HTTP 流式读取（httptest.NewServer +
// 逐行收帧——Recorder 不适合长连接），覆盖——鉴权门槛、首帧信封、事件扇出、
// 寿命轮转 bye；外加三处中间件闸的契约（gzip 直通 / apitimeout 白名单 / slowlog
// 豁免）与 subEvent 载荷字段名、onSubSignal 隐藏期抑制契约（对齐前端 types.ts）。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/push"
	"soj/internal/repo"
)

// sseClient 一条真实流式连接：读协程把帧行送入 channel，供测试逐帧断言。
type sseClient struct {
	lines  chan string
	cancel context.CancelFunc
}

func openSSE(t *testing.T, ts *httptest.Server, token string) (*sseClient, *http.Response) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("SSE 连接状态 = %d", resp.StatusCode)
	}
	c := &sseClient{lines: make(chan string, 256), cancel: cancel}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	t.Cleanup(func() { resp.Body.Close(); cancel() })
	return c, resp
}

func (c *sseClient) nextLine(t *testing.T) string {
	t.Helper()
	select {
	case l, ok := <-c.lines:
		if !ok {
			t.Fatal("SSE 流已关闭")
		}
		return l
	case <-time.After(3 * time.Second):
		t.Fatal("等待 SSE 帧超时")
		return ""
	}
}

// waitClosed 断言流在超时前被服务端关闭（bye 帧后的主动关流）。
func (c *sseClient) waitClosed(t *testing.T, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-c.lines:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func newSSEServer(h *Handlers) *httptest.Server {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.GET("/events", AuthRequired(h), h.Events)
	return httptest.NewServer(r)
}

func TestSSERequiresAuth(t *testing.T) {
	f := baseFake()
	h := newTestHandlers(f)
	ts := newSSEServer(h)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，得到 %d", resp.StatusCode)
	}
}

func TestSSEStreamContract(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	ts := newSSEServer(h)
	t.Cleanup(ts.Close)
	tok := f.addUser(h, 11, "stu", repo.RoleDefault, 1)

	c, resp := openSSE(t, ts, tok)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type 应为 text/event-stream: %q", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("反代禁缓冲头缺失")
	}

	// 首帧 hello（writeSSE 缺省 event 名 → 只有 data 行 + 空行分隔）
	l := c.nextLine(t)
	if !strings.HasPrefix(l, "data: ") {
		t.Fatalf("首帧应为 hello data 行: %q", l)
	}
	var hello map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(l, "data: ")), &hello); err != nil {
		t.Fatalf("hello 帧解析: %v", err)
	}
	if hello["uid"] != float64(11) {
		t.Fatalf("hello 载荷 uid 不符: %v", hello)
	}
	if l := c.nextLine(t); l != "" {
		t.Fatalf("帧应以空行结束: %q", l)
	}

	// 服务端扇出一条 sub 事件 → 客户端应收到 event: sub + data 两行
	push.ToUser(11, push.TopicSub, subEvent{SID: 42, Kind: "status", Status: repo.StatusAccepted})
	if l := c.nextLine(t); l != "event: sub" {
		t.Fatalf("应为 sub 事件帧: %q", l)
	}
	l = c.nextLine(t)
	if !strings.HasPrefix(l, "data: ") {
		t.Fatalf("事件缺 data 行: %q", l)
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(l, "data: ")), &ev); err != nil {
		t.Fatalf("sub 载荷解析: %v", err)
	}
	// 载荷字段名契约（与前端 SubPushEvent 逐字对齐）
	for _, key := range []string{"sid", "kind", "status"} {
		if _, ok := ev[key]; !ok {
			t.Fatalf("sub 载荷缺字段 %q: %v", key, ev)
		}
	}
}

func TestSSEByeRotation(t *testing.T) {
	// 寿命轮转：短 maxAge 下应收到 event: bye 后被服务端关流（客户端随即重连——
	// 这里只验证服务端侧的「有界寿命 + bye 帧序」）
	oldAge := sseMaxAge
	sseMaxAge = 150 * time.Millisecond
	t.Cleanup(func() { sseMaxAge = oldAge })

	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	ts := newSSEServer(h)
	t.Cleanup(ts.Close)
	tok := f.addUser(h, 12, "stu2", repo.RoleDefault, 1)

	c, _ := openSSE(t, ts, tok)
	sawBye := false
	for l := c.nextLine(t); !sawBye; l = c.nextLine(t) {
		if l == "event: bye" {
			sawBye = true
		}
	}
	if !sawBye {
		t.Fatal("寿命上限应发 bye 帧")
	}
	if !c.waitClosed(t, time.Second) {
		t.Fatal("bye 后服务端应关闭连接")
	}
}

// ---- 中间件三闸契约 ----

func TestGzipSkipsSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Gzip())
	r.GET("/api/events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		c.String(http.StatusOK, "data: {\"hello\":1}\n\n")
	})
	r.GET("/api/json", func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.String(http.StatusOK, strings.Repeat("{\"pad\":\"0123456789\"},", 60))
	})
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	// SSE 路径：带 Accept-Encoding: gzip 也必须直通（不压缩、可直读）
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/events", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("SSE 流不应被压缩")
	}
	if !strings.Contains(string(body), "data: ") {
		t.Fatalf("SSE 流应明文可读: %q", body)
	}

	// 对照组：普通 JSON 大响应照常压缩（闸只放行 events 路径，不影响存量行为）
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/json", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("对照 JSON 应照常压缩: %q", resp2.Header.Get("Content-Encoding"))
	}
}

func TestAPITimeoutWhitelistsSSE(t *testing.T) {
	if !isLongRoute("/api/events") {
		t.Fatal("SSE 路由必须在 apitimeout 长路由白名单（否则 30s 掐断）")
	}
}

func TestSlowLogSkipsSSE(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(slowLogWith(time.Millisecond))
	r.GET("/api/events", func(c *gin.Context) { time.Sleep(5 * time.Millisecond) })
	r.GET("/api/slow", func(c *gin.Context) { time.Sleep(5 * time.Millisecond) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	if strings.Contains(buf.String(), "慢 HTTP 请求") {
		t.Fatal("SSE 连接不应打慢请求 Warn（8min 轮转会产生噪音）")
	}
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/slow", nil))
	if !strings.Contains(buf.String(), "慢 HTTP 请求") {
		t.Fatal("对照路径应照常打 Warn")
	}
}

// ---- onSubSignal 分类与隐藏期抑制 ----

func TestOnSubSignalHiddenRoundSuppressed(t *testing.T) {
	f := baseFake()
	// 锁题底表：赛 7 = 赛期内隐藏（OI 揭榜制），赛 8 = 榜实时可见（ICPC 制）
	f.unpubContests = []*repo.Contest{
		{ID: 7, HideDuring: true},
		{ID: 8, HideDuring: false},
	}
	h := newTestHandlers(f)
	sub := h.hub.Subscribe(push.SubInfo{UID: 21})
	defer h.hub.Unsubscribe(sub)

	// 隐藏赛未发布：任何信号都不发（防 owner 提前窥探终态，v0.97.29 口径）
	h.onSubSignal(repo.SubSignal{SID: 1, UID: 21, Status: repo.StatusAccepted, ContestID: 7})
	select {
	case <-sub.Events():
		t.Fatal("隐藏赛未发布的提交不应产生事件")
	default:
	}

	// 非隐藏赛（榜实时可见）未发布：状态本就公开，事件放行
	h.onSubSignal(repo.SubSignal{SID: 2, UID: 21, Status: repo.StatusAccepted, ContestID: 8})
	ev := <-sub.Events()
	data, err := json.Marshal(ev.Data)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["kind"] != "status" || m["status"] != repo.StatusAccepted {
		t.Fatalf("非隐藏赛终态应发 status 事件: %v", m)
	}

	// 隐藏赛已发布（揭榜翻转后 published=true）：放行
	h.onSubSignal(repo.SubSignal{SID: 3, UID: 21, Status: repo.StatusAccepted, ContestID: 7, Published: true})
	<-sub.Events()

	// 判题中带进度：progress 事件（kind/进度计数/stage 字段名对齐前端）
	h.onSubSignal(repo.SubSignal{SID: 4, UID: 21, Status: repo.StatusJudging, CaseDone: 3, CaseTotal: 10, Stage: "run"})
	ev = <-sub.Events()
	data, _ = json.Marshal(ev.Data)
	_ = json.Unmarshal(data, &m)
	if m["kind"] != "progress" || m["case_done"] != float64(3) || m["stage"] != "run" {
		t.Fatalf("逐点进度应发 progress 事件: %v", m)
	}
}

func TestOnSubSignalRaceTopic(t *testing.T) {
	f := baseFake()
	h := newTestHandlers(f)
	watcher := h.hub.Subscribe(push.SubInfo{UID: 99})
	defer h.hub.Unsubscribe(watcher)

	// raceId 提交终态：sub（本人）+ race（广播，看板 watchers）双发
	h.onSubSignal(repo.SubSignal{SID: 5, UID: 21, Status: repo.StatusAccepted, RaceID: 33})
	var gotRace bool
	for {
		ev := <-watcher.Events()
		if ev.Topic == push.TopicRace {
			gotRace = true
			data, _ := json.Marshal(ev.Data)
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			if m["race_id"] != float64(33) {
				t.Fatalf("race 载荷不符: %v", m)
			}
			break
		}
		if ev.Topic == push.TopicSub {
			t.Fatal("非本人不应收到 sub 事件（99 ≠ 21）")
		}
	}
	if !gotRace {
		t.Fatal("raceId 提交终态应广播 race 事件")
	}
}
