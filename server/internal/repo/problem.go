package repo

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/mdrender"
)

// 可见性（v0.2 仅 public；v0.3 班级引入后扩展）
const VisibilityPublic = "public"

// CaseMeta 测试点元数据（v0.15 换血：卷内布局 data/<原始文件名>，本结构只存清单与校验
// 信息；样例机制已废弃——样例直接写在题面 problem.md 里）。
type CaseMeta struct {
	Index   int    `bson:"index"   json:"index"`
	InName  string `bson:"inName"  json:"inName"`  // data/test1.in（原名）
	AnsName string `bson:"ansName" json:"ansName"` // data/test1.out / .ans（原名）；交互题可空
	InSize  int64  `bson:"inSize"  json:"inSize"`
	AnsSize int64  `bson:"ansSize" json:"ansSize"`
	InSHA   string `bson:"inSha"   json:"inSha"`
	AnsSHA  string `bson:"ansSha"  json:"ansSha"`
}

// 比对模式（v0.5.2-1）：exact = 现状语义（行尾空白/文末换行无关的全文比对）；
// tolerant = 在 exact 基础上按空白分词、数字 token 按容差比较。空值 = exact（存量零迁移）。
const (
	CompareExact    = "exact"
	CompareTolerant = "tolerant"

	DefaultFloatTolerance = 1e-4
)

// ValidCompare 比对模式是否合法。
func ValidCompare(c string) bool { return c == CompareExact || c == CompareTolerant }

type Problem struct {
	ID                   int64               `bson:"_id"`
	Title                string              `bson:"title"`
	Statement            string              `bson:"statement"`               // 题面（Markdown，v0.8 起单一事实源；v0.6–v0.7 富文本时代的 contentHtml 已启动回迁）
	StatementHtml        string              `bson:"statementHtml,omitempty"` // 渲染缓存（v0.30 写入时渲染；与 v0.8 contentHtml 编辑源语义无关，缺失时前端回退 md 渲染）
	Type                 string              `bson:"type,omitempty"`          // 题目类型（v0.9）：空 = 传统题；objective = 客观题族
	Questions            []ObjectiveQuestion `bson:"questions,omitempty"`     // 客观题小题列表（v0.9.3；单题时代的 question 由启动迁移并入）
	Difficulty           int                 `bson:"difficulty,omitempty"`    // 难度 1–10（v0.11；0 = 未设置，展示按 D3）
	TimeLimitMS          int                 `bson:"timeLimitMs"`
	MemoryLimitKB        int                 `bson:"memoryLimitKb"`
	Visibility           string              `bson:"visibility"`
	Hidden               bool                `bson:"hidden"`              // 题目隐藏：无「题库全量可见」权限的视角看不到也不可交（备题用）
	CreatedBy            int64               `bson:"createdBy"`           // 导入者 uid（0 = 系统/命令行导入，仅 root 可改其隐藏态）
	Tags                 []int64             `bson:"tags"`                // 知识树节点 ID（v0.5.5，2026-10-25 更名），名字与层级由 tags 集合知识树实时解析
	MetaTags             []int64             `bson:"meta_tags,omitempty"` // 来源标签树节点 ID（2026-10-25 双树：地区/赛事/题库/年份/其他），名字由来源树实时解析
	Subtasks             []Subtask           `bson:"subtasks,omitempty"`  // nil = 单点等分
	Cases                []CaseMeta          `bson:"cases"`
	Compare              string              `bson:"compare,omitempty"`        // 比对模式，空 = exact
	FloatTolerance       float64             `bson:"floatTolerance,omitempty"` // tolerant 时的浮点容差，0 = 默认 1e-4
	CheckerFile          string              `bson:"checkerFile,omitempty"`    // 校验器源文件（存测试数据卷，非空 = checker 判定）
	CheckerSHA           string              `bson:"checkerSha,omitempty"`
	CheckerFileID        string              `bson:"checkerFileId,omitempty"` // 沙箱编译产物缓存（失效自动重编）
	DriverFile           string              `bson:"driverFile,omitempty"`    // 函数题驱动器（卷内固定名 driver.cpp，含 main 调用学生函数）
	DriverSHA            string              `bson:"driverSha,omitempty"`
	TemplateFile         string              `bson:"templateFile,omitempty"`   // 函数题学生端模板（卷内固定名 template.cpp，提交页预填）
	InteractorFile       string              `bson:"interactorFile,omitempty"` // 交互题交互器（卷内固定名 interactor.cpp）
	InteractorSHA        string              `bson:"interactorSha,omitempty"`
	InteractorFileID     string              `bson:"interactorFileId,omitempty"` // 交互器编译产物缓存（同 checker 懒编译）
	FileIO               bool                `bson:"fileIo,omitempty"`           // 文件 IO 模式（v0.5.2-3 生效）
	FileIOInput          string              `bson:"fileIoInput,omitempty"`
	FileIOOutput         string              `bson:"fileIoOutput,omitempty"`
	ChallengeTimeSec     int                 `bson:"challengeTimeSec,omitempty"`     // 挑战时长秒（v0.23；0 = 按难度×5min 默认，60–14400 生效）
	VideoURL             string              `bson:"videoUrl,omitempty"`             // 答案解析视频链接（v0.23；B 站/直链 mp4，题解同门槛可见）
	DiscussCodeThreshold int                 `bson:"discussCodeThreshold,omitempty"` // 题内讨论代码门槛覆盖（v0.24；0 = 跟随全局默认，1–100 生效）
	CreatedAt            time.Time           `bson:"createdAt"`
	UpdatedAt            time.Time           `bson:"updatedAt"`
}

