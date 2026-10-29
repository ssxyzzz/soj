package repo

// v0.31 资产链集成测试（真 Mongo，本地不可达自动 skip 同 openTestRepo）：
// assets 包导入 + 相对引用/外链转存改写（statementHtml 渲染前）、失败降级、
// ReplaceData 不波及 assets、保存后异步转存的定点替换、单次张数上限。
// 外链服务器走 httptest + NewWithCheck 注入放行回环（生产口径拒回环）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"soj/internal/safefetch"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

func allowAnyAddr(string, string) error { return nil }

// newAssetImporter 测试用导入器（放行回环的 fetcher + 临时数据卷）。
func newAssetImporter(t *testing.T, transfer bool) *Importer {
	t.Helper()
	return &Importer{
		Repo:           openTestRepo(t),
		DataDir:        t.TempDir(),
		CreatorUID:     1,
		TransferImages: transfer,
		ImageFetcher:   safefetch.NewWithCheck(allowAnyAddr),
	}
}

// newImageServer 外链图源：按路径生成不同内容的 png（内容寻址去重下仍可区分张数），
// /gone 前缀 404（失败降级用）。
func newImageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/gone") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(append(append([]byte{}, testPNG...), r.URL.Path...))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// assetFiles 卷内本题资产文件名清单。
func assetFiles(t *testing.T, dataDir string, pid int64) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, strconv.FormatInt(pid, 10), "assets", "*"))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, filepath.Base(m))
	}
	return out
}

