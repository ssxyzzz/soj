package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
	"soj/internal/testdata"
)

// ListCheckerProblems 内存版：题目表里配了校验器的题（题号升序），语义同真查询。
func (f *fakeRepo) ListCheckerProblems(context.Context) ([]repo.CheckerMeta, bool, error) {
	var out []repo.CheckerMeta
	for id, p := range f.problems {
		if p.CheckerFile != "" {
			out = append(out, repo.CheckerMeta{ID: id, Title: p.Title, File: p.CheckerFile})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, false, nil
}

func TestCheckerSourceExt(t *testing.T) {
	cases := map[string]bool{
		"checker.cpp":    true,
		"checker.cc":     true,
		"checker.cxx":    true,
		"CHECKER.CC":     true,
		"checker.txt":    false,
		"checker.cc.txt": false,
		"checker":        false,
		"checker.h":      false,
		"":               false,
	}
	for name, want := range cases {
		if got := checkerSourceExt(name); got != want {
			t.Errorf("checkerSourceExt(%q) = %v, want %v", name, got, want)
		}
	}
}

// 校验器上传入口：洛谷惯例的 checker.cc 等扩展名放行，落卷仍是固定名 data/checker.cpp。
func TestUpdateCheckerUpload(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	h.cfg.DataDir = t.TempDir() // 不预置 store：覆盖 provider() 的惰性构造路径
	f.problems[7] = &repo.Problem{ID: 7, Title: "SPJ 题"}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api", AuthRequired(h))
	authed.POST("/problems/:id/checker",
		RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.UpdateChecker)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)
	stuTok := f.addUser(h, 2, "stu", repo.RoleDefault, 0)

	src := []byte("#include \"testlib.h\"\nint main(int argc, char* argv[]) {\n    registerTestlibCmd(argc, argv);\n    return 0;\n}\n")
	wantSHA := fmt.Sprintf("%x", sha256.Sum256(src))
	const path = "/api/problems/7/checker"
	for _, name := range []string{"checker.cc", "checker.CPP", "checker.cxx"} {
		code, out := doMultipart(r, path, rootTok, "checker", name, src)
		if code != http.StatusOK {
			t.Fatalf("上传 %s：状态 %d，响应 %v", name, code, out)
		}
		if got := out["data"].(map[string]any)["sha"]; got != wantSHA {
			t.Errorf("上传 %s：响应 sha = %v, want %s", name, got, wantSHA)
		}
	}
	got, err := testdata.ReadAll(h.provider(), 7, "data/checker.cpp")
	if err != nil || string(got) != string(src) {
		t.Fatalf("落卷 data/checker.cpp = %q, err %v；want 上传内容原样", got, err)
	}

	code, out := doMultipart(r, path, rootTok, "checker", "checker.txt", src)
	if code != http.StatusBadRequest {
		t.Errorf("非 C++ 源文件应 400，实际 %d（%v）", code, out)
	}
	code, _ = doMultipart(r, path, stuTok, "checker", "checker.cc", src)
	if code != http.StatusForbidden {
		t.Errorf("无编辑权限应 403，实际 %d", code)
	}
	code, _ = doMultipart(r, "/api/problems/99/checker", rootTok, "checker", "checker.cc", src)
	if code != http.StatusNotFound {
		t.Errorf("题目不存在应 404，实际 %d", code)
	}
}

// 全站校验器清单：只列配了校验器的题、源码逐字节保真、单题读失败不拖垮整表、有读取上限、
// 无「编辑任意题目」权限拿不到。
func TestListProblemCheckers(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	h.cfg.DataDir = t.TempDir() // 不预置 store：清单接口可能是进程启动后首个碰数据卷的请求
	f.problems[3] = &repo.Problem{ID: 3, Title: "乙", CheckerFile: "data/checker.cc"}
	f.problems[1] = &repo.Problem{ID: 1, Title: "甲", CheckerFile: "data/checker.cpp"}
	f.problems[2] = &repo.Problem{ID: 2, Title: "没配校验器"}
	f.problems[4] = &repo.Problem{ID: 4, Title: "文件缺失", CheckerFile: "data/checker.cpp"}
	f.problems[5] = &repo.Problem{ID: 5, Title: "文件超大", CheckerFile: "data/checker.cpp"}
	// 含非 UTF-8 字节与 NUL：transport 必须 base64 逐字节保真
	srcA := []byte("#include \"testlib.h\"\n\xff\xfe\x00ok\n")
	srcB := []byte("int main() { return 0; }\n")
	// 源文件直接落盘（不经 provider()），保证 h.store 在 handler 运行前仍是 nil
	mustWrite := func(pid int64, rel string, content []byte) {
		t.Helper()
		full := filepath.Join(h.cfg.DataDir, fmt.Sprint(pid), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(1, "data/checker.cpp", srcA)
	mustWrite(3, "data/checker.cc", srcB)
	mustWrite(5, "data/checker.cpp", make([]byte, checkerSourceMax+1))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api", AuthRequired(h))
	authed.GET("/problems/checkers", RequirePerm(repo.PermProblemEditAll), h.ListProblemCheckers)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)
	stuTok := f.addUser(h, 2, "stu", repo.RoleDefault, 0)

	const listPath = "/api/problems/checkers"
	code, out := doJSON(r, http.MethodGet, listPath, rootTok, "")
	if code != http.StatusOK {
		t.Fatalf("状态 %d，响应 %v", code, out)
	}
	data := out["data"].(map[string]any)
	if data["truncated"] != false {
		t.Errorf("truncated = %v, want false", data["truncated"])
	}
	rows := data["checkers"].([]any)
	if len(rows) != 4 {
		t.Fatalf("应列出 4 题（没配校验器的不列），实际 %d：%v", len(rows), rows)
	}
	byID := map[float64]map[string]any{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		byID[row["id"].(float64)] = row
	}
	if len(byID) != 4 {
		t.Fatalf("题号应各不相同：%v", byID)
	}
	row := byID[1]
	if row["title"] != "甲" || row["file"] != "data/checker.cpp" ||
		row["sha"] != fmt.Sprintf("%x", sha256.Sum256(srcA)) {
		t.Errorf("题 1 元信息不符：%v", row)
	}
	got, err := base64.StdEncoding.DecodeString(row["source_b64"].(string))
	if err != nil || string(got) != string(srcA) {
		t.Errorf("题 1 源码应逐字节保真，err %v", err)
	}
	if byID[3]["sha"] != fmt.Sprintf("%x", sha256.Sum256(srcB)) {
		t.Errorf("题 3 sha 不符：%v", byID[3])
	}
	if _, has := byID[4]["error"]; !has || byID[4]["sha"] != nil {
		t.Errorf("题 4 文件缺失应带 error 且无 sha：%v", byID[4])
	}
	if e, _ := byID[5]["error"].(string); !strings.Contains(e, "上限") {
		t.Errorf("题 5 超过读取上限应报错：%v", byID[5])
	}

	if code, _ := doJSON(r, http.MethodGet, listPath, stuTok, ""); code != http.StatusForbidden {
		t.Errorf("无「编辑任意题目」权限应 403，实际 %d", code)
	}
}

// 移除校验器同样走 provider()：进程启动后首个请求就是它也不能空指针。
// 源文件用系统调用直接落盘，保证 h.store 此时仍是 nil。
func TestRemoveCheckerLazyStore(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	h := newTestHandlers(f)
	h.cfg.DataDir = t.TempDir()
	f.problems[7] = &repo.Problem{ID: 7, Title: "SPJ 题", CheckerFile: "data/checker.cpp"}
	file := filepath.Join(h.cfg.DataDir, "7", "data", "checker.cpp")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("int main() { return 0; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api", AuthRequired(h))
	authed.DELETE("/problems/:id/checker",
		RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.RemoveChecker)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)

	code, out := doJSON(r, http.MethodDelete, "/api/problems/7/checker", rootTok, "")
	if code != http.StatusOK || out["data"].(map[string]any)["removed"] != true {
		t.Fatalf("状态 %d，响应 %v", code, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("校验器源文件应已删除，stat err = %v", err)
	}
}
