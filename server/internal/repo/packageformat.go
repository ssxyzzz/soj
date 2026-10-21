package repo

// 题包无感嗅探与归一（v0.9）：上传/导入只有一个入口，后端按特征判定来源并把
// 目录归一为 SOJ v2 布局（根 problem.yaml + cases/ + 题型专属文件），能转的全自动转：
//
//	优先级 1  根 problem.yaml 且无 testdata/   → SOJ 包（v2 单 yaml；同包根另有 config.yaml
//	                                          走旧版双层覆盖解析——老下载包/压扁 Hydro 包）
//	优先级 2  testdata/ 子目录                 → Hydro 包（flatten + 题面/交互器归一后
//	                                          落入旧版双层解析）
//	优先级 3  仅 *.in/*.ans|.out、无任何 yaml   → 裸数据兜底（传统题，标题取目录名）
//	优先级 4  DOMjudge 特征（data/testdata）    → 明确报暂不支持
//
// 检测顺序即优先级，三路互斥；结构对不上时报「包里实际看到什么 + 支持的结构」。

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// 题包来源标签（导入反馈用）
const (
	PkgSourceSOJ   = "soj"
	PkgSourceHydro = "hydro"
	PkgSourceBare  = "bare"
)

// DetectAndNormalize 嗅探题包目录并归一为 SOJ v2 布局，返回来源标签。
// ImportDir / ReplaceCases 前调用；归一幂等（已归一过的目录再跑一遍无副作用）。
func DetectAndNormalize(dir string) (string, error) {
	if fi, err := os.Stat(filepath.Join(dir, "testdata")); err == nil && fi.IsDir() {
		if err := normalizeHydroPackage(dir); err != nil {
			return "", err
		}
		return PkgSourceHydro, nil
	}
	// DOMjudge 特征先于 SOJ 判（DOMjudge 包同样有根 problem.yaml，靠 data/ 区分）
	if fi, err := os.Stat(filepath.Join(dir, "data")); err == nil && fi.IsDir() {
		if _, err := os.Stat(filepath.Join(dir, "data", "testdata")); err == nil {
			return "", fmt.Errorf("暂不支持 DOMjudge 题包（识别到 data/testdata 结构）")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "problem.yaml")); err == nil {
		return PkgSourceSOJ, nil
	}
	if hasFlatInputs(dir) {
		// 裸数据兜底：最小 problem.yaml（标题 = 目录名，时限/内存走默认值）
		base := filepath.Base(dir)
		if err := os.WriteFile(filepath.Join(dir, "problem.yaml"),
			[]byte("title: "+quoteYaml(base)+"\n"), 0o644); err != nil {
			return "", err
		}
		return PkgSourceBare, nil
	}
	return "", fmt.Errorf("无法识别的题包结构（包内：%s）。支持的结构：SOJ 题包（problem.yaml + cases/，或另带 config.yaml 的旧版双层）、Hydro 题包（testdata/ 子目录）、仅测试数据（*.in + *.ans/.out）。可先下载站内任意题的数据包作为格式模板", describeEntries(dir))
}

// hasFlatInputs 根目录是否存在 *.in 文件（裸数据判定；归一 cases/ 由导入流程完成）。
func hasFlatInputs(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".in") {
			return true
		}
	}
	return false
}

// describeEntries 列出目录顶层内容（错误信息里「包里实际看到了什么」）。
func describeEntries(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "无法读取"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name() == "__MACOSX" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			names = append(names, e.Name()+"/")
		} else {
			names = append(names, e.Name())
		}
		if len(names) >= 8 {
			break
		}
	}
	if len(names) == 0 {
		return "空目录"
	}
	return strings.Join(names, ", ")
}

// normalizeHydroPackage Hydro 布局归一（原地修改目录）：
//  1. testdata/ 内容上翻到根（同名文件根优先——web 建题预写的 problem.yaml 不被覆盖）；
//  2. 题面 statement/zh.md|en.md → 根 statement.md（根已有则不动）；
//  3. interactor/、checker/ 子目录内的源文件上翻（config 引用原名时 flatten 后同样命中）；
//  4. 题型门禁：interactive / submit_answer / Hydro 客观题在 v0.9 明确报暂不支持。
func normalizeHydroPackage(dir string) error {
	td := filepath.Join(dir, "testdata")
	entries, err := os.ReadDir(td)
	if err != nil {
		return fmt.Errorf("读取 testdata 目录失败: %w", err)
	}
	for _, e := range entries {
		src := filepath.Join(td, e.Name())
		dst := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // 根已有同名：根优先
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("整理 Hydro 包结构失败: %w", err)
		}
	}
	_ = os.RemoveAll(td)

	// 题面上翻
	if _, err := os.Stat(filepath.Join(dir, "statement.md")); os.IsNotExist(err) {
		for _, cand := range []string{
			filepath.Join(dir, "statement", "zh.md"),
			filepath.Join(dir, "statement", "en.md"),
		} {
			if _, err := os.Stat(cand); err == nil {
				_ = os.Rename(cand, filepath.Join(dir, "statement.md"))
				break
			}
		}
	}

	// 交互器/校验器子目录上翻（单文件形态）
	hoistSingle := func(sub, want string) {
		srcDir := filepath.Join(dir, sub)
		if fis, err := os.ReadDir(srcDir); err == nil && len(fis) == 1 {
			name := fis[0].Name()
			if strings.HasSuffix(name, ".cc") || strings.HasSuffix(name, ".cpp") {
				_ = os.Rename(filepath.Join(srcDir, name), filepath.Join(dir, want))
				_ = os.RemoveAll(srcDir)
			}
		}
	}
	hoistSingle("interactor", "interactor.cpp")
	hoistSingle("checker", "checker.cpp")

	// 题型门禁：根 problem.yaml（Hydro 元数据）与 config.yaml 都可能带 type
	for _, name := range []string{"problem.yaml", "config.yaml"} {
		var probe struct {
			Type string `yaml:"type"`
		}
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil && yaml.Unmarshal(raw, &probe) == nil {
			switch probe.Type {
			case "interactive":
				// v0.9.2 放行：type 透传给 ImportDir，interactor.cpp 走固定名约定
			case "submit_answer":
				return fmt.Errorf("暂不支持提交答案题（submit_answer）")
			case "objective":
				return fmt.Errorf("暂不支持 Hydro 客观题包：请在网页上直接创建客观题（单选/多选/判断/填空）")
			}
		}
	}
	return nil
}

// quoteYaml YAML 双引号字符串（与 strconv.Quote 同转义面，标题含冒号/井号也安全）。
func quoteYaml(s string) string { return strconv.Quote(s) }

// probePackageType 松探测目录内声明的题目类型（problem.yaml 与 config.yaml 取先非空者；
// 解析失败不影响主流程，返回空 = 未声明）。
func probePackageType(dir string) string {
	for _, name := range []string{"problem.yaml", "config.yaml"} {
		var probe struct {
			Type string `yaml:"type"`
		}
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil && yaml.Unmarshal(raw, &probe) == nil && probe.Type != "" {
			return probe.Type
		}
	}
	return ""
}
