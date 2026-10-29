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
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/safefetch"
	"soj/internal/testdata"
)

type Importer struct {
	Repo       *Repo
	DataDir    string // testdata 存储根目录
	CreatorUID int64  // 导入者 uid（0 = 系统/命令行导入，其题目隐藏态仅 root 可改）

	// ---- 表单直传（建题路径权威输入，非零值优先于包内声明）----
	TagIDs         []int64 // 知识树节点 ID
	MetaTagIDs     []int64 // 来源标签树节点 ID（2026-10-25 双树）
	Difficulty     int     // 难度 1–10
	TimeLimitMS    int     // 时限毫秒
	MemoryLimitKB  int     // 内存 KB
	FileIOName     string  // 文件 IO 基名（way → way.in/way.out）
	Compare        string  // exact / tolerant
	FloatTolerance float64
	Hidden         bool // 显式隐藏态（建题表单无此输入，恒 false；批量走 DefaultHidden）
	// ObjectiveQuestions 网页建客观题直传（含答案的完整定义，非空时优先于包内 questions）。
	ObjectiveQuestions []ObjectiveQuestion
	// Checker 表单上传校验器的 data/ 内固定名（非空 = 声明启用；文件不存在则忽略）。
	Checker string

	// IgnoreDirPID 忽略目录名数字前缀、题号一律自动分配（批量导入防顶号）。
	IgnoreDirPID bool
	// DefaultHidden yaml 未声明 hidden 时的缺省值（批量导入「导入后默认隐藏」开关）。
	DefaultHidden bool
	// TransferImages 题面外链图急切转存（v0.31）：导入时同步下载外链图落 assets 并改写
	// 站内 URL（导入页开关，默认开——零值 false 由各调用方显式置 true；网页建题路径
	// 置 false 改走保存后异步转存，不阻塞建题请求）。
	TransferImages bool
	// ImageFetcher 外链图下载器（测试注入 httptest 用；nil = 生产 safefetch.New）。
	ImageFetcher *safefetch.Fetcher
}

// DirPID 从目录名解析数字前缀题号（无前缀返回 0）；server 层「已存在题号跳过」
// 预检与 Importer 内部（seedPID 同义）共用这一语义。
func DirPID(name string) int64 { return seedPID(name) }

