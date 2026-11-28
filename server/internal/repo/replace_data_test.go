package repo

// ReplaceData 的题型专属文件同步（真 Mongo，本地不可达自动 skip 同 openTestRepo）：
// 校验器 / 交互器在编辑页是即时上传替换的，不随数据包提交——整包替换时新包带了才换
// （并作废编译产物缓存），没带就原样沿用（源码文件在 data/ 清空前读出、落完新数据写回）。
// 此前：换了校验器源码却不清 CheckerFileID，旧编译产物继续判到沙箱缓存过期；
// 新包没带时元数据指向已被清空的文件，缓存一过期整题 System Error。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"soj/internal/testdata"
)

// writeReplaceDataDir 造一个可直接喂给 ImportDir / ReplaceData 的题包目录
// （files 为 data/ 内的附加文件，同名覆盖默认数据对）。
func writeReplaceDataDir(t *testing.T, dir, typ, config string, files map[string]string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("problem.md", "题面")
	write("problem.yaml", "title: 替换测试\ntype: "+typ+"\n")
	write("data/test1.in", "1\n")
	if typ != TypeInteractive {
		write("data/test1.ans", "2\n")
	}
	write("data/config.yaml", config)
	for rel, content := range files {
		write("data/"+rel, content)
	}
}

// readStored 读题目数据卷内的一个文件。
func readStored(t *testing.T, im *Importer, pid int64, rel string) string {
	t.Helper()
	src, err := testdata.ReadAll(testdata.NewLocalFS(im.DataDir), pid, rel)
	if err != nil {
		t.Fatalf("读取 %s: %v", rel, err)
	}
	return string(src)
}

