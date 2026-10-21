package repo

// 客观题族（v0.9）：单选 single / 多选 multiple / 判断 judge / 填空 blank。
// 定义、校验、判分与对外脱敏视图集中在此；判分纯函数无沙箱，提交即出终态。
//
// 防泄答案约定：题目接口对外只回 ObjectiveView（题型/选项/空数），
// 正确答案（answer/blanks.accept）与解析（explanation）仅在满分 ∨ 教师群体时下发。

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 题目类型（Problem.Type；空 = 传统题，存量零迁移）
const (
	TypeDefault     = ""            // 传统题（比对/SPJ/宽容/子任务/文件 IO）
	TypeObjective   = "objective"   // 客观题族
	TypeFunction    = "function"    // 函数题（driver.cpp + 学生实现合编，v0.9.1）
	TypeInteractive = "interactive" // 交互题（interactor.cpp 双进程管道，v0.9.2）
)

// 客观题小类（ObjectiveQuestion.Kind）
const (
	ObjSingle   = "single"   // 单选
	ObjMultiple = "multiple" // 多选
	ObjJudge    = "judge"    // 判断（选项固定 对/错）
	ObjBlank    = "blank"    // 填空（题面 ____ 占位，一空一节）
)

// ObjectiveQuestion 客观题单小题定义（problem.yaml 的 questions[] 元素同构）。
// Stem 为小题题干（Markdown；填空用 ____ 标记空位）；Score 为该小题分值，合计须为 100。
type ObjectiveQuestion struct {
	Stem        string           `bson:"stem,omitempty"          yaml:"stem,omitempty"        json:"stem,omitempty"`
	Kind        string           `bson:"kind"                    yaml:"kind"                  json:"kind"`
	Options     []string         `bson:"options,omitempty"       yaml:"options,omitempty"     json:"options,omitempty"`
	Answer      []int            `bson:"answer,omitempty"        yaml:"answer,omitempty"      json:"answer,omitempty"`
	Blanks      []ObjectiveBlank `bson:"blanks,omitempty"        yaml:"blanks,omitempty"      json:"blanks,omitempty"`
	Explanation string           `bson:"explanation,omitempty"   yaml:"explanation,omitempty" json:"explanation,omitempty"`
	Score       int              `bson:"score,omitempty"         yaml:"score,omitempty"       json:"score,omitempty"` // 缺省 0 = 等分
}

// ObjectiveBlank 填空题一空：可接受答案列表（任一命中即对）+ 大小写敏感开关。
type ObjectiveBlank struct {
	Accept        []string `bson:"accept"                  yaml:"accept"                  json:"accept"`
	CaseSensitive bool     `bson:"caseSensitive,omitempty" yaml:"case_sensitive,omitempty" json:"case_sensitive,omitempty"`
}

