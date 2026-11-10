package server

// 题目收藏（v0.86）handler 契约：toggle 幂等往返、列表星标只给登录视角、
// 游客列表无字段、不存在题 404。复用 handler_httptest_test.go 的 fakeRepo 装配，
// 路由在本文件自挂（testEngine 不动，防与进行中批次互踩）。

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

func (f *fakeRepo) ToggleProblemFavorite(_ context.Context, uid, pid int64) (bool, error) {
	if f.favorites == nil {
		f.favorites = map[int64]map[int64]bool{}
	}
	set, ok := f.favorites[uid]
	if !ok {
		set = map[int64]bool{}
		f.favorites[uid] = set
	}
	if set[pid] {
		delete(set, pid)
		return false, nil
	}
	set[pid] = true
	return true, nil
}

func (f *fakeRepo) FavoritePIDs(_ context.Context, uid int64, pids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	set := f.favorites[uid]
	for _, pid := range pids {
		if set[pid] {
			out[pid] = true
		}
	}
	return out, nil
}

// favoriteEngine 收藏测试专用路由（与生产注册同形：列表 AuthOptional + 收藏 AuthRequired）。
func favoriteEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.GET("/problems", AuthOptional(h), h.ListProblems)
	api.POST("/problems/:id/favorite", AuthRequired(h), h.ToggleProblemFavorite)
	return r
}

func TestToggleProblemFavorite(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.problems[7] = &repo.Problem{ID: 7, Title: "收藏我", Difficulty: 1}
	h := newTestHandlers(f)
	r := favoriteEngine(h)
	tok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)

	if code, _ := doJSON(r, "POST", "/api/problems/7/favorite", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("游客收藏 = %d，期望 401", code)
	}
	if code, _ := doJSON(r, "POST", "/api/problems/999/favorite", tok, ""); code != http.StatusNotFound {
		t.Fatalf("不存在题收藏 = %d，期望 404", code)
	}
	if code, body := doJSON(r, "POST", "/api/problems/7/favorite", tok, ""); code != http.StatusOK || body["data"].(map[string]any)["favorited"] != true {
		t.Fatalf("首点收藏 = %d %v，期望 favorited=true", code, body)
	}
	if code, body := doJSON(r, "POST", "/api/problems/7/favorite", tok, ""); code != http.StatusOK || body["data"].(map[string]any)["favorited"] != false {
		t.Fatalf("再点取消 = %d %v，期望 favorited=false", code, body)
	}
}

func TestListProblemsFavoriteFlag(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.problems[1] = &repo.Problem{ID: 1, Title: "甲", Difficulty: 1}
	f.problems[2] = &repo.Problem{ID: 2, Title: "乙", Difficulty: 1}
	f.favorites = map[int64]map[int64]bool{100: {1: true}} // 用户 100 收藏了题 1
	h := newTestHandlers(f)
	r := favoriteEngine(h)
	tok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)

	// 登录视角：题 1 带 favorite，题 2 不带
	code, body := doJSON(r, "GET", "/api/problems", tok, "")
	if code != http.StatusOK {
		t.Fatalf("登录列表 = %d", code)
	}
	list := body["data"].(map[string]any)["list"].([]any)
	byID := map[float64]map[string]any{}
	for _, raw := range list {
		it := raw.(map[string]any)
		byID[it["id"].(float64)] = it
	}
	if byID[1]["favorite"] != true {
		t.Fatalf("已收藏题应带 favorite=true，得到 %v", byID[1])
	}
	if _, has := byID[2]["favorite"]; has {
		t.Fatalf("未收藏题不应下发 favorite 字段")
	}

	// 游客视角：整表无 favorite 字段
	_, body = doJSON(r, "GET", "/api/problems", "", "")
	list = body["data"].(map[string]any)["list"].([]any)
	for _, raw := range list {
		if _, has := raw.(map[string]any)["favorite"]; has {
			t.Fatalf("游客列表不应下发 favorite 字段")
		}
	}
}
