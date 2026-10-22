package repo

// data/config.yaml 解析与渲染（import-design.md：时限/内存/文件 IO/子任务/校验器都住在
// data/ 里；samples 样例标记已随样例机制整体废弃）。时间/内存兼容纯数字与 Hydro 惯例
// 带单位字符串（1s / 256m）；subtasks.cases 兼容编号列表与 Hydro 文件对列表。

import (
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type yamlSubtask struct {
	Cases []int
	Score int `yaml:"score"`
}

// UnmarshalYAML cases 兼容两种写法：编号列表 [1, 2]，以及 Hydro 惯例的文件对列表
//（- input: test1.in / output: test1.out，编号取输入文件名的末段数字）。
func (s *yamlSubtask) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Cases subtaskCases `yaml:"cases"`
		Score int          `yaml:"score"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	s.Cases, s.Score = raw.Cases, raw.Score
	return nil
}

// subtaskCases 测试点编号列表。
type subtaskCases []int

func (c *subtaskCases) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("subtasks.cases 需为列表（测试点编号或 {input, output} 文件对）")
	}
	out := make([]int, 0, len(node.Content))
	for _, item := range node.Content {
		var n int
		if err := item.Decode(&n); err == nil {
			out = append(out, n)
			continue
		}
		var pair struct {
			Input  string `yaml:"input"`
			Output string `yaml:"output"`
		}
		if err := item.Decode(&pair); err != nil {
			return fmt.Errorf("subtasks.cases 元素需为测试点编号或 {input, output} 文件对")
		}
		name := pair.Input
		if name == "" {
			name = pair.Output
		}
		idx, ok := trailingNumber(path.Base(name))
		if !ok {
			return fmt.Errorf("无法从测试点文件名 %q 推断编号（文件名需含数字，如 test1.in / sound3.in）", name)
		}
		out = append(out, idx)
	}
	*c = out
	return nil
}

// timeLimit 时限秒数：兼容纯数字与带单位字符串（1s / 500ms / 2m / 1h）；导出带单位。
type timeLimit float64

func (t timeLimit) MarshalYAML() (any, error) {
	sec := float64(t)
	if sec == math.Trunc(sec) {
		return fmt.Sprintf("%ds", int64(sec)), nil
	}
	return strconv.FormatFloat(sec, 'f', -1, 64) + "s", nil
}

func (t *timeLimit) UnmarshalYAML(node *yaml.Node) error {
	var num float64
	if err := node.Decode(&num); err == nil {
		*t = timeLimit(num)
		return nil
	}
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("时限需为数字（秒）或带单位字符串（1s/500ms/2m/1h）")
	}
	v, err := parseUnitValue(s, map[string]float64{"ms": 0.001, "s": 1, "m": 60, "h": 3600}, "时限")
	if err != nil {
		return err
	}
	*t = timeLimit(v)
	return nil
}

// memLimit 内存 MB 数：兼容纯数字与带单位字符串（256m / 512mb / 1g）；导出带单位。
type memLimit float64

func (m memLimit) MarshalYAML() (any, error) {
	kb := float64(m) * 1024
	if kb < 1024 {
		return fmt.Sprintf("%dKB", int64(kb)), nil
	}
	mb := float64(m)
	if mb == math.Trunc(mb) {
		return fmt.Sprintf("%dMB", int64(mb)), nil
	}
	return strconv.FormatFloat(mb, 'f', -1, 64) + "MB", nil
}

func (m *memLimit) UnmarshalYAML(node *yaml.Node) error {
	var num float64
	if err := node.Decode(&num); err == nil {
		*m = memLimit(num)
		return nil
	}
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("内存需为数字（MB）或带单位字符串（256m/1g）")
	}
	v, err := parseUnitValue(s, map[string]float64{"kb": 1.0 / 1024, "k": 1.0 / 1024, "mb": 1, "m": 1, "gb": 1024, "g": 1024}, "内存")
	if err != nil {
		return err
	}
	*m = memLimit(v)
	return nil
}

// parseUnitValue 解析「数值 + 单位后缀」标量：长后缀优先（ms 先于 s、kb 先于 k）。
func parseUnitValue(s string, units map[string]float64, field string) (float64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	suffixes := make([]string, 0, len(units))
	for k := range units {
		suffixes = append(suffixes, k)
	}
	sort.Slice(suffixes, func(i, j int) bool { return len(suffixes[i]) > len(suffixes[j]) })
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			num, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, suffix)), 64)
			if err != nil {
				return 0, fmt.Errorf("%s %q 无法解析为数值", field, s)
			}
			return num * units[suffix], nil
		}
	}
	num, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q 单位无法识别（可用：%s）", field, s, strings.Join(sortedKeys(units), "/"))
	}
	return num, nil
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// configYAML data/config.yaml 支持的字段（未知字段忽略）。
type configYAML struct {
	Time           *timeLimit     `yaml:"time"`
	TimeLimitMS    *int           `yaml:"timeLimitMs"` // 本站惯例（毫秒），优先级更高
	MemoryMB       *memLimit      `yaml:"memory"`
	MemoryLimitKB  *int           `yaml:"memoryLimitKb"` // 本站惯例（KB），优先级更高
	Compare        *string        `yaml:"compare"`       // exact / tolerant
	CheckerType    *string        `yaml:"checker_type"`  // Hydro 惯例：default→tolerant、strict→exact
	Checker        *string        `yaml:"checker"`       // 校验器源文件（data/ 内相对名）
	FloatTolerance *float64       `yaml:"float_tolerance"`
	Type           *string        `yaml:"type"` // Hydro 兼容落点（SOJ 格式题型在 problem.yaml）
	FileIO         *bool          `yaml:"file_io"`
	FileIOInput    *string        `yaml:"file_io_input"`
	FileIOOutput   *string        `yaml:"file_io_output"`
	Filename       *string        `yaml:"filename"` // Hydro 惯例文件 IO：way → way.in/way.out
	Subtasks       *[]yamlSubtask `yaml:"subtasks"`
}

// ProblemConfig data/config.yaml 解析结果（无 config.yaml 时为 nil）。
type ProblemConfig struct {
	TimeLimitMS    *int
	MemoryLimitKB  *int
	Compare        *string
	FloatTolerance *float64
	Checker        *string
	Type           string
	FileIO         *bool
	FileIOInput    *string
	FileIOOutput   *string
	Subtasks       *[]yamlSubtask
}

// parseProblemConfig 读取 dir 内 config.yaml（不存在返回 nil, nil）——dir 传 data/ 目录。
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
	// Hydro 惯例别名归一：time(秒)→timeLimitMs、memory(MB)→memoryLimitKb、
	// checker_type→compare、filename→文件 IO（仅未显式声明时补位）
	if y.TimeLimitMS == nil && y.Time != nil && *y.Time > 0 {
		ms := int(float64(*y.Time) * 1000)
		y.TimeLimitMS = &ms
	}
	if y.MemoryLimitKB == nil && y.MemoryMB != nil && *y.MemoryMB > 0 {
		kb := int(float64(*y.MemoryMB)) * 1024
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
	if y.Filename != nil && *y.Filename != "" {
		if y.FileIO == nil {
			on := true
			y.FileIO = &on
		}
		if y.FileIOInput == nil {
			in := *y.Filename + ".in"
			y.FileIOInput = &in
		}
		if y.FileIOOutput == nil {
			out := *y.Filename + ".out"
			y.FileIOOutput = &out
		}
	}
	var typ string
	if y.Type != nil {
		typ = strings.TrimSpace(*y.Type)
	}
	return &ProblemConfig{
		TimeLimitMS:    y.TimeLimitMS,
		MemoryLimitKB:  y.MemoryLimitKB,
		Compare:        y.Compare,
		FloatTolerance: y.FloatTolerance,
		Checker:        y.Checker,
		Type:           typ,
		FileIO:         y.FileIO,
		FileIOInput:    y.FileIOInput,
		FileIOOutput:   y.FileIOOutput,
		Subtasks:       y.Subtasks,
	}, nil
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

// configOut 权威导出 data/config.yaml 的字段面（时间/内存直白带单位）。
type configOut struct {
	Time           *timeLimit     `yaml:"time"`
	Memory         *memLimit      `yaml:"memory"`
	Compare        string         `yaml:"compare,omitempty"`
	FloatTolerance float64        `yaml:"float_tolerance,omitempty"`
	Checker        string         `yaml:"checker,omitempty"` // data/ 内相对名
	Filename       string         `yaml:"filename,omitempty"`
	FileIOInput    string         `yaml:"file_io_input,omitempty"`
	FileIOOutput   string         `yaml:"file_io_output,omitempty"`
	Subtasks       []yamlSubtask  `yaml:"subtasks,omitempty"`
}

// trailingNumber 取文件名（去扩展名）末段的连续数字：
// "test10"→10、"sound3"→3、"1"→1；无数字返回 false。
func trailingNumber(name string) (int, bool) {
	base := name
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	end := len(base)
	for end > 0 && base[end-1] >= '0' && base[end-1] <= '9' {
		end--
	}
	if end == len(base) {
		return 0, false
	}
	n, err := strconv.Atoi(base[end:])
	return n, err == nil
}
