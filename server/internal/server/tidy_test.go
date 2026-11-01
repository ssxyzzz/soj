package server

// 题库一键整理（v0.43）单测：纯函数（JSON 解析/通过率分档/唯一名映射）+ apply 校验链
// + meta 规则任务全流程（fake 全程无 Mongo，照 handler_httptest 模式）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/repo"
)

func TestExtractJSONAndParsers(t *testing.T) {
	// 围栏 + 闲话包裹的难度输出
	d, err := parseTidyDifficulty("好的，以下是评估结果：\n```json\n{\"difficulty\": 7}\n```")
	if err != nil || d != 7 {
		t.Fatalf("parseTidyDifficulty 围栏 = %d, %v，期望 7 nil", d, err)
	}
	if _, err := parseTidyDifficulty(`{"difficulty": 11}`); err == nil {
		t.Fatal("越界难度应报错")
	}
	if _, err := parseTidyDifficulty(`{"difficulty": 0}`); err == nil {
		t.Fatal("0（未设置）不是合法建议，应报错走兜底")
	}
	names, err := parseTidyNameList("```json\n{\"tags\":[\"动态规划\",\"单调队列\"]}\n```")
	if err != nil || len(names) != 2 || names[0] != "动态规划" {
		t.Fatalf("parseTidyNameList = %v, %v", names, err)
	}
}

func TestRateTier(t *testing.T) {
	cases := []struct {
		rate      float64
		attempted int64
		want      int
	}{
		{0, 0, 3},      // 无人做过 → D3
		{0.95, 20, 1},  // ≥0.9
		{0.85, 20, 2},  // ≥0.8
		{0.6, 20, 3},   // ≥0.5
		{0.35, 20, 4},  // ≥0.3
		{0.25, 20, 5},  // ≥0.2
		{0.12, 20, 6},  // ≥0.1
		{0.06, 20, 7},  // ≥0.05
		{0.03, 20, 8},  // ≥0.02
		{0.01, 20, 9},  // 其余
	}
	for _, c := range cases {
		if got := rateTier(c.rate, c.attempted); got != c.want {
			t.Errorf("rateTier(%v, %d) = %d，期望 %d", c.rate, c.attempted, got, c.want)
		}
	}
}

func TestUniqueTagLookup(t *testing.T) {
	tree := []repo.TagNode{
		{ID: 1, Name: "算法", Children: []repo.TagNode{
			{ID: 11, Name: "动态规划"},
			{ID: 12, Name: "搜索"},
		}},
		{ID: 2, Name: "数据结构", Children: []repo.TagNode{
			{ID: 21, Name: "动态规划"}, // 故意重名：两个大类下各有「动态规划」
		}},
	}
	uniq, ambiguous := uniqueTagLookup(tree)
	if uniq["搜索"] != 12 {
		t.Errorf("唯一名映射失败：搜索 = %d", uniq["搜索"])
	}
	if !ambiguous["动态规划"] {
		t.Fatal("重名「动态规划」应判歧义")
	}
	if _, ok := uniq["动态规划"]; ok {
		t.Fatal("歧义名不得进唯一映射")
	}
}

// ---- fake 补齐（仅本文件测试用到的接口方法）----

