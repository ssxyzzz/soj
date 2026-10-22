package server

// 批量导入题包切分（v0.15）：zip 根即单题包 vs 根下每目录一题，纯文件系统单测。

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBatchFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSplitBatchProblemDirs(t *testing.T) {
	t.Run("根即单题包（problem.yaml + cases）", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "problem.yaml"), "title: A\n")
		writeBatchFile(t, filepath.Join(root, "cases", "1", "in"), "1")
		dirs, err := splitBatchProblemDirs(root)
		if err != nil || len(dirs) != 1 || dirs[0] != root {
			t.Fatalf("want [root], got %v err=%v", dirs, err)
		}
	})
	t.Run("根即单题包（Hydro testdata）", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "testdata", "1.in"), "1")
		dirs, err := splitBatchProblemDirs(root)
		if err != nil || len(dirs) != 1 || dirs[0] != root {
			t.Fatalf("want [root], got %v err=%v", dirs, err)
		}
	})
	t.Run("根即单题包（平铺数据对）", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "1.in"), "1")
		writeBatchFile(t, filepath.Join(root, "1.ans"), "2")
		dirs, err := splitBatchProblemDirs(root)
		if err != nil || len(dirs) != 1 {
			t.Fatalf("want single, got %v err=%v", dirs, err)
		}
	})
	t.Run("多题目录混装（排序、剔除杂物）", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "2_hydro", "testdata", "1.in"), "1")
		writeBatchFile(t, filepath.Join(root, "1_soj", "problem.yaml"), "title: B\n")
		writeBatchFile(t, filepath.Join(root, "3_bare", "1.in"), "1")
		writeBatchFile(t, filepath.Join(root, "3_bare", "1.ans"), "1")
		writeBatchFile(t, filepath.Join(root, "__MACOSX", "junk"), "x")
		writeBatchFile(t, filepath.Join(root, ".DS_Store"), "x")
		writeBatchFile(t, filepath.Join(root, "说明.txt"), "x")
		dirs, err := splitBatchProblemDirs(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(dirs) != 3 || filepath.Base(dirs[0]) != "1_soj" || filepath.Base(dirs[1]) != "2_hydro" || filepath.Base(dirs[2]) != "3_bare" {
			t.Fatalf("want 1_soj,2_hydro,3_bare got %v", dirs)
		}
	})
	t.Run("空目录报错带所见内容", func(t *testing.T) {
		root := t.TempDir()
		_, err := splitBatchProblemDirs(root)
		if err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("仅杂物文件报错", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "readme.txt"), "x")
		_, err := splitBatchProblemDirs(root)
		if err == nil {
			t.Fatal("want error")
		}
	})
}

func TestBatchDirTitle(t *testing.T) {
	t.Run("yaml 标题优先", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "problem.yaml"), "title: 修理牛棚\ntimeLimitMs: 1000\n")
		if got := batchDirTitle(root); got != "修理牛棚" {
			t.Fatalf("want 修理牛棚 got %q", got)
		}
	})
	t.Run("缺省目录名", func(t *testing.T) {
		root := t.TempDir()
		writeBatchFile(t, filepath.Join(root, "1.in"), "1")
		if got := batchDirTitle(root); got != filepath.Base(root) {
			t.Fatalf("want dir name got %q", got)
		}
	})
}
