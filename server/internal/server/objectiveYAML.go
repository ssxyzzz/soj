package server

// 客观题小题 YAML 双向（v0.10）：导出给老师离线编辑/备份；导入只做解析与校验，
// 回填到编辑表单后仍走原有的「确认修改」保存路径，避免出现第二条写库入口。
//
// 格式（version 可省略；也兼容裸 questions 列表）：
//
//	version: 1
//	questions:
//	  - kind: single
//	    score: 40
//	    stem: |
//	      int f(int a, int b) { return a | b; }
//	    options: [...]
//	    answer: [2]
//
// 导出统一走 yaml.Marshal，`|`/`||` 由序列化器自动决定引用/块标量，不手工拼字符串。

import (
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"soj/internal/repo"
)

const maxObjectiveYAML = 256 << 10

type objectiveQuestionsDoc struct {
	Version   int                      `yaml:"version,omitempty"`
	Questions []repo.ObjectiveQuestion `yaml:"questions"`
}

// ExportObjectiveYAML 导出已保存的客观题小题（含答案/解析，仅编辑者）。
func (h *Handlers) ExportObjectiveYAML(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	p, err := h.repo.GetProblem(c.Request.Context(), id)
	if respondRepoErr(c, err, "题目不存在", "读取题目失败") {
		return
	}
	if !h.problemEditable(c, p) {
		fail(c, http.StatusForbidden, "需要编辑权限才能导出客观题 YAML")
		return
	}
	if !p.IsObjective() {
		fail(c, http.StatusBadRequest, "仅客观题支持 YAML 导出")
		return
	}
	b, err := yaml.Marshal(objectiveQuestionsDoc{Version: 1, Questions: p.Questions})
	if err != nil {
		fail(c, http.StatusInternalServerError, "生成 YAML 失败")
		return
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": fmt.Sprintf("problem - %d - %s.questions.yaml", id, downloadSafeName(p.Title)),
	}))
	c.Data(http.StatusOK, "application/x-yaml; charset=utf-8", b)
}

// ParseObjectiveYAML 解析并校验上传的小题 YAML，返回规范化后的小题定义供表单回填。
func (h *Handlers) ParseObjectiveYAML(c *gin.Context) {
	var req struct {
		YAML string `json:"yaml"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	src := strings.TrimSpace(req.YAML)
	if src == "" {
		fail(c, http.StatusBadRequest, "YAML 内容为空")
		return
	}
	if len(src) > maxObjectiveYAML {
		fail(c, http.StatusBadRequest, "YAML 超过 256KB 上限")
		return
	}
	qs, err := parseObjectiveYAML(src)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"questions": qs})
}

// parseObjectiveYAML 支持 version+questions 文档，也兼容直接列出的 questions 数组；
// 统一做 Normalize + 等分补全 + 整卷校验（分值合计必须 100）。
func parseObjectiveYAML(src string) ([]repo.ObjectiveQuestion, error) {
	var doc objectiveQuestionsDoc
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		var list []repo.ObjectiveQuestion
		if errList := yaml.Unmarshal([]byte(src), &list); errList != nil {
			return nil, fmt.Errorf("YAML 解析失败: %v", err)
		}
		doc.Questions = list
	}
	if doc.Version != 0 && doc.Version != 1 {
		return nil, fmt.Errorf("不支持的 YAML 版本 %d（当前仅支持 1）", doc.Version)
	}
	if len(doc.Questions) == 0 {
		return nil, fmt.Errorf("YAML 里没有 questions 小题")
	}
	for i := range doc.Questions {
		repo.NormalizeObjective(&doc.Questions[i])
	}
	repo.NormalizeScores(doc.Questions)
	if err := repo.ValidateObjectiveSet(doc.Questions); err != nil {
		return nil, err
	}
	return doc.Questions, nil
}
