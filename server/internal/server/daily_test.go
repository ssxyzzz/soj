package server

// 今日挑战（v0.97.12 登录专属 + 按等级个性化）契约测试：GET /api/daily。
// 核心口径：未登录 enabled:false 前端整卡隐藏；登录下发本人个性化选题
//（PickDailyProblem 必须收到登录 uid）+ done 完成态（首过玉记录现查）；
// 空库降级 enabled:false；v0.41 全站同题与 solved_today 从众计数已退役（响应
// 无该字段由断言钉死）。

import (
	"context"
	"net/http"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/repo"
)

func (f *fakeRepo) PickDailyProblem(_ context.Context, uid int64) (*repo.Problem, error) {
	f.lastDailyUID = uid
	if f.dailyNil {
		return nil, nil
	}
	return &repo.Problem{ID: 500, Title: "今日挑战题", Difficulty: 6}, nil
}

func (f *fakeRepo) GetJadeAward(_ context.Context, uid, pid int64) (*repo.JadeAward, error) {
	if uid == 100 && pid == 500 && f.dailyDone {
		return &repo.JadeAward{Base: 6, Total: 12}, nil
	}
	return nil, mongo.ErrNoDocuments
}

func TestDailyContract(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.users[100] = &repo.User{ID: 100, Username: "stu", Role: repo.RoleDefault}
	// 池内种子题：PickDailyProblem 回放它，handler 缓存命中后再经 GetProblemMeta 瘦读
	f.problems[500] = &repo.Problem{ID: 500, Title: "今日挑战题", Difficulty: 6}
	h := newTestHandlers(f)
	r := testEngine(h)
	tok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)

	// 未登录：enabled:false，无 solved_today / done
	code, body := doJSON(r, "GET", "/api/daily", "", "")
	if code != http.StatusOK {
		t.Fatalf("匿名 /api/daily = %d，期望 200", code)
	}
	d := body["data"].(map[string]any)
	if d["enabled"] != false {
		t.Fatalf("匿名 enabled = %v，期望 false", d["enabled"])
	}
	if _, has := d["solved_today"]; has {
		t.Fatal("solved_today 应随从众计数退役，响应不应携带")
	}

	// 登录：个性化选题（fake 断言收到本人 uid）+ done=false
	code, body = doJSON(r, "GET", "/api/daily", tok, "")
	if code != http.StatusOK {
		t.Fatalf("登录 /api/daily = %d，期望 200", code)
	}
	d = body["data"].(map[string]any)
	if d["enabled"] != true || int64(d["pid"].(float64)) != 500 || d["title"] != "今日挑战题" {
		t.Fatalf("登录选题 = %v，期望 enabled=true pid=500", d)
	}
	if int(d["difficulty"].(float64)) != 6 {
		t.Fatalf("difficulty = %v，期望 6", d["difficulty"])
	}
	if d["done"] != false {
		t.Fatalf("未首过 done = %v，期望 false", d["done"])
	}
	if f.lastDailyUID != 100 {
		t.Fatalf("PickDailyProblem 收到 uid = %d，期望登录本人 100", f.lastDailyUID)
	}
	if _, has := d["solved_today"]; has {
		t.Fatal("solved_today 应随从众计数退役，响应不应携带")
	}

	// 首过后：done=true
	f.dailyDone = true
	code, body = doJSON(r, "GET", "/api/daily", tok, "")
	if code != http.StatusOK {
		t.Fatalf("done 态 /api/daily = %d，期望 200", code)
	}
	if d := body["data"].(map[string]any); d["done"] != true {
		t.Fatalf("首过后 done = %v，期望 true", d["done"])
	}

	// 空库：enabled:false（换一个未缓存过的 uid 绕开 30s 选题缓存）
	f.dailyNil = true
	tok2 := f.addUser(h, 101, "stu2", repo.RoleDefault, 0)
	code, body = doJSON(r, "GET", "/api/daily", tok2, "")
	if code != http.StatusOK {
		t.Fatalf("空库 /api/daily = %d，期望 200", code)
	}
	if d := body["data"].(map[string]any); d["enabled"] != false {
		t.Fatalf("空库 enabled = %v，期望 false", d["enabled"])
	}
}
