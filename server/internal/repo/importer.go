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

	"go.mongodb.org/mongo-driver/v2/bson"

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
	Title         string              `yaml:"title"`
	Type          string              `yaml:"type,omitempty"`          // 题目类型（v0.9）：空 = 传统题；objective = 客观题族
	Questions     []ObjectiveQuestion `yaml:"questions,omitempty"`     // objective 小题列表（v0.9.3）
	Question      *ObjectiveQuestion  `yaml:"question,omitempty"`      // 单题旧写法（解析兼容，导入时并入 questions）
	Time          *timeLimit          `yaml:"time,omitempty"`          // 导出主形态（"1s"/"0.5s"）；解析时低于 timeLimitMs
	TimeLimitMS   *int                `yaml:"timeLimitMs,omitempty"`   // 旧字段：仅解析兼容，不再导出
	Memory        *memLimit           `yaml:"memory,omitempty"`        // 导出主形态（"256MB"）；解析时低于 memoryLimitKb
	MemoryLimitKB *int                `yaml:"memoryLimitKb,omitempty"` // 旧字段：仅解析兼容，不再导出
	Samples       []int               `yaml:"samples,omitempty"`
	Visibility    string              `yaml:"visibility,omitempty"`
	Hidden        bool                `yaml:"hidden,omitempty"` // 主题库隐藏
	Tags          []string            `yaml:"tags,omitempty"`
	Compare       string              `yaml:"compare,omitempty"`       // 比对模式 exact/tolerant（v0.5.2-1）
	Subtasks      []yamlSubtask       `yaml:"subtasks,omitempty"`      // 可选：子任务分组给分（组内全过才得分）
	Checker       string              `yaml:"checker,omitempty"`       // 校验器源文件名（卷内相对路径）
	Filename      string              `yaml:"filename,omitempty"`      // 文件 IO 直白形态：atbash → atbash.in/out
	FileIO        string              `yaml:"file_io,omitempty"`       // 同 filename 的别名（解析兼容）
	FileIOInput   string              `yaml:"file_io_input,omitempty"` // 显式对（in/out 不同名时的保真形态）
	FileIOOutput  string              `yaml:"file_io_output,omitempty"`
}

// resolvedFileIO v2 单 yaml 的文件 IO 归一：显式 input/output 优先于别名；
// 未声明时返回 (false, "", "")（由 config.yaml 覆盖层或场次级开关接管）。
func (y *problemYAML) resolvedFileIO() (bool, string, string) {
	in, out := y.FileIOInput, y.FileIOOutput
	name := y.Filename // filename 与 file_io 同义别名，filename 优先
	if name == "" {
		name = y.FileIO
	}
	if name != "" {
		if in == "" {
			in = name + ".in"
		}
		if out == "" {
			out = name + ".out"
		}
	}
	return (name != "" || in != "" || out != ""), in, out
}

// applyYAMLLimits 归一 problem.yaml 的时限/内存：本站字段优先于 Hydro 惯例带单位字段，
// 缺省/非法回落默认（1s / 256MB）。返回 (ms, kb)。
func (y *problemYAML) applyYAMLLimits() (int, int) {
	ms, kb := 1000, 262144
	if y.TimeLimitMS != nil && *y.TimeLimitMS > 0 {
		ms = *y.TimeLimitMS
	} else if y.Time != nil && *y.Time > 0 {
		ms = int(float64(*y.Time) * 1000)
	}
	if y.MemoryLimitKB != nil && *y.MemoryLimitKB > 0 {
		kb = *y.MemoryLimitKB
	} else if y.Memory != nil && *y.Memory > 0 {
		kb = int(float64(*y.Memory) * 1024)
	}
	return ms, kb
}

// seedPID 从目录名解析题号：数字前缀即题号（如 "6_hidden" → 6）；无前缀、
// 非数字或非正数返回 0（由调用方决定自动分配）。ImportDir 与 SeedAll 共用同一条规则。
func seedPID(name string) int64 {
	if n, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64); err == nil && n > 0 {
		return n
	}
	return 0
}

