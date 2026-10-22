package repo

// Importer 把「SOJ 题包目录」导入系统（import-design.md 定稿格式，2026-10-22 换血）：
//
//	<题目目录>/
//	  problem.md      题面（唯一来源；样例直接写在正文里）
//	  problem.yaml    title/type/difficulty/tag/来源四字段（客观题另带 questions 小题定义）
//	  answer.yaml     客观题纯答案
//	  answer.md       客观题解析
//	  data/           数据对原名（*.in + *.ans|.out）+ config.yaml + 附属文件
//	    config.yaml   time/memory/filename/subtasks/checker
//
// 表单直传字段（建题路径）优先于包内声明；题号一律自动分配（IgnoreDirPID 防顶号）。
// 旧格式（statement.md / cases/ 序号 / samples 样例标记 / extra/）不再支持。

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

type Importer struct {
	Repo       *Repo
	DataDir    string // testdata 存储根目录
	CreatorUID int64  // 导入者 uid（0 = 系统/命令行导入，其题目隐藏态仅 root 可改）

	// ---- 表单直传（建题路径权威输入，非零值优先于包内声明）----
	TagIDs          []int64 // 标签节点 ID
	Difficulty      int     // 难度 1–10
	SourceType      string  // 来源类型
	Source          string
	Year            int
	Region          string
	TimeLimitMS     int // 时限毫秒
	MemoryLimitKB   int // 内存 KB
	FileIOName      string // 文件 IO 基名（way → way.in/way.out）
	Compare         string // exact / tolerant
	FloatTolerance  float64
	Hidden          bool  // 显式隐藏态（建题表单无此输入，恒 false；批量走 DefaultHidden）
	// ObjectiveQuestions 网页建客观题直传（含答案的完整定义，非空时优先于包内 questions）。
	ObjectiveQuestions []ObjectiveQuestion
	// Checker 表单上传校验器的 data/ 内固定名（非空 = 声明启用；文件不存在则忽略）。
	Checker string

	// IgnoreDirPID 忽略目录名数字前缀、题号一律自动分配（批量导入防顶号）。
	IgnoreDirPID bool
	// DefaultHidden yaml 未声明 hidden 时的缺省值（批量导入「导入后默认隐藏」开关）。
	DefaultHidden bool
}

// seedPID 从目录名解析题号（仅种子播种用）：数字前缀即题号；无前缀返回 0。
func seedPID(name string) int64 {
	if n, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64); err == nil && n > 0 {
		return n
	}
	return 0
}

// dirTitleFallback 无 problem.yaml/title 时的标题：目录名剥掉前导「数字+分隔符」。
func dirTitleFallback(name string) string {
	name = strings.TrimLeft(name, "0123456789")
	return strings.TrimLeft(name, "_- .")
}

