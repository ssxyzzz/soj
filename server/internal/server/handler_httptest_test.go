package server

// handler HTTP 测试开张（v0.32 分层收紧第三步）：借 RepoAPI 接口缝注入 fake repo，
// 无 Mongo 跑 httptest——先立模式不追全覆盖，四类核心场景各 1-2 例：
// ① 权限中间件链（AuthRequired / RequirePerm / RequireRoot）
// ② 题库列表三视角（游客/学生/特权的隐藏题与锁题排除契约）
// ③ 提交创建的锁题与限流
// ④ 讨论代码门槛（纯函数 + 集成：分数达标前后读时裁剪）。
// fakeRepo 内嵌 RepoAPI（nil）：未覆盖的方法被调用即 panic——测试立刻暴露真实依赖面。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/auth"
	"soj/internal/config"
	"soj/internal/repo"
)

// ---- fake repo ----

type fakeRepo struct {
	RepoAPI // nil 兜底：未覆盖方法一调用即 panic

	users    map[int64]*repo.User
	roles    map[string]*repo.Role
	settings *repo.SystemSettings
	problems map[int64]*repo.Problem
	exams    []*repo.Contest // Exam = Contest 别名
	isMember bool

	// 题库三视角：捕获 server 传给 repo 的过滤契约 + 回放列表
	lastFilter *repo.ProblemListFilter
	listPage   func(f repo.ProblemListFilter) ([]*repo.Problem, int64)

	createSubmissions int
	bestScores        map[int64]int // pid → 该用户本题最高分（讨论门槛视角）
	discussTopic      *repo.DiscussTopic
}

