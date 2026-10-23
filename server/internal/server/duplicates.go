package server

// 重复题扫描（v0.18.1 验收追加）：按「标题 + 题面 markdown」双键找疑似重复组，
// 只列出不自动删——处置（编辑/隐藏）由管理员在题目编辑页自行完成。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// AdminScanDuplicateProblems GET /api/admin/problems/duplicates（problem.edit.all，
// root 恒过）——一键扫描全库，返回疑似重复组列表。
func (h *Handlers) AdminScanDuplicateProblems(c *gin.Context) {
	groups, err := h.repo.ScanDuplicateProblems(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "扫描失败")
		return
	}
	if groups == nil {
		groups = []repo.DuplicateProblemGroup{}
	}
	ok(c, gin.H{"groups": groups, "total": len(groups)})
}