// SeedAll 首次部署播种 seedDir 下全部题目目录（SOJ 格式）：meta.migrations.seedV1
// 置位后跳过；点开头的隐藏目录不是题包直接忽略。
func (im *Importer) SeedAll(ctx context.Context, seedDir string) error {
	done, err := im.Repo.seedDone(ctx)
	if err != nil {
		return err
	}
	if done {
		fmt.Println("[seed] 已播种过（meta.seedV1），跳过——新增题目用批量导入或网页建题")
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
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
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
// 无前缀按 problem.yaml 标题查同名题。
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
	title := dirTitleFallback(name)
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

// ImportDir 导入单个 SOJ 题目目录，返回创建的题目。
func (im *Importer) ImportDir(ctx context.Context, dir string) (*Problem, error) {
	base := filepath.Base(dir)
	pid := int64(0)
	if !im.IgnoreDirPID {
		pid = seedPID(base)
	}

	// problem.md = 题目目录的身份证（缺失即不是 SOJ 题包）
	statement, err := os.ReadFile(filepath.Join(dir, "problem.md"))
	if err != nil {
		return nil, fmt.Errorf("缺少 problem.md（SOJ 题包以 problem.md 题面识别题目目录；纯数据上传请走建题/编辑页）")
	}

	var meta problemYAML
	if raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml")); err == nil {
		if err := yaml.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("解析 problem.yaml: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = dirTitleFallback(base)
	}
	if meta.Title == "" {
		return nil, fmt.Errorf("标题为空：problem.yaml 缺 title 且目录名无可用文字")
	}

	// 表单直传覆盖（建题路径权威输入）；yaml 的 "default" 归一为空串（TypeDefault）
	if im.ObjectiveQuestions != nil {
		meta.Type = TypeObjective
	}
	if meta.Type == "default" {
		meta.Type = TypeDefault
	}
	switch meta.Type {
	case TypeDefault, TypeObjective, TypeFunction, TypeInteractive:
	default:
		return nil, fmt.Errorf("暂不支持题型 %q（当前支持：传统题 default、客观题 objective、函数题 function、交互题 interactive）", meta.Type)
	}

	// 难度归一：表单 > yaml > 默认 D3
	d := im.Difficulty
	if d == 0 {
		d = meta.Difficulty
	}
	if d != 0 && !ValidDifficulty(d) {
		return nil, fmt.Errorf("难度需在 1-10 之间")
	}
	if d == 0 {
		d = 3
	}
	meta.Difficulty = d

	// 来源四字段归一：表单 > yaml > 标题方括号识别
	if err := im.applySourceFields(meta.Title, &meta); err != nil {
		return nil, err
	}
	if meta.Source != "" {
		_ = im.Repo.UpsertSourceByName(ctx, meta.Source, meta.SourceType)
	}

	// 标签：表单节点 ID > yaml tag 名匹配（未匹配入来源库 + rawTags 保留）
	tagIDs, rawNames, err := im.resolveTags(ctx, meta.Tag)
	if err != nil {
		return nil, fmt.Errorf("挂载标签: %w", err)
	}
	for _, n := range rawNames {
		_ = im.Repo.UpsertSourceByName(ctx, n, "")
	}

	// 客观题：无 data/；小题定义三源合并（表单直传 > problem.yaml + answer.yaml + answer.md）
	if meta.Type == TypeObjective {
		return im.importObjective(ctx, dir, pid, &meta, statement, tagIDs, rawNames)
	}

	// ---- 编程题：data/ 收集 ----
	pairs, attachments, cfg, err := collectDataDir(filepath.Join(dir, "data"), meta.Type)
	if err != nil {
		return nil, err
	}

	// 时限/内存：config > 表单 > 默认
	msLimit, kbLimit := 1000, 262144
	if cfg != nil {
		if cfg.TimeLimitMS != nil {
			msLimit = *cfg.TimeLimitMS
		}
		if cfg.MemoryLimitKB != nil {
			kbLimit = *cfg.MemoryLimitKB
		}
	}
	if im.TimeLimitMS != 0 {
		msLimit = im.TimeLimitMS
	}
	if im.MemoryLimitKB != 0 {
		kbLimit = im.MemoryLimitKB
	}

	// 校验器声明：表单固定名 > data/config.yaml 的 checker 字段
	checkerRel := im.Checker
	if checkerRel == "" && cfg != nil && cfg.Checker != nil && *cfg.Checker != "" {
		checkerRel = *cfg.Checker
	}
	if im.Checker != "" {
		if _, err := os.Stat(filepath.Join(dir, "data", im.Checker)); err != nil {
			checkerRel = "" // 表单声明了但文件没落盘（未上传）——不启用
		}
	}

	if pid == 0 {
		if pid, err = im.Repo.nextSeq(ctx, "problem"); err != nil {
			return nil, err
		}
	}
	store := testdata.NewLocalFS(im.DataDir)

	// 数据对落卷（原名 data/<name>），序号按排序重排 1..N
	cases := make([]CaseMeta, 0, len(pairs))
	for i, pr := range pairs {
		inData, err := os.ReadFile(filepath.Join(dir, "data", pr.Base+".in"))
		if err != nil {
			return nil, fmt.Errorf("读取测试点 %s.in: %w", pr.Base, err)
		}
		var ansData []byte
		ansName := ""
		if pr.AnsExt != "" {
			ansName = "data/" + pr.Base + pr.AnsExt
			ansData, err = os.ReadFile(filepath.Join(dir, "data", pr.Base+pr.AnsExt))
			if err != nil {
				return nil, fmt.Errorf("读取答案 %s: %w", pr.Base+pr.AnsExt, err)
			}
		} else if meta.Type != TypeInteractive {
			return nil, fmt.Errorf("测试点 %s 缺少答案文件（.ans/.out）", pr.Base)
		}
		if err := store.WriteFile(pid, "data/"+pr.Base+".in", inData); err != nil {
			return nil, err
		}
		if ansName != "" {
			if err := store.WriteFile(pid, ansName, ansData); err != nil {
				return nil, err
			}
		}
		cases = append(cases, CaseMeta{
			Index:   i + 1,
			InName:  "data/" + pr.Base + ".in",
			AnsName: ansName,
			InSize:  int64(len(inData)),
			AnsSize: int64(len(ansData)),
			InSHA:   sha256Hex(inData),
			AnsSHA:  sha256Hex(ansData),
		})
	}
	if len(cases) == 0 && meta.Type != TypeFunction {
		// 无数据空壳题允许（题面立即可用、暂不可提交）；函数题必须有 driver 也不必强求数据
		_ = cases
	}

	// 子任务校验（config.subtasks；引用 = 测试点序号，Hydro 文件对写法归一为序号）
	subtasks, err := validateSubtasks(cfgSubtasks(cfg), len(cases))
	if err != nil {
		return nil, err
	}

	// 函数/交互题固定名文件（data/ 内）；校验器声明文件
	p := &Problem{
		ID:             pid,
		Title:          meta.Title,
		Statement:      string(statement),
		Type:           meta.Type,
		Difficulty:     d,
		SourceType:     meta.SourceType,
		Source:         meta.Source,
		Year:           meta.Year,
		Region:         meta.Region,
		TimeLimitMS:    msLimit,
		MemoryLimitKB:  kbLimit,
		Visibility:     VisibilityPublic,
		Hidden:         im.effHidden(&meta),
		CreatedBy:      im.CreatorUID,
		Tags:           tagIDs,
		RawTags:        rawNames,
		Subtasks:       subtasks,
		Cases:          cases,
	}
	if err := im.writeTypeFiles(store, p, dir, checkerRel); err != nil {
		return nil, err
	}
	// 附属文件落卷（data/<原名>，与数据对同目录）
	for _, rel := range attachments {
		data, err := os.ReadFile(filepath.Join(dir, "data", filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("读取附属文件 %s: %w", rel, err)
		}
		if err := store.WriteFile(pid, "data/"+rel, data); err != nil {
			return nil, err
		}
	}
	im.applyConfigExtras(p, cfg)

	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// writeTypeFiles 题型专属文件落卷（data/ 固定名）：函数题 driver 必须、template 可选；
// 交互题 interactor 必须；校验器按 config.checker 声明。
func (im *Importer) writeTypeFiles(store testdata.Provider, p *Problem, dir, checkerRel string) error {
	pid := p.ID
	read := func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, "data", name))
	}
	if p.Type == TypeFunction {
		if src, err := read("driver.cpp"); err == nil {
			if err := store.WriteFile(pid, "data/driver.cpp", src); err != nil {
				return err
			}
			p.DriverFile, p.DriverSHA = "data/driver.cpp", sha256Hex(src)
		} else {
			return fmt.Errorf("函数题缺少 data/driver.cpp（含 main、调用学生实现的函数）")
		}
		if src, err := read("template.cpp"); err == nil {
			if err := store.WriteFile(pid, "data/template.cpp", src); err != nil {
				return err
			}
			p.TemplateFile = "data/template.cpp"
		}
	}
	if p.Type == TypeInteractive {
		src, err := read("interactor.cpp")
		if err != nil {
			return fmt.Errorf("交互题缺少 data/interactor.cpp（读 argv[1] 输入文件、stdin/stdout 与选手双向通信、退出码 0 = 通过）")
		}
		if err := store.WriteFile(pid, "data/interactor.cpp", src); err != nil {
			return err
		}
		p.InteractorFile, p.InteractorSHA = "data/interactor.cpp", sha256Hex(src)
	}
	if checkerRel != "" {
		src, err := os.ReadFile(filepath.Join(dir, "data", filepath.FromSlash(checkerRel)))
		if err != nil {
			return fmt.Errorf("读取校验器文件 %s: %w", checkerRel, err)
		}
		rel := "data/" + checkerRel
		if err := store.WriteFile(pid, rel, src); err != nil {
			return err
		}
		p.CheckerFile, p.CheckerSHA = rel, sha256Hex(src)
	}
	return nil
}

// applyConfigExtras config.yaml 的比对模式/文件 IO 归一（时限内存已单独处理）。
func (im *Importer) applyConfigExtras(p *Problem, cfg *ProblemConfig) {
	compare := ""
	tol := 0.0
	if cfg != nil {
		if cfg.Compare != nil {
			compare = *cfg.Compare
		}
		if cfg.FloatTolerance != nil {
			tol = *cfg.FloatTolerance
		}
	}
	if im.Compare != "" {
		compare = im.Compare // 表单直传优先
		if im.Compare == CompareTolerant && im.FloatTolerance > 0 {
			tol = im.FloatTolerance
		}
	}
	if p.CheckerFile == "" && compare != "" {
		p.Compare = compare
		if compare == CompareTolerant && tol > 0 {
			p.FloatTolerance = tol
		}
	}
	fileIO, inName, outName := false, "", ""
	if cfg != nil {
		fileIO = cfg.resolvedFileIO()
		inName, outName = cfg.resolvedFileIOInput(), cfg.resolvedFileIOOutput()
	}
	if im.FileIOName != "" {
		fileIO = true
		inName, outName = im.FileIOName+".in", im.FileIOName+".out"
	}
	if fileIO {
		p.FileIO, p.FileIOInput, p.FileIOOutput = true, inName, outName
	}
}

// cfgSubtasks config 声明的子任务（未声明返回 nil）。
func cfgSubtasks(cfg *ProblemConfig) []yamlSubtask {
	if cfg == nil || cfg.Subtasks == nil {
		return nil
	}
	return *cfg.Subtasks
}

// validateSubtasks 子任务校验：引用的测试点序号必须存在、分值合计 100。
func validateSubtasks(ysts []yamlSubtask, caseCount int) ([]Subtask, error) {
	if len(ysts) == 0 {
		return nil, nil
	}
	if caseCount == 0 {
		return nil, fmt.Errorf("声明了 subtasks 但 data/ 里没有数据对")
	}
	exists := map[int]bool{}
	for i := 1; i <= caseCount; i++ {
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
				return nil, fmt.Errorf("子任务引用了不存在的测试点 %d（本包共 %d 个数据对）", ci, caseCount)
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

// importObjective 客观题导入：小题定义 = 表单直传（含答案）∨ problem.yaml questions
// + answer.yaml 答案 + answer.md 解析 三源合并；ValidateObjectiveSet 兜底校验。
func (im *Importer) importObjective(ctx context.Context, dir string, pid int64, meta *problemYAML, statement []byte, tagIDs []int64, rawNames []string) (*Problem, error) {
	qs := im.ObjectiveQuestions
	if qs == nil {
		answers, err := parseAnswerFile(filepath.Join(dir, "answer.yaml"))
		if err != nil {
			return nil, err
		}
		expRaw, _ := os.ReadFile(filepath.Join(dir, "answer.md"))
		explanations := splitAnswerMarkdown(string(expRaw))
		qs, err = mergeObjective(meta.Questions, answers, explanations)
		if err != nil {
			return nil, err
		}
	}
	for i := range qs {
		NormalizeObjective(&qs[i])
	}
	NormalizeScores(qs)
	if err := ValidateObjectiveSet(qs); err != nil {
		return nil, fmt.Errorf("客观题定义不合法: %w", err)
	}
	var err error
	if pid == 0 {
		if pid, err = im.Repo.nextSeq(ctx, "problem"); err != nil {
			return nil, err
		}
	}
	p := &Problem{
		ID: pid, Title: meta.Title, Statement: string(statement),
		Type: TypeObjective, Questions: qs,
		Difficulty: meta.Difficulty,
		SourceType: meta.SourceType, Source: meta.Source, Year: meta.Year, Region: meta.Region,
		// 客观题不消费时空限制，占位默认值
		TimeLimitMS: 1000, MemoryLimitKB: 262144,
		Visibility: VisibilityPublic,
		Hidden:     im.effHidden(meta),
		CreatedBy:  im.CreatorUID,
		Tags:       tagIDs,
		RawTags:    rawNames,
		Cases:      []CaseMeta{},
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func parseAnswerFile(path string) (answerYAML, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseAnswerYAML(raw)
}

// ReplaceData 编辑页整包替换数据（zip 只认 data/）：数据对/附属/题型文件/校验器全量重建，
// data/config.yaml 可覆盖时限内存/子任务/文件 IO/比对模式；题面与题目元数据（标题/难度/
// 来源/标签/隐藏）一律不动。
func (im *Importer) ReplaceData(ctx context.Context, pid int64, dir string) (*Problem, error) {
	p, err := im.Repo.GetProblem(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("读取原题目: %w", err)
	}
	if p.IsObjective() {
		return nil, fmt.Errorf("客观题没有测试数据：请在编辑页直接修改题目内容")
	}
	dataDir := filepath.Join(dir, "data")
	if fi, err := os.Stat(dataDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("数据包缺少 data/ 目录（建题/编辑页 zip 只收 data/ 数据与 config.yaml 配置）")
	}
	pairs, attachments, cfg, err := collectDataDir(dataDir, p.Type)
	if err != nil {
		return nil, err
	}
	store := testdata.NewLocalFS(im.DataDir)
	_ = store.RemoveDir(pid, "data") // 纯替换语义：旧数据/附属全清

	cases := make([]CaseMeta, 0, len(pairs))
	for i, pr := range pairs {
		inData, err := os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(pr.Base+".in")))
		if err != nil {
			return nil, fmt.Errorf("读取测试点 %s: %w", pr.Base+".in", err)
		}
		ansName := ""
		var ansData []byte
		if pr.AnsExt != "" {
			ansName = "data/" + pr.Base + pr.AnsExt
			ansData, err = os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(pr.Base+pr.AnsExt)))
			if err != nil {
				return nil, fmt.Errorf("读取答案 %s: %w", pr.Base+pr.AnsExt, err)
			}
		} else if p.Type != TypeInteractive {
			return nil, fmt.Errorf("测试点 %s 缺少答案文件（.ans/.out）", pr.Base)
		}
		if err := store.WriteFile(pid, "data/"+pr.Base+".in", inData); err != nil {
			return nil, err
		}
		if ansName != "" {
			if err := store.WriteFile(pid, ansName, ansData); err != nil {
				return nil, err
			}
		}
		cases = append(cases, CaseMeta{
			Index: i + 1, InName: "data/" + pr.Base + ".in", AnsName: ansName,
			InSize: int64(len(inData)), AnsSize: int64(len(ansData)),
			InSHA: sha256Hex(inData), AnsSHA: sha256Hex(ansData),
		})
	}

	// 子任务：config 声明 > 沿用原配置（引用序号必须仍存在）
	var subtasks []Subtask
	switch {
	case cfg != nil && cfg.Subtasks != nil:
		subtasks, err = validateSubtasks(*cfg.Subtasks, len(cases))
		if err != nil {
			return nil, err
		}
	case len(p.Subtasks) > 0 && len(cases) > 0:
		subtasks, err = validateSubtasks(subtasksOf(p.Subtasks), len(cases))
		if err != nil {
			return nil, fmt.Errorf("新数据不满足原子任务配置（请在 data/config.yaml 重新声明 subtasks）: %w", err)
		}
	}

	checkerRel := ""
	if cfg != nil && cfg.Checker != nil && *cfg.Checker != "" {
		checkerRel = *cfg.Checker
	}
	if err := im.writeTypeFiles(store, p, dir, checkerRel); err != nil {
		return nil, err
	}
	for _, rel := range attachments {
		data, err := os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("读取附属文件 %s: %w", rel, err)
		}
		if err := store.WriteFile(pid, "data/"+rel, data); err != nil {
			return nil, err
		}
	}

	// config 覆盖（时限/内存/文件 IO/比对），声明才动
	if cfg != nil {
		if cfg.TimeLimitMS != nil {
			p.TimeLimitMS = *cfg.TimeLimitMS
		}
		if cfg.MemoryLimitKB != nil {
			p.MemoryLimitKB = *cfg.MemoryLimitKB
		}
		if cfg.FileIO != nil {
			p.FileIO = *cfg.FileIO
		}
		if cfg.FileIOInput != nil {
			p.FileIOInput = *cfg.FileIOInput
		}
		if cfg.FileIOOutput != nil {
			p.FileIOOutput = *cfg.FileIOOutput
		}
		if p.CheckerFile == "" && cfg.Compare != nil && *cfg.Compare != "" {
			p.Compare = *cfg.Compare
		}
	}
	p.Cases = cases
	p.Subtasks = subtasks
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func subtasksOf(ss []Subtask) []yamlSubtask {
	out := make([]yamlSubtask, 0, len(ss))
	for _, st := range ss {
		out = append(out, yamlSubtask(st))
	}
	return out
}

// collectDataDir 收集 data/ 目录：数据对（*.in + 同名 .ans|.out，交互题答案可缺）+
// 附属文件（其余全部，含 gen.py/std.cpp；config.yaml 与数据对文件除外）+ config 解析。
func collectDataDir(dataDir, typ string) (pairs []dataPair, attachments []string, cfg *ProblemConfig, err error) {
	if fi, e := os.Stat(dataDir); e != nil || !fi.IsDir() {
		return nil, nil, nil, nil // data/ 可缺省 = 无数据题
	}
	cfg, err = parseProblemConfig(dataDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析 data/config.yaml: %w", err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, nil, nil, err
	}
	inFiles := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".in") {
			inFiles[strings.TrimSuffix(e.Name(), ".in")] = true
		}
	}
	claimed := map[string]bool{"config.yaml": true}
	pairs = make([]dataPair, 0, len(inFiles))
	for base := range inFiles {
		ext := ""
		if _, err := os.Stat(filepath.Join(dataDir, base+".ans")); err == nil {
			ext = ".ans"
		} else if _, err := os.Stat(filepath.Join(dataDir, base+".out")); err == nil {
			ext = ".out"
		} else if typ != TypeInteractive {
			return nil, nil, nil, fmt.Errorf("数据对 %s.in 缺少答案文件（%s.ans 或 %s.out）", base, base, base)
		}
		pairs = append(pairs, dataPair{Base: base, InName: base + ".in", AnsExt: ext})
		claimed[base+".in"] = true
		claimed[base+".ans"] = true // 两种答案扩展名都认领：未选中的同名兄弟不是附属文件
		claimed[base+".out"] = true
	}
	sortDataPairs(pairs)
	for _, e := range entries {
		if e.IsDir() || claimed[e.Name()] || isPackJunk(e.Name()) {
			continue
		}
		attachments = append(attachments, e.Name())
	}
	sort.Strings(attachments)
	return pairs, attachments, cfg, nil
}

