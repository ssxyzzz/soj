package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"soj/internal/model"
)

func queryPage(c *gin.Context) (page, pageSize int) {
	page = atoiDefault(c.Query("page"), 1)
	pageSize = atoiDefault(c.Query("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

func (h *Handlers) ListProblems(c *gin.Context) {
	page, pageSize := queryPage(c)
	list, total, err := model.ListProblems(c.Request.Context(), page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题库失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, p := range list {
		items = append(items, gin.H{"id": p.ID, "title": p.Title})
	}
	ok(c, gin.H{"list": items, "total": total})
}

func (h *Handlers) GetProblem(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	p, err := model.GetProblem(ctx, id)
	if model.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	samples, err := model.ListSampleCases(ctx, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取样例失败")
		return
	}
	sampleItems := make([]gin.H, 0, len(samples))
	for _, tc := range samples {
		sampleItems = append(sampleItems, gin.H{"id": tc.ID, "input": tc.Input, "output": tc.Output})
	}
	ok(c, gin.H{
		"id":              p.ID,
		"title":           p.Title,
		"description":     p.Description,
		"time_limit_ms":   p.TimeLimitMS,
		"memory_limit_kb": p.MemoryLimitKB,
		"samples":         sampleItems,
	})
}
