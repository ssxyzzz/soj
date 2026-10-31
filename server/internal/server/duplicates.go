package server

// 重复题扫描（v0.18.1 立项，v0.47 换血相似度判重）：标题+题面归一化相似度 ≥80%
// 聚类成组（repo/duplicates.go），只列出不自动删——处置（面板一键标重复/编辑/隐藏）
// 由管理员自行完成。

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