func (f *fakeRepo) GetUserByID(_ context.Context, id int64) (*repo.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) GetRole(_ context.Context, id string) (*repo.Role, error) {
	if r, ok := f.roles[id]; ok {
		return r, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) PendingDepth(context.Context) (int64, error) { return 0, nil }

func (f *fakeRepo) GetSystemSettings(context.Context) (*repo.SystemSettings, error) {
	return f.settings, nil
}

func (f *fakeRepo) HiddenProblemIDs(context.Context) ([]int64, error) {
	var ids []int64
	for _, p := range f.problems {
		if p.Hidden {
			ids = append(ids, p.ID)
		}
	}
	return ids, nil
}

func (f *fakeRepo) ListProblemsPage(_ context.Context, flt repo.ProblemListFilter, _, _ int) ([]*repo.Problem, int64, error) {
	cp := flt
	f.lastFilter = &cp
	excluded := map[int64]bool{}
	for _, id := range flt.ExcludeIDs {
		excluded[id] = true
	}
	var out []*repo.Problem
	for _, p := range f.problems {
		if !excluded[p.ID] {
			out = append(out, p)
		}
	}
	return out, int64(len(out)), nil
}

func (f *fakeRepo) ListProblemStats(context.Context, []int64) (map[int64]repo.ProblemStat, error) {
	return map[int64]repo.ProblemStat{}, nil
}

func (f *fakeRepo) ListUnpublishedContests(context.Context) ([]*repo.Contest, error) {
	return nil, nil
}

func (f *fakeRepo) ListUnpublishedExams(context.Context) ([]*repo.Contest, error) {
	return f.exams, nil
}

func (f *fakeRepo) IsMember(context.Context, int64, int64) (bool, error) {
	return f.isMember, nil
}

func (f *fakeRepo) GetProblem(_ context.Context, id int64) (*repo.Problem, error) {
	if p, ok := f.problems[id]; ok {
		return p, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) CreateSubmission(context.Context, int64, string, *repo.Problem, string, string, *repo.Contest, *repo.Contest, int64) (int64, error) {
	f.createSubmissions++
	return 999, nil
}

func (f *fakeRepo) UserBestStatuses(_ context.Context, uid int64, pids []int64) (map[int64]repo.UserBestStatus, error) {
	out := map[int64]repo.UserBestStatus{}
	for _, pid := range pids {
		out[pid] = repo.UserBestStatus{Score: f.bestScores[pid]}
	}
	_ = uid
	return out, nil
}

func (f *fakeRepo) GetDiscussTopic(_ context.Context, tid int64) (*repo.DiscussTopic, error) {
	if f.discussTopic != nil && f.discussTopic.ID == tid {
		return f.discussTopic, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) ListDiscussReplyPage(context.Context, int64, int) ([]*repo.DiscussReply, int64, error) {
	return nil, 0, nil
}

func (f *fakeRepo) ListDiscussReplyChildren(context.Context, int64, []int64) ([]*repo.DiscussReply, error) {
	return nil, nil
}

func (f *fakeRepo) LikedDiscussTargets(context.Context, int64, string, []int64) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}

func (f *fakeRepo) GetUsersByIDs(context.Context, []int64) (map[int64]*repo.User, error) {
	return map[int64]*repo.User{}, nil
}

// ---- 测试装配 ----

const testJWTSecret = "httptest-secret-0123456789abcdef"

func newTestHandlers(f *fakeRepo) *Handlers {
	return &Handlers{
		cfg:            &config.Config{Addr: ":0", JWTSecret: testJWTSecret},
		repo:           f,
		limiter:        newRateLimiter(),
		runLimit:       newRateLimiter(),
		idents:         newIdentCache(),
		ipLimitGlobal:  newIPLimiter(ipRateGlobalPerMin, time.Minute),
		ipLimitAuth:    newIPLimiter(ipRateAuthPerMin, time.Minute),
		leaderCache:    map[string]leaderEntry{},
		rank:           newRankCache(nil),
		boards:         newBoardsCache(nil),
	}
}

// testEngine 手工挂被测路由（链与路径对齐 Run 的注册；不追全覆盖，先立模式）。
func testEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.GET("/problems", AuthOptional(h), h.ListProblems)
	api.POST("/submissions", AuthRequired(h), h.CreateSubmission)
	api.GET("/discuss/:tid", AuthRequired(h), h.GetDiscussTopic)
	authed := api.Group("", AuthRequired(h))
	authed.GET("/admin/judge/stats", RequireRoot(), h.AdminJudgeStats)
	return r
}

func (f *fakeRepo) addUser(h *Handlers, id int64, uname, role string, pwdVer int) string {
	f.users[id] = &repo.User{ID: id, Username: uname, Role: role, PwdVer: pwdVer}
	tok, err := auth.GenerateToken(id, uname, pwdVer, testJWTSecret)
	if err != nil {
		panic(err)
	}
	return tok
}

func doJSON(r *gin.Engine, method, path, token string, body string) (int, map[string]any) {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func baseFake() *fakeRepo {
	return &fakeRepo{
		users:    map[int64]*repo.User{},
		roles:    map[string]*repo.Role{},
		problems: map[int64]*repo.Problem{},
		settings: &repo.SystemSettings{RateLimitPerMinute: 100},
		bestScores: map[int64]int{},
	}
}

// ---- ① 权限中间件链 ----

func TestMiddlewareChain(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := testEngine(h)

	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)
	stuTok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)
	staleTok := f.addUser(h, 101, "stale", repo.RoleDefault, 7)
	f.users[101].PwdVer = 8 // 签发后改密：旧 token 必须全端下线

	if code, body := doJSON(r, "GET", "/api/admin/judge/stats", "", ""); code != http.StatusUnauthorized || body["message"] != "未登录或登录已过期" {
		t.Fatalf("无 token = %d %v，期望 401 未登录", code, body)
	}
	if code, body := doJSON(r, "GET", "/api/admin/judge/stats", staleTok, ""); code != http.StatusUnauthorized || body["message"] != "密码已变更，请重新登录" {
		t.Fatalf("改密旧 token = %d %v，期望 401 密码已变更", code, body)
	}
	if code, body := doJSON(r, "GET", "/api/admin/judge/stats", stuTok, ""); code != http.StatusForbidden {
		t.Fatalf("学生打 root 端点 = %d %v，期望 403", code, body)
	}
	if code, body := doJSON(r, "GET", "/api/admin/judge/stats", rootTok, ""); code != http.StatusOK || body["code"] != float64(0) {
		t.Fatalf("root 打统计端点 = %d %v，期望 200 code=0", code, body)
	}
	if code, _ := doJSON(r, "GET", "/api/discuss/1", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("无 token 讨论详情 = %d，期望 401", code)
	}
}

// ---- ② 题库列表三视角 ----

