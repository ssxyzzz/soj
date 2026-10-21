package repo

// config.yaml 白名单解析族（v0.5.4 ② 自 importer.go 拆出）：题包配置的解析、
// 别名归一与平铺布局归一，与导入/替换流程（importer.go）解耦——zip 上传与
// 命令行导入两条路径共用。yamlSubtask 同属本族（subtasks 字段类型）。

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

// UnmarshalYAML cases 兼容两种写法：本站惯例 [1, 2]（测试点编号），以及 Hydro 题包
// 惯例的文件对列表（- input: 1.in / output: 1.out，编号取输入文件名的数字前缀）。
// Score 走默认解码；其他字段（如 Hydro 的 type）忽略。
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

// subtaskCases 测试点编号列表（见 yamlSubtask.UnmarshalYAML）。
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
			return fmt.Errorf("无法从测试点文件名 %q 推断编号（文件名需含数字，如 1.in / atbash1.in）", name)
		}
		out = append(out, idx)
	}
	*c = out
	return nil
}

// timeLimit 时限秒数：兼容纯数字与 Hydro 惯例带单位字符串（1s / 500ms / 2m / 1h）。
// 导出（MarshalYAML）为直白带单位形态：整秒 "1s"、非整 "0.5s"。
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

// memLimit 内存 MB 数：兼容纯数字与带单位字符串（256m / 512mb / 1g / 262144k）。
// 导出为直白带单位形态：整 MB "256MB"、不足 1MB "512KB"。
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
		return fmt.Errorf("内存需为数字（MB）或带单位字符串（256m/1g/262144k）")
	}
	v, err := parseUnitValue(s, map[string]float64{"kb": 1.0 / 1024, "k": 1.0 / 1024, "mb": 1, "m": 1, "gb": 1024, "g": 1024}, "内存")
	if err != nil {
		return err
	}
	*m = memLimit(v)
	return nil
}

// parseUnitValue 解析「数值 + 单位后缀」标量：长后缀优先匹配（ms 先于 s、kb 先于 k），
// 无后缀按基准单位处理，未知后缀报错并回显原值。
func parseUnitValue(s string, units map[string]float64, field string) (float64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	// 后缀按长度降序逐个尝试：HasSuffix 会同时命中长短后缀（"500ms" 以 "s" 结尾），
	// 短的先匹配会把 "ms" 拆成数值 50 + 单位 m 之外的残串
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

// configYAML 支持的字段（含 Hydro 惯例别名 time/memory/filename）。未知字段 yaml 解码自动忽略。
type configYAML struct {
	Time           *timeLimit     `yaml:"time"`          // 秒（数字或 Hydro 惯例 "1s"/"500ms" 带单位写法）
	TimeLimitMS    *int           `yaml:"timeLimitMs"`   // 毫秒（本站惯例，优先级更高）
	MemoryMB       *memLimit      `yaml:"memory"`        // MB（数字或 "256m"/"1g" 带单位写法）
	MemoryLimitKB  *int           `yaml:"memoryLimitKb"` // KB（本站惯例，优先级更高）
	Compare        *string        `yaml:"compare"`       // exact / tolerant
	CheckerType    *string        `yaml:"checker_type"`  // default→exact、tokens→tolerant、strict→exact、custom→需配 checker 文件
	Checker        *string        `yaml:"checker"`       // 校验器源文件名（zip 内）
	FloatTolerance *float64       `yaml:"float_tolerance"`
	Type           *string        `yaml:"type"`           // 题目类型（压扁 Hydro 包/旧双层包的 type 落点）
	FileIO         *bool          `yaml:"file_io"`        // v0.5.2-3 生效
	FileIOInput    *string        `yaml:"file_io_input"`  // v0.5.2-3 生效
	FileIOOutput   *string        `yaml:"file_io_output"` // v0.5.2-3 生效
	Filename       *string        `yaml:"filename"`       // Hydro 惯例文件 IO：reverse → reverse.in / reverse.out
	Samples        *[]int         `yaml:"samples"`
	Subtasks       *[]yamlSubtask `yaml:"subtasks"`
}

// ProblemConfig 从目录内 config.yaml 解析出的白名单配置（无 config.yaml 时为 nil）。
type ProblemConfig struct {
	Type           string
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
	// Hydro 惯例别名归一：time(秒)→timeLimitMs、memory(MB)→memoryLimitKb、checker_type→compare、
	// filename(name)→文件 IO（name.in / name.out，仅未显式声明时补位）
	if y.TimeLimitMS == nil && y.Time != nil && *y.Time > 0 {
		ms := int(float64(*y.Time) * 1000)
		y.TimeLimitMS = &ms
	}
	if y.MemoryLimitKB == nil && y.MemoryMB != nil && *y.MemoryMB > 0 {
		kb := int(float64(*y.MemoryMB)) * 1024 // config 按 MB 填写，存储按 KB
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
	if y.Type != nil {
		y2 := strings.TrimSpace(*y.Type)
		y.Type = &y2
	}
	return &ProblemConfig{
		Type:           y.TypeString(),
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
		v := *c.TimeLimitMS
		m.TimeLimitMS = &v
	}
	if c.MemoryLimitKB != nil {
		v := *c.MemoryLimitKB
		m.MemoryLimitKB = &v
	}
	if m.Type == "" && c.Type != "" {
		m.Type = c.Type
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
		ni, iok := trailingNumber(names[i])
		nj, jok := trailingNumber(names[j])
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
		// 移动（非复制）：源文件留在根会被附带文件存档误收，造成测试数据双份
		for src, dst := range map[string]string{
			pairs[base].in:  filepath.Join(sub, "in"),
			pairs[base].ans: filepath.Join(sub, "ans"),
		} {
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

// trailingNumber 取文件名（去扩展名）末段的连续数字：
// "1.in"→1、"atbash1.in"→1、"test10.in"→10；无数字返回 false。
// 兼容 testlib/Hydro 两种命名（数字前缀与字母前缀+数字后缀）。
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

// TypeString config.yaml 声明的题型（未声明返回空）。
func (y *configYAML) TypeString() string {
	if y.Type == nil {
		return ""
	}
	return *y.Type
}
