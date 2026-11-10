package server

// v0.87 赛时隐藏修复批契约测试：
//  ① roundUpdate 改时间「重启」已发布场次 → 整场回炉（UnpublishRound 先于改时间执行）；
//     未发布场次改时间 / 已发布场次只改标题 / 已发布改到隐藏期已过 → 均不触发；
//  ② AiDebug 赛时封锁在 AC 判定之前——锁题期 AC 提交得 403「已锁定」而非 400「已通过」
//     （后者等于向参赛者泄漏判题结果）；
//  ③ GetProblem 赛时/隐藏期不下发 submit_count/accepted_count（与赛事详情列表口径对齐），
//     组织者不受限。

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/ai"
	"soj/internal/repo"
)

// ---- fakeRepo 补丁（本文件局部）----

func (f *fakeRepo) UpdateRoundFields(_ context.Context, _ string, id int64, p repo.RoundPatch) error {
	if f.updatedRound == nil {
		f.updatedRound = map[int64]repo.RoundPatch{}
	}
	f.updatedRound[id] = p
	return nil
}

func (f *fakeRepo) UnpublishRound(_ context.Context, _ string, id int64) error {
	f.unpublishCalls = append(f.unpublishCalls, id)
	return nil
}

func (f *fakeRepo) GetSubmission(_ context.Context, id int64) (*repo.Submission, error) {
	if s, ok := f.submissions[id]; ok {
		return s, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) HasSolved(context.Context, int64, int64) (bool, error) { return false, nil }

func (f *fakeRepo) HasJadeSolutionUnlock(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (f *fakeRepo) GetLevelConfig(context.Context) (*repo.LevelConfig, error) {
	return &repo.LevelConfig{}, nil
}

func (f *fakeRepo) SumXpAdjust(context.Context, []int64) (map[int64]int64, error) {
	return map[int64]int64{}, nil
}

func (f *fakeRepo) SolvedDifficultyCounts(context.Context, []int64) (map[int64][10]int64, error) {
	return map[int64][10]int64{}, nil
}

// unpublishEngine 回炉测试路由（与生产注册同形）。
func unpublishEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", AuthRequired(h))
	api.PATCH("/contests/:id", h.UpdateContest)
	api.GET("/problems/:id", AuthOptional(h), h.GetProblem)
	api.POST("/submissions/:id/ai-debug", AuthRequired(h), h.AiDebug)
	return r
}

// ---- ① 改时间重启回炉 ----

func TestRoundUpdateUnpublishOnRestart(t *testing.T) {
	now := time.Now()
	mk := func() (*gin.Engine, string, *fakeRepo) {
		f := baseFake()
		f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
		f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
		// 已发布、已结束的场次（用户实测场景：上上周办过，今天改时间重启）
		f.rounds = []*repo.Contest{{
			ID: 9, ClassID: 0, CreatedBy: 1, Published: true, HideDuring: true, HideMeta: true,
			Start: now.Add(-48 * time.Hour), End: now.Add(-24 * time.Hour),
			Problems: []repo.ContestProblem{{PID: 100, Score: 100}},
		}}
		h := newTestHandlers(f)
		r := unpublishEngine(h)
		tok := f.addUser(h, 1, "root", repo.RoleRoot, 0)
		return r, tok, f
	}
	patchBody := func(s, e time.Time) string {
		return `{"start":"` + s.UTC().Format(time.RFC3339) + `","end":"` + e.UTC().Format(time.RFC3339) + `"}`
	}

	// 复活（新隐藏期未过）→ 先回炉再改时间
	r, tok, f := mk()
	body := patchBody(now.Add(-time.Minute), now.Add(2*time.Hour))
	if code, out := doJSON(r, "PATCH", "/api/contests/9", tok, body); code != http.StatusOK {
		t.Fatalf("改时间重启 = %d %v", code, out)
	}
	if len(f.unpublishCalls) != 1 || f.unpublishCalls[0] != 9 {
		t.Fatalf("复活重启必须整场回炉，实际 unpublish=%v", f.unpublishCalls)
	}
	if len(f.updatedRound) != 1 {
		t.Fatal("回炉后时间更新仍应执行")
	}

	// 已发布但改到的隐藏期已过（改历史时间轴）→ 不回炉
	r, tok, f = mk()
	if code, out := doJSON(r, "PATCH", "/api/contests/9", tok, patchBody(now.Add(-72*time.Hour), now.Add(-60*time.Hour))); code != http.StatusOK {
		t.Fatalf("改到过去 = %d %v", code, out)
	}
	if len(f.unpublishCalls) != 0 {
		t.Fatalf("隐藏期已过不应回炉，实际 %v", f.unpublishCalls)
	}

	// 已发布只改标题 → 不回炉
	r, tok, f = mk()
	if code, out := doJSON(r, "PATCH", "/api/contests/9", tok, `{"title":"改名不重启"}`); code != http.StatusOK {
		t.Fatalf("改标题 = %d %v", code, out)
	}
	if len(f.unpublishCalls) != 0 {
		t.Fatalf("只改标题不应回炉，实际 %v", f.unpublishCalls)
	}

	// 未发布场次改时间 → 不回炉（本来就 unpublished）
	r, tok, f = mk()
	f.rounds[0].Published = false
	if code, out := doJSON(r, "PATCH", "/api/contests/9", tok, patchBody(now.Add(-time.Minute), now.Add(2*time.Hour))); code != http.StatusOK {
		t.Fatalf("未发布改时间 = %d %v", code, out)
	}
	if len(f.unpublishCalls) != 0 {
		t.Fatalf("未发布场次不应回炉，实际 %v", f.unpublishCalls)
	}
}

// ---- ② AiDebug 赛时封锁先于 AC 判定 ----

func TestAiDebugGateBeforeAC(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.aiCfg = &ai.Config{Providers: []ai.Provider{{Name: "t", BaseURL: "http://x", APIKey: "k"}}, Active: "t"}
	f.problems[100] = &repo.Problem{ID: 100, Title: "赛题", Difficulty: 3, Cases: []repo.CaseMeta{{InName: "1.in"}}}
	// 未发布 running 比赛（Bank 锁题，root 以外全锁）
	f.unpubContests = []*repo.Contest{{
		ID: 9, ClassID: 0, CreatedBy: 1, Published: false, HideDuring: true,
		Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Hour),
		Problems: []repo.ContestProblem{{PID: 100, Score: 100}},
	}}
	// 参赛者的 AC 提交：旧顺序会先撞 400「已通过」泄漏 AC，新顺序应 403 赛时锁定
	f.submissions = map[int64]*repo.Submission{55: {
		ID: 55, UID: 42, ProblemID: 100, Status: repo.StatusAccepted, Score: 100,
	}}
	h := newTestHandlers(f)
	r := unpublishEngine(h)
	tok := f.addUser(h, 42, "stu", repo.RoleDefault, 0)

	code, out := doJSON(r, "POST", "/api/submissions/55/ai-debug", tok, "")
	if code != http.StatusForbidden {
		t.Fatalf("锁题期 AC 提交应 403 赛时封锁，实际 = %d %v", code, out)
	}
	if msg, _ := out["message"].(string); msg != "该题正在比赛中，AI 助教已锁定，赛后开放" {
		t.Fatalf("应命中赛时封锁文案，实际 %q", msg)
	}
}

