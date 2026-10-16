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
