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

func (f *fakeRepo) FavoriteRows(_ context.Context, uid int64, limit int) ([]repo.FavoriteRow, error) {
	out := []repo.FavoriteRow{}
	set := f.favorites[uid]
	for pid := range set {
		if len(out) >= limit {
			break
		}
		hidden := false
		if p, ok := f.problems[pid]; ok {
			hidden = p.Hidden
		}
		out = append(out, repo.FavoriteRow{PID: pid, Title: f.problems[pid].Title, Difficulty: f.problems[pid].Difficulty, Hidden: hidden})
	}
	return out, nil
}

// favoriteEngine 收藏测试专用路由（与生产注册同形：列表 AuthOptional + 收藏 AuthRequired + 主页收藏卡）。
func favoriteEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.GET("/problems", AuthOptional(h), h.ListProblems)
	api.POST("/problems/:id/favorite", AuthRequired(h), h.ToggleProblemFavorite)
	api.GET("/users/:id/favorites", AuthRequired(h), h.UserFavorites)
	return r
}

// TestUserFavorites 收藏卡（v0.89 铺开）：登录可见他人收藏、隐藏题对无特权视角剔除。
func TestUserFavorites(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.problems[7] = &repo.Problem{ID: 7, Title: "公开题", Difficulty: 2}
	f.problems[8] = &repo.Problem{ID: 8, Title: "隐藏题", Difficulty: 3, Hidden: true}
	f.problems[9] = &repo.Problem{ID: 9, Title: "普通题", Difficulty: 1}
	f.favorites = map[int64]map[int64]bool{100: {7: true, 8: true, 9: true}}
	f.users[100] = &repo.User{ID: 100, Username: "favOwner", Role: repo.RoleDefault, PwdVer: 0}
	h := newTestHandlers(f)
	r := favoriteEngine(h)
	viewer := f.addUser(h, 200, "viewer", repo.RoleDefault, 0)
	root := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	if code, _ := doJSON(r, "GET", "/api/users/100/favorites", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("游客收藏卡 = %d，期望 401", code)
	}
	if code, _ := doJSON(r, "GET", "/api/users/999/favorites", viewer, ""); code != http.StatusNotFound {
		t.Fatalf("不存在用户 = %d，期望 404", code)
	}
	// 学生视角：隐藏题 8 剔除，余 7/9
	_, body := doJSON(r, "GET", "/api/users/100/favorites", viewer, "")
	list := body["data"].(map[string]any)["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("学生视角收藏 = %d 条，期望 2（隐藏题剔除）", len(list))
	}
	// root 视角：全量 3 条
	_, body = doJSON(r, "GET", "/api/users/100/favorites", root, "")
	list = body["data"].(map[string]any)["list"].([]any)
	if len(list) != 3 {
		t.Fatalf("root 视角收藏 = %d 条，期望 3", len(list))
	}
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