// 新包声明了校验器：落新源码，编译产物缓存作废（判题时按新源码重编）。
func TestReplaceDataNewCheckerInvalidatesCompileCache(t *testing.T) {
	im := newAssetImporter(t, false)
	ctx := context.Background()
	const cfg = "time: 1s\nmemory: 256m\nchecker: chk.cc\n"
	const oldSrc = "int main() { return 0; } // 旧校验器\n"
	const newSrc = "int main() { return 1; } // 新校验器\n"

	dir := t.TempDir()
	writeReplaceDataDir(t, dir, TypeDefault, cfg, map[string]string{"chk.cc": oldSrc})
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	// 模拟判题机懒编译后回填的缓存产物
	cached := "cached-old"
	if err := im.Repo.UpdateProblemMeta(ctx, p.ID, ProblemMetaPatch{CheckerFileID: &cached}); err != nil {
		t.Fatal(err)
	}

	dir2 := t.TempDir()
	writeReplaceDataDir(t, dir2, TypeDefault, cfg, map[string]string{"chk.cc": newSrc})
	if _, err := im.ReplaceData(ctx, p.ID, dir2); err != nil {
		t.Fatalf("ReplaceData: %v", err)
	}

	got, err := im.Repo.GetProblem(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantSHA := sha256Hex([]byte(newSrc))
	if got.CheckerFile != "data/chk.cc" || got.CheckerSHA != wantSHA {
		t.Errorf("校验器元数据 = (%q, %s), want (data/chk.cc, %s)", got.CheckerFile, got.CheckerSHA, wantSHA)
	}
	if got.CheckerFileID != "" {
		t.Errorf("换了校验器源码后编译缓存应作废：CheckerFileID = %q, want 空", got.CheckerFileID)
	}
	if src := readStored(t, im, p.ID, "data/chk.cc"); src != newSrc {
		t.Errorf("落卷源码 = %q, want 新校验器", src)
	}
}

// 新包没带校验器：现有校验器原样沿用——元数据、编译缓存不动，源码文件在 data/
// 整目录清空后写回；新数据本身照常替换。
func TestReplaceDataWithoutCheckerKeepsExisting(t *testing.T) {
	im := newAssetImporter(t, false)
	ctx := context.Background()
	const oldSrc = "int main() { return 0; } // 旧校验器\n"

	dir := t.TempDir()
	writeReplaceDataDir(t, dir, TypeDefault, "time: 1s\nmemory: 256m\nchecker: chk.cc\n", map[string]string{"chk.cc": oldSrc})
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	cached := "cached-old"
	if err := im.Repo.UpdateProblemMeta(ctx, p.ID, ProblemMetaPatch{CheckerFileID: &cached}); err != nil {
		t.Fatal(err)
	}

	dir2 := t.TempDir()
	writeReplaceDataDir(t, dir2, TypeDefault, "time: 1s\nmemory: 256m\n", map[string]string{"test1.ans": "3\n"})
	if _, err := im.ReplaceData(ctx, p.ID, dir2); err != nil {
		t.Fatalf("ReplaceData: %v", err)
	}

	got, err := im.Repo.GetProblem(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CheckerFile != "data/chk.cc" || got.CheckerSHA != sha256Hex([]byte(oldSrc)) {
		t.Errorf("新包没带校验器时元数据应沿用：得到 (%q, %s)", got.CheckerFile, got.CheckerSHA)
	}
	if got.CheckerFileID != cached {
		t.Errorf("沿用的校验器编译缓存应保留：CheckerFileID = %q, want %q", got.CheckerFileID, cached)
	}
	if src := readStored(t, im, p.ID, "data/chk.cc"); src != oldSrc {
		t.Errorf("沿用的校验器源码应写回：得到 %q", src)
	}
	if ans := readStored(t, im, p.ID, "data/test1.ans"); ans != "3\n" {
		t.Errorf("新数据应已替换：test1.ans = %q", ans)
	}
}

// 交互器同理：新包带了 → 换源码并作废编译缓存；没带 → 沿用现有的。
func TestReplaceDataInteractorCompileCache(t *testing.T) {
	im := newAssetImporter(t, false)
	ctx := context.Background()
	const cfg = "time: 1s\nmemory: 256m\n"
	const oldSrc = "int main() { return 0; } // 旧交互器\n"
	const newSrc = "int main() { return 1; } // 新交互器\n"

	dir := t.TempDir()
	writeReplaceDataDir(t, dir, TypeInteractive, cfg, map[string]string{"interactor.cpp": oldSrc})
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	cached := "cached-old"
	if err := im.Repo.UpdateProblemMeta(ctx, p.ID, ProblemMetaPatch{InteractorFileID: &cached}); err != nil {
		t.Fatal(err)
	}

	dir2 := t.TempDir()
	writeReplaceDataDir(t, dir2, TypeInteractive, cfg, map[string]string{"interactor.cpp": newSrc})
	if _, err := im.ReplaceData(ctx, p.ID, dir2); err != nil {
		t.Fatalf("ReplaceData（带新交互器）: %v", err)
	}
	got, err := im.Repo.GetProblem(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	newSHA := sha256Hex([]byte(newSrc))
	if got.InteractorFile != "data/interactor.cpp" || got.InteractorSHA != newSHA {
		t.Errorf("交互器元数据 = (%q, %s), want (data/interactor.cpp, %s)", got.InteractorFile, got.InteractorSHA, newSHA)
	}
	if got.InteractorFileID != "" {
		t.Errorf("换了交互器源码后编译缓存应作废：InteractorFileID = %q, want 空", got.InteractorFileID)
	}
	if src := readStored(t, im, p.ID, "data/interactor.cpp"); src != newSrc {
		t.Errorf("落卷源码 = %q, want 新交互器", src)
	}

	// 没带交互器的数据包：沿用，不报缺文件
	cached = "cached-new"
	if err := im.Repo.UpdateProblemMeta(ctx, p.ID, ProblemMetaPatch{InteractorFileID: &cached}); err != nil {
		t.Fatal(err)
	}
	dir3 := t.TempDir()
	writeReplaceDataDir(t, dir3, TypeInteractive, cfg, nil)
	if _, err := im.ReplaceData(ctx, p.ID, dir3); err != nil {
		t.Fatalf("ReplaceData（没带交互器）应沿用现有的，实际报错: %v", err)
	}
	got, err = im.Repo.GetProblem(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.InteractorSHA != newSHA || got.InteractorFileID != cached {
		t.Errorf("没带交互器时应沿用：得到 (sha %s, cache %q), want (sha %s, cache %q)", got.InteractorSHA, got.InteractorFileID, newSHA, cached)
	}
	if src := readStored(t, im, p.ID, "data/interactor.cpp"); src != newSrc {
		t.Errorf("沿用的交互器源码应写回：得到 %q", src)
	}
}
