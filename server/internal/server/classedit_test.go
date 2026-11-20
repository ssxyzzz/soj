package server

// 班级编辑权限位（v0.96）契约测试：classManageable 收口 = class.edit.all 可管任意班 /
// class.edit.own 限建班人与班级管理员 / 无位者（含建班人）不可编辑；root 恒通走
// hasPerm 的 "*" 判定。以 GetClass 的 manageable 标志 + UpdateClass 的 200/403 双面钉死。

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/repo"
)

func (f *fakeRepo) GetClass(_ context.Context, id int64) (*repo.Class, error) {
	if cl, ok := f.classes[id]; ok {
		return cl, nil
	}
	return nil, mongo.ErrNoDocuments
}

// ClassMemberRole 口径同真库：非成员返回 ("", nil)。
func (f *fakeRepo) ClassMemberRole(_ context.Context, classID, uid int64) (string, error) {
	return f.memberRoles[[2]int64{classID, uid}], nil
}

func (f *fakeRepo) UpdateClass(_ context.Context, id int64, p repo.ClassPatch) error {
	if _, ok := f.classes[id]; !ok {
		return mongo.ErrNoDocuments
	}
	f.classPatches[id] = p
	return nil
}

// ListMembers 从成员角色表派生（v0.97 作业总结测试起需要真实成员清单；uid 升序稳定）。
func (f *fakeRepo) ListMembers(_ context.Context, classID int64) ([]repo.ClassMember, error) {
	out := make([]repo.ClassMember, 0, len(f.memberRoles))
	for key, role := range f.memberRoles {
		if key[0] != classID {
			continue
		}
		uname := ""
		if u := f.users[key[1]]; u != nil {
			uname = u.Username
		}
		out = append(out, repo.ClassMember{ClassID: classID, UID: key[1], Uname: uname, Role: role})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out, nil
}

func classEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api", AuthRequired(h))
	authed.GET("/classes/:id", h.GetClass)
	authed.PATCH("/classes/:id", h.UpdateClass)
	authed.DELETE("/classes/:id/members/:uid", h.RemoveClassMember)
	authed.GET("/classes/:id/assignments", h.ListClassAssignments)
	authed.GET("/classes/:id/exams", h.ListClassExams)
	authed.GET("/classes/:id/race", h.GetClassRace)
	return r
}

// ---------- class.view.all 只读透视（v0.97.20）内存件 ----------

