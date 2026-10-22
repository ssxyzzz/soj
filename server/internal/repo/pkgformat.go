package repo

// SOJ 题包格式核心（import-design.md 定稿，2026-10-22）：单题目录 =
//
//	<题号+标题>/
//	├── problem.md      题面（唯一题面来源，样例直接写在正文里）
//	├── problem.yaml    title / type / difficulty / tag / 来源四字段 /（客观题 questions）
//	├── answer.yaml     客观题纯答案（方便比对；single/judge=下标、multiple=下标列表、blank=每空可接受串）
//	├── answer.md       客观题解析（按「### 第 N 题」小节对应）
//	└── data/           数据对原名（*.in + *.ans|.out）+ config.yaml + 附属文件
//	    └── config.yaml time / memory / filename / subtasks / checker
//
// 不做旧格式兼容（statement.md / cases/ 序号名 / samples 样例标记 / extra/ 一并废弃）。

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// problemYAML 根 problem.yaml（字段顺序即导出顺序）。
type problemYAML struct {
	Title      string            `yaml:"title"`
	Type       string            `yaml:"type,omitempty"`      // default·objective·function·interactive
	Difficulty int               `yaml:"difficulty,omitempty"` // 1–10，缺省 D3
	Tag        []string          `yaml:"tag,omitempty"`        // 标签名（匹配标签树，未匹配入来源库）
	SourceType string            `yaml:"source_type,omitempty"`
	Source     string            `yaml:"source,omitempty"`
	Year       int               `yaml:"year,omitempty"`
	Region     string            `yaml:"region,omitempty"`
	Hidden     *bool             `yaml:"hidden,omitempty"` // 指针区分「未声明」（取 DefaultHidden）与显式 true/false
	Questions  []objQuestionDef  `yaml:"questions,omitempty"` // 客观题小题定义（不含答案）
}

// objQuestionDef 客观题小题公开定义（problem.yaml；答案与解析在 answer.yaml / answer.md）。
type objQuestionDef struct {
	Kind    string   `yaml:"kind"` // single·multiple·judge·blank
	Stem    string   `yaml:"stem"`
	Options []string `yaml:"options,omitempty"`
	Score   int      `yaml:"score"`
}

// blankAnswerDef answer.yaml 填空空的答案：纯列表 = 大小写不敏感；对象形态带 case_sensitive。
type blankAnswerDef struct {
	Accept        []string `yaml:"accept"`
	CaseSensitive bool     `yaml:"case_sensitive,omitempty"`
}

// answerYAML 客观题纯答案：题号 → 下标（single/judge）/ 下标列表（multiple）/ 每空答案（blank）。
type answerYAML map[int]any

// RenderAnswerMarkdown 渲染 answer.md：每题一节「### 第 N 题」+ 解析（导出用）。
func RenderAnswerMarkdown(qs []ObjectiveQuestion) []byte {
	var b strings.Builder
	for i, q := range qs {
		if strings.TrimSpace(q.Explanation) == "" {
			continue
		}
		fmt.Fprintf(&b, "### 第 %d 题\n\n%s\n\n", i+1, strings.TrimSpace(q.Explanation))
	}
	return []byte(b.String())
}

// splitAnswerMarkdown 解析 answer.md 为 题号→解析（标题行「###/## 第 N 题」宽松匹配）。
func splitAnswerMarkdown(md string) map[int]string {
	out := map[int]string{}
	cur := -1
	var buf []string
	flush := func() {
		if cur > 0 {
			out[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
	}
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "###") || strings.HasPrefix(t, "##") {
			n := parseZhQuestionNo(t)
			if n > 0 {
				flush()
				cur, buf = n, nil
				continue
			}
		}
		if cur > 0 {
			buf = append(buf, line)
		}
	}
	flush()
	return out
}

// parseZhQuestionNo 「第 N 题」标题取题号。
func parseZhQuestionNo(title string) int {
	i := strings.Index(title, "第")
	j := strings.Index(title, "题")
	if i < 0 || j <= i+1 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(title[i+len("第"):j]))
	if err != nil {
		return 0
	}
	return n
}

// RenderAnswerYAML 客观题答案渲染（题号升序；填空默认纯列表、case_sensitive 才用对象形态）。
// 用 yaml.Node 保序整文档一次 Marshal，不手拼缩进（导出用）。
func RenderAnswerYAML(qs []ObjectiveQuestion) []byte {
	root := &yaml.Node{Kind: yaml.MappingNode}
	for i := range qs {
		q := qs[i]
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(i + 1)}
		var val *yaml.Node
		switch q.Kind {
		case ObjSingle, ObjJudge:
			idx := 0
			if len(q.Answer) > 0 {
				idx = q.Answer[0]
			}
			val = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(idx)}
		case ObjMultiple:
			val = yamlSeqNode(intsToAny(q.Answer))
		case ObjBlank:
			items := make([]any, 0, len(q.Blanks))
			for _, blank := range q.Blanks {
				if blank.CaseSensitive {
					items = append(items, map[string]any{"accept": blank.Accept, "case_sensitive": true})
				} else {
					items = append(items, blank.Accept)
				}
			}
			val = yamlSeqNode(items)
		default:
			val = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
		}
		root.Content = append(root.Content, key, val)
	}
	yb, err := yaml.Marshal(root)
	if err != nil {
		return nil
	}
	return yb
}

// yamlSeqNode 任意切片转 Sequence 节点（每项 Marshal→Node，交给库排版）。
func yamlSeqNode(items []any) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, item := range items {
		raw, err := yaml.Marshal(item)
		if err != nil {
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err == nil && len(doc.Content) > 0 {
			seq.Content = append(seq.Content, doc.Content[0])
		}
	}
	return seq
}

