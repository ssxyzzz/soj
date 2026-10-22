package repo

import (
	"path/filepath"
	"testing"
)

// 新格式数据对收集与排序（v0.15）：*.in 配对同名 .ans|.out（.ans 优先）、
// 附属文件归集、末段数字排序；交互题允许缺答案。
func TestCollectDataDir(t *testing.T) {
	t.Run("ans 优先于 out + 附属文件归集", func(t *testing.T) {
		dir := mkDir(t, map[string]string{
			"config.yaml": "time: 1s\n",
			"test1.in":    "1", "test1.ans": "2",
			"test2.in":    "3", "test2.out": "4", "test2.ans": "5",
			"gen.py":      "print(1)",
			".DS_Store":   "junk",
		})
		pairs, atts, cfg, err := collectDataDir(dir, TypeDefault)
		if err != nil {
			t.Fatal(err)
		}
		if len(pairs) != 2 {
			t.Fatalf("want 2 pairs got %v", pairs)
		}
		if pairs[0].AnsExt != ".ans" || pairs[1].AnsExt != ".ans" {
			t.Fatalf("ans 应优先于 out: %+v", pairs)
		}
		if len(atts) != 1 || atts[0] != "gen.py" {
			t.Fatalf("附属文件应只有 gen.py: %v", atts)
		}
		if cfg == nil || cfg.TimeLimitMS == nil || *cfg.TimeLimitMS != 1000 {
			t.Fatalf("config 未解析: %+v", cfg)
		}
	})

	t.Run("数字排序 test2 < test10", func(t *testing.T) {
		dir := mkDir(t, map[string]string{
			"test10.in": "1", "test10.out": "1",
			"test2.in":  "1", "test2.out": "1",
			"test1.in":  "1", "test1.out": "1",
		})
		pairs, _, _, err := collectDataDir(dir, TypeDefault)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"test1", "test2", "test10"}
		for i, w := range want {
			if pairs[i].Base != w {
				t.Fatalf("排序不符: %v", pairs)
			}
		}
	})

	t.Run("缺答案报错（交互题放行）", func(t *testing.T) {
		dir := mkDir(t, map[string]string{"x1.in": "1"})
		if _, _, _, err := collectDataDir(dir, TypeDefault); err == nil {
			t.Fatal("传统题缺答案应报错")
		}
		pairs, _, _, err := collectDataDir(dir, TypeInteractive)
		if err != nil || len(pairs) != 1 || pairs[0].AnsExt != "" {
			t.Fatalf("交互题应允许缺答案: %+v err=%v", pairs, err)
		}
	})

	t.Run("data 目录缺省 = 无数据题", func(t *testing.T) {
		dir := t.TempDir()
		pairs, atts, cfg, err := collectDataDir(filepath.Join(dir, "data"), TypeDefault)
		if err != nil || pairs != nil || atts != nil || cfg != nil {
			t.Fatalf("缺省 data/ 应返回空: %v %v %v %v", pairs, atts, cfg, err)
		}
	})
}