func (f *fakeRepo) ListAssignments(_ context.Context, classID int64) ([]*repo.Assignment, error) {
	out := make([]*repo.Assignment, 0)
	for _, a := range f.assignments {
		if a.ClassID == classID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListClassExams(_ context.Context, _ int64) ([]*repo.Exam, error) { return nil, nil }

func (f *fakeRepo) ActiveClassroomRace(context.Context, int64) (*repo.ClassroomRace, error) {
	return nil, nil
}

func (f *fakeRepo) LatestClassroomRace(context.Context, int64) (*repo.ClassroomRace, error) {
	return nil, nil
}

// TestClassViewAllXray 钉死 class.view.all 只读透视契约（v0.97.20 实现追平权限目录
// 文案）：私密班不 404、邀请码与成员表下发（manageable 仍 false）、作业/考试列表
// visible、课堂 PK 可读；无位非成员四路全拒。
func TestClassViewAllXray(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.roles["inspector"] = &repo.Role{ID: "inspector", Perms: []string{repo.PermClassViewAll}}

	// 班 9 私密班：建班人 100，成员 101（admin）/102（member）
	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 100, InviteCode: "ab12", Private: true}
	f.memberRoles[[2]int64{9, 100}] = repo.ClassRoleOwner
	f.memberRoles[[2]int64{9, 101}] = repo.ClassRoleAdmin
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember
	f.assignments[1] = &repo.Assignment{ID: 1, ClassID: 9, Title: "作业一"}

	h := newTestHandlers(f)
	r := classEngine(h)
	inspector := f.addUser(h, 103, "inspector", "inspector", 0)
	stranger := f.addUser(h, 104, "stranger", repo.RoleDefault, 0)

	// 私密班详情：不 404，邀请码+成员表下发，joined/manageable 仍 false
	code, body := doJSON(r, "GET", "/api/classes/9", inspector, "")
	if code != http.StatusOK {
		t.Fatalf("透视者看私密班 = %d，期望 200", code)
	}
	d := body["data"].(map[string]any)
	if d["manageable"] != false || d["joined"] != false {
		t.Fatalf("透视者 manageable/joined 应为 false，got %v/%v", d["manageable"], d["joined"])
	}
	if d["invite_code"] != "ab12" {
		t.Fatalf("透视者应收到邀请码，got %v", d["invite_code"])
	}
	if ms, ok := d["members"].([]any); !ok || len(ms) != 3 {
		t.Fatalf("透视者应收到 3 条成员（含建班人），got %v", d["members"])
	}

	// 作业/考试列表 visible；课堂 PK 可读（200 非 403）
	for _, path := range []string{"/api/classes/9/assignments", "/api/classes/9/exams"} {
		if code, b := doJSON(r, "GET", path, inspector, ""); code != http.StatusOK {
			t.Fatalf("透视者 GET %s = %d", path, code)
		} else if !b["data"].(map[string]any)["visible"].(bool) {
			t.Fatalf("透视者 GET %s visible 应为 true", path)
		}
	}
	if code := func() int { c, _ := doJSON(r, "GET", "/api/classes/9/race", inspector, ""); return c }(); code != http.StatusOK {
		t.Fatalf("透视者看课堂 PK = %d，期望 200", code)
	}

	// 无位非成员四路全拒：私密班 404、作业/考试 visible=false、PK 403
	if code, _ := doJSON(r, "GET", "/api/classes/9", stranger, ""); code != http.StatusNotFound {
		t.Fatalf("无位非成员看私密班 = %d，期望 404", code)
	}
	for _, path := range []string{"/api/classes/9/assignments", "/api/classes/9/exams"} {
		if code, b := doJSON(r, "GET", path, stranger, ""); code != http.StatusOK {
			t.Fatalf("无位非成员 GET %s = %d", path, code)
		} else if b["data"].(map[string]any)["visible"].(bool) {
			t.Fatalf("无位非成员 GET %s visible 应为 false", path)
		}
	}
	if code, _ := doJSON(r, "GET", "/api/classes/9/race", stranger, ""); code != http.StatusForbidden {
		t.Fatalf("无位非成员看课堂 PK = %d，期望 403", code)
	}
}

// RemoveMember 内存版：role==owner 过滤同真库（建班人不可移 = 找不到文档）。
func (f *fakeRepo) RemoveMember(_ context.Context, classID, uid int64) error {
	key := [2]int64{classID, uid}
	if f.memberRoles[key] == "" || f.memberRoles[key] == repo.ClassRoleOwner {
		return mongo.ErrNoDocuments
	}
	delete(f.memberRoles, key)
	return nil
}

func TestClassRemoveMember(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault, Perms: []string{repo.PermClassEditOwn}}
	f.roles[repo.RoleAdmin] = &repo.Role{ID: repo.RoleAdmin, Perms: []string{repo.PermClassEditOwn, repo.PermClassViewAll}}
	f.roles["locked"] = &repo.Role{ID: "locked"}

	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 100, InviteCode: "ab12"}
	h := newTestHandlers(f)
	r := classEngine(h)
	owner := f.addUser(h, 100, "teacher", repo.RoleAdmin, 0)
	classAdmin := f.addUser(h, 101, "monitor", repo.RoleDefault, 0)
	member := f.addUser(h, 102, "stu", repo.RoleDefault, 0)
	stranger := f.addUser(h, 103, "stranger", repo.RoleDefault, 0)
	lockedOwner := f.addUser(h, 104, "locked", "locked", 0)
	f.memberRoles[[2]int64{9, 100}] = repo.ClassRoleOwner
	f.memberRoles[[2]int64{9, 101}] = repo.ClassRoleAdmin
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember

	del := func(tok string, classID, uid string) int {
		code, _ := doJSON(r, "DELETE", "/api/classes/"+classID+"/members/"+uid, tok, "")
		return code
	}

	// 班级管理员移出普通成员：200 且成员表落删除
	if code := del(classAdmin, "9", "102"); code != http.StatusOK {
		t.Fatalf("班级管理员移出成员 = %d，期望 200", code)
	}
	if _, ok := f.memberRoles[[2]int64{9, 102}]; ok {
		t.Fatal("被移出成员应从成员表中删除")
	}
	// 移出后该用户已非成员：再删一次 404
	if code := del(classAdmin, "9", "102"); code != http.StatusNotFound {
		t.Fatalf("重复移除非成员 = %d，期望 404", code)
	}

	// 回种成员，建班人自己移普通成员同样可行；两路管理者均不可移建班人
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember
	if code := del(owner, "9", "102"); code != http.StatusOK {
		t.Fatalf("建班人移出成员 = %d，期望 200", code)
	}
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember
	if code := del(owner, "9", "100"); code != http.StatusBadRequest {
		t.Fatalf("移除建班人 = %d，期望 400", code)
	}
	if code := del(classAdmin, "9", "100"); code != http.StatusBadRequest {
		t.Fatalf("班级管理员移除建班人 = %d，期望 400", code)
	}

	// 权限：普通成员 / 班外路人无权移除；无 edit.own 的建班人被收权
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember
	if code := del(member, "9", "102"); code != http.StatusForbidden {
		t.Fatalf("普通成员移人 = %d，期望 403", code)
	}
	if code := del(stranger, "9", "102"); code != http.StatusForbidden {
		t.Fatalf("班外路人移人 = %d，期望 403", code)
	}
	if code := del(lockedOwner, "9", "102"); code != http.StatusForbidden {
		t.Fatalf("无位角色（含建班人身份）移人 = %d，期望 403", code)
	}

	// 班级不存在 404
	if code := del(classAdmin, "404", "102"); code != http.StatusNotFound {
		t.Fatalf("移除不存在的班成员 = %d，期望 404", code)
	}
}

