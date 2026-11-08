package server

// 题单 AI 一键整理（v0.71）单测：叶子构建 / AI 输出解析（幻觉 key/批外 pid/重复丢弃）/
// run 校验链（形状/范围/AI 供应商/权限）+ 全流程（本地 OpenAI 兼容假 AI 服务器回放归类，
// fake 全程无 Mongo 照 handler_httptest 模式）——分组落位、组内难度升序（未设殿后）、
// 范围口径（隐藏/重复排除、include_mounted 已挂纳入且标 mounted、筛选只收窄题库拉取）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"github.com/gin-gonic/gin"

	"soj/internal/ai"
	"soj/internal/repo"
)

// ---- fake 补齐（题单 AI 一键整理用）----

func (f *fakeRepo) GetProblemList(_ context.Context, id int64) (*repo.ProblemList, error) {
	if l, ok := f.lists[id]; ok {
		return l, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) GetAIConfig(context.Context) (*ai.Config, error) {
	return f.aiCfg, nil
}

func (f *fakeRepo) GetTagTree(context.Context) ([]repo.TagNode, error) {
	return f.tagTreeNodes, nil
}

func (f *fakeRepo) RecordAiCall(context.Context, *repo.AiCall) {}

// fakeAIProvider 起一个 OpenAI 兼容本地假 AI 服务器：从 user prompt 抽题号（#N），
// 按 fn(pid) 回放归类 leaf（返回 "" = 不输出该题 → 落未匹配）；回包裹闲话 + json 围栏，
// 顺带测 extractJSON 剥壳。
func fakeAIProvider(t *testing.T, fn func(pid int64) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var user string
		for _, m := range req.Messages {
			if m.Role == "user" {
				user = m.Content
			}
		}
		var b strings.Builder
		b.WriteString(`{"assign":[`)
		first := true
		seen := map[int64]bool{}
		for _, m := range regexp.MustCompile(`#(\d+)`).FindAllStringSubmatch(user, -1) {
			var pid int64
			_, _ = fmt.Sscanf(m[1], "%d", &pid)
			if seen[pid] {
				continue
			}
			seen[pid] = true
			leaf := fn(pid)
			if leaf == "" {
				continue
			}
			if !first {
				b.WriteString(",")
			}
			first = false
			fmt.Fprintf(&b, `{"pid":%d,"leaf":"%s"}`, pid, leaf)
		}
		b.WriteString(`]}`)
		content := "好的，归类如下：\n```json\n" + b.String() + "\n```"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content}}},
			"usage":   map[string]any{"total_tokens": 42},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeAIConfig(baseURL string) *ai.Config {
	return &ai.Config{
		Providers: []ai.Provider{{Name: "test", BaseURL: baseURL, APIKey: "sk-test", Model: "test-model"}},
		Active:    "test",
	}
}

// registerOrganize 注册整理四端点（测试引擎同真路由：AuthRequired，handler 内查权限）。
func registerOrganize(r *gin.RouterGroup, h *Handlers) {
	r.POST("/api/lists/:id/organize/run", AuthRequired(h), h.ListOrganizeRun)
	r.GET("/api/lists/:id/organize/status", AuthRequired(h), h.ListOrganizeStatus)
	r.GET("/api/lists/:id/organize/suggestions", AuthRequired(h), h.ListOrganizeSuggestions)
	r.POST("/api/lists/:id/organize/cancel", AuthRequired(h), h.ListOrganizeCancel)
}