// ValidDifficulty 难度是否在 1–10 档内。
func ValidDifficulty(d int) bool { return d >= 1 && d <= 10 }

// EffDifficulty 生效难度：未设置/越界时按默认 D3（difficultyV1 之前的存量展示兜底）。
func (p *Problem) EffDifficulty() int {
	if ValidDifficulty(p.Difficulty) {
		return p.Difficulty
	}
	return 3
}

// FileIONames 本题文件 IO 的文件名对：老师未显式配置时默认 problem.in / problem.out，
// 保证任意题目都可以被「本场启用文件 IO」直接使用（无配置门槛）。
func (p *Problem) FileIONames() (inName, outName string) {
	inName, outName = p.FileIOInput, p.FileIOOutput
	if inName == "" {
		inName = "problem.in"
	}
	if outName == "" {
		outName = "problem.out"
	}
	return inName, outName
}

// Subtask 子任务分组（data/config.yaml 可选配置）：组内测试点全过才得该组分。
type Subtask struct {
	Cases []int `bson:"cases"`
	Score int   `bson:"score"`
}

// UpsertProblem 按 ID 整体覆盖题目（导入工具调用）。
func (r *Repo) UpsertProblem(ctx context.Context, p *Problem) error {
	now := time.Now()
	p.UpdatedAt = now
	p.StatementHtml = mdrender.Render(p.Statement)
	// 新建时补 createdAt 并把计数器推进到已占用的最大 pid
	if err := r.problems.FindOne(ctx, bson.M{"_id": p.ID}).Err(); IsNotFound(err) {
		p.CreatedAt = now
		var probe struct {
			Seq int64 `bson:"seq"`
		}
		if err := r.counters.FindOneAndUpdate(ctx,
			bson.M{"_id": "problem"},
			bson.M{"$max": bson.M{"seq": p.ID}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&probe); err != nil && !IsNotFound(err) {
			return err
		}
	} else if err != nil {
		return err
	}

	_, err := r.problems.ReplaceOne(ctx,
		bson.M{"_id": p.ID}, p, options.Replace().SetUpsert(true))
	return err
}

// CreateProblemShell 无测试数据建壳题（v0.6.1 验收反馈：zip 可缺省直接创建题面）：
// 取号插入 problem 文档（Cases 空），测试数据后续在编辑页整包上传补齐。
func (r *Repo) CreateProblemShell(ctx context.Context, p *Problem) error {
	id, err := r.nextProblemSeq(ctx)
	if err != nil {
		return err
	}
	p.ID = id
	p.CreatedAt = time.Now()
	p.StatementHtml = mdrender.Render(p.Statement)
	_, err = r.problems.InsertOne(ctx, p)
	return err
}

// DefaultProblemSeqStart 自动题号起点默认值（后台「自动题号起点」可调，见 SystemSettings.ProblemSeqStart）。
// 1~1000 惯例保留给导入题包：无前缀自动取号从 1001 起。
const DefaultProblemSeqStart int64 = 1001

// nextProblemSeq 自动分配题号：计数器低于后台设定的「自动题号起点」时垫到 start-1 重取
// ——管理员运行期调大起点即时生效（首个自动题号 = 起点）；计数器已超起点（或起点调小）
// 时从计数器继续，只前进不回退。导入/建题指定题号走 UpsertProblem，不经过此处。
func (r *Repo) nextProblemSeq(ctx context.Context) (int64, error) {
	start := DefaultProblemSeqStart
	if s, err := r.GetSystemSettings(ctx); err == nil {
		start = s.ProblemSeqStartVal()
	}
	pid, err := r.nextSeq(ctx, "problem")
	if err != nil {
		return 0, err
	}
	if pid < start {
		var doc struct {
			Seq int64 `bson:"seq"`
		}
		if merr := r.counters.FindOneAndUpdate(ctx,
			bson.M{"_id": "problem"},
			bson.M{"$max": bson.M{"seq": start - 1}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&doc); merr == nil {
			return r.nextSeq(ctx, "problem")
		}
	}
	return pid, nil
}

// SeedProblemCounter 自动题号起点守卫（Bootstrap 幂等快路径）：problem 计数器 $max 到起点-1，
// 让首题免走 nextProblemSeq 的垫高重取；运行期改动由 nextProblemSeq 兜底，两者口径一致。
// 与 syncProblemCounter 的空集跳过不同：题集合为空也写入，保证全新库首题 = 起点。
func (r *Repo) SeedProblemCounter(ctx context.Context) error {
	start := DefaultProblemSeqStart
	if s, err := r.GetSystemSettings(ctx); err == nil {
		start = s.ProblemSeqStartVal()
	}
	var before struct {
		Seq int64 `bson:"seq"`
	}
	err := r.counters.FindOneAndUpdate(ctx,
		bson.M{"_id": "problem"},
		bson.M{"$max": bson.M{"seq": start - 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before),
	).Decode(&before)
	if IsNotFound(err) {
		return nil // 全新计数器，本次 $max 已写入起点-1
	}
	if err == nil && before.Seq < start-1 {
		slog.Info("problem 计数器已垫高（自动题号起点）", "component", "boot", "from", before.Seq, "to", start-1, "start", start)
	}
	return err
}

func (r *Repo) GetProblem(ctx context.Context, id int64) (*Problem, error) {
	var p Problem
	if err := r.problems.FindOne(ctx, bson.M{"_id": id}).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// metaProjection 题面两大字段（statement/statementHtml，各上限 256KB）排除——
// 详情视图以外的消费面（场次题目表 / 判题）用瘦读，v0.30 双存后全量读各多带一份。
var metaProjection = bson.M{"statement": 0, "statementHtml": 0}

// GetProblemMeta 单题瘦读（排除题面两大字段）：判题 evaluate 等只要判题元数据的场景。
func (r *Repo) GetProblemMeta(ctx context.Context, id int64) (*Problem, error) {
	var p Problem
	if err := r.problems.FindOne(ctx, bson.M{"_id": id},
		options.FindOne().SetProjection(metaProjection)).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProblemMetas 批量瘦读（后端快赢批）：场次详情的题目表此前每题一次全量
// GetProblem（N+1，组织者编辑清单再来一轮）——30 题比赛 = 30~60 次全文档读。
// 缺席（已删）不进 map，调用方按 miss 跳过，与原逐题 err 跳过行为一致。
func (r *Repo) GetProblemMetas(ctx context.Context, ids []int64) (map[int64]*Problem, error) {
	out := make(map[int64]*Problem, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cursor, err := r.problems.Find(ctx, bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(metaProjection))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Problem, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	for _, p := range list {
		out[p.ID] = p
	}
	return out, nil
}

// ProblemListFilter 题库列表下推查询（v0.26）：搜索过滤 + 可见性反向排除，
// 过滤与分页都在 DB 侧完成——原「全量 cursor.All 后内存过滤切片」在万题规模
// 每请求传输几十 MB；下推后每请求只回当前页（≤100 条）。
type ProblemListFilter struct {
	ExcludeIDs []int64 // 反向排除（隐藏题 ∪ 非运行期锁题），由 handler 按视角算好（特权视角不排除）
	Q          string  // 标题子串（不分大小写）∨ 题号精确匹配
	// 搜索命中的知识点/来源标签（v0.26.1：搜索词命中树节点名 → 子树展开），与 Q 并集；
	// 与下方 Tags/MetaTags 显式筛选（AND）正交——搜索从宽、筛选从严
	SearchTags     []int64
	SearchMetaTags []int64
	Tags           []int64 // 知识标签并集
	MetaTags       []int64 // 来源标签并集（子树展开后）
	Difficulty     []int   // 生效难度并集（EffDifficulty 语义：未设置/越界按 D3）
}

// ListProblemsPage 题库分页列表：DB 侧过滤 + _id 升序 + skip/limit，返回当前页与总数。
func (r *Repo) ListProblemsPage(ctx context.Context, f ProblemListFilter, page, pageSize int) ([]*Problem, int64, error) {
	query := bson.M{}
	if len(f.ExcludeIDs) > 0 {
		query["_id"] = bson.M{"$nin": f.ExcludeIDs}
	}
	// 搜索条件组（v0.26.1）：标题/题号/知识点/来源标签命中取并集，与其余过滤条件 AND
	var searchOr bson.A
	if f.Q != "" {
		// 题号口径与原实现一致：仅「纯数字且与格式化结果完全一致」按 ID 精确匹配（"0100" 不算 #100）
		if id, err := strconv.ParseInt(f.Q, 10, 64); err == nil && id > 0 && strconv.FormatInt(id, 10) == f.Q {
			searchOr = append(searchOr, bson.M{"_id": id})
		}
		searchOr = append(searchOr, bson.M{"title": bson.M{"$regex": regexp.QuoteMeta(f.Q), "$options": "i"}})
	}
	if len(f.SearchTags) > 0 {
		searchOr = append(searchOr, bson.M{"tags": bson.M{"$in": f.SearchTags}})
	}
	if len(f.SearchMetaTags) > 0 {
		searchOr = append(searchOr, bson.M{"meta_tags": bson.M{"$in": f.SearchMetaTags}})
	}
	var andClauses bson.A
	switch {
	case len(searchOr) == 1:
		andClauses = append(andClauses, searchOr[0])
	case len(searchOr) > 1:
		andClauses = append(andClauses, bson.M{"$or": searchOr})
	}
	if len(f.Difficulty) > 0 {
		// EffDifficulty 语义：difficulty 未设置/越界（非 1..10）按 D3——筛 D3 时须并上「无效值」文档；
		// $nin 对缺失字段也命中，正好覆盖「未设置」
		or := bson.A{bson.M{"difficulty": bson.M{"$in": f.Difficulty}}}
		if intIn(f.Difficulty, 3) {
			valid := bson.A{}
			for d := 1; d <= 10; d++ {
				valid = append(valid, d)
			}
			or = append(or, bson.M{"difficulty": bson.M{"$nin": valid}})
		}
		andClauses = append(andClauses, bson.M{"$or": or})
	}
	if len(andClauses) > 0 {
		query["$and"] = andClauses
	}
	if len(f.Tags) > 0 {
		query["tags"] = bson.M{"$in": f.Tags}
	}
	if len(f.MetaTags) > 0 {
		query["meta_tags"] = bson.M{"$in": f.MetaTags}
	}
	total, err := r.problems.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	// cases 元数据需随行返回（列表「无数据」标记取 len(Cases)），仍排除 statement 正文
	cursor, err := r.problems.Find(ctx, query,
		options.Find().
			// statementHtml 与 statement 同量级（各上限 256KB），v0.30 双存后列表
			// 每页 100 行整篇回传 Go 层再丢弃——一并排除（快赢批）
			SetProjection(bson.M{"statement": 0, "statementHtml": 0}).
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetSkip(int64((page-1)*pageSize)).
			SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*Problem, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// HiddenProblemIDs 全部隐藏题 ID（小集合：手动标记的备题/下架题），题库列表反向排除用。
func (r *Repo) HiddenProblemIDs(ctx context.Context) ([]int64, error) {
	cursor, err := r.problems.Find(ctx, bson.M{"hidden": true},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		ID int64 `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// intIn 定长小切片包含判断（避免为两处使用引入 slices 依赖口径分歧）。
func intIn(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// SetProblemHidden 设置主题库隐藏标记（权限由调用方校验）。
func (r *Repo) SetProblemHidden(ctx context.Context, pid int64, hidden bool) error {
	res, err := r.problems.UpdateOne(ctx,
		bson.M{"_id": pid}, bson.M{"$set": bson.M{"hidden": hidden}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SetProblemStatement 更新题面（Markdown，权限由调用方校验）。
func (r *Repo) SetProblemStatement(ctx context.Context, pid int64, statement string) error {
	res, err := r.problems.UpdateOne(ctx,
		bson.M{"_id": pid},
		bson.M{"$set": bson.M{"statement": statement, "statementHtml": mdrender.Render(statement), "updatedAt": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// UpdateProblemMeta 网页编辑题目元数据（set 键值由调用方校验，自动带上 updatedAt）。
// ProblemMetaPatch 题目元数据补丁（nil 不动；v0.32 typed 化——字段名拼错从「静默写未知
// 字段」变编译错）。覆盖元数据编辑表单（标题/时限/内存/难度/双树标签/比对/文件 IO/
// 视频/挑战时长/讨论门槛/客观题整卷）与文件面（checker/driver/template/interactor
// 四类附属文件的卷内名、sha、编译缓存 fileId——上传/移除/缓存失效三态全走这里）。
type ProblemMetaPatch struct {
	Title               *string
	TimeLimitMS         *int
	MemoryLimitKB       *int
	Compare             *string
	FloatTolerance      *float64
	FileIOInput         *string
	FileIOOutput        *string
	Difficulty          *int
	Tags                *[]int64
	MetaTags            *[]int64 // bson: meta_tags
	VideoURL            *string  // bson: videoUrl
	ChallengeTimeSec    *int
	DiscussCodeThreshold *int
	Questions           *[]ObjectiveQuestion
	CheckerFile         *string
	CheckerSHA          *string // bson: checkerSha
	CheckerFileID       *string // bson: checkerFileId（编译产物缓存；空串 = 失效重编）
	DriverFile          *string
	DriverSHA           *string
	TemplateFile        *string
	InteractorFile      *string
	InteractorSHA       *string
	InteractorFileID    *string
}

func (p ProblemMetaPatch) Empty() bool {
	return p.Title == nil && p.TimeLimitMS == nil && p.MemoryLimitKB == nil &&
		p.Compare == nil && p.FloatTolerance == nil && p.FileIOInput == nil &&
		p.FileIOOutput == nil && p.Difficulty == nil && p.Tags == nil && p.MetaTags == nil &&
		p.VideoURL == nil && p.ChallengeTimeSec == nil && p.DiscussCodeThreshold == nil &&
		p.Questions == nil && p.CheckerFile == nil && p.CheckerSHA == nil &&
		p.CheckerFileID == nil && p.DriverFile == nil && p.DriverSHA == nil &&
		p.TemplateFile == nil && p.InteractorFile == nil && p.InteractorSHA == nil &&
		p.InteractorFileID == nil
}

func (r *Repo) UpdateProblemMeta(ctx context.Context, pid int64, p ProblemMetaPatch) error {
	set := bson.M{
		"updatedAt": time.Now(),
	}
	setIf := func(ok bool, key string, v any) {
		if ok {
			set[key] = v
		}
	}
	setIf(p.Title != nil, "title", p.Title)
	setIf(p.TimeLimitMS != nil, "timeLimitMs", p.TimeLimitMS)
	setIf(p.MemoryLimitKB != nil, "memoryLimitKb", p.MemoryLimitKB)
	setIf(p.Compare != nil, "compare", p.Compare)
	setIf(p.FloatTolerance != nil, "floatTolerance", p.FloatTolerance)
	setIf(p.FileIOInput != nil, "fileIoInput", p.FileIOInput)
	setIf(p.FileIOOutput != nil, "fileIoOutput", p.FileIOOutput)
	setIf(p.Difficulty != nil, "difficulty", p.Difficulty)
	setIf(p.Tags != nil, "tags", p.Tags)
	setIf(p.MetaTags != nil, "meta_tags", p.MetaTags)
	setIf(p.VideoURL != nil, "videoUrl", p.VideoURL)
	setIf(p.ChallengeTimeSec != nil, "challengeTimeSec", p.ChallengeTimeSec)
	setIf(p.DiscussCodeThreshold != nil, "discussCodeThreshold", p.DiscussCodeThreshold)
	setIf(p.Questions != nil, "questions", p.Questions)
	setIf(p.CheckerFile != nil, "checkerFile", p.CheckerFile)
	setIf(p.CheckerSHA != nil, "checkerSha", p.CheckerSHA)
	setIf(p.CheckerFileID != nil, "checkerFileId", p.CheckerFileID)
	setIf(p.DriverFile != nil, "driverFile", p.DriverFile)
	setIf(p.DriverSHA != nil, "driverSha", p.DriverSHA)
	setIf(p.TemplateFile != nil, "templateFile", p.TemplateFile)
	setIf(p.InteractorFile != nil, "interactorFile", p.InteractorFile)
	setIf(p.InteractorSHA != nil, "interactorSha", p.InteractorSHA)
	setIf(p.InteractorFileID != nil, "interactorFileId", p.InteractorFileID)
	if len(set) == 1 { // 仅 updatedAt
		return ErrEmptyPatch
	}
	res, err := r.problems.UpdateOne(ctx,
		bson.M{"_id": pid}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}
