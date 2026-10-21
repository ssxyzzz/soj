package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeConfig 临时目录写入 config.yaml 并解析（parseProblemConfig 以目录为入口）。
func writeConfig(t *testing.T, content string) (*ProblemConfig, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return parseProblemConfig(dir)
}

// Hydro 惯例带单位写法：用户报障的原始样式（time: 1s、memory: 256m、filename: reverse）。
func TestParseProblemConfigHydroUnits(t *testing.T) {
	cfg, err := writeConfig(t, "type: default\ntime: 1s\nmemory: 256m\nfilename: reverse\n")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.TimeLimitMS == nil || *cfg.TimeLimitMS != 1000 {
		t.Errorf("timeLimitMs = %v, want 1000", cfg.TimeLimitMS)
	}
	if cfg.MemoryLimitKB == nil || *cfg.MemoryLimitKB != 262144 {
		t.Errorf("memoryLimitKb = %v, want 262144", cfg.MemoryLimitKB)
	}
	if cfg.FileIO == nil || !*cfg.FileIO {
		t.Errorf("fileIO = %v, want true（filename 隐含开启文件 IO）", cfg.FileIO)
	}
	if cfg.FileIOInput == nil || *cfg.FileIOInput != "reverse.in" {
		t.Errorf("fileIOInput = %v, want reverse.in", cfg.FileIOInput)
	}
	if cfg.FileIOOutput == nil || *cfg.FileIOOutput != "reverse.out" {
		t.Errorf("fileIOOutput = %v, want reverse.out", cfg.FileIOOutput)
	}
}

func TestParseProblemConfigUnitVariants(t *testing.T) {
	cases := []struct {
		yaml   string
		wantMS int
		wantKB int
	}{
		{"time: 500ms\nmemory: 1g\n", 500, 1048576},
		{"time: 2\nmemory: 512\n", 2000, 524288}, // 纯数字 = 秒 / MB
		{"time: 0.5s\nmemory: 262144k\n", 500, 262144},
		{"time: \"1 s\"\nmemory: \"256 MB\"\n", 1000, 262144}, // 引号包住时是字符串标量，单位大小写不敏感
	}
	for _, c := range cases {
		cfg, err := writeConfig(t, c.yaml)
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", c.yaml, err)
		}
		if cfg.TimeLimitMS == nil || *cfg.TimeLimitMS != c.wantMS {
			t.Errorf("%q timeLimitMs = %v, want %d", c.yaml, cfg.TimeLimitMS, c.wantMS)
		}
		if cfg.MemoryLimitKB == nil || *cfg.MemoryLimitKB != c.wantKB {
			t.Errorf("%q memoryLimitKb = %v, want %d", c.yaml, cfg.MemoryLimitKB, c.wantKB)
		}
	}
}

// 本站惯例字段优先级高于 Hydro 别名；显式 file_io_input 不被 filename 覆盖。
func TestParseProblemConfigPriority(t *testing.T) {
	cfg, err := writeConfig(t, "time: 1s\ntimeLimitMs: 2000\nmemory: 256m\nmemoryLimitKb: 131072\nfilename: foo\nfile_io_output: bar.out\n")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if *cfg.TimeLimitMS != 2000 || *cfg.MemoryLimitKB != 131072 {
		t.Errorf("本站字段应优先：ms=%d kb=%d", *cfg.TimeLimitMS, *cfg.MemoryLimitKB)
	}
	if *cfg.FileIOInput != "foo.in" || *cfg.FileIOOutput != "bar.out" {
		t.Errorf("显式声明的 file_io_output 应保留：in=%s out=%s", *cfg.FileIOInput, *cfg.FileIOOutput)
	}
}

func TestParseProblemConfigBadUnit(t *testing.T) {
	if _, err := writeConfig(t, "time: 1x\n"); err == nil {
		t.Error("未知单位应报错")
	}
	if _, err := writeConfig(t, "memory: lots\n"); err == nil {
		t.Error("非数值内存应报错")
	}
}

