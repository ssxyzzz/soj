package repo

// Hydro 适配器（import-design.md §2.3，独立于 SOJ 格式）：把 HydroOJ「题库导出」的
// 单题目录原地归一为 SOJ 格式，随后走 ImportDir。实测结构（data/test 样包核对）：
//
//	<数字题目ID>/
//	  problem.yaml     title / tag 列表 / owner…（只取 title 与 tag）
//	  problem_zh.md    题面（识别优先级 problem_zh.md > problem.md > problem_en.md）
//	  testdata/        config.yaml（type/time/memory/filename/subtasks）+ 数据对 + gen.py 等附属
//
// 目录名数字不作为题号；submit_answer / objective 类型明确拒绝。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// hydroProblemYAML Hydro 导出 problem.yaml 的可读字段（其余忽略）。
type hydroProblemYAML struct {
	Title string   `yaml:"title"`
	Tag   []string `yaml:"tag"`
	Type  string   `yaml:"type"`
}

// NormalizeHydroDir Hydro 单题目录归一为 SOJ 格式（原地）：
//  1. 题面 problem_zh.md > problem.md > problem_en.md → problem.md；
//  2. Hydro problem.yaml（title/tag）→ SOJ problem.yaml（tag 字段同名沿用）；
//  3. testdata/* 上翻为 data/*（原名，含 config.yaml 与附属文件）。
func NormalizeHydroDir(dir string) error {
	if fi, err := os.Stat(filepath.Join(dir, "testdata")); err != nil || !fi.IsDir() {
		return fmt.Errorf("不是 Hydro 题包（缺 testdata/ 目录；Hydro 导出包每题应有 testdata/）")
	}

	// 题面三命名优先级
	statement := ""
	for _, name := range []string{"problem_zh.md", "problem.md", "problem_en.md"} {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			statement = string(raw)
			break
		}
	}
	if statement == "" {
		return fmt.Errorf("Hydro 题包缺题面（problem_zh.md / problem.md / problem_en.md 均不存在）")
	}

	var hyd hydroProblemYAML
	if raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml")); err == nil {
		if err := yaml.Unmarshal(raw, &hyd); err != nil {
			return fmt.Errorf("解析 Hydro problem.yaml: %w", err)
		}
	}
	switch strings.TrimSpace(hyd.Type) {
	case "interactive":
		// 放行：interactor.cpp 走 data/ 固定名
	case "submit_answer":
		return fmt.Errorf("暂不支持提交答案题（submit_answer）")
	case "objective":
		return fmt.Errorf("暂不支持 Hydro 客观题包：请在网页上直接创建客观题")
	}

	// testdata/ 内容上翻为 data/（同名冲突 data/ 优先——归一幂等重跑安全）
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		return err
	}
	td := filepath.Join(dir, "testdata")
	entries, err := os.ReadDir(td)
	if err != nil {
		return err
	}
	for _, e := range entries {
		dst := filepath.Join(dir, "data", e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(td, e.Name()), dst); err != nil {
			return fmt.Errorf("整理 Hydro testdata 失败: %w", err)
		}
	}
	_ = os.RemoveAll(td)

	// Hydro type 落到 SOJ problem.yaml；交互题的 interactor 由 testdata/interactor/ 子目录
	// 单文件形态上翻为 data/interactor.cpp（Hydro 惯例）
	hoistHydroSingle := func(sub, want string) {
		srcDir := filepath.Join(dir, "data", sub)
		if fis, err := os.ReadDir(srcDir); err == nil && len(fis) == 1 {
			name := fis[0].Name()
			if strings.HasSuffix(name, ".cc") || strings.HasSuffix(name, ".cpp") {
				_ = os.Rename(filepath.Join(srcDir, name), filepath.Join(dir, "data", want))
				_ = os.RemoveAll(srcDir)
			}
		}
	}
	hoistHydroSingle("interactor", "interactor.cpp")
	hoistHydroSingle("checker", "checker.cpp")

	meta := problemYAML{Title: strings.TrimSpace(hyd.Title), Tag: hyd.Tag}
	if t := strings.TrimSpace(hyd.Type); t == "interactive" {
		meta.Type = TypeInteractive
	}
	yb, err := yaml.Marshal(&meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "problem.yaml"), yb, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "problem.md"), []byte(statement), 0o644)
}
