package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// withIdent 构造带身份的测试上下文（模拟 AuthRequired 写入的键）。
func withIdent(t *testing.T, uid int64, role string, perms []string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if role != "" {
		c.Set("uid", uid)
		c.Set("uname", "tester")
		c.Set("role", role)
		c.Set("perms", perms)
	}
	return c, w
}

func TestHasPerm(t *testing.T) {
	c, _ := withIdent(t, 7, "backup-operator", []string{repo.PermBackupRun})
	if !hasPerm(c, repo.PermBackupRun) {
		t.Error("持有权限位应通过")
	}
	if hasPerm(c, repo.PermAdminSettings) {
		t.Error("未持有的权限位不应通过")
	}
}

func TestHasPermRootAlwaysPasses(t *testing.T) {
	// root 的权限判定不依赖文档/缓存内容（角色文档被篡改也不影响）
	c, _ := withIdent(t, 1, repo.RoleRoot, []string{})
	for _, p := range repo.PermCatalog {
		if !hasPerm(c, p.ID) {
			t.Fatalf("root 应对 %s 恒通过", p.ID)
		}
	}
}

func TestRequirePermDenyAndPass(t *testing.T) {
	// 未登录
	c, w := withIdent(t, 0, "", nil)
	RequirePerm(repo.PermClassCreate)(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401, got %d", w.Code)
	}
	// 已登录但无权限位
	c, w = withIdent(t, 7, "default", []string{})
	RequirePerm(repo.PermClassCreate)(c)
	if w.Code != http.StatusForbidden {
		t.Errorf("无权限位应 403, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Error("无权限时应中断请求链")
	}
	// 持有权限位放行
	c, w = withIdent(t, 7, "ta", []string{repo.PermClassCreate})
	RequirePerm(repo.PermClassCreate)(c)
	if w.Code != http.StatusOK {
		t.Errorf("持有权限位应放行, got %d", w.Code)
	}
}

func TestRequirePermAnyAndRoot(t *testing.T) {
	c, w := withIdent(t, 7, "author", []string{repo.PermProblemEditOwn})
	RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn)(c)
	if w.Code != http.StatusOK {
		t.Errorf("edit.own 应命中任一分支, got %d", w.Code)
	}

	c, w = withIdent(t, 2, repo.RoleAdmin, []string{})
	RequireRoot()(c)
	if w.Code != http.StatusForbidden {
		t.Errorf("非 root 访问角色管理应 403, got %d", w.Code)
	}

	c, w = withIdent(t, 1, repo.RoleRoot, nil)
	RequireRoot()(c)
	if w.Code != http.StatusOK {
		t.Errorf("root 应通过, got %d", w.Code)
	}
}