// waitOrganizeDone 轮询到终态（后台 goroutine，测试内 3s 超时）。
func waitOrganizeDone(r *gin.Engine, tok, path string) map[string]any {
	deadline := time.Now().Add(3 * time.Second)
	var snap map[string]any
	for time.Now().Before(deadline) {
		_, body := doJSON(r, "GET", path, tok, "")
		snap = body["data"].(map[string]any)
		if snap["state"] != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return snap
}

func organizeGroups(t *testing.T, data map[string]any) map[string][]map[string]any {
	t.Helper()
	out := map[string][]map[string]any{}
	for _, g := range asAnySlice(data["groups"]) {
		m := g.(map[string]any)
		items := make([]map[string]any, 0)
		for _, it := range asAnySlice(m["items"]) {
			items = append(items, it.(map[string]any))
		}
		out[m["key"].(string)] = items
	}
	return out
}

func TestListOrganizeLeaves(t *testing.T) {
	chapters := []repo.ListEntry{
		{Title: "基础", Desc: "语法起步", Problems: []int64{1}},
		{Title: "进阶", Desc: "专题深入", Children: []repo.ListEntry{{Title: "动态规划"}, {Title: "搜索", Desc: "DFS/BFS"}}},
	}
	leaves := listOrganizeLeaves(chapters)
	if len(leaves) != 3 {
		t.Fatalf("leaves = %d，期望 3（直挂章 + 两小节）", len(leaves))
	}
	want := []struct{ key, label, title, desc, path, chapDesc string }{
		{"ch:0", "第1章 基础", "基础", "语法起步", "基础", ""},
		{"sec:1-0", "2.1 动态规划", "动态规划", "", "进阶 › 动态规划", "专题深入"},
		{"sec:1-1", "2.2 搜索", "搜索", "DFS/BFS", "进阶 › 搜索", "专题深入"},
	}
	for i, w := range want {
		lf := leaves[i]
		if lf.Key != w.key || lf.Label != w.label || lf.Title != w.title || lf.Desc != w.desc || lf.Path != w.path || lf.ChapDesc != w.chapDesc {
			t.Fatalf("leaf[%d] = %+v，期望 %v", i, lf, w)
		}
	}
	// prompt 完整语境：路径 + 本叶描述 + 所属章描述
	prompt := listOrganizeLeavesPrompt(leaves)
	for _, want := range []string{
		"ch:0 基础 [语法起步]",
		"sec:1-0 进阶 › 动态规划（章：专题深入）",
		"sec:1-1 进阶 › 搜索 [DFS/BFS]（章：专题深入）",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt 缺少语境行 %q：\n%s", want, prompt)
		}
	}
}

func TestListOrganizeAssignParsing(t *testing.T) {
	keys := map[string]bool{"ch:0": true, "sec:1-0": true}
	batch := []*repo.Problem{{ID: 101}, {ID: 102}, {ID: 103}}
	content := "归类完成：\n```json\n{\"assign\":[{\"pid\":101,\"leaf\":\"ch:0\"}," +
		"{\"pid\":101,\"leaf\":\"sec:1-0\"}," + // 重复 pid 取首个
		"{\"pid\":102,\"leaf\":\"sec:9-9\"}," + // 幻觉 key 丢弃
		"{\"pid\":9999,\"leaf\":\"ch:0\"}," + // 批外 pid 丢弃
		"{\"pid\":103,\"leaf\":\"unmatched\"}]}\n```" // 显式未匹配 = 无映射
	m, err := parseListOrganizeAssign(content, batch, keys)
	if err != nil {
		t.Fatalf("parse = %v", err)
	}
	if len(m) != 1 || m[101] != "ch:0" {
		t.Fatalf("解析结果 = %v，期望仅 101→ch:0", m)
	}
	if _, err := parseListOrganizeAssign("完全不是 JSON", batch, keys); err == nil {
		t.Fatal("非 JSON 输出应报错")
	}
}

// TestListOrganizeContext 题单全局语境（v0.83.3）：标题/简介进 user prompt 顶部；
// 两者均空不加空段；user prompt 整体含「题单主题」行与章节结构。
func TestListOrganizeContext(t *testing.T) {
	if got := listOrganizeContext("", "  "); got != "" {
		t.Fatalf("全空应返回空串，得到 %q", got)
	}
	ctx := listOrganizeContext("基础算法复习（一）", "  按语法点分章  \n")
	if !strings.Contains(ctx, "题单主题：《基础算法复习（一）》 按语法点分章") {
		t.Fatalf("语境缺标题/简介：%q", ctx)
	}
	if got := listOrganizeContext("", "只有简介"); !strings.Contains(got, "只有简介") || strings.Contains(got, "《") {
		t.Fatalf("仅简介时不应带书名号：%q", got)
	}
	// user prompt 集成：语境在最前、后接章节结构与题目
	leaves := listOrganizeLeaves([]repo.ListEntry{{Title: "章一"}})
	user := listOrganizeUser(listOrganizeContext("复习单", ""), listOrganizeLeavesPrompt(leaves),
		[]*repo.Problem{{ID: 1, Title: "两数之和"}}, nil, nil)
	if !strings.Contains(user, "题单主题：《复习单》") || !strings.Contains(user, "ch:0 章一") || !strings.Contains(user, "#1 两数之和") {
		t.Fatalf("user prompt 组装不符：\n%s", user)
	}
}

