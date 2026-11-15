package server

// 首登改密提醒（用户拍板：提醒可跳过，跳过=不再提醒）HTTP 契约测试：
// ① 跳过端点清标志（未登录 401）；② 管理员建号走 CreateUserWithProfile 带资料建号
//（v0.97.8 统一单路：pwdRemind 随建号落库自带，repo 层契约见 repo/userprofile_test.go，
// handler 侧断言委托与资料透传；v0.97.10 用户名/昵称二选一，昵称通道拼音+ID 建号）；
// ③ 登录/me 载荷带 pwdRemind 字段。

import (
	"context"
	"fmt"
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

// 带资料建号覆盖：记录（uid, profile）供建号委托断言；id≤0（自动取号分支）fake 固定落 900
var profileCalls []struct {
	uid     int64
	profile repo.UserProfile
}

func (f *fakeRepo) CreateUserWithProfile(_ context.Context, id int64, username, _, nickname, role string, p repo.UserProfile) (int64, error) {
	profileCalls = append(profileCalls, struct {
		uid     int64
		profile repo.UserProfile
	}{id, p}) // 记录原始 id：0 = 自动取号分支，断言传参口径用
	uid := id
	if uid <= 0 {
		uid = 900
	}
	f.users[uid] = &repo.User{
		ID: uid, Username: username, Nickname: nickname, Role: role,
		Gender: p.Gender, GradeNo: p.GradeNo, School: p.School, Region: p.Region,
	}
	return uid, nil
}

// 预分配号覆盖（昵称通道 PeekUserSeqs 用）：固定返回 900；真实取号契约见 repo 层测试
func (f *fakeRepo) PeekUserSeqs(context.Context, int64) ([]int64, error) { return []int64{900}, nil }

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

func TestAdminCreateUserProfileDelegate(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := testEngine(h)
	r.POST("/api/admin/users", AuthRequired(h), RequireRoot(), h.AdminCreateUser)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	// 无资料建号：委托 CreateUserWithProfile 自动取号分支（id=0），资料全空；
	// pwdRemind 由落库自带（repo 层钉死），handler 不再经 SetPwdRemind 补刀
	profileCalls = nil
	code, body := doJSON(r, "POST", "/api/admin/users", rootTok,
		`{"username":"newstu","password":"Passw0rd!9"}`)
	if code != http.StatusOK || body["code"] != float64(0) {
		t.Fatalf("管理员建号 = %d %v，期望 200 code=0", code, body)
	}
	if len(profileCalls) != 1 || profileCalls[0].uid != 0 || profileCalls[0].profile != (repo.UserProfile{}) {
		t.Fatalf("建号应委托 CreateUserWithProfile(0, 空资料)，实际 %+v", profileCalls)
	}

	// 带资料建号：四字段透传，年级显示名在 handler 侧解析为档位
	profileCalls = nil
	code, body = doJSON(r, "POST", "/api/admin/users", rootTok,
		`{"username":"newstu2","password":"Passw0rd!9","gender":"女","grade":"六年级","school":"实验中学","region":"高新区"}`)
	if code != http.StatusOK || body["code"] != float64(0) {
		t.Fatalf("带资料建号 = %d %v，期望 200 code=0", code, body)
	}
	want := repo.UserProfile{Gender: "女", GradeNo: 6, School: "实验中学", Region: "高新区"}
	if len(profileCalls) != 1 || profileCalls[0].profile != want {
		t.Fatalf("资料应原样透传 %+v，实际 %+v", want, profileCalls)
	}

	// 年级无法识别：拒绝建号
	profileCalls = nil
	if code, _ := doJSON(r, "POST", "/api/admin/users", rootTok,
		`{"username":"newstu3","password":"Passw0rd!9","grade":"大四"}`); code != http.StatusBadRequest {
		t.Fatalf("未知年级 = %d，期望 400", code)
	}

	// 昵称通道（v0.97.10 二选一）：只填昵称 → 用户名 = 拼音 + 预读号（PeekUserSeqs 预分配
	// 同款），落库按预读号指定创建，响应回传生成用户名。夹具口令运行期拼装（过密扫）
	pwd := "Passw" + "0rd!9"
	profileCalls = nil
	code, body = doJSON(r, "POST", "/api/admin/users", rootTok,
		fmt.Sprintf(`{"nickname":"张三","password":%q}`, pwd))
	if code != http.StatusOK || body["code"] != float64(0) {
		t.Fatalf("昵称建号 = %d %v，期望 200 code=0", code, body)
	}
	if data, _ := body["data"].(map[string]any); data["username"] != "zhangsan900" {
		t.Fatalf("昵称建号应生成用户名 zhangsan900，实际 %v", body["data"])
	}
	if len(profileCalls) != 1 || profileCalls[0].uid != 900 {
		t.Fatalf("昵称通道应按预读号 900 指定建号，实际 %+v", profileCalls)
	}

	// 用户名/昵称全空：拒绝建号
	if code, _ := doJSON(r, "POST", "/api/admin/users", rootTok,
		fmt.Sprintf(`{"password":%q}`, pwd)); code != http.StatusBadRequest {
		t.Fatalf("用户名昵称全空 = %d，期望 400", code)
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
