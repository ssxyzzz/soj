package repo

// 覆盖模式（v0.18.1「按题号覆盖更新」）回归测试：Importer.Overwrite 分支——
// 前缀题号定位原题、字段保留（createdBy/createdAt/visibility/未声明 hidden）、
// 数据卷清旧写新、题型不可改、题号不存在拒绝、无前缀目录仍新建。
// 复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/testdata"
)

func mkOverwriteDir(t *testing.T, name, title, ptype string, dataFiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(d, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	stmt := "# " + title + "\n\n题面内容"
	if err := os.WriteFile(filepath.Join(d, "problem.md"), []byte(stmt), 0o644); err != nil {
		t.Fatal(err)
	}
	yml := "title: " + title + "\ntype: " + ptype + "\n"
	if err := os.WriteFile(filepath.Join(d, "problem.yaml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	if ptype == TypeObjective {
		if err := os.WriteFile(filepath.Join(d, "answer.yaml"), []byte("- answer: 0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for fn, content := range dataFiles {
		if err := os.WriteFile(filepath.Join(d, "data", fn), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestImportOverwrite(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	dataRoot := t.TempDir()

	// 存量题 #980011：default 型、隐藏态开、属主 4321、卷里有旧数据
	const pid = int64(980011)
	if _, err := r.Problems.InsertOne(ctx, bson.M{
		"_id": pid, "title": "旧标题", "type": TypeDefault, "hidden": true,
		"createdBy": int64(4321), "visibility": "public", "statement": "旧题面",
		"difficulty": 3, "timeLimitMs": 1000, "memoryLimitKb": 262144,
		"cases": []bson.M{{"index": 1, "in": "data/old.in", "ans": "data/old.ans"}},
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	oldStore := testdata.NewLocalFS(dataRoot)
	if err := oldStore.WriteFile(pid, "data/old.in", []byte("old")); err != nil {
		t.Fatal(err)
	}

	// 覆盖目录 980011_新标题/：新题面 + 新数据对（yaml 未声明 hidden → 保留库内 true）
	dir := mkOverwriteDir(t, "980011_新标题", "新标题", TypeDefault,
		map[string]string{"a.in": "1 2\n", "a.ans": "3\n"})
	im := &Importer{Repo: r, DataDir: dataRoot, Overwrite: true, IgnoreDirPID: false}
	p, err := im.ImportDir(ctx, dir)
	if err != nil {
		t.Fatalf("覆盖导入: %v", err)
	}
	if p.ID != pid {
		t.Fatalf("覆盖应保持题号 %d，得到 %d", pid, p.ID)
	}
	got, err := r.GetProblem(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "新标题" || got.Statement == "旧题面" {
		t.Fatalf("题面/标题未更新: %+v", got)
	}
	if got.CreatedBy != 4321 {
		t.Fatalf("createdBy 应保留 4321，得到 %d", got.CreatedBy)
	}
	if !got.Hidden {
		t.Fatal("yaml 未声明 hidden 应保留库内隐藏态")
	}
	// 旧数据文件应被清掉、新数据在
	store := testdata.NewLocalFS(dataRoot)
	if _, err := store.Open(pid, "data/old.in"); err == nil {
		t.Fatal("覆盖后旧数据文件 data/old.in 应已清除")
	}
	f, err := store.Open(pid, "data/a.in")
	if err != nil {
		t.Fatalf("新数据未落卷: %v", err)
	}
	f.Close()

	// 题型不可改：库内 default，包内 objective → 拒绝
	dir2 := mkOverwriteDir(t, "980011_伪装客观题", "伪装客观题", TypeObjective, nil)
	if _, err := im.ImportDir(ctx, filepath.Join(dir2, "980011_伪装客观题")); err == nil {
		t.Fatal("题型不一致应拒绝")
	}

	// 前缀题号不存在 → 拒绝（不静默新建）
	dir3 := mkOverwriteDir(t, "989999_不存在的题", "不存在的题", TypeDefault,
		map[string]string{"a.in": "1\n", "a.ans": "1\n"})
	before, _ := r.Problems.CountDocuments(ctx, bson.M{})
	if _, err := im.ImportDir(ctx, filepath.Join(dir3, "989999_不存在的题")); err == nil {
		t.Fatal("题号不存在应拒绝")
	}
	after, _ := r.Problems.CountDocuments(ctx, bson.M{})
	if after != before {
		t.Fatal("拒绝时不应新建任何题")
	}

	// 无前缀目录在覆盖模式下仍新建（自动取号）
	dir4 := mkOverwriteDir(t, "无前缀新题", "无前缀新题", TypeDefault,
		map[string]string{"b.in": "5\n", "b.ans": "6\n"})
	np, err := im.ImportDir(ctx, dir4)
	if err != nil {
		t.Fatalf("无前缀应新建: %v", err)
	}
	if np.ID == pid || np.ID <= 0 {
		t.Fatalf("无前缀应自动取新题号，得到 %d", np.ID)
	}
}