// seedPID 从目录名解析数字前缀题号：前导数字串以结尾、下划线或空格收尾才算
// （1000_标题 导出规范 / 1000 标题 Windows 直接压缩习惯，两种都收）；其余返回 0。
func seedPID(name string) int64 {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 || (i < len(name) && name[i] != '_' && name[i] != ' ') {
		return 0
	}
	n, err := strconv.ParseInt(name[:i], 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
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
	outcomes, err := im.SeedDeploy(ctx, seedDir)
	if err != nil {
		return err
	}
	for _, o := range outcomes {
		switch o.Status {
		case SeedImported:
			fmt.Printf("[seed] 已导入题目 #%d (%s)\n", o.PID, o.Dir)
		case SeedSkipped:
			fmt.Printf("[seed] 跳过 %s（%s）\n", o.Dir, o.Reason)
		case SeedFailed:
			fmt.Printf("[seed] 导入失败 %s: %s\n", o.Dir, o.Reason)
		}
	}
	if n := countSeedFailed(outcomes); n > 0 {
		return fmt.Errorf("%d 个种子目录导入失败", n) // 不标记，待下次启动重试
	}
	fmt.Println("[seed] 首次播种完成，已标记 seedV1（此后启动不再播种）")
	return nil
}

// 种子部署的三种逐目录结果（后台种子题管理面板与启动播种共用口径，同批量导入 batchResult）。
const (
	SeedImported = "imported"
	SeedSkipped  = "skipped"
	SeedFailed   = "failed"
)

// SeedOutcome 单个种子目录的部署结果。
type SeedOutcome struct {
	Dir    string `json:"dir"`
	Status string `json:"status"` // imported | skipped | failed
	PID    int64  `json:"pid,omitempty"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func countSeedFailed(outcomes []SeedOutcome) int {
	n := 0
	for _, o := range outcomes {
		if o.Status == SeedFailed {
			n++
		}
	}
	return n
}

// SeedDeploy 部署种子题（后台「重新部署种子题」）：与启动播种同规则逐目录导入
// （补缺不覆盖，保护线上修改），但不受 seedV1 标记拦截；全部成功后补置标记。
// 单目录失败不中断（继续导其余目录），失败明细随结果返回——err 仅在全量读目录
// 失败时非空。
func (im *Importer) SeedDeploy(ctx context.Context, seedDir string) ([]SeedOutcome, error) {
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	outcomes := make([]SeedOutcome, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(seedDir, name)
		o := SeedOutcome{Dir: name, Status: SeedImported}
		skip, why, err := im.seedSkipReason(ctx, dir, name)
		if err != nil {
			o.Status, o.Reason = SeedFailed, err.Error()
			outcomes = append(outcomes, o)
			continue
		}
		if skip {
			o.Status, o.Reason = SeedSkipped, why
			outcomes = append(outcomes, o)
			continue
		}
		pr, err := im.ImportDir(ctx, dir)
		if err != nil {
			o.Status, o.Reason = SeedFailed, err.Error()
			outcomes = append(outcomes, o)
			continue
		}
		o.PID, o.Title = pr.ID, pr.Title
		outcomes = append(outcomes, o)
	}
	if countSeedFailed(outcomes) == 0 {
		// 与启动播种同口径：部署成功即视为完成首次播种，此后启动不再自动播种
		if err := im.Repo.markSeedDone(ctx); err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}

// SeedRow 种子目录 ↔ 库内现状对账行（后台面板展示与操作前确认）。
type SeedRow struct {
	Dir         string `json:"dir"`                // 种子目录名
	Title       string `json:"title"`              // 期望标题（problem.yaml title > 目录名回退）
	Exists      bool   `json:"exists"`             // 库内已部署
	PID         int64  `json:"pid,omitempty"`      // 已部署时的库内题号（无前缀目录按标题命中，题号为导入时自动分配）
	DBTitle     string `json:"db_title,omitempty"` // 已部署时的实际标题
	Submissions int64  `json:"submissions"`        // 该题现有提交数（清除将一并删除）
}

// SeedInventory 对账种子目录与库内现状：数字前缀目录按题号查、无前缀目录按标题查
// （与 seedSkipReason 同口径），供清除/重部署前预览影响面。
func (im *Importer) SeedInventory(ctx context.Context, seedDir string) ([]SeedRow, error) {
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	rows := make([]SeedRow, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(seedDir, name)
		row := SeedRow{Dir: name, Title: dirTitleFallback(name)}
		if raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml")); err == nil {
			var meta problemYAML
			if yaml.Unmarshal(raw, &meta) == nil && meta.Title != "" {
				row.Title = meta.Title
			}
		}
		var p *Problem
		if pid := seedPID(name); pid > 0 {
			p, err = im.Repo.GetProblem(ctx, pid)
			if err != nil && !IsNotFound(err) {
				return nil, err
			}
		} else {
			var doc *Problem
			doc, err = im.problemByTitle(ctx, row.Title)
			if err != nil {
				return nil, err
			}
			p = doc
		}
		if p != nil {
			row.Exists, row.PID, row.DBTitle = true, p.ID, p.Title
			n, err := im.Repo.Submissions.CountDocuments(ctx, bson.M{"pid": p.ID})
			if err != nil {
				return nil, err
			}
			row.Submissions = n
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// problemByTitle 按标题取题目（无前缀种子目录的库内对账/清除定位）。
func (im *Importer) problemByTitle(ctx context.Context, title string) (*Problem, error) {
	var p Problem
	err := im.Repo.Problems.FindOne(ctx, bson.M{"title": title}).Decode(&p)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SeedClearStats 清除种子题的删除统计。
type SeedClearStats struct {
	Problems    int64   `json:"problems"`
	PIDs        []int64 `json:"pids"`
	Submissions int64   `json:"submissions"`
	Challenges  int64   `json:"challenges"`
	Solutions   int64   `json:"solutions"`
	Discussions int64   `json:"discussions"`
	Replies     int64   `json:"replies"`
}

// SeedClear 清除种子题：题目文档 + 数据卷 + 提交 + 挑战 + 题解 + 题目讨论（含回复/
// 点赞/举报）一并删除——提交是等级/榜单的统计源，随题删除才不产生孤儿统计。
// 题单/比赛/作业对已删题会自动剔除展示（lists 已有先例），文档本身不动，由管理员
// 自行调整；清除后置位 seedV1，防止重启自动播种「复活」刚删掉的题。
func (im *Importer) SeedClear(ctx context.Context, seedDir string) (*SeedClearStats, error) {
	rows, err := im.SeedInventory(ctx, seedDir)
	if err != nil {
		return nil, err
	}
	pids := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.Exists {
			pids = append(pids, r.PID)
		}
	}
	stats := &SeedClearStats{PIDs: pids}
	if len(pids) == 0 {
		return stats, nil
	}

	// 题目讨论级联：主题 → 回复 → 点赞/举报（点赞/举报挂 topic/reply 两类目标）。
	// nil 切片会序列化成 $in: null 报错，故按目标有无分别构造过滤器。
	var tids []int64
	{
		cur, err := im.Repo.Discussions.Find(ctx,
			bson.M{"scope": DiscussScopeProblem, "scopeId": bson.M{"$in": pids}},
			options.Find().SetProjection(bson.M{"_id": 1}))
		if err != nil {
			return nil, err
		}
		defer cur.Close(ctx)
		var docs []struct {
			ID int64 `bson:"_id"`
		}
		if err := cur.All(ctx, &docs); err != nil {
			return nil, err
		}
		for _, d := range docs {
			tids = append(tids, d.ID)
		}
	}
	var replyIDs []int64
	if len(tids) > 0 {
		cur, err := im.Repo.DiscussReplies.Find(ctx,
			bson.M{"tid": bson.M{"$in": tids}},
			options.Find().SetProjection(bson.M{"_id": 1}))
		if err != nil {
			return nil, err
		}
		defer cur.Close(ctx)
		var docs []struct {
			ID int64 `bson:"_id"`
		}
		if err := cur.All(ctx, &docs); err != nil {
			return nil, err
		}
		for _, d := range docs {
			replyIDs = append(replyIDs, d.ID)
		}
	}

	pidsIn := bson.M{"$in": pids}
	type del struct {
		col    *mongo.Collection
		filter bson.M
	}
	dels := []del{
		{im.Repo.Solutions, bson.M{"pid": pidsIn}},
		{im.Repo.Challenges, bson.M{"pid": pidsIn}},
		{im.Repo.Submissions, bson.M{"pid": pidsIn}},
		{im.Repo.Problems, bson.M{"_id": pidsIn}},
	}
	if len(tids) > 0 {
		tidsIn := bson.M{"$in": tids}
		dels = append(dels,
			del{im.Repo.DiscussReports, bson.M{"tType": "topic", "tId": tidsIn}},
			del{im.Repo.DiscussLikes, bson.M{"t": "topic", "id": tidsIn}},
			del{im.Repo.DiscussReplies, bson.M{"tid": tidsIn}},
			del{im.Repo.Discussions, bson.M{"_id": tidsIn}},
		)
	}
	if len(replyIDs) > 0 {
		replyIn := bson.M{"$in": replyIDs}
		dels = append(dels,
			del{im.Repo.DiscussReports, bson.M{"tType": "reply", "tId": replyIn}},
			del{im.Repo.DiscussLikes, bson.M{"t": "reply", "id": replyIn}},
		)
	}
	for _, d := range dels {
		n, err := d.col.DeleteMany(ctx, d.filter)
		if err != nil {
			return nil, err
		}
		switch d.col {
		case im.Repo.DiscussReplies:
			stats.Replies = n.DeletedCount
		case im.Repo.Discussions:
			stats.Discussions = n.DeletedCount
		case im.Repo.Solutions:
			stats.Solutions = n.DeletedCount
		case im.Repo.Challenges:
			stats.Challenges = n.DeletedCount
		case im.Repo.Submissions:
			stats.Submissions = n.DeletedCount
		case im.Repo.Problems:
			stats.Problems = n.DeletedCount
		}
	}

	store := testdata.NewLocalFS(im.DataDir)
	for _, pid := range pids {
		_ = store.RemoveDir(pid, ".") // "." = 题目数据卷根（RemoveDir 对 relPath 做 Clean 校验）
	}
	if err := im.Repo.markSeedDone(ctx); err != nil {
		return nil, err
	}
	return stats, nil
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

	// 来源（2026-10-25 双树）：标题方括号线索 → 来源标签树节点匹配，匹配不上直接丢弃
	metaTagIDs, err := im.resolveMetaTags(ctx, meta.Title)
	if err != nil {
		return nil, err
	}

	// 知识标签：表单节点 ID > yaml tag 名匹配（未匹配名直接丢弃，不再有 rawTags 待认领）
	tagIDs, err := im.resolveTags(ctx, meta.Tag)
	if err != nil {
		return nil, fmt.Errorf("挂载标签: %w", err)
	}

	// 客观题：无 data/；小题定义三源合并（表单直传 > problem.yaml + answer.yaml + answer.md）
	if meta.Type == TypeObjective {
		return im.importObjective(ctx, dir, pid, &meta, statement, tagIDs, metaTagIDs)
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
		if pid, err = im.Repo.nextProblemSeq(ctx); err != nil {
			return nil, err
		}
	}
	store := testdata.NewLocalFS(im.DataDir)

	// 题面终加工（v0.31 资产链）：assets/ 落库 + 相对引用改写 + 外链图转存——
	// 必须在 UpsertProblem 渲染 statementHtml 之前完成
	statement = []byte(im.prepareStatement(ctx, store, pid, dir, string(statement)))

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
		ID:            pid,
		Title:         meta.Title,
		Statement:     string(statement),
		Type:          meta.Type,
		Difficulty:    d,
		MetaTags:      metaTagIDs,
		TimeLimitMS:   msLimit,
		MemoryLimitKB: kbLimit,
		Visibility:    VisibilityPublic,
		Hidden:        im.effHidden(&meta),
		CreatedBy:     im.CreatorUID,
		Tags:          tagIDs,
		Subtasks:      subtasks,
		Cases:         cases,
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
func (im *Importer) importObjective(ctx context.Context, dir string, pid int64, meta *problemYAML, statement []byte, tagIDs []int64, metaTagIDs []int64) (*Problem, error) {
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
		if pid, err = im.Repo.nextProblemSeq(ctx); err != nil {
			return nil, err
		}
	}
	// 客观题题面同样可有图（v0.31 资产链，statementHtml 渲染前改写）
	statement = []byte(im.prepareStatement(ctx, testdata.NewLocalFS(im.DataDir), pid, dir, string(statement)))
	p := &Problem{
		ID: pid, Title: meta.Title, Statement: string(statement),
		Type: TypeObjective, Questions: qs,
		Difficulty: meta.Difficulty,
		MetaTags:   metaTagIDs,
		// 客观题不消费时空限制，占位默认值
		TimeLimitMS: 1000, MemoryLimitKB: 262144,
		Visibility: VisibilityPublic,
		Hidden:     im.effHidden(meta),
		CreatedBy:  im.CreatorUID,
		Tags:       tagIDs,
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
	_ = store.RemoveDir(pid, "data") // 纯替换语义：旧数据/附属全清（只清 data/，题面资产 assets/ 不波及——v0.31）

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

// resolveMetaTags 来源线索归一（2026-10-25 双树）：标题方括号识别（ParseSourceHint）
// → 来源标签树节点匹配；匹配不上的线索直接丢弃（rawTags 待认领机制随之退役）。
func (im *Importer) resolveMetaTags(ctx context.Context, title string) ([]int64, error) {
	return im.Repo.MatchMetaTags(ctx, ParseSourceHint(title)), nil
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

// resolveTags 标签归一为知识树节点 ID：TagIDs 非空（网页建题）时校验存在；否则按 yaml tag
// 名匹配知识树（未匹配的丢弃——v0.21 双树，来源库与 rawTags 已退役）。
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
	ids, _ := matchTagNames(TagNameLookup(tree), names)
	return ids, nil
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
