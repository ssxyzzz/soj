package server

// 标签树（v0.5.5）：读公开（题库筛选/详情卡/建题表单共用，AuthOptional 与题库一致），
// 写仅 root（硬编码 RequireRoot，不下放权限位，同角色管理先例）。
// 题目侧只存节点 ID；整树覆盖保存时拦截「删除仍被引用的节点」并返回引用题目明细。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// treeDocID 解析 ?tree= 参数：meta = 来源标签树，缺省 = 知识树（原标签树）。
func treeDocID(tree string) string {
	if tree == "meta" {
		return repo.TreeDocMeta
	}
	return repo.TreeDocKnowledge
}

// treeField 来源树挂载在题目的 meta_tags 字段上（知识树 = tags）。
func treeField(tree string) string {
	if tree == "meta" {
		return "meta_tags"
	}
	return "tags"
}

// treeCounterKey 来源树独立取号（tag_meta），与知识树（tag）ID 空间互不相干。
func treeCounterKey(tree string) string {
	if tree == "meta" {
		return "tag_meta"
	}
	return "tag"
}

// GetTagTree 整棵树（?tree=meta 取来源树；从未配置时返回空数组）。
func (h *Handlers) GetTagTree(c *gin.Context) {
	nodes, err := h.repo.GetTagTreeDoc(c.Request.Context(), treeDocID(c.Query("tree")))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取标签树失败")
		return
	}
	if nodes == nil {
		nodes = []repo.TagNode{}
	}
	ok(c, gin.H{"nodes": nodes})
}

// AdminTagTree root 管理面板数据：整棵树 + 每节点被引用题数（角标常驻展示，
// 并供前端把「删除/改名被引用拦截」提前到交互层，不必等保存 400）。
func (h *Handlers) AdminTagTree(c *gin.Context) {
	ctx := c.Request.Context()
	tree := c.Query("tree")
	nodes, err := h.repo.GetTagTreeDoc(ctx, treeDocID(tree))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取标签树失败")
		return
	}
	if nodes == nil {
		nodes = []repo.TagNode{}
	}
	counts, err := h.repo.TagUsageCountsField(ctx, treeField(tree))
	if err != nil {
		fail(c, http.StatusInternalServerError, "统计标签引用失败")
		return
	}
	ok(c, gin.H{"nodes": nodes, "counts": counts})
}

// AdminReplaceTagTree root 整树覆盖保存：形状校验 → 按名对齐身份（同名继承旧 ID、
// 新名取新号）→ 删除拦截（待删节点被题目引用则 400 附引用题目明细）→ 覆盖写入。
func (h *Handlers) AdminReplaceTagTree(c *gin.Context) {
	var req struct {
		Nodes []repo.TagNode `json:"nodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := repo.ValidateTagTreeShape(req.Nodes); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ctx := c.Request.Context()
	tree := c.Query("tree")
	oldNodes, err := h.repo.GetTagTreeDoc(ctx, treeDocID(tree))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取标签树失败")
		return
	}
	nodes, removed, err := h.repo.ReconcileTagTreeDoc(ctx, treeDocID(tree), treeCounterKey(tree), req.Nodes)
	if err != nil {
		fail(c, http.StatusInternalServerError, "标签树对齐失败")
		return
	}

	// 删除拦截：待删节点仍被题目引用时拒绝保存，返回题目清单（ID/标题/各自标签名）
	if len(removed) > 0 {
		refs, err := h.repo.TagRefProblemsField(ctx, treeField(tree), removed)
		if err != nil {
			fail(c, http.StatusInternalServerError, "检查标签引用失败")
			return
		}
		if len(refs) > 0 {
			names := repo.TagIDNames(oldNodes)
			blocked := map[int64]bool{}
			for _, id := range removed {
				blocked[id] = true
			}
			firstID := int64(0) // removed 已升序，取命中的最小 ID 点名
			details := make([]gin.H, 0, len(refs))
			for _, p := range refs {
				own := p.Tags
				if tree == "meta" {
					own = p.MetaTags
				}
				tagNames := make([]string, 0, len(own))
				for _, id := range own {
					if name, ok := names[id]; ok {
						tagNames = append(tagNames, name)
						if blocked[id] && firstID == 0 {
							firstID = id
						}
					}
				}
				details = append(details, gin.H{"id": p.ID, "title": p.Title, "tags": tagNames})
			}
			failData(c, http.StatusBadRequest,
				fmt.Sprintf("标签「%s」被 %d 道题使用，请先在题目上解绑再删除", names[firstID], len(refs)),
				gin.H{"problems": details, "removed_ids": removed})
			return
		}
	}

	if err = h.repo.ReplaceTagTreeDoc(ctx, treeDocID(tree), nodes); err != nil {
		fail(c, http.StatusInternalServerError, "保存标签树失败")
		return
	}
	final := map[int64]bool{}
	repo.CollectTagIDs(nodes, final)
	ok(c, gin.H{"count": len(final)})
}

// AdminUnbindTags root 从指定题目批量摘除指定标签（标签树保存被删拦截后的
// 「一键解绑全部 / 逐题解绑」）：$pull 只动列出的标签，题目其余标签保留。
// 上限：单次 ≤500 题（拦截明细规模即题目引用规模，正常远小于此）。
func (h *Handlers) AdminUnbindTags(c *gin.Context) {
	var req struct {
		ProblemIDs []int64 `json:"problem_ids"`
		TagIDs     []int64 `json:"tag_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil ||
		len(req.ProblemIDs) == 0 || len(req.ProblemIDs) > 500 || len(req.TagIDs) == 0 {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	for _, id := range append(append([]int64{}, req.ProblemIDs...), req.TagIDs...) {
		if id <= 0 {
			fail(c, http.StatusBadRequest, "参数错误")
			return
		}
	}
	n, err := h.repo.UnbindTagsField(c.Request.Context(), treeField(c.Query("tree")), req.ProblemIDs, req.TagIDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "解绑失败")
		return
	}
	ok(c, gin.H{"unbound": n})
}

// parseTagFilter 解析题库列表的 tags=1,5,9 筛选参数（精确匹配节点 ID）。
func parseTagFilter(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("tags 参数需为逗号分隔的正整数标签 ID")
		}
		out = append(out, id)
	}
	return out, nil
}