// isPackJunk macOS 打包杂物。
func isPackJunk(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._") || name == "__MACOSX"
}

// effHidden 隐藏态归一：yaml 显式声明优先，未声明取 DefaultHidden（批量导入开关）。
func (im *Importer) effHidden(meta *problemYAML) bool {
	if meta.Hidden != nil {
		return *meta.Hidden
	}
	if im.Hidden {
		return true
	}
	return im.DefaultHidden
}

// hiddenPtr 导出用：仅 true 落 yaml（false 省略）。
func hiddenPtr(v bool) *bool {
	if v {
		return &v
	}
	return nil
}

// applySourceFields 来源四字段归一：表单直传 > yaml > 标题方括号识别（仅全空才识别）。
func (im *Importer) applySourceFields(title string, meta *problemYAML) error {
	if im.SourceType != "" {
		meta.SourceType = im.SourceType
	}
	if im.Source != "" {
		meta.Source = im.Source
	}
	if im.Year != 0 {
		meta.Year = im.Year
	}
	if im.Region != "" {
		meta.Region = im.Region
	}
	if meta.SourceType == "" && meta.Source == "" && meta.Year == 0 && meta.Region == "" {
		if h := ParseSourceHint(title); h.Source != "" || h.Region != "" || h.Year != 0 {
			meta.SourceType, meta.Source, meta.Year, meta.Region = h.SourceType, h.Source, h.Year, h.Region
		}
	}
	if !ValidSourceType(meta.SourceType) {
		return fmt.Errorf("source_type 需为 oj / contest / region / local / other")
	}
	if meta.Year != 0 && (meta.Year < 1900 || meta.Year > 2100) {
		return fmt.Errorf("year 需在 1900-2100 之间")
	}
	meta.Source = truncateRunes(meta.Source, 64)
	meta.Region = truncateRunes(meta.Region, 64)
	return nil
}