// SeedAll 首次部署播种 seedDir 下全部题目目录：meta.migrations.seedV1 置位后跳过
// （每次启动重播会以整文档替换冲掉网页端对种子题的修改）。中途失败不置旗标、
// 下次启动重试，循环内的逐目录补缺跳过保护重试窗口。
func (im *Importer) SeedAll(ctx context.Context, seedDir string) error {
	done, err := im.Repo.seedDone(ctx)
	if err != nil {
		return err
	}
	if done {
		fmt.Println("[seed] 已播种过（meta.seedV1），跳过——新增题目用 make import 或网页建题")
		return nil
	}
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
		dir := filepath.Join(seedDir, name)
		skip, why, err := im.seedSkipReason(ctx, dir, name)
		if err != nil {
			return err
		}
		if skip {
			fmt.Printf("[seed] 跳过 %s（%s）\n", name, why)
			continue
		}
		pr, err := im.ImportDir(ctx, dir)
		if err != nil {
			return fmt.Errorf("导入 %s: %w", name, err)
		}
		fmt.Printf("[seed] 已导入题目 #%d (%s)\n", pr.ID, name)
	}
	if err := im.Repo.markSeedDone(ctx); err != nil {
		return err
	}
	fmt.Println("[seed] 首次播种完成，已标记 seedV1（此后启动不再播种）")
	return nil
}

// seedSkipReason 判断种子目录是否跳过（补缺不覆盖）：有数字前缀按题号查库，
// 无前缀按 problem.yaml 标题（缺省用目录名）查同名题。返回 (是否跳过, 原因, 错误)。
func (im *Importer) seedSkipReason(ctx context.Context, dir, name string) (bool, string, error) {
	if pid := seedPID(name); pid > 0 {
		err := im.Repo.Problems.FindOne(ctx, bson.M{"_id": pid}).Err()
		if err == nil {
			return true, fmt.Sprintf("#%d 已存在，补缺不覆盖，保护线上修改", pid), nil
		}
		if !IsNotFound(err) {
			return false, "", err
		}
		return false, "", nil
	}
	title := name
	if raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml")); err == nil {
		var meta problemYAML
		if yaml.Unmarshal(raw, &meta) == nil && meta.Title != "" {
			title = meta.Title
		}
	}
	n, err := im.Repo.Problems.CountDocuments(ctx, bson.M{"title": title})
	if err != nil {
		return false, "", err
	}
	if n > 0 {
		return true, fmt.Sprintf("已存在同名题「%s」，防重复播种", title), nil
	}
	return false, "", nil
}

