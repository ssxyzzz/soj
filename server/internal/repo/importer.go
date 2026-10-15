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
		pid, err := im.ImportDir(ctx, filepath.Join(seedDir, name))
		if err != nil {
			return fmt.Errorf("导入 %s: %w", name, err)
		}
		fmt.Printf("[seed] 已导入题目 #%d (%s)\n", pid, name)
	}
	return nil
}

// ImportDir 导入单个题目目录，返回题目 ID。
func (im *Importer) ImportDir(ctx context.Context, dir string) (int64, error) {
	base := filepath.Base(dir)
	pid := int64(0)
	if n, err := strconv.ParseInt(strings.SplitN(base, "_", 2)[0], 10, 64); err == nil && n > 0 {
		pid = n
	}

	raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml"))
	if err != nil {
		return 0, fmt.Errorf("读取 problem.yaml: %w", err)
	}
	var meta problemYAML
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return 0, fmt.Errorf("解析 problem.yaml: %w", err)
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
	statement, err := os.ReadFile(filepath.Join(dir, "statement.md"))
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}

	// 收集测试点
	casesDir := filepath.Join(dir, "cases")
	caseEntries, err := os.ReadDir(casesDir)
	if err != nil {
		return 0, fmt.Errorf("读取 cases 目录: %w", err)
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
		return 0, fmt.Errorf("cases 目录下没有测试点")
	}

	// 题目 ID 未指定则自动分配
	if pid == 0 {
		pid, err = im.Repo.nextSeq(ctx, "problem")
		if err != nil {
			return 0, err
		}
	}

	store := testdata.NewLocalFS(im.DataDir)
	sampleSet := map[int]bool{}
	for _, i := range meta.Samples {
		sampleSet[i] = true
	}

	cases := make([]CaseMeta, 0, len(indexes))
	for _, i := range indexes {
		inData, err := os.ReadFile(filepath.Join(casesDir, strconv.Itoa(i), "in"))
		if err != nil {
			return 0, fmt.Errorf("读取测试点 %d 输入: %w", i, err)
		}
		ansData, err := os.ReadFile(filepath.Join(casesDir, strconv.Itoa(i), "ans"))
		if err != nil {
			return 0, fmt.Errorf("读取测试点 %d 答案: %w", i, err)
		}
		inName, ansName := fmt.Sprintf("cases/%d/in", i), fmt.Sprintf("cases/%d/ans", i)
		if err := store.WriteFile(pid, inName, inData); err != nil {
			return 0, err
		}
		if err := store.WriteFile(pid, ansName, ansData); err != nil {
			return 0, err
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
				return 0, fmt.Errorf("子任务不能为空")
			}
			for _, ci := range st.Cases {
				if !exists[ci] {
					return 0, fmt.Errorf("子任务引用了不存在的测试点 %d", ci)
				}
			}
			if st.Score <= 0 {
				return 0, fmt.Errorf("子任务分值必须为正数")
			}
			sum += st.Score
			subtasks = append(subtasks, Subtask{Cases: st.Cases, Score: st.Score})
		}
		if sum != 100 {
			return 0, fmt.Errorf("子任务分值之和必须为 100，当前为 %d", sum)
		}
	}

	// problem.yaml 与题面一并归档到存储目录，保证数据目录自包含
	_ = store.WriteFile(pid, "problem.yaml", raw)
	if len(statement) > 0 {
		_ = store.WriteFile(pid, "statement.md", statement)
	}

	p := &Problem{
		ID:            pid,
		Title:         meta.Title,
		Statement:     string(statement),
		TimeLimitMS:   meta.TimeLimitMS,
		MemoryLimitKB: meta.MemoryLimitKB,
		Visibility:    meta.Visibility,
		Hidden:        meta.Hidden,
		CreatedBy:     im.CreatorUID,
		Tags:          meta.Tags,
		Subtasks:      subtasks,
		Cases:         cases,
	}
	if err := im.Repo.UpsertProblem(ctx, p); err != nil {
		return 0, err
	}
	return pid, nil
}

func sha256Hex(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