// truncateRunes 按 rune 截断到上限。
func truncateRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

func sha256Hex(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// tagNamesOf 节点 ID → 名字（导出渲染 problem.yaml 的 tag 字段用）。
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

// resolveTags 标签归一为树节点 ID：TagIDs 非空（网页建题）时校验存在；否则按 yaml tag
// 名匹配（未匹配的入来源库 + rawTags 保留，v0.12）。
func (im *Importer) resolveTags(ctx context.Context, names []string) ([]int64, []string, error) {
	if len(im.TagIDs) > 0 {
		if err := im.Repo.ValidateTagRefs(ctx, im.TagIDs); err != nil {
			return nil, nil, err
		}
		return im.TagIDs, nil, nil
	}
	if len(names) == 0 {
		return nil, nil, nil
	}
	tree, err := im.Repo.GetTagTree(ctx)
	if err != nil {
		return nil, nil, err
	}
	ids, unmatched := matchTagNames(TagNameLookup(tree), names)
	return ids, unmatched, nil
}

// matchTagNames 按名匹配树节点（纯函数，单测覆盖）。
func matchTagNames(lookup map[string]int64, names []string) (ids []int64, unmatched []string) {
	seenID := map[int64]bool{}
	seenName := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seenName[name] {
			continue
		}
		seenName[name] = true
		if id, ok := lookup[name]; ok {
			if !seenID[id] {
				ids = append(ids, id)
				seenID[id] = true
			}
		} else {
			unmatched = append(unmatched, name)
		}
	}
	return ids, unmatched
}

