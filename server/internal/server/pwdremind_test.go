package server

// 首登改密提醒（用户拍板：提醒可跳过，跳过=不再提醒）HTTP 契约测试：
// ① 跳过端点清标志（未登录 401）；② 管理员建号置位（无 ID 分支走 CreateUserWithPassword
// 与注册/种子同源，置位在建号成功后补刀）；③ 登录/me 载荷带 pwdRemind 字段。

import (
	"context"
	"net/http"
	"testing"

	"soj/internal/repo"
)

// fakeRepo 的 SetPwdRemind / AddSecurityLog 覆盖（记录调用参数供断言；包级记录，测试不并行）
var pwdRemindCalls []struct {
	uid     int64
	pending bool
}

func (f *fakeRepo) SetPwdRemind(_ context.Context, uid int64, pending bool) error {
	pwdRemindCalls = append(pwdRemindCalls, struct {
		uid     int64
		pending bool
	}{uid, pending})
	return nil
}

func (f *fakeRepo) AddSecurityLog(context.Context, repo.SecurityLog) error { return nil }

func (f *fakeRepo) CreateUserWithPassword(_ context.Context, username, _, nickname, role string) (int64, error) {
	f.users[900] = &repo.User{ID: 900, Username: username, Nickname: nickname, Role: role}
	return 900, nil
}

func TestSkipPwdRemind(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/me/pwd-remind-skip", AuthRequired(h), h.SkipPwdRemind)
	tok := f.addUser(h, 100, "stu", repo.RoleDefault, 0)

	pwdRemindCalls = nil
	if code, body := doJSON(r, "POST", "/api/me/pwd-remind-skip", tok, ""); code != http.StatusOK || body["code"] != float64(0) {
		t.Fatalf("已登录跳过 = %d %v，期望 200 code=0", code, body)
	}
	if len(pwdRemindCalls) != 1 || pwdRemindCalls[0].uid != 100 || pwdRemindCalls[0].pending {
		t.Fatalf("跳过应清标志 SetPwdRemind(100,false)，实际 %+v", pwdRemindCalls)
	}
	if code, body := doJSON(r, "POST", "/api/me/pwd-remind-skip", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("未登录跳过 = %d %v，期望 401", code, body)
	}
}

func TestAdminCreateUserSetsPwdRemind(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/users", AuthRequired(h), RequireRoot(), h.AdminCreateUser)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	pwdRemindCalls = nil
	code, body := doJSON(r, "POST", "/api/admin/users", rootTok,
		`{"username":"newstu","password":"Passw0rd!9"}`)
	if code != http.StatusOK || body["code"] != float64(0) {
		t.Fatalf("管理员建号 = %d %v，期望 200 code=0", code, body)
	}
	if len(pwdRemindCalls) != 1 || pwdRemindCalls[0].uid != 900 || !pwdRemindCalls[0].pending {
		t.Fatalf("管理员建号应置位 SetPwdRemind(900,true)，实际 %+v", pwdRemindCalls)
	}
}

func TestUserJSONCarriesPwdRemind(t *testing.T) {
	if v, _ := userJSON(&repo.User{PwdRemind: true})["pwdRemind"].(bool); !v {
		t.Fatalf("登录/me 载荷应携带 pwdRemind=true")
	}
	if v, _ := userJSON(&repo.User{})["pwdRemind"].(bool); v {
		t.Fatalf("未置位用户载荷 pwdRemind 应为 false")
	}
}
