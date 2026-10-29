package archive

// 解压/摊平纯函数测试（自 server/problemAdmin_test.go 随迁——v0.32 巨型文件治理，
// 用例与断言零变化，仅调用名改导出）。无 Mongo、无网络，CI 可跑。

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func mkTree(t *testing.T, base string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
}

func mustExist(t *testing.T, base, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("%s 应存在: %v", rel, err)
	}
}

func mustNotExist(t *testing.T, base, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("%s 应不存在（err = %v）", rel, err)
	}
}

func TestUnwrapSingleRootDir(t *testing.T) {
	t.Run("顶层包裹目录整体上移", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"A+B Problem/cases/1/in":  "1 2\n",
			"A+B Problem/cases/1/ans": "3\n",
			"A+B Problem/config.yaml": "samples: [1]\n",
			"A+B Problem/2.in":        "4 5\n",
			"A+B Problem/2.ans":       "9\n",
		})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "cases/1/in")
		mustExist(t, dir, "config.yaml")
		mustExist(t, dir, "2.in")
		mustNotExist(t, dir, "A+B Problem")
	})

	t.Run("多层包裹逐层下钻", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"下载包/problem/cases/1/in":  "1 2\n",
			"下载包/problem/cases/1/ans": "3\n",
		})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "cases/1/in")
		mustNotExist(t, dir, "下载包")
	})

	t.Run("标准 cases 结构不动", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"cases/1/in":  "1 2\n",
			"cases/1/ans": "3\n",
		})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "cases/1/in")
	})

	t.Run("根下平铺 .in/.ans 不动", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"1.in":  "1 2\n",
			"1.ans": "3\n",
		})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "1.in")
	})

	t.Run("打包杂物不影响判断且保留", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"pkg/cases/1/in":    "1 2\n",
			"pkg/cases/1/ans":   "3\n",
			"__MACOSX/pkg/._in": "\x00\x01",
			".DS_Store":         "",
		})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "cases/1/in")
		mustNotExist(t, dir, "pkg")
		mustExist(t, dir, "__MACOSX")
	})

	t.Run("data 为保留名不下钻（v0.15 数据目录）", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"data/test1.in": "1\n",
		})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "data/test1.in")
	})

	t.Run("根下仅剩单个文件不动", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{"config.yaml": "samples: [1]\n"})
		if err := UnwrapSingleRootDir(dir); err != nil {
			t.Fatalf("UnwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "config.yaml")
	})
}

// TestSafeExtractZipGBKNames Windows 资源管理器压缩的中文包回归：条目名 GBK 字节、
// 无 UTF-8 标志位（卖皮鞋的 GBK 序列含非法 UTF-8 字节，zip writer 恰好不设标志），
// 解压后目录名应为正常中文；UTF-8 标志位正常的条目不受影响。
// 限额沿用建题/替换口径（2000 条 / 200MB 总量 / 20MB 单文件）。
func TestSafeExtractZipGBKNames(t *testing.T) {
	gbk := func(s string) string {
		b, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
		if err != nil {
			t.Fatalf("gbk encode %q: %v", s, err)
		}
		return string(b)
	}
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pkg.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for _, e := range []struct{ name, body string }{
		{gbk("1000 卖皮鞋/problem.md"), "题面"},
		{gbk("1000 卖皮鞋/data/test1.in"), "1\n"},
		{gbk("1000 卖皮鞋/data/test1.out"), "2\n"},
		{"1001_两个数的和/problem.md", "题面"}, // 正常 UTF-8 名（带标志位）
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip create %q: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("zip write %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	dst := filepath.Join(dir, "out")
	if err := SafeExtractZipLimits(zipPath, dst, 2000, 200<<20, 20<<20, "20MB", "200MB", nil); err != nil {
		t.Fatalf("SafeExtractZipLimits: %v", err)
	}
	mustExist(t, dst, "1000 卖皮鞋/problem.md")
	mustExist(t, dst, "1000 卖皮鞋/data/test1.in")
	mustExist(t, dst, "1000 卖皮鞋/data/test1.out")
	mustExist(t, dst, "1001_两个数的和/problem.md")
}