// pickNonEmpty 返回第一个非空值。
func pickNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// RenderProblemYAML 权威导出根 problem.yaml（按 db 当前值；客观题小题定义不带答案）。
func (im *Importer) RenderProblemYAML(ctx context.Context, p *Problem) []byte {
	yb, err := renderProblemYAML(p, im.tagNamesOf(ctx, p.Tags))
	if err != nil {
		return nil
	}
	return yb
}

// RenderConfigYAML 权威导出 data/config.yaml（时限/内存/文件 IO/子任务/校验器）。
func (im *Importer) RenderConfigYAML(p *Problem) []byte {
	out := configOut{
		Time:   timeLimitPtr(timeLimit(p.TimeLimitMS) / 1000),
		Memory: memLimitPtr(memLimit(p.MemoryLimitKB) / 1024),
	}
	if ms, kb := p.TimeLimitMS, p.MemoryLimitKB; ms <= 0 || kb <= 0 {
		if ms <= 0 {
			out.Time = timeLimitPtr(1)
		}
		if kb <= 0 {
			out.Memory = memLimitPtr(256)
		}
	}
	if p.Compare != "" {
		out.Compare = p.Compare
		if p.Compare == CompareTolerant && p.FloatTolerance > 0 {
			out.FloatTolerance = p.FloatTolerance
		}
	}
	if p.CheckerFile != "" {
		out.Checker = strings.TrimPrefix(p.CheckerFile, "data/")
	}
	if p.FileIO {
		inB, outB := fileIOBase(p.FileIOInput), fileIOBase(p.FileIOOutput)
		if inB == outB && inB != "" {
			out.Filename = inB
		} else {
			out.FileIOInput, out.FileIOOutput = p.FileIOInput, p.FileIOOutput
		}
	}
	for _, st := range p.Subtasks {
		out.Subtasks = append(out.Subtasks, yamlSubtask(st))
	}
	yb, err := yaml.Marshal(&out)
	if err != nil {
		return nil
	}
	return yb
}

func fileIOBase(name string) string {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[:i]
	}
	return name
}

func timeLimitPtr(v timeLimit) *timeLimit { return &v }
func memLimitPtr(v memLimit) *memLimit    { return &v }