// ---- ③ GetProblem 赛时不通过率 ----

func TestGetProblemStatsHiddenDuringRound(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.problems[100] = &repo.Problem{ID: 100, Title: "赛题", Difficulty: 3, Cases: []repo.CaseMeta{{InName: "1.in"}}}
	f.stats = map[int64]repo.ProblemStat{100: {Total: 26, Accepted: 20}}
	f.unpubContests = []*repo.Contest{{
		ID: 9, ClassID: 0, CreatedBy: 1, Published: false, HideDuring: true, HideMeta: true,
		Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Hour),
		Problems: []repo.ContestProblem{{PID: 100, Score: 100}},
	}}
	h := newTestHandlers(f)
	r := unpublishEngine(h)

	// 参赛学生：不下发 submit_count/accepted_count（running 且 HideDuring）
	stuTok := f.addUser(h, 42, "stu", repo.RoleDefault, 0)
	_, out := doJSON(r, "GET", "/api/problems/100", stuTok, "")
	if d, ok := out["data"].(map[string]any); ok {
		if _, has := d["submit_count"]; has {
			t.Fatalf("赛时隐藏期不得下发通过率: %v", d["submit_count"])
		}
	} else {
		t.Fatalf("running 期参赛者应可读题面: %v", out)
	}

	// 组织者：不受限
	orgTok := f.addUser(h, 1, "org", repo.RoleDefault, 0)
	f.rounds = []*repo.Contest{f.unpubContests[0]} // GetRound 走 rounds
	_, out = doJSON(r, "GET", "/api/problems/100", orgTok, "")
	d, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("组织者读取异常: %v", out)
	}
	if !ok || d["submit_count"] == nil {
		t.Fatal("组织者应见通过率")
	}
}
