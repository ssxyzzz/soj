package server

// 标签树（v0.5f）：读公开（题库筛选/详情卡/建题表单共用，AuthOptional 与题库一致），
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

// GetTagTree 整棵标签树（从未配置时返回空数组）。
func (h *Handlers) GetTagTree(c *gin.Context) {
	nodes, err := h.repo.GetTagTree(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取标签树失败")
		return
	}
	if nodes == nil {
		nodes = []repo.TagNode{}
	}
	ok(c, gin.H{"nodes": nodes})
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
	oldNodes, err := h.repo.GetTagTree(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取标签树失败")
		return
	}
	nodes, removed, err := h.repo.ReconcileTagTree(ctx, req.Nodes)
	if err != nil {
		fail(c, http.StatusInternalServerError, "标签树对齐失败")
		return
	}

	// 删除拦截：待删节点仍被题目引用时拒绝保存，返回题目清单（ID/标题/各自标签名）
	if len(removed) > 0 {
		refs, err := h.repo.TagRefProblems(ctx, removed)
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
				tagNames := make([]string, 0, len(p.Tags))
				for _, id := range p.Tags {
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
				gin.H{"problems": details})
			return
		}
	}

	if err = h.repo.ReplaceTagTree(ctx, nodes); err != nil {
		fail(c, http.StatusInternalServerError, "保存标签树失败")
		return
	}
	final := map[int64]bool{}
	repo.CollectTagIDs(nodes, final)
	ok(c, gin.H{"count": len(final)})
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