func intsToAny(ns []int) []any {
	out := make([]any, 0, len(ns))
	for _, n := range ns {
		out = append(out, n)
	}
	return out
}

// parseAnswerYAML 解析 answer.yaml（yaml.MapSlice 保序需求不高，直接 map[int]any）。
func parseAnswerYAML(raw []byte) (answerYAML, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("answer.yaml 格式错误: %w", err)
	}
	out := answerYAML{}
	for k, v := range m {
		n, err := strconv.Atoi(strings.TrimSpace(k))
		if err != nil {
			return nil, fmt.Errorf("answer.yaml 键 %q 需为题号数字", k)
		}
		out[n] = v
	}
	return out, nil
}

// mergeObjective 把 problem.yaml 小题定义 + answer.yaml 答案 + answer.md 解析 合并成完整
// 客观题定义（题号 1..N）。答案缺省（无 answer.yaml）时按空答案入库，编辑页可补。
func mergeObjective(defs []objQuestionDef, answers answerYAML, explanations map[int]string) ([]ObjectiveQuestion, error) {
	qs := make([]ObjectiveQuestion, 0, len(defs))
	for i, d := range defs {
		q := ObjectiveQuestion{
			Kind:    d.Kind,
			Stem:    d.Stem,
			Options: d.Options,
			Score:   d.Score,
		}
		if exp := explanations[i+1]; exp != "" {
			q.Explanation = exp
		}
		switch d.Kind {
		case ObjSingle, ObjJudge:
			if q.Options == nil {
				q.Options = []string{}
			}
			if v, ok := answers[i+1]; ok {
				n, err := answerIndex(v)
				if err != nil {
					return nil, fmt.Errorf("第 %d 题答案需为选项下标数字", i+1)
				}
				q.Answer = []int{n}
			}
		case ObjMultiple:
			if q.Options == nil {
				q.Options = []string{}
			}
			if v, ok := answers[i+1]; ok {
				ns, err := answerIndexList(v)
				if err != nil {
					return nil, fmt.Errorf("第 %d 题答案需为下标列表（如 [0, 2]）", i+1)
				}
				q.Answer = ns
			}
		case ObjBlank:
			if v, ok := answers[i+1]; ok {
				blanks, err := answerBlanks(v)
				if err != nil {
					return nil, fmt.Errorf("第 %d 题答案需为每空可接受串列表: %w", i+1, err)
				}
				q.Blanks = blanks
			}
		default:
			return nil, fmt.Errorf("第 %d 题类型 %q 需为 single / multiple / judge / blank", i+1, d.Kind)
		}
		qs = append(qs, q)
	}
	return qs, nil
}

func answerIndex(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	}
	return 0, fmt.Errorf("需为数字")
}

func answerIndexList(v any) ([]int, error) {
	list, ok := v.([]any)
	if !ok {
		n, err := answerIndex(v) // 单个下标也宽容接受
		if err != nil {
			return nil, err
		}
		return []int{n}, nil
	}
	out := make([]int, 0, len(list))
	for _, item := range list {
		n, err := answerIndex(item)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// answerBlanks 填空每空答案：纯列表（大小写不敏感）或 {accept: [], case_sensitive: true}。
func answerBlanks(v any) ([]ObjectiveBlank, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("需为列表")
	}
	out := make([]ObjectiveBlank, 0, len(list))
	for _, item := range list {
		switch b := item.(type) {
		case []any: // 纯字符串列表
			accept := make([]string, 0, len(b))
			for _, s := range b {
				accept = append(accept, fmt.Sprintf("%v", s))
			}
			out = append(out, ObjectiveBlank{Accept: accept})
		case map[string]any:
			var def blankAnswerDef
			if raw, err := yaml.Marshal(b); err == nil {
				_ = yaml.Unmarshal(raw, &def)
			}
			if len(def.Accept) == 0 {
				return nil, fmt.Errorf("空答案对象需含 accept 列表")
			}
			out = append(out, ObjectiveBlank{Accept: def.Accept, CaseSensitive: def.CaseSensitive})
		default:
			// 单字符串 = 该空唯一可接受答案
			out = append(out, ObjectiveBlank{Accept: []string{fmt.Sprintf("%v", b)}})
		}
	}
	return out, nil
}

// dataPair data/ 内一组数据对（文件保持原名落卷：data/<原名>）。
type dataPair struct {
	Base   string // 去扩展名基名（排序用）
	InName string // data/test1.in
	AnsExt string // 答案扩展名（.ans/.out；交互题可无答案 = 空串）
}

// renderProblemYAML 权威导出根 problem.yaml（按库当前值；客观题小题定义不带答案）。
func renderProblemYAML(p *Problem, tagNames []string) ([]byte, error) {
	meta := problemYAML{
		Title:      p.Title,
		Type:       p.Type,
		Difficulty: p.Difficulty,
		Tag:        tagNames,
		SourceType: p.SourceType,
		Source:     p.Source,
		Year:       p.Year,
		Region:     p.Region,
		Hidden:     hiddenPtr(p.Hidden),
	}
	for _, q := range p.Questions {
		meta.Questions = append(meta.Questions, objQuestionDef{
			Kind: q.Kind, Stem: q.Stem, Options: q.Options, Score: q.Score,
		})
	}
	return yaml.Marshal(&meta)
}

// sortDataPairs 数据对排序：文件名末段数字升序（test2 < test10），无数字按字典序。
func sortDataPairs(pairs []dataPair) {
	sort.Slice(pairs, func(i, j int) bool {
		ni, iok := trailingNumber(pairs[i].Base)
		nj, jok := trailingNumber(pairs[j].Base)
		if iok && jok {
			return ni < nj
		}
		if iok != jok {
			return iok // 有数字的排前
		}
		return pairs[i].Base < pairs[j].Base
	})
}