func TestClassEditPerms(t *testing.T) {
	f := baseFake()
	// 角色：default/admin 内置含 edit.own（v0.96 补发）；教务自定义角色只勾 edit.all；
	// 无位角色用于验证建班人被收权
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault, Perms: []string{repo.PermClassEditOwn}}
	f.roles[repo.RoleAdmin] = &repo.Role{ID: repo.RoleAdmin, Perms: []string{repo.PermClassEditOwn, repo.PermClassViewAll}}
	f.roles["jiaowu"] = &repo.Role{ID: "jiaowu", Perms: []string{repo.PermClassEditAll}}
	f.roles["locked"] = &repo.Role{ID: "locked"}

	// 班级：9 班建班人 = 100（admin）；10 班建班人 = 104（locked）
	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 100, InviteCode: "ab12"}
	f.classes[10] = &repo.Class{ID: 10, Name: "十班", OwnerID: 104, InviteCode: "cd34"}
	f.memberRoles[[2]int64{9, 101}] = repo.ClassRoleAdmin  // 学生班长（班级管理员）
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember // 普通成员

	h := newTestHandlers(f)
	r := classEngine(h)
	owner := f.addUser(h, 100, "teacher", repo.RoleAdmin, 0)
	classAdmin := f.addUser(h, 101, "monitor", repo.RoleDefault, 0)
	member := f.addUser(h, 102, "stu", repo.RoleDefault, 0)
	jiaowu := f.addUser(h, 103, "jiaowu", "jiaowu", 0) // 非任何班成员
	lockedOwner := f.addUser(h, 104, "locked", "locked", 0)

	manageable := func(tok string, classID string) bool {
		code, body := doJSON(r, "GET", "/api/classes/"+classID, tok, "")
		if code != http.StatusOK {
			t.Fatalf("GET /classes/%s = %d %v，期望 200", classID, code, body)
		}
		return body["data"].(map[string]any)["manageable"].(bool)
	}

	// edit.own 三态：建班人 ✓ / 班级管理员 ✓ / 普通成员 ✗
	if !manageable(owner, "9") {
		t.Fatal("建班人持 class.edit.own 应可管理本班")
	}
	if !manageable(classAdmin, "9") {
		t.Fatal("班级管理员持 class.edit.own 应可管理本班")
	}
	if manageable(member, "9") {
		t.Fatal("普通成员即便持 class.edit.own 也不可管理（仅建班人/班级管理员）")
	}
	// edit.all：非成员可管任意班，管理面（邀请码/成员表）随 manage 下发
	if !manageable(jiaowu, "9") || !manageable(jiaowu, "10") {
		t.Fatal("class.edit.all 应可管理任意班级（含他人班）")
	}
	_, body := doJSON(r, "GET", "/api/classes/9", jiaowu, "")
		data := body["data"].(map[string]any)
	if data["invite_code"] != "ab12" {
		t.Fatalf("edit.all 管理视角应下发邀请码，got %v", data["invite_code"])
	}
	// 无位收权：建班人也不可管理
	if manageable(lockedOwner, "10") {
		t.Fatal("无 class.edit.own 的建班人应被收权（仅 root 不受限）")
	}

	// 编辑端点同口径：成员/被收权建班人 403；edit.all 他人班 200 且落 patch
	if code, _ := doJSON(r, "PATCH", "/api/classes/9", member, `{"name":"乱改"}`); code != http.StatusForbidden {
		t.Fatalf("普通成员改班级 = %d，期望 403", code)
	}
	if code, _ := doJSON(r, "PATCH", "/api/classes/10", lockedOwner, `{"name":"自救"}`); code != http.StatusForbidden {
		t.Fatalf("被收权建班人改自己班 = %d，期望 403", code)
	}
	if code, body := doJSON(r, "PATCH", "/api/classes/10", jiaowu, `{"name":"教务代管"}`); code != http.StatusOK {
		t.Fatalf("edit.all 改他人班 = %d %v，期望 200", code, body)
	}
	if got := f.classPatches[10].Name; got == nil || *got != "教务代管" {
		t.Fatalf("edit.all 编辑应落 patch，got %+v", f.classPatches[10])
	}
}
