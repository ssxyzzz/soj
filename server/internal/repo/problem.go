package repo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
	ID               int64               `bson:"_id"`
	Title            string              `bson:"title"`
	Statement        string              `bson:"statement"`             // 题面（Markdown，v0.8 起单一事实源；v0.6–v0.7 富文本时代的 contentHtml 已启动回迁）
	Type             string              `bson:"type,omitempty"`        // 题目类型（v0.9）：空 = 传统题；objective = 客观题族
	Questions        []ObjectiveQuestion `bson:"questions,omitempty"`   // 客观题小题列表（v0.9.3；单题时代的 question 由启动迁移并入）
	Difficulty       int                 `bson:"difficulty,omitempty"`  // 难度 1–10（v0.11；0 = 未设置，展示按 D3）
	SourceType       string              `bson:"source_type,omitempty"` // 来源类型（v0.12）：空 = 未归类；oj/contest/region/local/other
	Source           string              `bson:"source,omitempty"`      // 来源名（洛谷、NOIP、合肥市信息学科普日……）
	Year             int                 `bson:"year,omitempty"`        // 年份（0 = 未设置）
	Region           string              `bson:"region,omitempty"`      // 地区（建议「省·市·区」自便）
	TimeLimitMS      int                 `bson:"timeLimitMs"`
	MemoryLimitKB    int                 `bson:"memoryLimitKb"`
	Visibility       string              `bson:"visibility"`
	Hidden           bool                `bson:"hidden"`             // 题目隐藏：无「题库全量可见」权限的视角看不到也不可交（备题用）
	CreatedBy        int64               `bson:"createdBy"`          // 导入者 uid（0 = 系统/命令行导入，仅 root 可改其隐藏态）
	Tags             []int64             `bson:"tags"`               // 标签树节点 ID（v0.5.5），名字与层级由 tags 集合的树实时解析
	RawTags          []string            `bson:"rawTags,omitempty"`  // 题包导入未匹配标签树的原始名（v0.12）：保留在题目上，root 可在来源库「转为标签」归队
	Subtasks         []Subtask           `bson:"subtasks,omitempty"` // nil = 单点等分
	Cases            []CaseMeta          `bson:"cases"`
	Compare          string              `bson:"compare,omitempty"`        // 比对模式，空 = exact
	FloatTolerance   float64             `bson:"floatTolerance,omitempty"` // tolerant 时的浮点容差，0 = 默认 1e-4
	CheckerFile      string              `bson:"checkerFile,omitempty"`    // 校验器源文件（存测试数据卷，非空 = checker 判定）
	CheckerSHA       string              `bson:"checkerSha,omitempty"`
	CheckerFileID    string              `bson:"checkerFileId,omitempty"` // 沙箱编译产物缓存（失效自动重编）
	DriverFile       string              `bson:"driverFile,omitempty"`    // 函数题驱动器（卷内固定名 driver.cpp，含 main 调用学生函数）
	DriverSHA        string              `bson:"driverSha,omitempty"`
	TemplateFile     string              `bson:"templateFile,omitempty"`   // 函数题学生端模板（卷内固定名 template.cpp，提交页预填）
	InteractorFile   string              `bson:"interactorFile,omitempty"` // 交互题交互器（卷内固定名 interactor.cpp）
	InteractorSHA    string              `bson:"interactorSha,omitempty"`
	InteractorFileID string              `bson:"interactorFileId,omitempty"` // 交互器编译产物缓存（同 checker 懒编译）
	FileIO           bool                `bson:"fileIo,omitempty"`           // 文件 IO 模式（v0.5.2-3 生效）
	FileIOInput      string              `bson:"fileIoInput,omitempty"`
	FileIOOutput     string              `bson:"fileIoOutput,omitempty"`
	CreatedAt        time.Time           `bson:"createdAt"`
	UpdatedAt        time.Time           `bson:"updatedAt"`
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
	// 新建时补 createdAt 并把计数器推进到已占用的最大 pid
	if err := r.Problems.FindOne(ctx, bson.M{"_id": p.ID}).Err(); IsNotFound(err) {
		p.CreatedAt = now
		var probe struct {
			Seq int64 `bson:"seq"`
		}
		if err := r.Counters.FindOneAndUpdate(ctx,
			bson.M{"_id": "problem"},
			bson.M{"$max": bson.M{"seq": p.ID}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&probe); err != nil && !IsNotFound(err) {
			return err
		}
	} else if err != nil {
		return err
	}

	_, err := r.Problems.ReplaceOne(ctx,
		bson.M{"_id": p.ID}, p, options.Replace().SetUpsert(true))
	return err
}

// CreateProblemShell 无测试数据建壳题（v0.6.1 验收反馈：zip 可缺省直接创建题面）：
// 取号插入 problem 文档（Cases 空），测试数据后续在编辑页整包上传补齐。
func (r *Repo) CreateProblemShell(ctx context.Context, p *Problem) error {
	id, err := r.nextSeq(ctx, "problem")
	if err != nil {
		return err
	}
	p.ID = id
	p.CreatedAt = time.Now()
	_, err = r.Problems.InsertOne(ctx, p)
	return err
}

func (r *Repo) GetProblem(ctx context.Context, id int64) (*Problem, error) {
	var p Problem
	if err := r.Problems.FindOne(ctx, bson.M{"_id": id}).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProblems 题库全量列表（v0.4 规模小，隐藏/锁题过滤与分页在 handler 按视角处理后切片）。
func (r *Repo) ListProblems(ctx context.Context) ([]*Problem, error) {
	// cases 元数据需随行返回（列表「无数据」标记取 len(Cases)，v0.6.1 验收反馈）——
	// 单题几十 case 的元数据仅 KB 级，现实规模可接受；仍排除 statement 正文
	cursor, err := r.Problems.Find(ctx, bson.M{},
		options.Find().
			SetProjection(bson.M{
				"statement": 0,
			}).
			SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Problem, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// SetProblemHidden 设置主题库隐藏标记（权限由调用方校验）。
func (r *Repo) SetProblemHidden(ctx context.Context, pid int64, hidden bool) error {
	res, err := r.Problems.UpdateOne(ctx,
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
	res, err := r.Problems.UpdateOne(ctx,
		bson.M{"_id": pid},
		bson.M{"$set": bson.M{"statement": statement, "updatedAt": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// UpdateProblemMeta 网页编辑题目元数据（set 键值由调用方校验，自动带上 updatedAt）。
func (r *Repo) UpdateProblemMeta(ctx context.Context, pid int64, set bson.M) error {
	set["updatedAt"] = time.Now()
	res, err := r.Problems.UpdateOne(ctx,
		bson.M{"_id": pid}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}
