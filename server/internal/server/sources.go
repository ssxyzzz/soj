package server

// 来源库 API（v0.12）：
//   - GET  /sources/meta          公开：来源名建议（表单 datalist）+ 年份实有值（题库筛选）
//   - GET  /admin/sources         root 面板：全部条目 + 实时引用数
//   - POST /admin/sources         root 手工新增
//   - PATCH /admin/sources/:id    root 改名（跟随题目）/ 改型
//   - DELETE /admin/sources/:id   root 删除（仅删登记，题目字段不动）
//   - POST /admin/sources/:id/to-tag  转为标签：指定已有节点或新建同名节点，
//     引用题目批量改挂该节点、rawTags 移除该名，条目出库

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// SourceMeta 公开来源元数据（题库高级筛选的年份选项 + 建题表单 datalist 共用）。
func (h *Handlers) SourceMeta(c *gin.Context) {
	names, years, err := h.repo.SourceMeta(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取来源信息失败")
		return
	}
	if names == nil {
		names = []string{}
	}
	if years == nil {
		years = []int{}
	}
	ok(c, gin.H{"names": names, "years": years})
}

// AdminListSources 来源面板列表（类型/引用数；引用数按题目侧实时聚合）。
func (h *Handlers) AdminListSources(c *gin.Context) {
	ctx := c.Request.Context()
	entries, err := h.repo.ListSources(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取来源库失败")
		return
	}
	list := make([]gin.H, 0, len(entries))
	for _, e := range entries {
		refs, err := h.repo.SourceRefCount(ctx, e.Name)
		if err != nil {
			fail(c, http.StatusInternalServerError, "统计引用失败")
			return
		}
		list = append(list, gin.H{"id": e.ID, "name": e.Name, "type": e.Type, "refs": refs})
	}
	ok(c, gin.H{"list": list})
}

// AdminCreateSource root 手工新增来源（名称唯一）。
func (h *Handlers) AdminCreateSource(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
		Type string `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	id, err := h.repo.CreateSource(c.Request.Context(), strings.TrimSpace(req.Name), strings.TrimSpace(req.Type))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// AdminUpdateSource root 改名/改型；改名跟随题目（source 字段与 rawTags 数组同步替换）。
func (h *Handlers) AdminUpdateSource(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Name *string `json:"name"`
		Type *string `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Name == nil && req.Type == nil) {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.repo.UpdateSource(c.Request.Context(), id, req.Name, req.Type); err != nil {
		if repo.IsNotFound(err) {
			fail(c, http.StatusNotFound, "来源不存在")
			return
		}
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"updated": id})
}

// AdminDeleteSource root 删除来源条目。
func (h *Handlers) AdminDeleteSource(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	if err := h.repo.DeleteSource(c.Request.Context(), id); err != nil {
		if repo.IsNotFound(err) {
			fail(c, http.StatusNotFound, "来源不存在")
			return
		}
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": id})
}

// AdminSourceToTag 来源转标签：node_id 指定已有节点，或省略时按 name（缺省取条目名）
// 在 parent_id（0 = 根级）下新建同名节点；随后引用题目批量补挂该节点、rawTags 移除
// 该名、条目出库。返回新节点 ID 与受影响题目数。
func (h *Handlers) AdminSourceToTag(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		NodeID   int64  `json:"node_id"`
		ParentID int64  `json:"parent_id"`
		Name     string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ctx := c.Request.Context()
	entry, err := h.repo.GetSource(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "来源不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取来源失败")
		return
	}

	nodeID := req.NodeID
	if nodeID != 0 {
		// 指定已有节点：校验存在于树
		tree, err := h.repo.GetTagTree(ctx)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取标签树失败")
			return
		}
		exists := map[int64]bool{}
		repo.CollectTagIDs(tree, exists)
		if !exists[nodeID] {
			fail(c, http.StatusBadRequest, "指定节点不存在于标签树")
			return
		}
	} else {
		// 新建同名节点（name 可覆盖条目名，如归队时顺带修正大小写）
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = entry.Name
		}
		nodeID, err = h.repo.AppendTagNode(ctx, req.ParentID, name)
		if err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	updated, err := h.repo.AttachSourceTag(ctx, entry.Name, nodeID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "改挂标签失败："+err.Error())
		return
	}
	if err := h.repo.DeleteSource(ctx, id); err != nil {
		fail(c, http.StatusInternalServerError, "来源已转标签，但条目删除失败")
		return
	}
	ok(c, gin.H{"node_id": nodeID, "updated": updated})
}