func TestListProblemsThreeViews(t *testing.T) {
	mk := func(id int64, title string, hidden bool) *repo.Problem {
		return &repo.Problem{ID: id, Title: title, Hidden: hidden, Difficulty: 3,
			Cases: []repo.CaseMeta{{InName: "1.in"}}}
	}
	now := time.Now()
	newView := func(role string) (*fakeRepo, *gin.Engine, string) {
		f := baseFake()
		f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
		f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
		f.roles[repo.RoleAdmin] = &repo.Role{ID: repo.RoleAdmin, Perms: []string{repo.PermProblemViewAll}}
		f.problems[1001] = mk(1001, "可见题", false)
		f.problems[1002] = mk(1002, "隐藏题", true)
		f.problems[1003] = mk(1003, "考试隐藏期题", false)
		// 班级考试（ClassID=9，成员锁定）：1003 处于未发布赛事 → 非特权视角排除
		f.exams = []*repo.Contest{{
			ID: 50, ClassID: 9, Title: "期中", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour),
			Published: false, Problems: []repo.ContestProblem{{PID: 1003}},
		}}
		f.isMember = true
		h := newTestHandlers(f)
		var tok string
		switch role {
		case "guest":
		case "student":
			tok = f.addUser(h, 100, "stu", repo.RoleDefault, 0)
		case "admin":
			tok = f.addUser(h, 1, "adm", repo.RoleAdmin, 0)
		default:
			t.Fatalf("未知视角 %q", role)
		}
		return f, testEngine(h), tok
	}

	// 游客：非本班成员，班级考试题对其无锁——只排除隐藏题；学生（本班成员）：隐藏题与赛题都排除
	for _, tc := range []struct {
		role      string
		seeExamPb bool
		exclExam  bool
	}{
		{"guest", true, false},
		{"student", false, true},
	} {
		f, r, tok := newView(tc.role)
		code, body := doJSON(r, "GET", "/api/problems", tok, "")
		if code != http.StatusOK {
			t.Fatalf("%s 视角 = %d，期望 200", tc.role, code)
		}
		got := map[int64]bool{}
		for _, it := range body["data"].(map[string]any)["list"].([]any) {
			got[int64(it.(map[string]any)["id"].(float64))] = true
		}
		if got[1002] || !got[1001] || got[1003] != tc.seeExamPb {
			t.Fatalf("%s 视角可见集 = %v（期望 1003 可见=%v）", tc.role, got, tc.seeExamPb)
		}
		excl := map[int64]bool{}
		for _, id := range f.lastFilter.ExcludeIDs {
			excl[id] = true
		}
		if !excl[1002] || excl[1003] != tc.exclExam {
			t.Fatalf("%s 视角过滤契约 = %v（期望含 1002、1003 排除=%v）", tc.role, excl, tc.exclExam)
		}
	}

	// 特权（problem.view.all）：零排除，全量可见
	f, r, tok := newView("admin")
	code, _ := doJSON(r, "GET", "/api/problems", tok, "")
	if code != http.StatusOK {
		t.Fatalf("特权视角 = %d，期望 200", code)
	}
	if len(f.lastFilter.ExcludeIDs) != 0 {
		t.Fatalf("特权视角过滤契约 ExcludeIDs = %v，期望空", f.lastFilter.ExcludeIDs)
	}
}

// ---- ③ 提交创建：锁题 + 限流 ----

