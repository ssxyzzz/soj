package repo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"soj/internal/testdata"
)

// Importer 把「题目目录」导入系统：文件本体复制进 testdata 存储，元数据写入数据库。
// 目录布局（与存储布局一致）:
//
//	<pid>_<title>/           # 目录名前缀数字作为题目 ID，可省略（省略则自动分配）
//	  problem.yaml           # title / 时限 / 内存 / samples / hidden / subtasks
//	  statement.md           # 题面
//	  cases/1/in 1/ans ...   # 每个测试点一对文件
type Importer struct {
	Repo       *Repo
	DataDir    string // testdata 存储根目录
	CreatorUID int64  // 导入者 uid（0 = 系统/命令行导入，其题目隐藏态仅 root 可改）
}

type problemYAML struct {
	Title         string        `yaml:"title"`
	TimeLimitMS   int           `yaml:"timeLimitMs"`
	MemoryLimitKB int           `yaml:"memoryLimitKb"`
	Samples       []int         `yaml:"samples"`
	Visibility    string        `yaml:"visibility"`
	Hidden        bool          `yaml:"hidden"` // 主题库隐藏
	Tags          []string      `yaml:"tags"`
	Compare       string        `yaml:"compare"`  // 比对模式 exact/tolerant（v0.5b-1）
	Subtasks      []yamlSubtask `yaml:"subtasks"` // 可选：子任务分组给分（组内全过才得分）
}

type yamlSubtask struct {
	Cases []int `yaml:"cases"`
	Score int   `yaml:"score"`
}

// SeedAll 导入 seedDir 下的全部题目目录（按目录名排序），幂等可重复执行。
func (im *Importer) SeedAll(ctx context.Context, seedDir string) error {
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		pr, err := im.ImportDir(ctx, filepath.Join(seedDir, name))
		if err != nil {
			return fmt.Errorf("导入 %s: %w", name, err)
		}
		fmt.Printf("[seed] 已导入题目 #%d (%s)\n", pr.ID, name)
	}
	return nil
}