// TestListOrganizeRunValidations run 校验链：chapters 必传且非空、形状校验、难度筛选
// 越界、AI 供应商未配置、非创建者 403、题单不存在 404。
func TestListOrganizeRunValidations(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.lists[7] = &repo.ProblemList{ID: 7, Title: "校验题单", CreatedBy: 2}
	f.problems[101] = &repo.Problem{ID: 101, Title: "题A"}
	h := newTestHandlers(f)
	r := testEngine(h)
	registerOrganize(r.Group(""), h)
	creator := f.addUser(h, 2, "creator", repo.RoleDefault, 0)
	other := f.addUser(h, 100, "other", repo.RoleDefault, 0)

	bad := `{"chapters":[{"title":"","problems":[]}]}`
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator, bad); code != 400 {
		t.Fatalf("空章标题应 400，得 %d", code)
	}
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator, `{"chapters":[]}`); code != 400 {
		t.Fatalf("空章节应 400，得 %d", code)
	}
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator,
		`{"chapters":[{"title":"第一章","problems":[]}],"difficulty":[11]}`); code != 400 {
		t.Fatalf("难度越界应 400，得 %d", code)
	}
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator,
		`{"chapters":[{"title":"第一章","problems":[]}]}`); code != 400 {
		t.Fatalf("AI 未配置应 400，得 %d", code)
	}
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", other,
		`{"chapters":[{"title":"第一章","problems":[]}]}`); code != 403 {
		t.Fatalf("非创建者应 403，得 %d", code)
	}
	if code, _ := doJSON(r, "POST", "/api/lists/999/organize/run", creator,
		`{"chapters":[{"title":"第一章","problems":[]}]}`); code != 404 {
		t.Fatalf("题单不存在应 404，得 %d", code)
	}
}

