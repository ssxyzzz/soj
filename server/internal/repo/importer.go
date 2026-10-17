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
	DataDir    string  // testdata 存储根目录
	CreatorUID int64   // 导入者 uid（0 = 系统/命令行导入，其题目隐藏态仅 root 可改）
	TagIDs     []int64 // 网页建题直传的标签节点 ID（非空时优先于 yaml tags 按名匹配）
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

	// 标签挂载（v0.5f）：网页建题直传节点 ID（校验存在性），否则 yaml tags 按名匹配
	//（同名取首个，未命中忽略——树里没有的名字不阻塞导入）
	tagIDs, err := im.resolveTags(ctx, meta.Tags)
	if err != nil {
		return nil, fmt.Errorf("挂载标签: %w", err)
	}

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
			subtasks = append(subtasks, Subtask(st))
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
		Tags:           tagIDs,
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

// tagNamesOf 节点 ID → 名字（problem.yaml 归档用；树中已不存在的 ID 忽略，
// 读取树失败时返回 nil——归档是尽力而为，不阻塞替换主流程）。
func (im *Importer) tagNamesOf(ctx context.Context, ids []int64) []string {
	if len(ids) == 0 {
		return nil
	}
	tree, err := im.Repo.GetTagTree(ctx)
	if err != nil {
		return nil
	}
	names := TagIDNames(tree)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, ok := names[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

// resolveTags 把标签来源归一为树节点 ID：TagIDs 非空（网页建题）时校验每个 ID
// 在树中存在；否则按名字匹配（同名取首个，未命中的名字忽略不报错）。
func (im *Importer) resolveTags(ctx context.Context, names []string) ([]int64, error) {
	if len(im.TagIDs) > 0 {
		if err := im.Repo.ValidateTagRefs(ctx, im.TagIDs); err != nil {
			return nil, err
		}
		return im.TagIDs, nil
	}
	if len(names) == 0 {
		return nil, nil
	}
	tree, err := im.Repo.GetTagTree(ctx)
	if err != nil {
		return nil, err
	}
	lookup := TagNameLookup(tree)
	out := make([]int64, 0, len(names))
	seen := map[int64]bool{}
	for _, name := range names {
		if id, ok := lookup[name]; ok && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, nil
}

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
			out = append(out, Subtask(st))
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

	// problem.yaml 归档重建（数据卷保持自包含，反映替换后的数据形态）；
	// 标签写名字而非节点 ID——yaml 保持可读，重导入按名回匹配同一节点
	meta := problemYAML{
		Title:         p.Title,
		TimeLimitMS:   p.TimeLimitMS,
		MemoryLimitKB: p.MemoryLimitKB,
		Visibility:    p.Visibility,
		Hidden:        p.Hidden,
		Tags:          im.tagNamesOf(ctx, p.Tags),
		Compare:       p.Compare,
	}
	for _, c := range cases {
		if c.Sample {
			meta.Samples = append(meta.Samples, c.Index)
		}
	}
	for _, st := range subtasks {
		meta.Subtasks = append(meta.Subtasks, yamlSubtask(st))
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
