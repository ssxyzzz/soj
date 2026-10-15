package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
	"soj/internal/testdata"
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

// ListProblems 题库列表（按视角过滤）：
//   - 主题库隐藏题对 guest/default 不可见（被主题库比赛赛中解锁时除外）；
//   - 被比赛锁定（赛前/隐藏期）的题对受影响视角不可见，赛中带锁标记与指引；
//   - admin/root 可见全部并带标记。
func (h *Handlers) ListProblems(c *gin.Context) {
	uid, _, role, _ := currentUser(c)
	page, pageSize := queryPage(c)
	privileged := role == repo.RoleAdmin || role == repo.RoleRoot

	ctx := c.Request.Context()
	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	all, err := h.repo.ListProblems(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题库失败")
		return
	}

	items := make([]gin.H, 0)
	for _, p := range all {
		lk := lockMap[p.ID]
		if !privileged {
			hiddenBlocked := p.Hidden && !(lk != nil && lk.Phase == "running" && lk.Bank)
			if hiddenBlocked || (lk != nil && lk.Phase != "running") {
				continue
			}
		}
		item := gin.H{"id": p.ID, "title": p.Title, "hidden": p.Hidden}
		if lk != nil {
			item["locked"] = true
			item["contest_id"] = lk.Contest.ID
			item["lock_phase"] = lk.Phase
		}
		items = append(items, item)
	}

	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	ok(c, gin.H{"list": items[start:end], "total": total})
}

// GetProblem 题目详情（含按视角的锁题/隐藏判定与样例按需读取）。
func (h *Handlers) GetProblem(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	p, err := h.repo.GetProblem(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}

	uid, _, role, _ := currentUser(c)
	privileged := role == repo.RoleAdmin || role == repo.RoleRoot
	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	lk := lockMap[p.ID]

	// 隐藏题：default/guest 不可见（主题库比赛赛中解锁）
	if p.Hidden && !privileged && !(lk != nil && lk.Phase == "running" && lk.Bank) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	// 赛前 / 隐藏期：对受影响视角不可见
	if lk != nil && lk.Phase != "running" {
		fail(c, http.StatusNotFound, "该题已被比赛锁定，暂不可见")
		return
	}

	sampleItems := make([]gin.H, 0)
	for _, tc := range p.Cases {
		if !tc.Sample {
			continue
		}
		input, err := testdata.ReadAll(h.provider(), id, tc.InName)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取样例失败")
			return
		}
		output, err := testdata.ReadAll(h.provider(), id, tc.AnsName)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取样例失败")
			return
		}
		sampleItems = append(sampleItems, gin.H{
			"id":     int64(tc.Index),
			"input":  string(input),
			"output": string(output),
		})
	}

	resp := gin.H{
		"id":              p.ID,
		"title":           p.Title,
		"description":     p.Statement,
		"time_limit_ms":   p.TimeLimitMS,
		"memory_limit_kb": p.MemoryLimitKB,
		"samples":         sampleItems,
		"has_subtasks":    len(p.Subtasks) > 0,
		"hidden":          p.Hidden,
		"can_set_hidden":  role == repo.RoleRoot || (role == repo.RoleAdmin && p.CreatedBy == uid),
	}
	if lk != nil {
		resp["locked"] = true
		resp["contest_id"] = lk.Contest.ID
		resp["contest_title"] = lk.Contest.Title
	}
	ok(c, resp)
}

// SetProblemHidden 设置主题库隐藏标记（root 任意题；admin 仅自己导入的题）。
func (h *Handlers) SetProblemHidden(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Hidden *bool `json:"hidden" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	uid, _, role, _ := currentUser(c)
	ctx := c.Request.Context()
	p, err := h.repo.GetProblem(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if role != repo.RoleRoot && !(role == repo.RoleAdmin && p.CreatedBy == uid) {
		fail(c, http.StatusForbidden, "只有 root 或题目导入者可以设置隐藏")
		return
	}
	if err := h.repo.SetProblemHidden(ctx, id, *req.Hidden); err != nil {
		fail(c, http.StatusInternalServerError, "设置失败")
		return
	}
	ok(c, gin.H{"id": id, "hidden": *req.Hidden})
}
