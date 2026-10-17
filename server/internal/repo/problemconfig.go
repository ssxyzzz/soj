package repo

// config.yaml 白名单解析族（v0.5e ② 自 importer.go 拆出）：题包配置的解析、
// 别名归一与平铺布局归一，与导入/替换流程（importer.go）解耦——zip 上传与
// 命令行导入两条路径共用。yamlSubtask 同属本族（subtasks 字段类型）。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type yamlSubtask struct {
	Cases []int `yaml:"cases"`
	Score int   `yaml:"score"`
}

// configYAML 支持的字段（含 Hydro 惯例别名 time/memory）。未知字段 yaml 解码自动忽略。
type configYAML struct {
	Time           *float64       `yaml:"time"`          // 秒（Hydro 惯例）
	TimeLimitMS    *int           `yaml:"timeLimitMs"`   // 毫秒（本站惯例，优先级更高）
	MemoryMB       *float64       `yaml:"memory"`        // MB（Hydro 惯例）
	MemoryLimitKB  *int           `yaml:"memoryLimitKb"` // KB（本站惯例，优先级更高）
	Compare        *string        `yaml:"compare"`       // exact / tolerant
	CheckerType    *string        `yaml:"checker_type"`  // default→exact、tokens→tolerant、strict→exact、custom→需配 checker 文件
	Checker        *string        `yaml:"checker"`       // 校验器源文件名（zip 内）
	FloatTolerance *float64       `yaml:"float_tolerance"`
	FileIO         *bool          `yaml:"file_io"`        // v0.5b-3 生效
	FileIOInput    *string        `yaml:"file_io_input"`  // v0.5b-3 生效
	FileIOOutput   *string        `yaml:"file_io_output"` // v0.5b-3 生效
	Samples        *[]int         `yaml:"samples"`
	Subtasks       *[]yamlSubtask `yaml:"subtasks"`
}

// ProblemConfig 从目录内 config.yaml 解析出的白名单配置（无 config.yaml 时为 nil）。
type ProblemConfig struct {
	TimeLimitMS    *int
	MemoryLimitKB  *int
	Compare        *string
	FloatTolerance *float64
	Checker        *string
	FileIO         *bool
	FileIOInput    *string
	FileIOOutput   *string
	Samples        *[]int
	Subtasks       *[]yamlSubtask

	raw []byte
}

// parseProblemConfig 读取目录内 config.yaml（不存在返回 nil, nil）。
func parseProblemConfig(dir string) (*ProblemConfig, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var y configYAML
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return nil, err
	}
	// Hydro 惯例别名归一：time(秒)→timeLimitMs、memory(MB)→memoryLimitKb、checker_type→compare
	if y.TimeLimitMS == nil && y.Time != nil && *y.Time > 0 {
		ms := int(*y.Time * 1000)
		y.TimeLimitMS = &ms
	}
	if y.MemoryLimitKB == nil && y.MemoryMB != nil && *y.MemoryMB > 0 {
		kb := int(*y.MemoryMB) * 1024 // config 按 MB 填写，存储按 KB
		y.MemoryLimitKB = &kb
	}
	if y.Compare == nil && y.CheckerType != nil {
		switch *y.CheckerType {
		case "strict":
			mapped := CompareExact
			y.Compare = &mapped
		case "default", "tokens":
			mapped := CompareTolerant
			y.Compare = &mapped
		}
	}
	return &ProblemConfig{
		TimeLimitMS:    y.TimeLimitMS,
		MemoryLimitKB:  y.MemoryLimitKB,
		Compare:        y.Compare,
		FloatTolerance: y.FloatTolerance,
		Checker:        y.Checker,
		FileIO:         y.FileIO,
		FileIOInput:    y.FileIOInput,
		FileIOOutput:   y.FileIOOutput,
		Samples:        y.Samples,
		Subtasks:       y.Subtasks,
		raw:            raw,
	}, nil
}

// applyTo 把时限/内存/样例/子任务覆盖进 problem.yaml 解析结果（config 优先）。
// checker_type 的映射：strict→exact，default/tokens→tolerant，custom→需配 checker 文件。
func (c *ProblemConfig) applyTo(m *problemYAML) {
	if c == nil {
		return
	}
	if c.TimeLimitMS != nil {
		m.TimeLimitMS = *c.TimeLimitMS
	}
	if c.MemoryLimitKB != nil {
		m.MemoryLimitKB = *c.MemoryLimitKB
	}
	if c.Samples != nil {
		m.Samples = *c.Samples
	}
	if c.Subtasks != nil {
		m.Subtasks = *c.Subtasks
	}
	if c.Compare != nil && *c.Compare != "" {
		m.Compare = *c.Compare
	}
}

// resolvedCompare 比对模式：有校验器时交由 checker 全权判定（返回空），
// 否则 config.compare > problem.yaml compare > 空（exact）。
func (c *ProblemConfig) resolvedCompare(checkerRel string) string {
	if checkerRel != "" {
		return ""
	}
	if c != nil && c.Compare != nil {
		return *c.Compare
	}
	return ""
}

func (c *ProblemConfig) resolvedTolerance() float64 {
	if c != nil && c.FloatTolerance != nil {
		return *c.FloatTolerance
	}
	return 0
}

func (c *ProblemConfig) resolvedFileIO() bool {
	return c != nil && c.FileIO != nil && *c.FileIO
}

func (c *ProblemConfig) resolvedFileIOInput() string {
	if c != nil && c.FileIOInput != nil {
		return *c.FileIOInput
	}
	return ""
}

func (c *ProblemConfig) resolvedFileIOOutput() string {
	if c != nil && c.FileIOOutput != nil {
		return *c.FileIOOutput
	}
	return ""
}

// normalizeFlatCases 平铺测试点布局归一：根目录存在 *.in 且配有同名 .ans/.out 时，
// 按数字序重建为 cases/<序号>/in|ans。已是 cases/<N> 子目录结构则原样跳过。
func normalizeFlatCases(dir string) error {
	casesDir := filepath.Join(dir, "cases")
	if entries, err := os.ReadDir(casesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				return nil // 已是标准结构
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type pair struct{ in, ans string }
	names := make([]string, 0, 8)
	pairs := map[string]pair{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".in") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".in")
		ans := filepath.Join(dir, base+".ans")
		if _, err := os.Stat(ans); err != nil {
			ans = filepath.Join(dir, base+".out")
			if _, err := os.Stat(ans); err != nil {
				return fmt.Errorf("测试点 %s 缺少对应的答案文件（.ans/.out）", base)
			}
		}
		names = append(names, base)
		pairs[base] = pair{in: filepath.Join(dir, e.Name()), ans: ans}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Slice(names, func(i, j int) bool {
		ni, iok := numPrefix(names[i])
		nj, jok := numPrefix(names[j])
		if iok && jok {
			return ni < nj
		}
		return names[i] < names[j]
	})
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		return err
	}
	for i, base := range names {
		n := i + 1
		sub := filepath.Join(casesDir, strconv.Itoa(n))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		for src, dst := range map[string]string{
			pairs[base].in:  filepath.Join(sub, "in"),
			pairs[base].ans: filepath.Join(sub, "ans"),
		} {
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// numPrefix 取文件名的数字前缀（如 "12.in" → 12）。
func numPrefix(name string) (int, bool) {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(name[:i])
	return n, err == nil
}
