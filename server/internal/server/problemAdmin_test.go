package server

import (
	"os"
	"path/filepath"
	"testing"
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
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
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
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
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
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "cases/1/in")
	})

	t.Run("根下平铺 .in/.ans 不动", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"1.in":  "1 2\n",
			"1.ans": "3\n",
		})
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "1.in")
	})

	t.Run("打包杂物不影响判断且保留", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{
			"pkg/cases/1/in":     "1 2\n",
			"pkg/cases/1/ans":    "3\n",
			"__MACOSX/pkg/._in":  "\x00\x01",
			".DS_Store":          "",
		})
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
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
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "data/test1.in")
	})

	t.Run("根下仅剩单个文件不动", func(t *testing.T) {
		dir := t.TempDir()
		mkTree(t, dir, map[string]string{"config.yaml": "samples: [1]\n"})
		if err := unwrapSingleRootDir(dir); err != nil {
			t.Fatalf("unwrapSingleRootDir: %v", err)
		}
		mustExist(t, dir, "config.yaml")
	})
}
