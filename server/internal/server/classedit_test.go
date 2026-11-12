package server

// 班级编辑权限位（v0.96）契约测试：classManageable 收口 = class.edit.all 可管任意班 /
// class.edit.own 限建班人与班级管理员 / 无位者（含建班人）不可编辑；root 恒通走
// hasPerm 的 "*" 判定。以 GetClass 的 manageable 标志 + UpdateClass 的 200/403 双面钉死。

import (
	"context"
	"net/http"
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

func (f *fakeRepo) ListMembers(context.Context, int64) ([]repo.ClassMember, error) {
	return nil, nil
}

func classEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api", AuthRequired(h))
	authed.GET("/classes/:id", h.GetClass)
	authed.PATCH("/classes/:id", h.UpdateClass)
	return r
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