// TestListOrganizeFlow 全流程：假 AI 回放归类 → 分组落位与排序 → 范围口径
//（隐藏/重复排除、include_mounted、筛选收窄）→ suggestions 权限与状态门。
func TestListOrganizeFlow(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	srv := fakeAIProvider(t, func(pid int64) string {
		if pid == 104 {
			return "" // 104 无归属 → 未匹配
		}
		if pid%2 == 0 {
			return "sec:1-0"
		}
		return "ch:0"
	})
	f.aiCfg = fakeAIConfig(srv.URL)
	f.lists[7] = &repo.ProblemList{ID: 7, Title: "流程题单", CreatedBy: 2, Chapters: []repo.ListEntry{
		{Title: "基础"},
		{Title: "进阶", Children: []repo.ListEntry{{Title: "动态规划"}, {Title: "搜索"}}},
	}}
	f.lists[8] = &repo.ProblemList{ID: 8, Title: "无任务题单", CreatedBy: 2}
	// 题库：101(D2) 103(D5) → ch:0；102(D2) → sec:1-0；104(D0) → 未匹配；隐藏/重复排除
	f.problems[101] = &repo.Problem{ID: 101, Title: "题A", Difficulty: 2}
	f.problems[102] = &repo.Problem{ID: 102, Title: "题B", Difficulty: 2}
	f.problems[103] = &repo.Problem{ID: 103, Title: "题C", Difficulty: 5}
	f.problems[104] = &repo.Problem{ID: 104, Title: "无归属题"}
	f.problems[198] = &repo.Problem{ID: 198, Title: "重复题", Duplicate: true}
	f.problems[199] = &repo.Problem{ID: 199, Title: "隐藏题", Hidden: true}
	h := newTestHandlers(f)
	r := testEngine(h)
	registerOrganize(r.Group(""), h)
	creator := f.addUser(h, 2, "creator", repo.RoleDefault, 0)
	other := f.addUser(h, 100, "other", repo.RoleDefault, 0)
	body := func() string { return `{"chapters":[{"title":"基础"},{"title":"进阶","children":[{"title":"动态规划"},{"title":"搜索"}]}]}` }

	// 第一轮：全量
	if code, b := doJSON(r, "POST", "/api/lists/7/organize/run", creator, body()); code != 200 {
		t.Fatalf("run = %d %v", code, b)
	}
	snap := waitOrganizeDone(r, creator, "/api/lists/7/organize/status")
	if snap["state"] != "done" {
		t.Fatalf("终态 = %v，期望 done", snap)
	}
	if n, _ := snap["total"].(float64); n != 4 {
		t.Fatalf("total = %v，期望 4（隐藏/重复排除、104 计入）", snap["total"])
	}
	// 无本列表任务的其他列表 = idle
	_, b := doJSON(r, "GET", "/api/lists/8/organize/status", creator, "")
	if b["data"].(map[string]any)["state"] != "idle" {
		t.Fatalf("其他列表 status 应 idle，得 %v", b["data"])
	}
	// 非创建者拉建议 → 403
	if code, _ := doJSON(r, "GET", "/api/lists/7/organize/suggestions", other, ""); code != 403 {
		t.Fatalf("非创建者建议 = %d，期望 403", code)
	}
	_, b = doJSON(r, "GET", "/api/lists/7/organize/suggestions", creator, "")
	data := b["data"].(map[string]any)
	groups := organizeGroups(t, data)
	ch0 := groups["ch:0"]
	if len(ch0) != 2 || int64(ch0[0]["pid"].(float64)) != 101 || int64(ch0[1]["pid"].(float64)) != 103 {
		t.Fatalf("ch:0 = %v，期望 [101(D2), 103(D5)] 难度升序", groups["ch:0"])
	}
	sec := groups["sec:1-0"]
	if len(sec) != 1 || int64(sec[0]["pid"].(float64)) != 102 {
		t.Fatalf("sec:1-0 = %v，期望 [102]", groups["sec:1-0"])
	}
	if got := groups["sec:1-1"]; len(got) != 0 {
		t.Fatalf("sec:1-1 = %v，期望空组保留（前端直渲染）", got)
	}
	un := asAnySlice(data["unmatched"])
	if len(un) != 1 || int64(un[0].(map[string]any)["pid"].(float64)) != 104 {
		t.Fatalf("unmatched = %v，期望 [104]", un)
	}

	// 第二轮：难度筛选收窄（D2 → 只剩 101/102；筛选不作用后已换任务槽）
	if code, b := doJSON(r, "POST", "/api/lists/7/organize/run", creator,
		`{"chapters":[{"title":"基础"},{"title":"进阶","children":[{"title":"动态规划"},{"title":"搜索"}]}],"difficulty":[2]}`); code != 200 {
		t.Fatalf("run(筛选) = %d %v", code, b)
	}
	waitOrganizeDone(r, creator, "/api/lists/7/organize/status")
	_, b = doJSON(r, "GET", "/api/lists/7/organize/suggestions", creator, "")
	data = b["data"].(map[string]any)
	if n, _ := data["total"].(float64); n != 2 {
		t.Fatalf("筛选后 total = %v，期望 2", data["total"])
	}

	// 非法范围：起 > 止 → 400
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator,
		`{"chapters":[{"title":"基础"},{"title":"进阶","children":[{"title":"动态规划"},{"title":"搜索"}]}],"from":104,"to":102}`); code != 400 {
		t.Fatalf("from>to 应 400，得 %d", code)
	}

	// 第三轮：题号范围收窄（102–103 → 剩 102/103；含边界，快照带出任务边界）
	if code, b := doJSON(r, "POST", "/api/lists/7/organize/run", creator,
		`{"chapters":[{"title":"基础"},{"title":"进阶","children":[{"title":"动态规划"},{"title":"搜索"}]}],"from":102,"to":103}`); code != 200 {
		t.Fatalf("run(范围) = %d %v", code, b)
	}
	snap = waitOrganizeDone(r, creator, "/api/lists/7/organize/status")
	if snap["state"] != "done" {
		t.Fatalf("范围任务终态 = %v，期望 done", snap["state"])
	}
	if n, _ := snap["total"].(float64); n != 2 {
		t.Fatalf("范围后 total = %v，期望 2（102/103）", snap["total"])
	}
	if v, _ := snap["from"].(float64); v != 102 {
		t.Fatalf("快照 from = %v，期望 102", snap["from"])
	}
	if v, _ := snap["to"].(float64); v != 103 {
		t.Fatalf("快照 to = %v，期望 103", snap["to"])
	}
	_, b = doJSON(r, "GET", "/api/lists/7/organize/suggestions", creator, "")
	groups = organizeGroups(t, b["data"].(map[string]any))
	if len(groups["sec:1-0"]) != 1 || int64(groups["sec:1-0"][0]["pid"].(float64)) != 102 {
		t.Fatalf("范围后 sec:1-0 = %v，期望仅 102", groups["sec:1-0"])
	}
}