func (f *fakeRepo) TidyListProblems(context.Context) ([]*repo.Problem, error) {
	out := make([]*repo.Problem, 0, len(f.problems))
	for _, p := range f.problems {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeRepo) TidyPassRates(context.Context) (map[int64]float64, map[int64]int64, error) {
	return map[int64]float64{}, map[int64]int64{}, nil
}

func (f *fakeRepo) GetMetaTagTree(ctx context.Context) ([]repo.TagNode, error) {
	return f.metaTreeNodes, nil
}

func (f *fakeRepo) MatchMetaTags(_ context.Context, hint repo.SourceHint) []int64 {
	// 照真库语义：按名精确匹配 f.metaTreeNodes
	uniq, _ := uniqueTagLookup(f.metaTreeNodes)
	var ids []int64
	add := func(name string) {
		if id, ok := uniq[name]; ok {
			ids = append(ids, id)
		}
	}
	if hint.Year > 0 {
		add(itoa(int(hint.Year)))
	}
	add(hint.Region)
	add(hint.Source)
	return ids
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// UpdateProblemMeta 内存应用（apply 链测试用：只搬 tidy 场景的 Difficulty/Tags/MetaTags）
func (f *fakeRepo) UpdateProblemMeta(_ context.Context, pid int64, p repo.ProblemMetaPatch) error {
	prob, ok := f.problems[pid]
	if !ok {
		return mongo.ErrNoDocuments
	}
	if p.Difficulty != nil {
		prob.Difficulty = *p.Difficulty
	}
	if p.Tags != nil {
		prob.Tags = *p.Tags
	}
	if p.MetaTags != nil {
		prob.MetaTags = *p.MetaTags
	}
	return nil
}

// MarkTidied 内存水位（apply 后打点，断点续整测试用）
func (f *fakeRepo) MarkTidied(_ context.Context, kind string, pids []int64) error {
	for _, pid := range pids {
		if p, ok := f.problems[pid]; ok {
			if p.TidiedAt == nil {
				p.TidiedAt = map[string]time.Time{}
			}
			p.TidiedAt[kind] = time.Now()
		}
	}
	return nil
}

// ValidateTagRefs / ValidateMetaTagRefs 内存树校验（手动整理 apply 预检用，同真库报错口径）
func (f *fakeRepo) ValidateTagRefs(_ context.Context, ids []int64) error {
	return fakeValidateRefs(f.tagTreeNodes, ids, "标签节点 %d 不存在于标签树")
}

func (f *fakeRepo) ValidateMetaTagRefs(_ context.Context, ids []int64) error {
	return fakeValidateRefs(f.metaTreeNodes, ids, "来源节点 %d 不存在于来源标签树")
}

func fakeValidateRefs(tree []repo.TagNode, ids []int64, errFmt string) error {
	exists := map[int64]bool{}
	var walk func(list []repo.TagNode)
	walk = func(list []repo.TagNode) {
		for _, n := range list {
			exists[n.ID] = true
			walk(n.Children)
		}
	}
	walk(tree)
	for _, id := range ids {
		if !exists[id] {
			return fmt.Errorf(errFmt, id)
		}
	}
	return nil
}

func TestTidyApplyValidation(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.problems[3001] = &repo.Problem{ID: 3001, Title: "题A", Difficulty: 0}
	f.problems[3002] = &repo.Problem{ID: 3002, Title: "题B", Difficulty: 5}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/tidy/apply", AuthRequired(h), RequireRoot(), h.TidyApply)
	tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	// 难度越界 → 该项计失败不写库
	code, body := doJSON(r, "POST", "/api/admin/tidy/apply", tok,
		`{"kind":"difficulty","items":[{"pid":3001,"difficulty":11}]}`)
	if code != 200 || int(body["data"].(map[string]any)["failed"].(float64)) != 1 {
		t.Fatalf("越界难度 apply = %d %v，期望 failed=1", code, body)
	}
	if f.problems[3001].Difficulty != 0 {
		t.Fatalf("失败项不得写库，实际 difficulty=%d", f.problems[3001].Difficulty)
	}

	// 合法改判 → 写库 + 水位打点（下次默认跳过）
	code, body = doJSON(r, "POST", "/api/admin/tidy/apply", tok,
		`{"kind":"difficulty","items":[{"pid":3001,"difficulty":6},{"pid":3002,"difficulty":4}]}`)
	if code != 200 || int(body["data"].(map[string]any)["applied"].(float64)) != 2 {
		t.Fatalf("合法 apply = %d %v，期望 applied=2", code, body)
	}
	if f.problems[3001].Difficulty != 6 || f.problems[3002].Difficulty != 4 {
		t.Fatalf("改判未落库：%d %d", f.problems[3001].Difficulty, f.problems[3002].Difficulty)
	}
	if _, ok := f.problems[3001].TidiedAt["difficulty"]; !ok {
		t.Fatal("apply 后未打整理水位")
	}
}

func TestTidyRunMetaFlow(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.metaTreeNodes = []repo.TagNode{
		{ID: 500, Name: "洛谷"},
		{ID: 501, Name: "2025"},
		{ID: 502, Name: "瑶海区"},
	}
	f.problems[3101] = &repo.Problem{ID: 3101, Title: "[洛谷] A+B Problem"}                          // 有线索 → 重挂命中「洛谷」
	f.problems[3102] = &repo.Problem{ID: 3102, Title: "完全没有线索的题", MetaTags: []int64{9999}} // 无线索 + 树外孤儿 → 建议清孤儿（空集）
	f.problems[3103] = &repo.Problem{ID: 3103, Title: "也没有线索", MetaTags: []int64{500}}      // 无线索无孤儿 → 纯列出
	f.problems[3104] = &repo.Problem{ID: 3104, Title: "[洛谷] 已整理过的题",                        // 水位内 → 默认排除
		TidiedAt: map[string]time.Time{"meta": time.Now()}}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/tidy/run", AuthRequired(h), RequireRoot(), h.TidyRun)
	r.GET("/api/admin/tidy/status", AuthRequired(h), RequireRoot(), h.TidyStatus)
	tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	code, body := doJSON(r, "POST", "/api/admin/tidy/run", tok, `{"kind":"meta"}`)
	if code != 200 {
		t.Fatalf("tidy/run = %d %v", code, body)
	}
	// 轮询任务终态（后台 goroutine）
	deadline := time.Now().Add(3 * time.Second)
	var snap map[string]any
	for time.Now().Before(deadline) {
		_, body := doJSON(r, "GET", "/api/admin/tidy/status", tok, "")
		snap = body["data"].(map[string]any)
		if snap["state"] != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap["state"] != "done" {
		t.Fatalf("meta 任务终态 = %v（error=%v），期望 done", snap["state"], snap["error"])
	}
	sugs := snap["suggestions"].([]any)
	byP := map[int64]map[string]any{}
	for _, s := range sugs {
		m := s.(map[string]any)
		byP[int64(m["pid"].(float64))] = m
	}
	// 3101：标题线索「洛谷」命中 500（空集 + omitempty = 字段缺席，助手容错）
	ids := asAnySlice(byP[3101]["suggest_meta_ids"])
	if len(ids) != 1 || int64(ids[0].(float64)) != 500 {
		t.Fatalf("3101 建议 = %v，期望 [500]", ids)
	}
	// 3102：无线索但树外孤儿 9999 → 建议为空集（清孤儿）
	if s := byP[3102]; s["no_clue"] != true || len(asAnySlice(s["suggest_meta_ids"])) != 0 {
		t.Fatalf("3102 = %v，期望 no_clue + 空建议", s)
	}
	// 3103：无线索无孤儿 → 纯列出（suggest 字段缺席）
	if s := byP[3103]; s["no_clue"] != true || asAnySlice(s["suggest_meta_ids"]) != nil {
		t.Fatalf("3103 = %v，期望纯列出", s)
	}
	// 3104：水位内 → 默认排除（不出建议 + excluded 计数）
	if _, ok := byP[3104]; ok {
		t.Fatalf("3104 已整理应被水位排除，却出现在建议里")
	}
	if n, _ := snap["excluded"].(float64); n != 1 {
		t.Fatalf("excluded = %v，期望 1", snap["excluded"])
	}
}

func TestTidyRunRangeFilter(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.metaTreeNodes = []repo.TagNode{{ID: 500, Name: "洛谷"}}
	f.problems[3201] = &repo.Problem{ID: 3201, Title: "[洛谷] 范围外有线索"}                          // 范围外：不进任务、不计 excluded
	f.problems[3202] = &repo.Problem{ID: 3202, Title: "[洛谷] 范围内已整理", TidiedAt: map[string]time.Time{"meta": time.Now()}} // 范围内：水位排除计数
	f.problems[3203] = &repo.Problem{ID: 3203, Title: "[洛谷] 范围内正常"}                          // 范围内：正常出建议
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/tidy/run", AuthRequired(h), RequireRoot(), h.TidyRun)
	r.GET("/api/admin/tidy/status", AuthRequired(h), RequireRoot(), h.TidyStatus)
	tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	// 非法范围：起 > 止
	if code, _ := doJSON(r, "POST", "/api/admin/tidy/run", tok, `{"kind":"meta","from":9,"to":3}`); code != 400 {
		t.Fatalf("from>to 应 400，得 %d", code)
	}

	code, body := doJSON(r, "POST", "/api/admin/tidy/run", tok, `{"kind":"meta","from":3202,"to":3203}`)
	if code != 200 {
		t.Fatalf("tidy/run = %d %v", code, body)
	}
	deadline := time.Now().Add(3 * time.Second)
	var snap map[string]any
	for time.Now().Before(deadline) {
		_, body := doJSON(r, "GET", "/api/admin/tidy/status", tok, "")
		snap = body["data"].(map[string]any)
		if snap["state"] != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap["state"] != "done" {
		t.Fatalf("终态 = %v（error=%v），期望 done", snap["state"], snap["error"])
	}
	sugs := asAnySlice(snap["suggestions"])
	if len(sugs) != 1 || int64(sugs[0].(map[string]any)["pid"].(float64)) != 3203 {
		t.Fatalf("范围过滤失败，建议集 = %v，期望仅 3203", sugs)
	}
	if n, _ := snap["excluded"].(float64); n != 1 {
		t.Fatalf("excluded = %v，期望 1（3202；范围外 3201 不计）", n)
	}
	if got, _ := snap["from"].(float64); got != 3202 {
		t.Fatalf("快照 from = %v，期望 3202", got)
	}
}

// TestTidyManualProblems 手动整理分页题目表：分页切片 / q 过滤 / 原始难度 0 原样下发
//（非 EffDifficulty 的 D3 兜底）/ root 特权视角隐藏题在列。
func TestTidyManualProblems(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.problems[3301] = &repo.Problem{ID: 3301, Title: "Alpha 题", Difficulty: 0, Tags: []int64{11}}
	f.problems[3302] = &repo.Problem{ID: 3302, Title: "Beta 题", Difficulty: 7, Hidden: true}
	f.problems[3303] = &repo.Problem{ID: 3303, Title: "Gamma 题", Difficulty: 3, MetaTags: []int64{500}}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.GET("/api/admin/tidy/problems", AuthRequired(h), RequireRoot(), h.TidyManualProblems)
	tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	// 全量：按 ID 升序，隐藏题 3302 在列，原始难度 0 原样下发
	code, body := doJSON(r, "GET", "/api/admin/tidy/problems?page=1&page_size=2", tok, "")
	if code != 200 {
		t.Fatalf("manual problems = %d %v", code, body)
	}
	data := body["data"].(map[string]any)
	if n, _ := data["total"].(float64); n != 3 {
		t.Fatalf("total = %v，期望 3（root 视角隐藏题在列）", data["total"])
	}
	list := data["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("page_size=2 应切片 2 行，得 %d", len(list))
	}
	first := list[0].(map[string]any)
	if int64(first["id"].(float64)) != 3301 {
		t.Fatalf("首行 = %v，期望 3301（ID 升序）", first["id"])
	}
	if n, _ := first["difficulty"].(float64); n != 0 {
		t.Fatalf("未设难度应原样下发 0（非 EffDifficulty=3），得 %v", first["difficulty"])
	}
	if ids := asAnySlice(first["tags"]); len(ids) != 1 || int64(ids[0].(float64)) != 11 {
		t.Fatalf("tags = %v，期望 [11]", first["tags"])
	}

	// 第二页只剩 3303，meta_tags 带出
	_, body = doJSON(r, "GET", "/api/admin/tidy/problems?page=2&page_size=2", tok, "")
	list = body["data"].(map[string]any)["list"].([]any)
	if len(list) != 1 || int64(list[0].(map[string]any)["id"].(float64)) != 3303 {
		t.Fatalf("第二页 = %v，期望仅 3303", list)
	}

	// q 标题子串：只中 Beta（隐藏题也算——root 视角零排除）
	_, body = doJSON(r, "GET", "/api/admin/tidy/problems?q=Beta", tok, "")
	list = body["data"].(map[string]any)["list"].([]any)
	if len(list) != 1 || int64(list[0].(map[string]any)["id"].(float64)) != 3302 {
		t.Fatalf("q=Beta = %v，期望仅 3302（隐藏题在列）", list)
	}

	// 学生打 root 端点 → 403
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	stu := f.addUser(h, 100, "stu", repo.RoleDefault, 0)
	if code, _ := doJSON(r, "GET", "/api/admin/tidy/problems", stu, ""); code != 403 {
		t.Fatalf("学生打 manual problems = %d，期望 403", code)
	}
}

// TestTidyManualApply 手动整理批量写库：三字段混合最终值 / 按字段对应 kind 打水位 /
// 越界难度单题失败不阻断其余 / 双树引用过期整批 400 / 空 patch 计失败。
func TestTidyManualApply(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.tagTreeNodes = []repo.TagNode{{ID: 11, Name: "动态规划"}}
	f.metaTreeNodes = []repo.TagNode{{ID: 500, Name: "洛谷"}}
	f.problems[3401] = &repo.Problem{ID: 3401, Title: "题A", Difficulty: 0, Tags: []int64{11}, MetaTags: []int64{500}}
	f.problems[3402] = &repo.Problem{ID: 3402, Title: "题B", Difficulty: 5}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/tidy/manual", AuthRequired(h), RequireRoot(), h.TidyManualApply)
	tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	// 混合批：3401 三字段全改；3402 越界难度计失败不写库
	code, body := doJSON(r, "POST", "/api/admin/tidy/manual", tok,
		`{"items":[{"pid":3401,"difficulty":6,"tags":[],"meta_tags":[500]},
		          {"pid":3402,"difficulty":11}]}`)
	if code != 200 {
		t.Fatalf("manual apply = %d %v", code, body)
	}
	data := body["data"].(map[string]any)
	if int(data["applied"].(float64)) != 1 || int(data["failed"].(float64)) != 1 {
		t.Fatalf("applied/failed = %v/%v，期望 1/1", data["applied"], data["failed"])
	}
	p := f.problems[3401]
	if p.Difficulty != 6 || len(p.Tags) != 0 || len(p.MetaTags) != 1 || p.MetaTags[0] != 500 {
		t.Fatalf("3401 落库异常：difficulty=%d tags=%v meta=%v", p.Difficulty, p.Tags, p.MetaTags)
	}
	if f.problems[3402].Difficulty != 5 {
		t.Fatalf("3402 越界项不得写库")
	}
	// 水位按字段对应 kind 打点：3401 三 kind 全有；3402 失败无水位
	for _, kind := range []string{"difficulty", "knowledge", "meta"} {
		if _, ok := p.TidiedAt[kind]; !ok {
			t.Fatalf("3401 缺水位 kind=%s（difficulty/tags/meta_tags 应分别打点）", kind)
		}
	}
	if f.problems[3402].TidiedAt != nil {
		t.Fatalf("失败项不应打水位")
	}

	// 单字段：只动 tags → 只打 knowledge 水位，难度/来源不动
	_, body = doJSON(r, "POST", "/api/admin/tidy/manual", tok,
		`{"items":[{"pid":3402,"tags":[11]}]}`)
	if int(body["data"].(map[string]any)["applied"].(float64)) != 1 {
		t.Fatalf("单字段 apply = %v", body["data"])
	}
	p2 := f.problems[3402]
	if len(p2.Tags) != 1 || p2.Tags[0] != 11 || p2.Difficulty != 5 {
		t.Fatalf("3402 tags 落库异常：tags=%v difficulty=%d", p2.Tags, p2.Difficulty)
	}
	if _, ok := p2.TidiedAt["knowledge"]; !ok {
		t.Fatal("tags 字段应打 knowledge 水位")
	}
	if _, ok := p2.TidiedAt["difficulty"]; ok {
		t.Fatal("未携带 difficulty 字段不应打 difficulty 水位")
	}

	// 树外知识点引用 → 整批 400（store 过期防护），什么都没写
	code, _ = doJSON(r, "POST", "/api/admin/tidy/manual", tok,
		`{"items":[{"pid":3401,"tags":[9999]}]}`)
	if code != 400 {
		t.Fatalf("树外引用应整批 400，得 %d", code)
	}

	// 空 patch（三字段全缺）→ 计失败
	_, body = doJSON(r, "POST", "/api/admin/tidy/manual", tok, `{"items":[{"pid":3401}]}`)
	if int(body["data"].(map[string]any)["failed"].(float64)) != 1 {
		t.Fatalf("空 patch = %v，期望 failed=1", body["data"])
	}
}

// TestTidyStatusPaged 建议快照服务端分页（v0.49 验收期调整）：页面只持有当前页切片，
// suggestion_total/conflict_total 保持全量口径；only_conflict 全量过滤后再切页。
func TestTidyStatusPaged(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.metaTreeNodes = []repo.TagNode{{ID: 500, Name: "洛谷"}}
	// 250 题全带标题线索（meta 规则任务秒完成）
	for i := 1; i <= 250; i++ {
		f.problems[int64(4200+i)] = &repo.Problem{ID: int64(4200 + i), Title: "[洛谷] 分页压测"}
	}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/tidy/run", AuthRequired(h), RequireRoot(), h.TidyRun)
	r.GET("/api/admin/tidy/status", AuthRequired(h), RequireRoot(), h.TidyStatus)
	tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	if code, _ := doJSON(r, "POST", "/api/admin/tidy/run", tok, `{"kind":"meta"}`); code != 200 {
		t.Fatal("run 失败")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, body := doJSON(r, "GET", "/api/admin/tidy/status?page=1&page_size=100", tok, "")
		if body["data"].(map[string]any)["state"] != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 第 1 页：100 条切片 + 全量总数
	_, body := doJSON(r, "GET", "/api/admin/tidy/status?page=1&page_size=100", tok, "")
	data := body["data"].(map[string]any)
	if n, _ := data["suggestion_total"].(float64); n != 250 {
		t.Fatalf("suggestion_total = %v，期望 250", data["suggestion_total"])
	}
	if n, _ := data["conflict_total"].(float64); n != 0 {
		t.Fatalf("conflict_total = %v，期望 0（meta 无冲突）", data["conflict_total"])
	}
	sugs := asAnySlice(data["suggestions"])
	if len(sugs) != 100 {
		t.Fatalf("第 1 页 = %d 条，期望 100", len(sugs))
	}
	if pid := int64(sugs[0].(map[string]any)["pid"].(float64)); pid != 4201 {
		t.Fatalf("首页首条 = %d，期望 4201（题号升序）", pid)
	}
	// 第 3 页：剩 50 条
	_, body = doJSON(r, "GET", "/api/admin/tidy/status?page=3&page_size=100", tok, "")
	sugs = asAnySlice(body["data"].(map[string]any)["suggestions"])
	if len(sugs) != 50 || int64(sugs[0].(map[string]any)["pid"].(float64)) != 4401 {
		t.Fatalf("第 3 页 = %d 条首条 %v，期望 50 条起 4401", len(sugs), sugs[0])
	}
	// 越界页：空切片不炸
	_, body = doJSON(r, "GET", "/api/admin/tidy/status?page=9&page_size=100", tok, "")
	if sugs = asAnySlice(body["data"].(map[string]any)["suggestions"]); len(sugs) != 0 {
		t.Fatalf("越界页应空，得 %d 条", len(sugs))
	}
	// only_conflict=true：meta 无冲突 → 空切片；suggestion_total 为过滤后口径（翻页器一致）
	_, body = doJSON(r, "GET", "/api/admin/tidy/status?page=1&page_size=100&only_conflict=true", tok, "")
	data = body["data"].(map[string]any)
	if sugs = asAnySlice(data["suggestions"]); len(sugs) != 0 {
		t.Fatalf("only_conflict 应空，得 %d 条", len(sugs))
	}
	if n, _ := data["suggestion_total"].(float64); n != 0 {
		t.Fatalf("only_conflict 下 suggestion_total 应为过滤后口径 0，得 %v", data["suggestion_total"])
	}
}

// asAnySlice 断言助手：nil / 缺席字段 → nil，数组 → []any
func asAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
}