// ImportDir 导入单个题目目录，返回创建的题目（含 config.yaml/内置 checker 解析结果）。
func (im *Importer) ImportDir(ctx context.Context, dir string) (*Problem, error) {
	base := filepath.Base(dir)
	pid := seedPID(base)

	// 无感嗅探（v0.9）：SOJ / Hydro / 裸数据三路归一，无法识别时带所见内容报错
	if _, err := DetectAndNormalize(dir); err != nil {
		return nil, err
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
	if meta.Visibility == "" {
		meta.Visibility = VisibilityPublic
	}
	switch meta.Type {
	case "", "default":
		meta.Type = TypeDefault
	case TypeObjective, TypeFunction, TypeInteractive:
	default:
		return nil, fmt.Errorf("暂不支持题型 %q（当前支持：传统题 default、客观题 objective、函数题 function、交互题 interactive）", meta.Type)
	}

	// config.yaml 白名单字段覆盖 problem.yaml（v0.5.2-2）：未知字段忽略不报错
	cfg, err := parseProblemConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("解析 config.yaml: %w", err)
	}
	cfg.applyTo(&meta)
	// 限额归一须在 config 覆盖之后（Hydro 别名/旧双层覆盖层同样作用于时限内存）
	msLimit, kbLimit := meta.applyYAMLLimits()

	// config.yaml 声明的校验器文件（白名单成员，提前确定供附带文件收集排除）
	checkerRel := ""
	if cfg != nil && cfg.Checker != nil && *cfg.Checker != "" {
		checkerRel = *cfg.Checker
	}

	// 标签挂载（v0.5.5）：网页建题直传节点 ID（校验存在性），否则 yaml tags 按名匹配
	//（同名取首个，未命中忽略——树里没有的名字不阻塞导入）
	tagIDs, err := im.resolveTags(ctx, meta.Tags)
	if err != nil {
		return nil, fmt.Errorf("挂载标签: %w", err)
	}

	statement, err := os.ReadFile(filepath.Join(dir, "statement.md"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// 平铺测试点布局归一：根目录 N.in/N.ans 自动整理为 cases/<序号>/ 结构
	if err := normalizeFlatCases(dir); err != nil {
		return nil, err
	}

	// 附带文件存档（gen.py / std.cpp 等造数据脚本）：必须在平铺归一之后收集——
	// 否则根下的测试数据文件会被误当附带文件双份存档；__MACOSX/.DS_Store/._* 不入档
	extraFiles, err := collectExtraFiles(dir, checkerRel, typeWhitelist(meta.Type))
	if err != nil {
		return nil, fmt.Errorf("收集附带文件: %w", err)
	}

	// 客观题（v0.9）：无测试数据/校验器/子任务，定义校验后直接入库
	if meta.Type == TypeObjective {
		return im.importObjective(ctx, dir, pid, &meta, statement, tagIDs, extraFiles, raw)
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
	if len(indexes) == 0 && meta.Type != TypeFunction {
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

	// 内置校验器（v0.5.2-2）：目录内的 checker 源文件存入测试数据卷
	checkerSha, checkerSrc := "", []byte(nil)
	if checkerRel != "" {
		src, err := os.ReadFile(filepath.Join(dir, checkerRel))
		if err != nil {
			return nil, fmt.Errorf("读取校验器文件: %w", err)
		}
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
			if meta.Type != TypeInteractive {
				return nil, fmt.Errorf("读取测试点 %d 答案: %w", i, err)
			}
			ansData = []byte{} // 交互题答案文件可缺省（判定权在 interactor）
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
	// 函数题（v0.9.1）：driver/template 固定名落卷（driver 必须有，template 可选）
	driverFile, driverSHA, templateFile := "", "", ""
	interactorFile, interactorSHA := "", ""
	if meta.Type == TypeFunction {
		if src, err := os.ReadFile(filepath.Join(dir, "driver.cpp")); err == nil {
			if err := store.WriteFile(pid, "driver.cpp", src); err != nil {
				return nil, err
			}
			driverFile, driverSHA = "driver.cpp", sha256Hex(src)
		}
		if src, err := os.ReadFile(filepath.Join(dir, "template.cpp")); err == nil {
			if err := store.WriteFile(pid, "template.cpp", src); err != nil {
				return nil, err
			}
			templateFile = "template.cpp"
		}
		if driverFile == "" {
			return nil, fmt.Errorf("函数题缺少 driver.cpp（含 main、调用学生实现的函数）")
		}
	}
	// 交互题（v0.9.2）：interactor.cpp 固定名落卷（必须有，判题依赖）
	if meta.Type == TypeInteractive {
		src, ierr := os.ReadFile(filepath.Join(dir, "interactor.cpp"))
		if ierr != nil {
			return nil, fmt.Errorf("交互题缺少 interactor.cpp（读 argv[1] 输入文件、stdin/stdout 与选手双向通信、退出码 0 = 通过）")
		}
		if err := store.WriteFile(pid, "interactor.cpp", src); err != nil {
			return nil, err
		}
		interactorSHA = sha256Hex(src)
		interactorFile = "interactor.cpp"
	}
	// 附带文件落卷 extra/<zip内路径>：存档备用，判题流程不消费
	for _, rel := range extraFiles {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("读取附带文件 %s: %w", rel, err)
		}
		if err := store.WriteFile(pid, "extra/"+rel, data); err != nil {
			return nil, err
		}
	}

	p := &Problem{
		ID:             pid,
		Title:          meta.Title,
		Statement:      string(statement),
		Type:           meta.Type,
		TimeLimitMS:    msLimit,
		MemoryLimitKB:  kbLimit,
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
		DriverFile:     driverFile,
		DriverSHA:      driverSHA,
		TemplateFile:   templateFile,
		InteractorFile: interactorFile,
		InteractorSHA:  interactorSHA,
	}
	// 文件 IO 归一（v0.9 单 yaml）：config.yaml 覆盖层 > problem.yaml 声明 > 无
	if yFio, yIn, yOut := meta.resolvedFileIO(); yFio && !cfg.resolvedFileIO() {
		p.FileIO, p.FileIOInput, p.FileIOOutput = true, yIn, yOut
	} else {
		p.FileIO, p.FileIOInput, p.FileIOOutput = cfg.resolvedFileIO(), cfg.resolvedFileIOInput(), cfg.resolvedFileIOOutput()
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// importObjective 客观题导入路径（v0.9）：校验 question 定义与填空占位数，
// 无测试数据/校验器/子任务，problem.yaml/statement.md/extra 归档后入库。
func (im *Importer) importObjective(ctx context.Context, dir string, pid int64, meta *problemYAML, statement []byte, tagIDs []int64, extraFiles []string, raw []byte) (*Problem, error) {
	// 小题来源：questions[] 优先；旧包只有单题 question 时并入
	qs := meta.Questions
	if len(qs) == 0 && meta.Question != nil {
		qs = []ObjectiveQuestion{*meta.Question}
	}
	if len(qs) == 0 {
		return nil, fmt.Errorf("客观题缺少 questions 定义（problem.yaml 的 questions 列表）")
	}
	for i := range qs {
		NormalizeObjective(&qs[i])
	}
	NormalizeScores(qs)
	if err := ValidateObjectiveSet(qs); err != nil {
		return nil, fmt.Errorf("客观题定义不合法: %w", err)
	}
	// 单道填空题允许把题干写在整题 statement 里（旧包/单题习惯）
	if len(qs) == 1 && qs[0].Kind == ObjBlank && strings.TrimSpace(qs[0].Stem) == "" {
		if n := CountBlanksInStatement(string(statement)); n != len(qs[0].Blanks) {
			return nil, fmt.Errorf("题面里有 %d 个空位（____），定义里有 %d 空，请保持一致", n, len(qs[0].Blanks))
		}
		qs[0].Stem = string(statement)
	}
	var err error
	if pid == 0 {
		if pid, err = im.Repo.nextSeq(ctx, "problem"); err != nil {
			return nil, err
		}
	}
	store := testdata.NewLocalFS(im.DataDir)
	_ = store.WriteFile(pid, "problem.yaml", raw)
	if len(statement) > 0 {
		_ = store.WriteFile(pid, "statement.md", statement)
	}
	for _, rel := range extraFiles {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("读取附带文件 %s: %w", rel, err)
		}
		if err := store.WriteFile(pid, "extra/"+rel, data); err != nil {
			return nil, err
		}
	}
	p := &Problem{
		ID: pid, Title: meta.Title, Statement: string(statement),
		Type: TypeObjective, Questions: qs,
		// 客观题不消费时空限制，占位默认值（渲染导出时同样带上）
		TimeLimitMS: 1000, MemoryLimitKB: 262144,
		Visibility: meta.Visibility,
		Hidden:     meta.Hidden,
		CreatedBy:  im.CreatorUID,
		Tags:       tagIDs,
		Cases:      []CaseMeta{},
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// RenderProblemYAML 权威导出（v0.9 下载）：题目元数据按 db 当前值渲染为 v2 单 yaml，
// 下载包重新上传可完整重建。客观题定义全量导出（含答案与解析——下载本身有权限位+限额把门）。
func (im *Importer) RenderProblemYAML(ctx context.Context, p *Problem) []byte {
	ms, kb := p.TimeLimitMS, p.MemoryLimitKB
	if ms <= 0 {
		ms = 1000
	}
	if kb <= 0 {
		kb = 262144
	}
	meta := problemYAML{
		Title:      p.Title,
		Type:       p.Type,
		Time:       timeLimitPtr(timeLimit(ms) / 1000),
		Memory:     memLimitPtr(memLimit(kb) / 1024),
		Visibility: pickNonEmpty(p.Visibility, VisibilityPublic),
		Hidden:     p.Hidden,
		Tags:       im.tagNamesOf(ctx, p.Tags),
		Compare:    p.Compare,
		Questions:  p.Questions,
	}
	for _, c := range p.Cases {
		if c.Sample {
			meta.Samples = append(meta.Samples, c.Index)
		}
	}
	for _, st := range p.Subtasks {
		meta.Subtasks = append(meta.Subtasks, yamlSubtask{Cases: st.Cases, Score: st.Score})
	}
	if p.CheckerFile != "" {
		meta.Checker = p.CheckerFile
	}
	renderFileIO(&meta, p)
	yb, err := yaml.Marshal(&meta)
	if err != nil {
		return nil
	}
	return yb
}

func timeLimitPtr(v timeLimit) *timeLimit { return &v }
func memLimitPtr(v memLimit) *memLimit    { return &v }

// renderFileIO 文件 IO 渲染（直白优先）：in/out 同基名 → filename: atbash 一个字段；
// 显式配了不同名才落 file_io_input/output 保真。
func renderFileIO(meta *problemYAML, p *Problem) {
	if !p.FileIO {
		return
	}
	base := func(name, def string) string {
		if i := strings.LastIndex(name, "."); i > 0 {
			return name[:i]
		}
		return def
	}
	inB := base(p.FileIOInput, "problem")
	outB := base(p.FileIOOutput, inB)
	if inB == outB && inB != "problem" {
		meta.Filename = inB
		return
	}
	if p.FileIOInput != "" || p.FileIOOutput != "" {
		meta.FileIOInput, meta.FileIOOutput = p.FileIOInput, p.FileIOOutput
	} else {
		meta.Filename = inB
	}
}

// BuildObjectiveYAML 网页建客观题用：生成 v2 problem.yaml（title/type/questions 列表）。
func BuildObjectiveYAML(title string, qs []ObjectiveQuestion) ([]byte, error) {
	meta := problemYAML{Title: title, Type: TypeObjective, Questions: qs}
	return yaml.Marshal(&meta)
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

// ReplaceCases 整包替换既有题目的测试数据（编辑题目复用建题界面，v0.5.2-4）：
// dir 为新数据包解压目录（cases/<N>/in|ans 结构），旧测试点文件全部删除；
// 标题/时限/内存/隐藏态等元数据保持不变，新包 config.yaml 可覆盖
// 样例/子任务/文件 IO/校验器（与建题的「config 优先」语义一致）；
// 题面正文保留，样例章节按新数据重建（「## 样例」剥离后重追加）。
func (im *Importer) ReplaceCases(ctx context.Context, pid int64, dir string) (*Problem, error) {
	p, err := im.Repo.GetProblem(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("读取原题目: %w", err)
	}
	if p.IsObjective() {
		return nil, fmt.Errorf("客观题不支持整包替换：请在编辑页直接修改题目内容")
	}
	if _, err := DetectAndNormalize(dir); err != nil {
		return nil, err
	}
	if err := normalizeFlatCases(dir); err != nil {
		return nil, err
	}
	cfg, err := parseProblemConfig(dir)
	if err != nil {
		return nil, fmt.Errorf("解析 config.yaml: %w", err)
	}
	if probePackageType(dir) == TypeObjective {
		return nil, fmt.Errorf("整包替换不支持客观题包：请在编辑页直接修改题目内容")
	}
	// 校验器声明与附带文件清单（extra 存档）——收集在 normalizeFlatCases 之后的布局上进行
	checkerRel := ""
	if cfg != nil && cfg.Checker != nil && *cfg.Checker != "" {
		checkerRel = *cfg.Checker
	}
	extraFiles, err := collectExtraFiles(dir, checkerRel, typeWhitelist(p.Type))
	if err != nil {
		return nil, fmt.Errorf("收集附带文件: %w", err)
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

	// 旧测试点清理：新包未覆盖到的编号，文件与目录一并删除（纯替换语义）；
	// 附带文件存档同批清理，随后按新包重建
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
	_ = store.RemoveDir(pid, "extra")

	// 题面：正文保留，样例章节按新数据重建（md 单一格式，v0.8 起 HTML 题面已随回迁消失）
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

	// 附带文件按新包重建（extra/<zip内路径>；旧存档已在上方整目录清理）
	for _, rel := range extraFiles {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("读取附带文件 %s: %w", rel, err)
		}
		if err := store.WriteFile(pid, "extra/"+rel, data); err != nil {
			return nil, err
		}
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
		Title:      p.Title,
		Time:       timeLimitPtr(timeLimit(p.TimeLimitMS) / 1000),
		Memory:     memLimitPtr(memLimit(p.MemoryLimitKB) / 1024),
		Visibility: p.Visibility,
		Hidden:     p.Hidden,
		Tags:       im.tagNamesOf(ctx, p.Tags),
		Compare:    p.Compare,
	}
	for _, c := range cases {
		if c.Sample {
			meta.Samples = append(meta.Samples, c.Index)
		}
	}
	for _, st := range subtasks {
		meta.Subtasks = append(meta.Subtasks, yamlSubtask(st))
	}
	renderFileIO(&meta, p)
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

// collectExtraFiles 收集题包目录中判题白名单之外的文件（gen.py/std.cpp 等造数据脚本，
// 存档备用、随数据包下载，v0.8.3）。白名单 = cases/ 子树、problem.yaml、config.yaml、
// statement.md、config.yaml 声明的校验器文件；返回相对 dir 的正斜杠路径（稳定排序）。
func collectExtraFiles(dir, checkerRel string, extraWhitelist []string) ([]string, error) {
	skipRoot := map[string]bool{"problem.yaml": true, "config.yaml": true, "statement.md": true}
	for _, name := range extraWhitelist {
		skipRoot[name] = true
	}
	out := make([]string, 0, 8)
	err := filepath.WalkDir(dir, func(full string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// cases/ 子树整棵跳过（测试数据本体）；__MACOSX 打包杂物目录不入存档
			if full != dir && d.Name() == "cases" && filepath.Dir(full) == dir {
				return filepath.SkipDir
			}
			if d.Name() == "__MACOSX" {
				return filepath.SkipDir
			}
			return nil
		}
		// macOS 打包杂物：.DS_Store 与 AppleDouble（._*）不入存档
		if d.Name() == ".DS_Store" || strings.HasPrefix(d.Name(), "._") {
			return nil
		}
		rel, err := filepath.Rel(dir, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skipRoot[rel] || (checkerRel != "" && rel == checkerRel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// typeWhitelist 题型专属的固定名文件（不入 extra 存档：判题消费或前端预填）。
func typeWhitelist(t string) []string {
	switch t {
	case TypeFunction:
		return []string{"driver.cpp", "template.cpp"}
	case TypeInteractive:
		return []string{"interactor.cpp"}
	}
	return nil
}
