package repo

import (
	"reflect"
	"testing"
)

func TestValidPermSet(t *testing.T) {
	cases := []struct {
		name  string
		perms []string
		want  bool
	}{
		{"空集合法", []string{}, true},
		{"nil 合法", nil, true},
		{"目录内合法", []string{PermProblemCreate, PermClassCreate}, true},
		{"班级编辑双位合法", []string{PermClassEditAll, PermClassEditOwn}, true},
		{"拆分新位合法", []string{PermProblemImport}, true},
		{"通配符拒绝", []string{PermAll}, false},
		{"混合通配符拒绝", []string{PermProblemCreate, PermAll}, false},
		{"目录外拒绝", []string{"problem.delete"}, false},
		{"重复拒绝", []string{PermProblemCreate, PermProblemCreate}, false},
	}
	for _, tc := range cases {
		if got := ValidPermSet(tc.perms); got != tc.want {
			t.Errorf("%s: ValidPermSet(%v) = %v, want %v", tc.name, tc.perms, got, tc.want)
		}
	}
}

func TestEffectivePerms(t *testing.T) {
	if got := EffectivePerms(&Role{ID: RoleRoot, Perms: []string{PermAll}}); !reflect.DeepEqual(got, PermIDs()) {
		t.Errorf("root 应展开为全目录，got %v", got)
	}
	if got := EffectivePerms(&Role{ID: RoleRoot}); len(got) != len(PermCatalog) {
		t.Errorf("root 无视文档内容恒全量，got %d 项", len(got))
	}
	if got := EffectivePerms(nil); len(got) != len(PermCatalog) {
		t.Errorf("nil 角色按 root 处理，got %d 项", len(got))
	}
	// 目录外残留自动失效（权限位从目录移除后的自愈）
	got := EffectivePerms(&Role{ID: "custom", Perms: []string{PermBackupRun, "legacy.perm"}})
	if !reflect.DeepEqual(got, []string{PermBackupRun}) {
		t.Errorf("目录外权限位应被过滤，got %v", got)
	}
}

func TestBuiltinAdminHasCreateAndImport(t *testing.T) {
	// v0.74 严格拆分后 admin 内置角色保持建题+导入双能力（存量库由 problemImportPermV1 迁移补发）
	for _, r := range builtinRoles {
		if r.ID != RoleAdmin {
			continue
		}
		has := func(p string) bool {
			for _, x := range r.Perms {
				if x == p {
					return true
				}
			}
			return false
		}
		if !has(PermProblemCreate) || !has(PermProblemImport) {
			t.Fatalf("admin 内置角色应同时含建题与批量导入位: %v", r.Perms)
		}
	}
}

func TestBuiltinClassEditPerms(t *testing.T) {
	// v0.96 班级编辑权限位化：default/admin 内置含 class.edit.own（建班人/班级管理员
	// 现状可编辑，存量库由 classEditPermV1 迁移补发）；edit.all 刻意不给任何内置角色
	for _, r := range builtinRoles {
		has := func(p string) bool {
			for _, x := range r.Perms {
				if x == p {
					return true
				}
			}
			return false
		}
		switch r.ID {
		case RoleDefault, RoleAdmin:
			if !has(PermClassEditOwn) {
				t.Fatalf("%s 内置角色应含 class.edit.own: %v", r.ID, r.Perms)
			}
		}
		if has(PermClassEditAll) && r.ID != RoleRoot {
			t.Fatalf("%s 内置角色不应含 class.edit.all（按需授予自定义角色）: %v", r.ID, r.Perms)
		}
	}
}

func TestPermCatalogIntegrity(t *testing.T) {
	if len(PermIDs()) != len(PermCatalog) {
		t.Fatal("PermIDs 与 PermCatalog 长度不一致")
	}
	for _, p := range PermCatalog {
		if !PermExists(p.ID) {
			t.Errorf("目录条目 %s 未被索引", p.ID)
		}
		if p.Label == "" {
			t.Errorf("目录条目 %s 缺少中文标签", p.ID)
		}
	}
}
