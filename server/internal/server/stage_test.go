package server

// 数据 zip 暂存上传（v0.97.15 选包即传、确认才生效）契约测试：POST /api/problems/testdata/stage。
// 核心口径：①合法包落盘并返回 stage id（文件名内嵌 uid/ts）；②坏包/缺 data/ 当场 400
// 且零残留；③匿名 401 / 无权限 403；④stagedZipPath 属主与 TTL 校验（越权、过期、不存在
// 均拒绝）；⑤evictStaleStage 每 uid 上限逐出最旧；⑥CleanupStaleStage 只清过期。
// apply（建题/替换引用 stage）依赖 concreteRepo 具体 Mongo 实例，按项目惯例走 localhost
// ego 验收，不在 fake 层造假。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// zipBytes 内存构造 zip（entries：路径 → 内容）。
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// doMultipart POST multipart 表单（单文件字段），返回状态码与 JSON 响应。
func doMultipart(r *gin.Engine, path, token, field, filename string, content []byte) (int, map[string]any) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		panic(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// stageTestSweep 清掉测试落下的暂存文件（真实 os.TempDir，不留垃圾）。
func stageTestSweep(t *testing.T, uid int64) {
	t.Helper()
	t.Cleanup(func() {
		ms, _ := filepath.Glob(filepath.Join(os.TempDir(), fmt.Sprintf("%s%d-*.zip", stageFilePrefix, uid)))
		for _, m := range ms {
			_ = os.Remove(m)
		}
	})
}

func TestStageUploadContract(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleRoot] = &repo.Role{ID: repo.RoleRoot, Perms: []string{repo.PermAll}}
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	h := newTestHandlers(f)
	r := testEngine(h)
	rootTok := f.addUser(h, 1, "root", repo.RoleRoot, 0)
	stuTok := f.addUser(h, 2, "stu", repo.RoleDefault, 0)
	stageTestSweep(t, 1)

	good := zipBytes(t, map[string]string{"data/1.in": "3\n", "data/1.ans": "6\n"})

	// 匿名 401
	if code, _ := doMultipart(r, "/api/problems/testdata/stage", "", "file", "data.zip", good); code != http.StatusUnauthorized {
		t.Fatalf("匿名 stage = %d，期望 401", code)
	}
	// 无权限学生 403
	if code, _ := doMultipart(r, "/api/problems/testdata/stage", stuTok, "file", "data.zip", good); code != http.StatusForbidden {
		t.Fatalf("学生 stage = %d，期望 403", code)
	}

	// 合法包：200 + id 属主为登录 uid + 文件落盘
	code, body := doMultipart(r, "/api/problems/testdata/stage", rootTok, "file", "data.zip", good)
	if code != http.StatusOK {
		t.Fatalf("合法包 stage = %d：%v", code, body)
	}
	d := body["data"].(map[string]any)
	id := d["stage"].(string)
	m := stageFileRe.FindStringSubmatch(id)
	if m == nil || m[1] != "1" {
		t.Fatalf("stage id 未内嵌属主 uid：%q", id)
	}
	if d["name"] != "data.zip" {
		t.Fatalf("name 回显错误：%v", d["name"])
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), id)); err != nil {
		t.Fatal("暂存文件未落盘")
	}

	// 坏包（非 zip 字节）：400 + 零残留
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), stageFilePrefix+"1-*.zip"))
	if code, body := doMultipart(r, "/api/problems/testdata/stage", rootTok, "file", "bad.zip", []byte("这不是zip")); code != http.StatusBadRequest {
		t.Fatalf("坏包 stage = %d，期望 400：%v", code, body)
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), stageFilePrefix+"1-*.zip"))
	if len(after) != len(before) {
		t.Fatalf("坏包留下残留：%v", after)
	}

	// 缺 data/：400 明确提示
	noData := zipBytes(t, map[string]string{"foo.txt": "hi"})
	if code, body := doMultipart(r, "/api/problems/testdata/stage", rootTok, "file", "nodata.zip", noData); code != http.StatusBadRequest {
		t.Fatalf("缺 data/ stage = %d，期望 400：%v", code, body)
	} else if msg := body["message"].(string); !strings.Contains(msg, "data/") {
		t.Fatalf("缺 data/ 提示不含关键字：%q", msg)
	}

	// 缺 file 字段：400
	if code, _ := doMultipart(r, "/api/problems/testdata/stage", rootTok, "other", "x.zip", good); code != http.StatusBadRequest {
		t.Fatalf("缺 file 字段 = %d，期望 400", code)
	}
}

