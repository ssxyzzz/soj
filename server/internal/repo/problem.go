package repo

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/testdata"
)

// 可见性（v0.2 仅 public；v0.3 班级引入后扩展）
const VisibilityPublic = "public"

// CaseMeta 测试点元数据：内容本体在 testdata 存储，这里只存清单与校验信息。
type CaseMeta struct {
	Index   int    `bson:"index"   json:"index"`
	Sample  bool   `bson:"sample"  json:"sample"`
	InName  string `bson:"inName"  json:"inName"`
	AnsName string `bson:"ansName" json:"ansName"`
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
	Statement        string              `bson:"statement"`           // 题面（Markdown，v0.8 起单一事实源；v0.6–v0.7 富文本时代的 contentHtml 已启动回迁）
	Type             string              `bson:"type,omitempty"`      // 题目类型（v0.9）：空 = 传统题；objective = 客观题族
	Questions        []ObjectiveQuestion `bson:"questions,omitempty"` // 客观题小题列表（v0.9.3；单题时代的 question 由启动迁移并入）
	Difficulty       int                 `bson:"difficulty,omitempty"` // 难度 1–10（v0.11；0 = 未设置，展示按 D3）
	TimeLimitMS      int                 `bson:"timeLimitMs"`
	MemoryLimitKB    int                 `bson:"memoryLimitKb"`
	Visibility       string              `bson:"visibility"`
	Hidden           bool                `bson:"hidden"`             // 题目隐藏：无「题库全量可见」权限的视角看不到也不可交（备题用）
	CreatedBy        int64               `bson:"createdBy"`          // 导入者 uid（0 = 系统/命令行导入，仅 root 可改其隐藏态）
	Tags             []int64             `bson:"tags"`               // 标签树节点 ID（v0.5.5），名字与层级由 tags 集合的树实时解析
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

// Subtask 子任务分组（problem.yaml 可选配置）：组内测试点全过才得该组分。
type Subtask struct {
	Cases []int `bson:"cases"`
	Score int   `bson:"score"`
}

// SampleIO 样例输入/输出文本（样例并入题面用）。
type SampleIO struct{ Input, Output string }

// samplesMdHeading 样例章节标题：迁移与导入共用，也用作幂等判断标记
// （题面已含该章节则不再追加，避免重复）。
const samplesMdHeading = "## 样例"

// AppendSamplesMd 把样例以 Markdown 章节追加到题面末尾，返回新题面。
// 题面已含「## 样例」或无样例时原样返回——手写样例的题面不会被重复追加。
func AppendSamplesMd(statement string, samples []SampleIO) string {
	if len(samples) == 0 || strings.Contains(statement, samplesMdHeading) {
		return statement
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(statement, "\n\t "))
	b.WriteString("\n\n")
	b.WriteString(samplesMdHeading)
	for i, s := range samples {
		b.WriteString(fmt.Sprintf("\n\n### 样例 %d\n\n输入：\n\n```\n%s\n```\n\n输出：\n\n```\n%s\n```",
			i+1, strings.TrimRight(s.Input, "\n"), strings.TrimRight(s.Output, "\n")))
	}
	return b.String()
}

// AppendSamplesMdForType 按题型追样例：交互题没有固定输出（判定权在 interactor），
// 只展示输入并说明交互方式，避免生成空的「输出」代码块；其余题型沿用 AppendSamplesMd。
func AppendSamplesMdForType(statement string, samples []SampleIO, typ string) string {
	if typ != TypeInteractive {
		return AppendSamplesMd(statement, samples)
	}
	if len(samples) == 0 || strings.Contains(statement, samplesMdHeading) {
		return statement
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(statement, "\n\t "))
	b.WriteString("\n\n")
	b.WriteString(samplesMdHeading)
	for i, s := range samples {
		b.WriteString(fmt.Sprintf("\n\n### 样例 %d\n\n输入：\n\n```\n%s\n```",
			i+1, strings.TrimRight(s.Input, "\n")))
	}
	b.WriteString("\n\n交互题没有固定输出：程序向标准输出写猜测/询问，评测器按题面协议实时回应。")
	return b.String()
}

// StripSamplesMd 剥离题面中的「## 样例」章节（从标题行到下一个二级标题或文末），
// 返回不含样例章节的正文。替换测试数据后样例需按新数据重建：先剥离再重新追加。
func StripSamplesMd(statement string) string {
	lines := strings.Split(statement, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, samplesMdHeading) {
			start = i
			break
		}
	}
	if start < 0 {
		return statement
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(strings.Join(lines[:start], "\n"), "\n\t "))
	if end < len(lines) {
		b.WriteString("\n\n")
		b.WriteString(strings.Join(lines[end:], "\n"))
	}
	return b.String()
}

// migrateSamplesMd 样例并入题面（2026-10-16）：存量题目的样例此前由前端单独渲染，
// 现把样例内容以 Markdown 追加进题面正文，样例随「编辑题面」一并维护。
// 逐题幂等：题面已含「## 样例」的跳过。
func (r *Repo) migrateSamplesMd(ctx context.Context, store testdata.Provider) error {
	cursor, err := r.Problems.Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var list []*Problem
	if err := cursor.All(ctx, &list); err != nil {
		return err
	}
	changed := 0
	for _, p := range list {
		if strings.Contains(p.Statement, samplesMdHeading) {
			continue
		}
		// 按 Index 升序取样例内容
		cases := append([]CaseMeta(nil), p.Cases...)
		sort.Slice(cases, func(i, j int) bool { return cases[i].Index < cases[j].Index })
		samples := make([]SampleIO, 0, len(cases))
		for _, tc := range cases {
			if !tc.Sample {
				continue
			}
			in, err := testdata.ReadAll(store, p.ID, tc.InName)
			if err != nil {
				log.Printf("[migrate][samplesMd] 题目 %d 读取样例输入失败: %v", p.ID, err)
				continue
			}
			out, err := testdata.ReadAll(store, p.ID, tc.AnsName)
			if err != nil {
				log.Printf("[migrate][samplesMd] 题目 %d 读取样例输出失败: %v", p.ID, err)
				continue
			}
			samples = append(samples, SampleIO{Input: string(in), Output: string(out)})
		}
		if len(samples) == 0 {
			continue
		}
		if _, err := r.Problems.UpdateOne(ctx,
			bson.M{"_id": p.ID, "statement": bson.M{"$ne": p.Statement}},
			bson.M{"$set": bson.M{"statement": AppendSamplesMd(p.Statement, samples)}}); err != nil {
			return err
		}
		changed++
	}
	if changed > 0 {
		log.Printf("[migrate][samplesMd] 已把样例并入 %d 道题的题面", changed)
	}
	return nil
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