// ImportDir 导入单个题目目录，返回创建的题目（含 config.yaml/内置 checker 解析结果）。
func (im *Importer) ImportDir(ctx context.Context, dir string) (*Problem, error) {
	base := filepath.Base(dir)
	pid := int64(0)
	if n, err := strconv.ParseInt(strings.SplitN(base, "_", 2)[0], 10, 64); err == nil && n > 0 {
		pid = n
	}

	raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml"))
	if err != nil {
		return nil, fmt.Errorf("读取 problem.yaml: %w", err)
	}
	var meta problemYAML
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("解析 problem.yaml: %w", err)
	}
	if meta.Title == "" {
		meta.Title = base
	}
	if meta.TimeLimitMS <= 0 {
		meta.TimeLimitMS = 1000
	}
	if meta.MemoryLimitKB <= 0 {
		meta.MemoryLimitKB = 262144
	}
	if meta.Visibility == "" {
		meta.Visibility = VisibilityPublic
	}

	// config.yaml 白名单字段覆盖 problem.yaml（v0.5b-2）：未知字段忽略不报错
	cfg, err := parseProblemConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("解析 config.yaml: %w", err)
	}
	cfg.applyTo(&meta)

	// 平铺测试点布局归一：根目录 N.in/N.ans 自动整理为 cases/<序号>/ 结构
	if err := normalizeFlatCases(dir); err != nil {
		return nil, err
	}
	statement, err := os.ReadFile(filepath.Join(dir, "statement.md"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// 收集测试点
	casesDir := filepath.Join(dir, "cases")
	caseEntries, err := os.ReadDir(casesDir)
	if err != nil {
		return nil, fmt.Errorf("读取 cases 目录: %w", err)
	}
	indexes := make([]int, 0, len(caseEntries))
	for _, e := range caseEntries {
		if !e.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(e.Name()); err == nil {
			indexes = append(indexes, n)
		}
	}
	sort.Ints(indexes)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("cases 目录下没有测试点")
	}

	// 题目 ID 未指定则自动分配
	if pid == 0 {
		pid, err = im.Repo.nextSeq(ctx, "problem")
		if err != nil {
			return nil, err
		}
	}

	store := testdata.NewLocalFS(im.DataDir)
	sampleSet := map[int]bool{}
	for _, i := range meta.Samples {
		sampleSet[i] = true
	}

	// 内置校验器（v0.5b-2）：目录内的 checker 源文件存入测试数据卷
	checkerRel, checkerSha, checkerSrc := "", "", []byte(nil)
	if cfg != nil && cfg.Checker != nil && *cfg.Checker != "" {
		src, err := os.ReadFile(filepath.Join(dir, *cfg.Checker))
		if err != nil {
			return nil, fmt.Errorf("读取校验器文件: %w", err)
		}
		checkerRel = *cfg.Checker
		checkerSha = sha256Hex(src)
		checkerSrc = src
	}

	cases := make([]CaseMeta, 0, len(indexes))
	contents := make(map[int]SampleIO, len(indexes))
	for _, i := range indexes {
		inData, err := os.ReadFile(filepath.Join(casesDir, strconv.Itoa(i), "in"))
		if err != nil {
			return nil, fmt.Errorf("读取测试点 %d 输入: %w", i, err)
		}
		ansData, err := os.ReadFile(filepath.Join(casesDir, strconv.Itoa(i), "ans"))
		if err != nil {
			return nil, fmt.Errorf("读取测试点 %d 答案: %w", i, err)
		}
		contents[i] = SampleIO{Input: string(inData), Output: string(ansData)}
		inName, ansName := fmt.Sprintf("cases/%d/in", i), fmt.Sprintf("cases/%d/ans", i)
		if err := store.WriteFile(pid, inName, inData); err != nil {
			return nil, err
		}
		if err := store.WriteFile(pid, ansName, ansData); err != nil {
			return nil, err
		}
		cases = append(cases, CaseMeta{
			Index:   i,
			Sample:  sampleSet[i],
			InName:  inName,
			AnsName: ansName,
			InSize:  int64(len(inData)),
			AnsSize: int64(len(ansData)),
			InSHA:   sha256Hex(inData),
			AnsSHA:  sha256Hex(ansData),
		})
	}
	// 未声明样例时默认第一个测试点为样例
	if len(sampleSet) == 0 {
		cases[0].Sample = true
	}

	// 样例并入题面：以 Markdown 章节追加，编辑题面即可一并维护样例
	samples := make([]SampleIO, 0, 4)
	for _, tc := range cases {
		if tc.Sample {
			samples = append(samples, contents[tc.Index])
		}
	}
	statement = []byte(AppendSamplesMd(string(statement), samples))

	// 子任务校验：测试点必须存在、分值合计 100
	subtasks := make([]Subtask, 0, len(meta.Subtasks))
	if len(meta.Subtasks) > 0 {
		exists := map[int]bool{}
		for _, c := range cases {
			exists[c.Index] = true
		}
		sum := 0
		for _, st := range meta.Subtasks {
			if len(st.Cases) == 0 {
				return nil, fmt.Errorf("子任务不能为空")
			}
			for _, ci := range st.Cases {
				if !exists[ci] {
					return nil, fmt.Errorf("子任务引用了不存在的测试点 %d", ci)
				}
			}
			if st.Score <= 0 {
				return nil, fmt.Errorf("子任务分值必须为正数")
			}
			sum += st.Score
			subtasks = append(subtasks, Subtask{Cases: st.Cases, Score: st.Score})
		}
		if sum != 100 {
			return nil, fmt.Errorf("子任务分值之和必须为 100，当前为 %d", sum)
		}
	}

	// problem.yaml 与题面一并归档到存储目录，保证数据目录自包含
	_ = store.WriteFile(pid, "problem.yaml", raw)
	if cfg != nil {
		_ = store.WriteFile(pid, "config.yaml", cfg.raw)
	}
	if len(statement) > 0 {
		_ = store.WriteFile(pid, "statement.md", statement)
	}
	if checkerRel != "" {
		if err := store.WriteFile(pid, checkerRel, checkerSrc); err != nil {
			return nil, err
		}
	}

	p := &Problem{
		ID:             pid,
		Title:          meta.Title,
		Statement:      string(statement),
		TimeLimitMS:    meta.TimeLimitMS,
		MemoryLimitKB:  meta.MemoryLimitKB,
		Visibility:     meta.Visibility,
		Hidden:         meta.Hidden,
		CreatedBy:      im.CreatorUID,
		Tags:           meta.Tags,
		Subtasks:       subtasks,
		Cases:          cases,
		Compare:        pickNonEmpty(cfg.resolvedCompare(checkerRel), meta.Compare),
		FloatTolerance: cfg.resolvedTolerance(),
		CheckerFile:    checkerRel,
		CheckerSHA:     checkerSha,
		FileIO:         cfg.resolvedFileIO(),
		FileIOInput:    cfg.resolvedFileIOInput(),
		FileIOOutput:   cfg.resolvedFileIOOutput(),
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func sha256Hex(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// ReplaceCases 整包替换既有题目的测试数据（编辑题目复用建题界面，v0.5b-4）：
// dir 为新数据包解压目录（cases/<N>/in|ans 结构），旧测试点文件全部删除；
// 标题/时限/内存/隐藏态等元数据保持不变，新包 config.yaml 可覆盖
// 样例/子任务/文件 IO/校验器（与建题的「config 优先」语义一致）；
// 题面正文保留，样例章节按新数据重建（「## 样例」剥离后重追加）。
func (im *Importer) ReplaceCases(ctx context.Context, pid int64, dir string) (*Problem, error) {
	p, err := im.Repo.GetProblem(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("读取原题目: %w", err)
	}
	if err := normalizeFlatCases(dir); err != nil {
		return nil, err
	}
	cfg, err := parseProblemConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("解析 config.yaml: %w", err)
	}

	casesDir := filepath.Join(dir, "cases")
	caseEntries, err := os.ReadDir(casesDir)
	if err != nil {
		return nil, fmt.Errorf("读取 cases 目录: %w", err)
	}
	indexes := make([]int, 0, len(caseEntries))
	for _, e := range caseEntries {
		if !e.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(e.Name()); err == nil {
			indexes = append(indexes, n)
		}
	}
	sort.Ints(indexes)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("cases 目录下没有测试点")
	}

	store := testdata.NewLocalFS(im.DataDir)

	// 样例标记：config 声明 > 沿用原样例标记 > 首个测试点
	sampleSet := map[int]bool{}
	if cfg != nil && cfg.Samples != nil {
		for _, i := range *cfg.Samples {
			sampleSet[i] = true
		}
	} else {
		for _, c := range p.Cases {
			if c.Sample {
				sampleSet[c.Index] = true
			}
		}
	}

	// 子任务：config 声明（校验存在性与分值合计）> 沿用原子任务（引用的测试点必须仍存在）
	var subtasks []Subtask
	validateSubtasks := func(ysts []yamlSubtask) ([]Subtask, error) {
		exists := map[int]bool{}
		for _, i := range indexes {
			exists[i] = true
		}
		sum := 0
		out := make([]Subtask, 0, len(ysts))
		for _, st := range ysts {
			if len(st.Cases) == 0 {
				return nil, fmt.Errorf("子任务不能为空")
			}
			for _, ci := range st.Cases {
				if !exists[ci] {
					return nil, fmt.Errorf("子任务引用了不存在的测试点 %d", ci)
				}
			}
			if st.Score <= 0 {
				return nil, fmt.Errorf("子任务分值必须为正数")
			}
			sum += st.Score
			out = append(out, Subtask{Cases: st.Cases, Score: st.Score})
		}
		if sum != 100 {
			return nil, fmt.Errorf("子任务分值之和必须为 100，当前为 %d", sum)
		}
		return out, nil
	}
	switch {
	case cfg != nil && cfg.Subtasks != nil:
		if subtasks, err = validateSubtasks(*cfg.Subtasks); err != nil {
			return nil, err
		}
	case len(p.Subtasks) > 0:
		exists := map[int]bool{}
		for _, i := range indexes {
			exists[i] = true
		}
		for _, st := range p.Subtasks {
			for _, ci := range st.Cases {
				if !exists[ci] {
					return nil, fmt.Errorf("新数据缺少原子任务引用的测试点 %d（原题目配置了子任务计分，请在 zip 内 config.yaml 声明 subtasks）", ci)
				}
			}
		}
		subtasks = p.Subtasks
	}

	// 写入新测试点并重建元数据
	cases := make([]CaseMeta, 0, len(indexes))
	contents := make(map[int]SampleIO, len(indexes))
	for _, i := range indexes {
		inData, err := os.ReadFile(filepath.Join(casesDir, strconv.Itoa(i), "in"))
		if err != nil {
			return nil, fmt.Errorf("读取测试点 %d 输入: %w", i, err)
		}
		ansData, err := os.ReadFile(filepath.Join(casesDir, strconv.Itoa(i), "ans"))
		if err != nil {
			return nil, fmt.Errorf("读取测试点 %d 答案: %w", i, err)
		}
		contents[i] = SampleIO{Input: string(inData), Output: string(ansData)}
		inName, ansName := fmt.Sprintf("cases/%d/in", i), fmt.Sprintf("cases/%d/ans", i)
		if err := store.WriteFile(pid, inName, inData); err != nil {
			return nil, err
		}
		if err := store.WriteFile(pid, ansName, ansData); err != nil {
			return nil, err
		}
		cases = append(cases, CaseMeta{
			Index:   i,
			Sample:  sampleSet[i],
			InName:  inName,
			AnsName: ansName,
			InSize:  int64(len(inData)),
			AnsSize: int64(len(ansData)),
			InSHA:   sha256Hex(inData),
			AnsSHA:  sha256Hex(ansData),
		})
	}
	hasSample := false
	for i := range cases {
		if cases[i].Sample {
			hasSample = true
			break
		}
	}
	if !hasSample {
		cases[0].Sample = true
	}

	// 旧测试点清理：新包未覆盖到的编号，文件与目录一并删除（纯替换语义）
	newNames := map[string]bool{}
	newIdx := map[int]bool{}
	for _, c := range cases {
		newNames[c.InName], newNames[c.AnsName] = true, true
		newIdx[c.Index] = true
	}
	for _, c := range p.Cases {
		for _, name := range []string{c.InName, c.AnsName} {
			if !newNames[name] {
				_ = store.Delete(pid, name)
			}
		}
		if !newIdx[c.Index] {
			_ = store.RemoveDir(pid, fmt.Sprintf("cases/%d", c.Index))
		}
	}

	// 题面：正文保留，样例章节按新数据重建
	samples := make([]SampleIO, 0, 4)
	for _, tc := range cases {
		if tc.Sample {
			samples = append(samples, contents[tc.Index])
		}
	}
	p.Statement = AppendSamplesMd(StripSamplesMd(p.Statement), samples)
	if err := store.WriteFile(pid, "statement.md", []byte(p.Statement)); err != nil {
		return nil, err
	}

	// 校验器：新包 config.yaml 声明才替换，否则沿用（旧文件名不同则删除旧文件）
	if cfg != nil && cfg.Checker != nil && *cfg.Checker != "" {
		src, err := os.ReadFile(filepath.Join(dir, *cfg.Checker))
		if err != nil {
			return nil, fmt.Errorf("读取校验器文件: %w", err)
		}
		if err := store.WriteFile(pid, *cfg.Checker, src); err != nil {
			return nil, err
		}
		if p.CheckerFile != "" && p.CheckerFile != *cfg.Checker {
			_ = store.Delete(pid, p.CheckerFile)
		}
		p.CheckerFile, p.CheckerSHA = *cfg.Checker, sha256Hex(src)
	}

	// 文件 IO：新包 config.yaml 声明才覆盖
	if cfg != nil {
		if cfg.FileIO != nil {
			p.FileIO = *cfg.FileIO
		}
		if cfg.FileIOInput != nil {
			p.FileIOInput = *cfg.FileIOInput
		}
		if cfg.FileIOOutput != nil {
			p.FileIOOutput = *cfg.FileIOOutput
		}
	}

	// problem.yaml 归档重建（数据卷保持自包含，反映替换后的数据形态）
	meta := problemYAML{
		Title:         p.Title,
		TimeLimitMS:   p.TimeLimitMS,
		MemoryLimitKB: p.MemoryLimitKB,
		Visibility:    p.Visibility,
		Hidden:        p.Hidden,
		Tags:          p.Tags,
		Compare:       p.Compare,
	}
	for _, c := range cases {
		if c.Sample {
			meta.Samples = append(meta.Samples, c.Index)
		}
	}
	for _, st := range subtasks {
		meta.Subtasks = append(meta.Subtasks, yamlSubtask{Cases: st.Cases, Score: st.Score})
	}
	if yb, err := yaml.Marshal(&meta); err == nil {
		_ = store.WriteFile(pid, "problem.yaml", yb)
	}
	if cfg != nil && len(cfg.raw) > 0 {
		_ = store.WriteFile(pid, "config.yaml", cfg.raw)
	}

	p.Cases = cases
	p.Subtasks = subtasks
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// pickNonEmpty 返回第一个非空值（全空返回空串）。
func pickNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- config.yaml 白名单解析（v0.5b-2）----

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

	raw []byte // 原文归档进测试数据卷
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