func TestStagedZipPathGuards(t *testing.T) {
	// 手工铺暂存文件（绕开端点直接铺场景）：属主 7，两个时间点
	fresh := fmt.Sprintf("%s7-%d-0123456789abcdef.zip", stageFilePrefix, time.Now().Unix())
	expired := fmt.Sprintf("%s7-%d-0123456789abcdee.zip", stageFilePrefix, time.Now().Add(-25*time.Hour).Unix())
	for _, name := range []string{fresh, expired} {
		if err := os.WriteFile(filepath.Join(os.TempDir(), name), []byte("z"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(filepath.Join(os.TempDir(), name))
	}

	if _, err := stagedZipPath(7, fresh); err != nil {
		t.Fatalf("本人新鲜暂存应放行：%v", err)
	}
	if _, err := stagedZipPath(8, fresh); err == nil || !strings.Contains(err.Error(), "无权") {
		t.Fatalf("越权 uid 应拒，得到：%v", err)
	}
	if _, err := stagedZipPath(7, expired); err == nil || !strings.Contains(err.Error(), "过期") {
		t.Fatalf("过期暂存应拒，得到：%v", err)
	}
	if _, err := stagedZipPath(7, "../etc/passwd"); err == nil {
		t.Fatal("路径穿越 id 应拒")
	}
	if _, err := stagedZipPath(7, fmt.Sprintf("%s7-%d-ffffffffffffffff.zip", stageFilePrefix, time.Now().Unix())); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("不存在暂存应拒，得到：%v", err)
	}
}

func TestStageEvictAndCleanup(t *testing.T) {
	const uid int64 = 99042
	for i := range 4 {
		name := fmt.Sprintf("%s%d-%d-%016x.zip", stageFilePrefix, uid, 1000+i, i)
		if err := os.WriteFile(filepath.Join(os.TempDir(), name), []byte("z"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(filepath.Join(os.TempDir(), name))
	}
	evictStaleStage(uid) // 已有 4 个超上限：应清出 2 个空位（删最旧 2 个）
	ms, _ := filepath.Glob(filepath.Join(os.TempDir(), fmt.Sprintf("%s%d-*.zip", stageFilePrefix, uid)))
	if len(ms) != 2 {
		t.Fatalf("逐出后应剩 %d 个（stagePerUser-1），实际 %d：%v", stagePerUser-1, len(ms), ms)
	}
	for _, m := range ms {
		if strings.HasSuffix(m, "0000000000000000.zip") || strings.HasSuffix(m, "0000000000000001.zip") {
			t.Fatalf("删错文件：最旧两个应被逐出：%v", m)
		}
	}

	// CleanupStaleStage：过期清、新鲜留
	old := fmt.Sprintf("%s%d-%d-oldoldold01234567.zip", stageFilePrefix, uid, time.Now().Add(-48*time.Hour).Unix())
	newf := fmt.Sprintf("%s%d-%d-newnewnew01234567.zip", stageFilePrefix, uid, time.Now().Unix())
	for _, name := range []string{old, newf} {
		p := filepath.Join(os.TempDir(), name)
		if err := os.WriteFile(p, []byte("z"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(p)
	}
	st, _ := time.Parse("2006-01-02", "2020-01-01")
	_ = os.Chtimes(filepath.Join(os.TempDir(), old), st, st)
	if n := CleanupStaleStage(24 * time.Hour); n < 1 {
		t.Fatalf("过期暂存应被清扫，count=%d", n)
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), old)); !os.IsNotExist(err) {
		t.Fatal("过期暂存文件仍在")
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), newf)); err != nil {
		t.Fatal("新鲜暂存被误删")
	}
}

func TestResolveUploadZip(t *testing.T) {
	good := zipBytes(t, map[string]string{"data/1.in": "1\n"})

	// 直传 part：返回临时副本路径，调用方负责删
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("zip", "data.zip")
	_, _ = fw.Write(good)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	_ = req.ParseMultipartForm(maxCreateRequest)
	path, staged, err := resolveUploadZip(req.MultipartForm, 1)
	if err != nil || staged || path == "" {
		t.Fatalf("直传回落失败：path=%q staged=%v err=%v", path, staged, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("直传临时副本未落盘")
	}
	os.Remove(path)

	// 空：无 zip 无 stage → path=""（真实空 multipart 体）
	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	_ = mw2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/", &buf2)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	_ = req2.ParseMultipartForm(maxCreateRequest)
	if p, s, err := resolveUploadZip(req2.MultipartForm, 1); p != "" || s || err != nil {
		t.Fatalf("空表单应返回空路径：p=%q s=%v err=%v", p, s, err)
	}

	// stage 与 zip 同时提供 → 拒
	var buf3 bytes.Buffer
	mw3 := multipart.NewWriter(&buf3)
	fw3, _ := mw3.CreateFormFile("zip", "data.zip")
	_, _ = fw3.Write(good)
	_ = mw3.WriteField("stage", "soj-stage-1-1-0000000000000000.zip")
	_ = mw3.Close()
	req3 := httptest.NewRequest(http.MethodPost, "/", &buf3)
	req3.Header.Set("Content-Type", mw3.FormDataContentType())
	_ = req3.ParseMultipartForm(maxCreateRequest)
	if _, _, err := resolveUploadZip(req3.MultipartForm, 1); err == nil {
		t.Fatal("stage 与 zip 同时提供应拒绝")
	}
}