// writeAssetProblemDir 造一个带 assets/ 与外链的 SOJ 题包目录。
func writeAssetProblemDir(t *testing.T, dir, statement string, pkgAssets map[string][]byte) {
	t.Helper()
	files := map[string]string{
		"problem.md":       statement,
		"problem.yaml":     "title: 资产测试题\ndifficulty: 3\n",
		"data/test1.in":    "1\n",
		"data/test1.ans":   "2\n",
		"data/config.yaml": "time: 1s\nmemory: 256m\n",
	}
	for k, v := range files {
		p := filepath.Join(dir, k)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range pkgAssets {
		p := filepath.Join(dir, "assets", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// 导入主链：assets/ 落库改名 + 相对引用改写 + 外链转存改写，均发生在 statementHtml
// 渲染之前（html 里已是站内 URL）。
func TestImportAssetsAndExternalTransfer(t *testing.T) {
	im := newAssetImporter(t, true)
	srv := newImageServer(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeAssetProblemDir(t, dir,
		"题面：![本地图](assets/x.png) 与 ![外链图]("+srv.URL+"/img.png)，另有 ![缺失](assets/missing.png)",
		map[string][]byte{
			"x.png":   testPNG,
			"bad.xyz": []byte("junk"),   // 后缀不在白名单 → 跳过
			"lie.png": []byte("<svg/>"), // 伪装 png → 魔数拒
		})
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}

	got, err := im.Repo.GetProblem(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Statement, "/problems/"+strconv.FormatInt(p.ID, 10)+"/assets/") {
		t.Fatalf("题面未改写为站内 URL: %s", got.Statement)
	}
	if strings.Contains(got.Statement, "assets/x.png") || strings.Contains(got.Statement, srv.URL+"/img.png") {
		t.Fatalf("已转存的引用应全部站内化: %s", got.Statement)
	}
	if !strings.Contains(got.Statement, "assets/missing.png") {
		t.Fatalf("缺失引用应原样保留: %s", got.Statement)
	}
	if !strings.Contains(got.StatementHtml, "/problems/") {
		t.Fatalf("statementHtml 应由改写后的题面渲染（含站内 URL）: %s", got.StatementHtml)
	}
	files := assetFiles(t, im.DataDir, p.ID)
	if len(files) != 2 { // 包内 1 张合法 + 外链 1 张；白名单/魔数拒绝的不落库
		t.Fatalf("资产文件应恰 2 个，得到 %v", files)
	}
	for _, f := range files {
		if !ValidAssetName(f) {
			t.Fatalf("资产文件名形状非法: %q", f)
		}
	}
}

// 失败降级：外链 404 / 开关关闭 → 保留外链，题目照常落库。
func TestImportExternalTransferDegrades(t *testing.T) {
	im := newAssetImporter(t, true)
	srv := newImageServer(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeAssetProblemDir(t, dir,
		"![坏图]("+srv.URL+"/gone.png) ![好图]("+srv.URL+"/ok.png)", nil)
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatalf("降级不得阻塞导入: %v", err)
	}
	got, _ := im.Repo.GetProblem(ctx, p.ID)
	if !strings.Contains(got.Statement, srv.URL+"/gone.png") {
		t.Fatalf("失败外链应保留: %s", got.Statement)
	}
	if strings.Contains(got.Statement, srv.URL+"/ok.png") {
		t.Fatalf("成功外链应已站内化: %s", got.Statement)
	}

	// 开关关闭：外链一律保留
	im2 := newAssetImporter(t, false)
	dir2 := t.TempDir()
	writeAssetProblemDir(t, dir2, "![图]("+srv.URL+"/ok.png)", nil)
	p2, err := im2.ImportDir(ctx, dir2)
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := im2.Repo.GetProblem(ctx, p2.ID)
	if !strings.Contains(got2.Statement, srv.URL+"/ok.png") {
		t.Fatalf("开关关闭时外链应保留: %s", got2.Statement)
	}
}

// ReplaceData 整包替换只清 data/，题面资产不波及。
func TestReplaceDataKeepsAssets(t *testing.T) {
	im := newAssetImporter(t, false)
	ctx := context.Background()

	dir := t.TempDir()
	writeAssetProblemDir(t, dir, "![图](assets/x.png)", map[string][]byte{"x.png": testPNG})
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(assetFiles(t, im.DataDir, p.ID)) != 1 {
		t.Fatal("前置：应已落 1 个资产")
	}

	dir2 := t.TempDir()
	writeAssetProblemDir(t, dir2, "替换后的题面（无图）", nil)
	if _, err := im.ReplaceData(ctx, p.ID, dir2); err != nil {
		t.Fatalf("ReplaceData: %v", err)
	}
	if got := assetFiles(t, im.DataDir, p.ID); len(got) != 1 {
		t.Fatalf("ReplaceData 不应波及 assets，得到 %v", got)
	}
	// 题面也不动（ReplaceData 语义）
	got, _ := im.Repo.GetProblem(ctx, p.ID)
	if !strings.Contains(got.Statement, "assets/") || strings.Contains(got.Statement, "替换") {
		t.Fatalf("ReplaceData 不应改题面: %s", got.Statement)
	}
}

// 保存后异步转存（同步驱动）：定点替换 statement 与 statementHtml 双存——URL 在两处
// 同串出现，替换后无需重渲染。
func TestTransferProblemAssetsTargetedReplace(t *testing.T) {
	im := newAssetImporter(t, true)
	srv := newImageServer(t)
	ctx := context.Background()

	p := &Problem{
		ID: 999901, Title: "异步转存测试", Type: TypeDefault,
		Statement:   "题面 ![外链](" + srv.URL + "/a.png) 与 ![另一个](" + srv.URL + "/b.png)",
		TimeLimitMS: 1000, MemoryLimitKB: 262144, Visibility: VisibilityPublic,
		Tags: []int64{}, Cases: []CaseMeta{},
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.StatementHtml, srv.URL) {
		t.Fatal("前置：渲染 html 应含原 URL")
	}
	if err := im.transferProblemAssets(ctx, p.ID); err != nil {
		t.Fatalf("transferProblemAssets: %v", err)
	}
	got, _ := im.Repo.GetProblem(ctx, p.ID)
	if strings.Contains(got.Statement, srv.URL) || strings.Contains(got.StatementHtml, srv.URL) {
		t.Fatalf("双存应都已定点替换:\n%s\n%s", got.Statement, got.StatementHtml)
	}
	if !strings.Contains(got.StatementHtml, "<img") {
		t.Fatalf("html 应保持渲染形态（未重渲染也完整）: %s", got.StatementHtml)
	}
	if len(assetFiles(t, im.DataDir, p.ID)) != 2 {
		t.Fatalf("应落 2 个资产文件")
	}

	// 无外链题：零动作零写库
	p2 := &Problem{
		ID: 999902, Title: "无外链", Type: TypeDefault,
		Statement: "纯文字题面", TimeLimitMS: 1000, MemoryLimitKB: 262144,
		Visibility: VisibilityPublic, Tags: []int64{}, Cases: []CaseMeta{},
	}
	if err := im.Repo.UpsertProblem(ctx, p2); err != nil {
		t.Fatal(err)
	}
	if err := im.transferProblemAssets(ctx, p2.ID); err != nil {
		t.Fatalf("无外链应零动作: %v", err)
	}
}

// 单次转存张数上限：第 21 张保留外链（防单题拖垮整包导入时长）。
func TestTransferImageCountBudget(t *testing.T) {
	im := newAssetImporter(t, true)
	srv := newImageServer(t)
	ctx := context.Background()

	var sb strings.Builder
	for i := 0; i <= AssetMaxTransfer; i++ { // 21 张：前 20 转存、第 21 保留外链
		sb.WriteString("![图](" + srv.URL + "/n" + strconv.Itoa(i) + ".png) ")
	}
	p := &Problem{
		ID: 999903, Title: "张数上限", Type: TypeDefault,
		Statement: sb.String(), TimeLimitMS: 1000, MemoryLimitKB: 262144,
		Visibility: VisibilityPublic, Tags: []int64{}, Cases: []CaseMeta{},
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := im.transferProblemAssets(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := im.Repo.GetProblem(ctx, p.ID)
	last := srv.URL + "/n" + strconv.Itoa(AssetMaxTransfer) + ".png"
	if !strings.Contains(got.Statement, last) {
		t.Fatalf("第 %d 张应保留外链（超单次上限）", AssetMaxTransfer+1)
	}
	if strings.Contains(got.Statement, srv.URL+"/n0.png") {
		t.Fatalf("前 20 张应已转存")
	}
}