// UnmarshalYAML 兼容两种大小写开关键：新 YAML 用 case_sensitive（与 API/文档一致），
// 旧题包里的 caseSensitive 仍可读入，避免存量问题包导入失败。
func (b *ObjectiveBlank) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Accept        []string `yaml:"accept"`
		CaseSensitive bool     `yaml:"case_sensitive"`
		Legacy        bool     `yaml:"caseSensitive"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	b.Accept = raw.Accept
	b.CaseSensitive = raw.CaseSensitive || raw.Legacy
	return nil
}

// IsObjective 是否客观题（判题分派、提交校验、前端渲染都以此为开关）。
func (p *Problem) IsObjective() bool {
	return p != nil && p.Type == TypeObjective && len(p.Questions) > 0
}

// IsFunction 是否函数题（v0.9.1）：driver.cpp 与学生代码合编判题，仅 C++。
func (p *Problem) IsFunction() bool { return p != nil && p.Type == TypeFunction }

// IsInteractive 是否交互题（v0.9.2）：interactor 与选手程序双进程管道判题。
func (p *Problem) IsInteractive() bool { return p != nil && p.Type == TypeInteractive }

// ValidateObjective 校验客观题定义（建题/编辑/导入共用）：
// kind 合法、选择题答案索引在范围且去重、填空每空至少一个可接受答案。
func ValidateObjective(q *ObjectiveQuestion) error {
	if q == nil {
		return fmt.Errorf("缺少客观题定义")
	}
	switch q.Kind {
	case ObjSingle, ObjJudge:
		if len(q.Answer) != 1 {
			return fmt.Errorf("单选/判断题必须有且只有一个正确答案")
		}
	case ObjMultiple:
		if len(q.Answer) == 0 {
			return fmt.Errorf("多选题至少需要一个正确答案")
		}
	case ObjBlank:
		if len(q.Blanks) == 0 {
			return fmt.Errorf("填空题至少需要一个空位")
		}
		for i, b := range q.Blanks {
			if len(b.Accept) == 0 {
				return fmt.Errorf("第 %d 空缺少可接受答案", i+1)
			}
			for _, a := range b.Accept {
				if strings.TrimSpace(a) == "" {
					return fmt.Errorf("第 %d 空的可接受答案不能为空白", i+1)
				}
			}
		}
	default:
		return fmt.Errorf("客观题类型需为 single/multiple/judge/blank")
	}
	if q.Kind == ObjSingle || q.Kind == ObjMultiple || q.Kind == ObjJudge {
		if q.Kind != ObjJudge && len(q.Options) < 2 {
			return fmt.Errorf("选择题至少需要两个选项")
		}
		if q.Kind == ObjJudge {
			// 判断题选项固定 对/错，定义里不带也行；带了必须是这两项
			if len(q.Options) != 0 && !(len(q.Options) == 2 && q.Options[0] == "对" && q.Options[1] == "错") {
				return fmt.Errorf("判断题选项固定为 对/错（可省略不填）")
			}
		}
		n := len(q.Options)
		if q.Kind == ObjJudge && n == 0 {
			n = 2 // 未显式声明选项时按默认 对/错 两项校验（Normalize 入库前补齐）
		}
		seen := map[int]bool{}
		for _, a := range q.Answer {
			if a < 0 || a >= n {
				return fmt.Errorf("答案索引 %d 超出选项范围（0-%d）", a, n-1)
			}
			if seen[a] {
				return fmt.Errorf("答案索引 %d 重复", a)
			}
			seen[a] = true
		}
	}
	return nil
}

// NormalizeObjective 归一化存储形态：判断题补默认选项、选择题答案升序去重、
// 填空可接受答案 trim。入库前调用，保证判分比较的确定性。
func NormalizeObjective(q *ObjectiveQuestion) *ObjectiveQuestion {
	if q == nil {
		return nil
	}
	if q.Kind == ObjJudge && len(q.Options) == 0 {
		q.Options = []string{"对", "错"}
	}
	if len(q.Answer) > 1 {
		sort.Ints(q.Answer)
	}
	for i := range q.Blanks {
		acc := make([]string, 0, len(q.Blanks[i].Accept))
		for _, a := range q.Blanks[i].Accept {
			if t := strings.TrimSpace(a); t != "" {
				acc = append(acc, t)
			}
		}
		q.Blanks[i].Accept = acc
	}
	return q
}

// CountBlanksInStatement 统计题面里的填空占位数（连续 4 个及以上下划线）。
// 导入/保存时校验占位数与 blanks 数一致，不一致指明第几个空位。
func CountBlanksInStatement(statement string) int {
	n, i := 0, 0
	for i < len(statement) {
		if statement[i] == '_' {
			j := i
			for j < len(statement) && statement[j] == '_' {
				j++
			}
			if j-i >= 4 {
				n++
			}
			i = j
		} else {
			i++
		}
	}
	return n
}

// ValidateObjectiveSubmission 校验一次客观题作答（提交入口共用）：
// 选择题索引在选项范围内；填空数量与空位数一致。
func ValidateObjectiveSubmission(q *ObjectiveQuestion, answer []int, blanks []string) error {
	if q == nil {
		return fmt.Errorf("题目缺少客观题定义")
	}
	switch q.Kind {
	case ObjSingle, ObjJudge:
		if len(answer) != 1 {
			return fmt.Errorf("请选择一个答案")
		}
	case ObjMultiple:
		if len(answer) == 0 {
			return fmt.Errorf("请至少选择一个答案")
		}
	case ObjBlank:
		if len(blanks) != len(q.Blanks) {
			return fmt.Errorf("共 %d 个空，请填写全部空位", len(q.Blanks))
		}
	default:
		return fmt.Errorf("客观题类型需为 single/multiple/judge/blank")
	}
	n := len(q.Options)
	for _, a := range answer {
		if a < 0 || a >= n {
			return fmt.Errorf("选项索引 %d 超出范围", a)
		}
	}
	return nil
}

// GradeObjective 判分（纯函数）：全对 100 / 否则 0（v0.9 全对制定稿）。
// 选择题比较排序去重后的索引集合；填空逐空 trim 后与 accept 精确匹配
// （大小写按该空开关），任一命中即该空正确。
func GradeObjective(q *ObjectiveQuestion, answer []int, blanks []string) (status string, score int) {
	if q == nil {
		return StatusSystemError, 0
	}
	switch q.Kind {
	case ObjSingle, ObjJudge, ObjMultiple:
		want := append([]int(nil), q.Answer...)
		got := append([]int(nil), answer...)
		sort.Ints(want)
		sort.Ints(got)
		if intSlicesEqual(want, got) {
			return StatusAccepted, 100
		}
		return StatusWrongAnswer, 0
	case ObjBlank:
		for i, b := range q.Blanks {
			if !blankMatches(b, blanks[i]) {
				return StatusWrongAnswer, 0
			}
		}
		return StatusAccepted, 100
	}
	return StatusSystemError, 0
}

func blankMatches(b ObjectiveBlank, filled string) bool {
	filled = strings.TrimSpace(filled)
	for _, a := range b.Accept {
		acc := strings.TrimSpace(a)
		if b.CaseSensitive {
			if filled == acc {
				return true
			}
		} else if strings.EqualFold(filled, acc) {
			return true
		}
	}
	return false
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ObjectiveView 题目接口对外的客观题脱敏视图：不含正确答案与解析。
type ObjectiveView struct {
	Kind       string   `json:"kind"`
	Options    []string `json:"options"`
	BlankCount int      `json:"blank_count"`
}

// PublicView 脱敏视图（GetProblem / 做题页用）。
func (q *ObjectiveQuestion) PublicView() ObjectiveView {
	if q == nil {
		return ObjectiveView{}
	}
	v := ObjectiveView{Kind: q.Kind, Options: q.Options}
	if v.Options == nil {
		v.Options = []string{}
	}
	v.BlankCount = len(q.Blanks)
	return v
}

// ObjectiveKindLabel 中文标签（前端徽章/文案兜底）。
func ObjectiveKindLabel(kind string) string {
	switch kind {
	case ObjSingle:
		return "单选题"
	case ObjMultiple:
		return "多选题"
	case ObjJudge:
		return "判断题"
	case ObjBlank:
		return "填空题"
	}
	return "客观题"
}

// ---------- 多小题集合（v0.9.3）：一道客观题 = 多道小问 ----------

// ObjectiveAnswer 一道小题的作答（选择题走 Answer、填空题走 Blanks，二者按题型取一）。
type ObjectiveAnswer struct {
	Answer []int    `bson:"answer,omitempty" json:"answer,omitempty"`
	Blanks []string `bson:"blanks,omitempty" json:"blanks,omitempty"`
}

// NormalizeScores 分值归一：全部未设分（全 0）时按题数等分，余数给最后一题；
// 部分设分（有 0 有非 0）视为配置错误，由 ValidateObjectiveSet 拦截。
func NormalizeScores(qs []ObjectiveQuestion) {
	if len(qs) == 0 {
		return
	}
	allZero := true
	for _, q := range qs {
		if q.Score != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		return
	}
	base := 100 / len(qs)
	rest := 100 - base*len(qs)
	for i := range qs {
		qs[i].Score = base
	}
	qs[len(qs)-1].Score += rest
}

// ValidateObjectiveSet 整卷校验：每道小题定义合法 + 分值全部为正且合计 100。
// 返回的错误信息带题号，便于建题页直接定位。
func ValidateObjectiveSet(qs []ObjectiveQuestion) error {
	if len(qs) == 0 {
		return fmt.Errorf("客观题至少需要一道小题")
	}
	sum := 0
	for i := range qs {
		if err := ValidateObjective(&qs[i]); err != nil {
			return fmt.Errorf("第 %d 题：%w", i+1, err)
		}
		if qs[i].Score <= 0 {
			return fmt.Errorf("第 %d 题分值必须为正数（未设分时请全部留空按等分处理）", i+1)
		}
		sum += qs[i].Score
	}
	if sum != 100 {
		return fmt.Errorf("各小题分值合计必须为 100，当前为 %d", sum)
	}
	return nil
}

// ValidateObjectiveSetSubmission 整卷作答校验：题数一致、逐题按题型校验。
func ValidateObjectiveSetSubmission(qs []ObjectiveQuestion, answers []ObjectiveAnswer) error {
	if len(answers) != len(qs) {
		return fmt.Errorf("共 %d 道小题，请全部作答", len(qs))
	}
	for i := range qs {
		if err := ValidateObjectiveSubmission(&qs[i], answers[i].Answer, answers[i].Blanks); err != nil {
			return fmt.Errorf("第 %d 题：%w", i+1, err)
		}
	}
	return nil
}

// GradeObjectiveSet 整卷判分：逐题判定，答对得该题分、答错 0 分；
// 全对 = Accepted 100，否则 Wrong Answer 并返回已得分。
// detail 逐题一行（AC/WA + 说明），供提交详情展示。
func GradeObjectiveSet(qs []ObjectiveQuestion, answers []ObjectiveAnswer) (status string, score int, detail []*CaseResult) {
	all := len(qs) > 0
	for i := range qs {
		var a []int
		var b []string
		if i < len(answers) {
			a, b = answers[i].Answer, answers[i].Blanks
		}
		st, _ := GradeObjective(&qs[i], a, b)
		res := &CaseResult{Index: i + 1, Status: "WA"}
		switch st {
		case StatusAccepted:
			score += qs[i].Score
			res.Status = "AC"
			detail = append(detail, res)
		case StatusWrongAnswer:
			all = false
			res.Msg = "答案错误"
			detail = append(detail, res)
		default:
			all = false
			res.Msg = st
			detail = append(detail, res)
		}
	}
	if all {
		return StatusAccepted, 100, detail
	}
	return StatusWrongAnswer, score, detail
}

// PublicQuestionView 小题脱敏视图（不含答案与解析）。
type PublicQuestionView struct {
	Index      int      `json:"index"`
	Stem       string   `json:"stem"`
	Kind       string   `json:"kind"`
	Options    []string `json:"options"`
	BlankCount int      `json:"blank_count"`
	Score      int      `json:"score"`
}

// PublicSetView 整卷脱敏视图。
func PublicSetView(qs []ObjectiveQuestion) []PublicQuestionView {
	out := make([]PublicQuestionView, 0, len(qs))
	for i := range qs {
		v := PublicQuestionView{
			Index: i + 1, Stem: qs[i].Stem, Kind: qs[i].Kind,
			Options: qs[i].Options, BlankCount: len(qs[i].Blanks), Score: qs[i].Score,
		}
		if v.Options == nil {
			v.Options = []string{}
		}
		out = append(out, v)
	}
	return out
}

// SetKindSummary 整卷题型摘要（列表徽章用）：全部同型返回该型，混合返回 mixed。
func SetKindSummary(qs []ObjectiveQuestion) string {
	if len(qs) == 0 {
		return ""
	}
	k := qs[0].Kind
	for _, q := range qs[1:] {
		if q.Kind != k {
			return "mixed"
		}
	}
	return k
}
