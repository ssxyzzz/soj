package server

// 公开主页游戏三卡（v0.85）契约测试：GET /api/users/:id/achievements|starmap|wrongbook。
// 核心口径：三卡随公开主页对登录者可见，但隐藏题面按观察者权限收口——星图隐藏题
// 计数走 repo.StarMap includeHidden 参数（本测试钉参数契约），错题本隐藏题条目在
// handler 剔除；目标用户不存在回 404；成就墙无题目隐私面，payload 直接下发。

import (
	"context"
	"net/http"
	"testing"
	"time"

	"soj/internal/repo"
)

func (f *fakeRepo) StarMap(_ context.Context, _ int64, includeHidden bool) ([]repo.StarNode, error) {
	f.lastStarIncludeHidden = includeHidden
	return []repo.StarNode{{TagID: 7, Name: "枚举", Solved: 3, Lit: true}}, nil
}

func (f *fakeRepo) RawAchievements(_ context.Context, _ int64) ([]repo.Achievement, error) {
	return []repo.Achievement{{ID: 1, UID: 200, Code: "first_ac", At: time.Now()}}, nil
}

func (f *fakeRepo) Wrongbook(_ context.Context, _ int64, _ int, _ time.Duration) ([]repo.RevengeRow, error) {
	return f.wbRows, nil
}

func TestUserGameCardsHiddenContract(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.roles[repo.RoleAdmin] = &repo.Role{ID: repo.RoleAdmin, Perms: []string{repo.PermProblemViewAll}}
	f.users[200] = &repo.User{ID: 200, Username: "target", Role: repo.RoleDefault}
	f.wbRows = []repo.RevengeRow{
		{PID: 1001, Title: "可见错题", Difficulty: 3, BestScore: 40},
		{PID: 1002, Title: "隐藏错题", Difficulty: 3, BestScore: 20, Hidden: true},
	}
	h := newTestHandlers(f)
	r := testEngine(h)
	stuTok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)
	admTok := f.addUser(h, 1, "adm", repo.RoleAdmin, 0)

	// 学生（无 problem.view.all）：星图隐藏计数参数关、错题本仅可见条目
	if code, _ := doJSON(r, "GET", "/api/users/200/starmap", stuTok, ""); code != http.StatusOK {
		t.Fatalf("学生读星图 = %d，期望 200", code)
	}
	if f.lastStarIncludeHidden {
		t.Fatalf("学生视角 StarMap includeHidden = true，期望 false（隐藏题不进知识点计数）")
	}
	code, body := doJSON(r, "GET", "/api/users/200/wrongbook", stuTok, "")
	if code != http.StatusOK {
		t.Fatalf("学生读错题本 = %d，期望 200", code)
	}
	list := body["data"].(map[string]any)["list"].([]any)
	if len(list) != 1 || int64(list[0].(map[string]any)["pid"].(float64)) != 1001 {
		t.Fatalf("学生错题本 = %v，期望仅可见题 1001", list)
	}

	// 特权（problem.view.all）：隐藏计数参数开、错题本全量
	if code, _ := doJSON(r, "GET", "/api/users/200/starmap", admTok, ""); code != http.StatusOK {
		t.Fatalf("特权读星图 = %d，期望 200", code)
	}
	if !f.lastStarIncludeHidden {
		t.Fatalf("特权视角 StarMap includeHidden = false，期望 true")
	}
	code, body = doJSON(r, "GET", "/api/users/200/wrongbook", admTok, "")
	if code != http.StatusOK {
		t.Fatalf("特权读错题本 = %d，期望 200", code)
	}
	if n := len(body["data"].(map[string]any)["list"].([]any)); n != 2 {
		t.Fatalf("特权错题本条数 = %d，期望 2（含隐藏题）", n)
	}

	// 成就墙：无题目面，学生视角直接下发解锁态
	code, body = doJSON(r, "GET", "/api/users/200/achievements", stuTok, "")
	if code != http.StatusOK {
		t.Fatalf("学生读成就 = %d，期望 200", code)
	}
	if got := body["data"].(map[string]any)["unlocked"].(float64); got != 1 {
		t.Fatalf("成就 unlocked = %v，期望 1", got)
	}

	// 目标用户不存在：三卡统一 404
	for _, path := range []string{"/api/users/999/achievements", "/api/users/999/starmap", "/api/users/999/wrongbook"} {
		if code, _ := doJSON(r, "GET", path, stuTok, ""); code != http.StatusNotFound {
			t.Fatalf("%s = 期望 404", path)
		}
	}
}