// TestListOrganizeMountedScope include_mounted 口径：false 时已挂全排除（保持原挂载）；
// true 时已挂全量纳入（含隐藏题、不受筛选收窄）且标 mounted=true。
func TestListOrganizeMountedScope(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	srv := fakeAIProvider(t, func(pid int64) string { return "sec:0-0" })
	f.aiCfg = fakeAIConfig(srv.URL)
	// 已挂：301 直挂章（含隐藏 302）；题库另有两题（303 正常、304 隐藏）
	f.lists[7] = &repo.ProblemList{ID: 7, Title: "已挂口径题单", CreatedBy: 2, Chapters: []repo.ListEntry{
		{Title: "章节", Children: []repo.ListEntry{{Title: "小节", Problems: []int64{301, 302}}}},
	}}
	f.problems[301] = &repo.Problem{ID: 301, Title: "已挂A", Difficulty: 4}
	f.problems[302] = &repo.Problem{ID: 302, Title: "已挂隐藏", Difficulty: 3, Hidden: true}
	f.problems[303] = &repo.Problem{ID: 303, Title: "库内题", Difficulty: 2}
	f.problems[304] = &repo.Problem{ID: 304, Title: "库内隐藏", Hidden: true}
	h := newTestHandlers(f)
	r := testEngine(h)
	registerOrganize(r.Group(""), h)
	creator := f.addUser(h, 2, "creator", repo.RoleDefault, 0)
	chs := `{"chapters":[{"title":"章节","children":[{"title":"小节","problems":[301,302]}]}]}`

	// include_mounted=false：范围 = 仅 303（已挂全排除、库内隐藏排除）
	if code, b := doJSON(r, "POST", "/api/lists/7/organize/run", creator, chs); code != 200 {
		t.Fatalf("run = %d %v", code, b)
	}
	snap := waitOrganizeDone(r, creator, "/api/lists/7/organize/status")
	if n, _ := snap["total"].(float64); n != 1 {
		t.Fatalf("include_mounted=false total = %v，期望 1（仅 303）", snap["total"])
	}
	_, b := doJSON(r, "GET", "/api/lists/7/organize/suggestions", creator, "")
	groups := organizeGroups(t, b["data"].(map[string]any))
	if len(groups["sec:0-0"]) != 1 || groups["sec:0-0"][0]["mounted"] == true {
		t.Fatalf("sec:0-0 = %v，期望仅 303 且未标 mounted", groups["sec:0-0"])
	}

	// include_mounted=true：范围 = 301 + 302（隐藏已挂也纳入）+ 303 = 3；筛选难度 [2] 不作用已挂
	if code, b := doJSON(r, "POST", "/api/lists/7/organize/run", creator,
		`{"chapters":[{"title":"章节","children":[{"title":"小节","problems":[301,302]}]}],"include_mounted":true,"difficulty":[2]}`); code != 200 {
		t.Fatalf("run(include_mounted) = %d %v", code, b)
	}
	snap = waitOrganizeDone(r, creator, "/api/lists/7/organize/status")
	if n, _ := snap["total"].(float64); n != 3 {
		t.Fatalf("include_mounted=true total = %v，期望 3（301/302/303）", snap["total"])
	}
	if v, _ := snap["include_mounted"].(bool); !v {
		t.Fatalf("快照 include_mounted = %v，期望 true", snap["include_mounted"])
	}
	_, b = doJSON(r, "GET", "/api/lists/7/organize/suggestions", creator, "")
	groups = organizeGroups(t, b["data"].(map[string]any))
	items := groups["sec:0-0"]
	if len(items) != 3 {
		t.Fatalf("sec:0-0 = %d 条，期望 3", len(items))
	}
	// 组内难度升序：303(D2) → 302(D3) → 301(D4)；302 mounted 标记
	if int64(items[0]["pid"].(float64)) != 303 || int64(items[1]["pid"].(float64)) != 302 || int64(items[2]["pid"].(float64)) != 301 {
		t.Fatalf("排序 = %v，期望 [303,302,301]", items)
	}
	if items[1]["mounted"] != true || items[2]["mounted"] != true || items[0]["mounted"] == true {
		t.Fatalf("mounted 标记异常：%v", items)
	}

	// 任务进行中拉建议 → 409：用阻塞假 AI 保持 running
	block := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"assign\":[]}"}}]}`))
	}))
	defer hang.Close()
	defer close(block)
	prev := f.aiCfg
	f.aiCfg = fakeAIConfig(hang.URL)
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator, chs); code != 200 {
		t.Fatal("阻塞 run 失败")
	}
	if code, _ := doJSON(r, "GET", "/api/lists/7/organize/suggestions", creator, ""); code != 409 {
		t.Fatalf("running 时 suggestions = %d，期望 409", code)
	}
	if code, _ := doJSON(r, "POST", "/api/lists/7/organize/run", creator, chs); code != 409 {
		t.Fatalf("进行中再发起 = %d，期望 409", code)
	}
	if code, b := doJSON(r, "POST", "/api/lists/7/organize/cancel", creator, ""); code != 200 || b["data"].(map[string]any)["cancel_requested"] != true {
		t.Fatalf("cancel = %d %v", code, b)
	}
	snap = waitOrganizeDone(r, creator, "/api/lists/7/organize/status")
	if snap["state"] != "cancelled" {
		t.Fatalf("取消后终态 = %v，期望 cancelled", snap["state"])
	}
	f.aiCfg = prev
}