// 无 config.yaml 返回 nil, nil（不报错）。
func TestParseProblemConfigMissing(t *testing.T) {
	cfg, err := parseProblemConfig(t.TempDir())
	if err != nil || cfg != nil {
		t.Errorf("want nil, nil; got %v, %v", cfg, err)
	}
}

// Hydro 题包的 subtasks：cases 为 {input, output} 文件对列表（用户报障样式），
// 编号取文件名数字前缀；与数字写法可混用。
func TestParseProblemConfigHydroSubtasks(t *testing.T) {
	cfg, err := writeConfig(t, `subtasks:
  - score: 50
    cases:
      - input: 1.in
        output: 1.out
      - input: 2.in
        output: 2.out
  - score: 50
    cases: [3, 4]
`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.Subtasks == nil || len(*cfg.Subtasks) != 2 {
		t.Fatalf("subtasks = %+v, want 2 组", cfg.Subtasks)
	}
	st := *cfg.Subtasks
	if len(st[0].Cases) != 2 || st[0].Cases[0] != 1 || st[0].Cases[1] != 2 {
		t.Errorf("组 1 cases = %v, want [1 2]", st[0].Cases)
	}
	if st[0].Score != 50 || st[1].Score != 50 {
		t.Errorf("score = %d/%d, want 50/50", st[0].Score, st[1].Score)
	}
	if len(st[1].Cases) != 2 || st[1].Cases[0] != 3 || st[1].Cases[1] != 4 {
		t.Errorf("组 2 cases = %v, want [3 4]", st[1].Cases)
	}
}

// 字母前缀 + 数字后缀的文件名（atbash1.in，用户报障 2026-10-21）：
// 文件对编号与平铺归一排序都按末段数字，atbash10 不得因字典序排到 atbash2 前。
func TestTrailingNumberAndAtbashLayout(t *testing.T) {
	for name, want := range map[string]int{"1.in": 1, "atbash1.in": 1, "atbash10.in": 10, "test10.out": 10, "x9y7.in": 7} {
		if n, ok := trailingNumber(name); !ok || n != want {
			t.Errorf("trailingNumber(%q) = %d,%v want %d", name, n, ok, want)
		}
	}
	if _, ok := trailingNumber("abc.in"); ok {
		t.Error("无数字文件名应返回 false")
	}

	// 平铺归一排序：10 个 atbashN.in 应按 1..10 编号入 cases/
	dir := t.TempDir()
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("atbash%d", i)
		if err := os.WriteFile(filepath.Join(dir, name+".in"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".out"), []byte("y"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := normalizeFlatCases(dir); err != nil {
		t.Fatalf("平铺归一失败: %v", err)
	}
	for i := 1; i <= 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "cases", strconv.Itoa(i), "in")); err != nil {
			t.Errorf("cases/%d/in 缺失（排序/编号错位）: %v", i, err)
		}
	}
}

// 用户完整 config（2026-10-21 报障原文风格）：type+filename+subtasks 十个文件对，
// filename 别名必须同时补 in/out 两个文件名。
func TestParseProblemConfigAtbashFull(t *testing.T) {
	cfg, err := writeConfig(t, `type: default
filename: atbash
subtasks:
  - score: 100
    if: []
    id: 1
    type: sum
    cases:
      - input: atbash1.in
        output: atbash1.out
      - input: atbash10.in
        output: atbash10.out
`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.FileIO == nil || !*cfg.FileIO {
		t.Errorf("fileIO = %v, want true", cfg.FileIO)
	}
	if cfg.FileIOInput == nil || *cfg.FileIOInput != "atbash.in" {
		t.Errorf("fileIOInput = %v, want atbash.in", cfg.FileIOInput)
	}
	if cfg.FileIOOutput == nil || *cfg.FileIOOutput != "atbash.out" {
		t.Errorf("fileIOOutput = %v, want atbash.out", cfg.FileIOOutput)
	}
}
