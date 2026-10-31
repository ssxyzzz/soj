package server

// 题库一键整理（v0.43）单测：纯函数（JSON 解析/通过率分档/唯一名映射）+ apply 校验链
// + meta 规则任务全流程（fake 全程无 Mongo，照 handler_httptest 模式）。

import (
	"context"
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

	// 合法改判 → 写库
	code, body = doJSON(r, "POST", "/api/admin/tidy/apply", tok,
		`{"kind":"difficulty","items":[{"pid":3001,"difficulty":6},{"pid":3002,"difficulty":4}]}`)
	if code != 200 || int(body["data"].(map[string]any)["applied"].(float64)) != 2 {
		t.Fatalf("合法 apply = %d %v，期望 applied=2", code, body)
	}
	if f.problems[3001].Difficulty != 6 || f.problems[3002].Difficulty != 4 {
		t.Fatalf("改判未落库：%d %d", f.problems[3001].Difficulty, f.problems[3002].Difficulty)
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
}

// asAnySlice 断言助手：nil / 缺席字段 → nil，数组 → []any
func asAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
}