func TestCreateSubmissionLockAndRate(t *testing.T) {
	now := time.Now()
	mk := func(id int64, hidden bool) *repo.Problem {
		return &repo.Problem{ID: id, Title: "T", Hidden: hidden, Difficulty: 3,
			TimeLimitMS: 1000, MemoryLimitKB: 65536, Cases: []repo.CaseMeta{{InName: "1.in"}}}
	}
	post := func(r *gin.Engine, tok string, pid int64) (int, map[string]any) {
		return doJSON(r, "POST", "/api/submissions", tok,
			`{"problem_id":`+jsonInt(pid)+`,"language":"cpp","code":"int main(){return 0;}"}`)
	}

	// 赛中班级考试（本班成员）：提交被锁，指引进比赛页
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.problems[2001] = mk(2001, false)
	f.exams = []*repo.Contest{{
		ID: 60, ClassID: 9, Title: "月考", Start: now.Add(-time.Hour), End: now.Add(time.Hour),
		Published: false, Problems: []repo.ContestProblem{{PID: 2001}},
	}}
	f.isMember = true
	h := newTestHandlers(f)
	r := testEngine(h)
	tok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)
	if code, body := post(r, tok, 2001); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "请从考试页面提交") {
		t.Fatalf("赛中提交 = %d %v，期望 400 锁题指引", code, body)
	}

	// 隐藏题对非特权视角：不可见也不可提交（404 不泄漏存在性）
	f2 := baseFake()
	f2.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f2.problems[2002] = mk(2002, true)
	h2 := newTestHandlers(f2)
	r2 := testEngine(h2)
	tok2 := f2.addUser(h2, 101, "stu2", repo.RoleDefault, 0)
	if code, _ := post(r2, tok2, 2002); code != http.StatusNotFound {
		t.Fatalf("隐藏题提交 = %d，期望 404", code)
	}

	// 全局限流：每分钟 1 次——第二发 429
	f3 := baseFake()
	f3.settings.RateLimitPerMinute = 1
	f3.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f3.problems[2003] = mk(2003, false)
	h3 := newTestHandlers(f3)
	r3 := testEngine(h3)
	tok3 := f3.addUser(h3, 102, "stu3", repo.RoleDefault, 0)
	if code, _ := post(r3, tok3, 2003); code != http.StatusOK || f3.createSubmissions != 1 {
		t.Fatalf("首发 = %d（落库 %d），期望 200 落库 1 次", code, f3.createSubmissions)
	}
	if code, body := post(r3, tok3, 2003); code != http.StatusTooManyRequests || !strings.Contains(body["message"].(string), "提交过于频繁") {
		t.Fatalf("限流 = %d %v，期望 429", code, body)
	}
	if f3.createSubmissions != 1 {
		t.Fatalf("限流后仍落库（%d 次）", f3.createSubmissions)
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---- ④ 讨论代码门槛 ----

func TestStripDiscussCodeBlocks(t *testing.T) {
	src := "前置\n```cpp\nsecret_code();\n```\n后置"
	got := stripDiscussCodeBlocks(src, 60)
	if strings.Contains(got, "secret_code") || !strings.Contains(got, "需 ≥ 60 分") {
		t.Fatalf("未达标视角裁剪失效：%q", got)
	}
	if got := stripDiscussCodeBlocks(src, 0); got != src {
		t.Fatalf("门槛 0 不应裁剪：%q", got)
	}
}

func TestDiscussCodeThresholdGate(t *testing.T) {
	// 题内讨论帖含代码块：门槛 60 分，视角按本题最高分读时裁剪
	mk := func(best int) (*gin.Engine, string) {
		f := baseFake()
		f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
		f.problems[3001] = &repo.Problem{ID: 3001, Title: "P", Difficulty: 3}
		f.settings.DiscussCodeThreshold = func() *int { v := 60; return &v }()
		f.bestScores[3001] = best
		f.discussTopic = &repo.DiscussTopic{
			ID: 7, Scope: repo.DiscussScopeProblem, ScopeID: 3001, UID: 200, Title: "求助",
			Content: "我的做法\n```cpp\nsecret_code();\n```",
		}
		h := newTestHandlers(f)
		return testEngine(h), f.addUser(h, 100, "stu", repo.RoleDefault, 0)
	}

	r, tok := mk(30) // 最高 30 分 < 60：代码块被裁成门槛提示
	_, body := doJSON(r, "GET", "/api/discuss/7", tok, "")
	content := body["data"].(map[string]any)["topic"].(map[string]any)["content"].(string)
	if strings.Contains(content, "secret_code") {
		t.Fatalf("未达标视角泄漏代码：%q", content)
	}
	if !strings.Contains(content, "需 ≥ 60 分") {
		t.Fatalf("未达标视角缺门槛提示：%q", content)
	}

	r2, tok2 := mk(100) // 满分 ≥ 60：代码原样可见
	_, body2 := doJSON(r2, "GET", "/api/discuss/7", tok2, "")
	content2 := body2["data"].(map[string]any)["topic"].(map[string]any)["content"].(string)
	if !strings.Contains(content2, "secret_code") {
		t.Fatalf("达标视角代码被误裁：%q", content2)
	}
}
