package server

// 题解（v0.5.2-1，多作者模型）：
//   - 看与写门槛一致：该题拿到满分（score=100）或 老师群体（题面编辑权限位持有者，随时可写可看）；
//   - 作者本人可编辑/删除自己的；置顶与删除他人题解需 solution.manage（初始 admin），
//     题目 owner 也可删除；
//   - 内容 Markdown（MdEditor + 图片上传），上限与题面一致 256KB。

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const maxSolutionBytes = 256 << 10

// solutionAllowed 题解的看/写门槛：满分 ∨ 老师群体（题面编辑权限位）。root 恒过。
func (h *Handlers) solutionAllowed(c *gin.Context, uid, pid int64) bool {
	if hasPermAny(c, repo.PermProblemEditAll, repo.PermProblemEditOwn) {
		return true
	}
	solved, err := h.repo.HasSolved(c.Request.Context(), uid, pid)
	return err == nil && solved
}

func solutionJSON(s *repo.Solution, nickname string) gin.H {
	return gin.H{
		"id":         s.ID,
		"uid":        s.UID,
		"nickname":   nickname,
		"content":    s.Content, // 题解正文（Markdown，v0.8 起单一事实源）
		"pinned":     s.Pinned,
		"created_at": s.CreatedAt.Format(time.RFC3339),
		"updated_at": s.UpdatedAt.Format(time.RFC3339),
	}
}

// ListSolutions 某题的题解列表（置顶优先，按时间倒序；分页）。
func (h *Handlers) ListSolutions(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	if !h.solutionAllowed(c, uid, pid) {
		fail(c, http.StatusForbidden, "该题拿到满分后才能查看题解")
		return
	}
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListSolutions(c.Request.Context(), pid, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题解失败")
		return
	}
	uids := make([]int64, 0, len(list))
	for _, s := range list {
		uids = append(uids, s.UID)
	}
	users, err := h.repo.GetUsersByIDs(c.Request.Context(), uids)
	if err != nil {
		users = map[int64]*repo.User{}
	}
	items := make([]gin.H, 0, len(list))
	for _, s := range list {
		nickname := ""
		if u, ok := users[s.UID]; ok {
			nickname = u.Nickname
		}
		items = append(items, solutionJSON(s, nickname))
	}
	ok(c, gin.H{"list": items, "total": total, "can_manage": hasPerm(c, repo.PermSolutionManage)})
}

// PostSolution 发布题解。
func (h *Handlers) PostSolution(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	if !h.solutionAllowed(c, uid, pid) {
		fail(c, http.StatusForbidden, "该题拿到满分后才能发布题解")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if len(req.Content) > maxSolutionBytes {
		fail(c, http.StatusBadRequest, "题解过长（上限 256KB）")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		fail(c, http.StatusBadRequest, "题解内容不能为空")
		return
	}
	now := time.Now()
	s := &repo.Solution{PID: pid, UID: uid, Content: req.Content, CreatedAt: now, UpdatedAt: now}
	if err := h.repo.InsertSolution(c.Request.Context(), s); err != nil {
		fail(c, http.StatusInternalServerError, "发布失败")
		return
	}
	ok(c, gin.H{"id": s.ID})
}

// UpdateSolution 作者编辑自己的题解。
func (h *Handlers) UpdateSolution(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	sid, okID := parseID(c, "sid")
	if !okID {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if len(req.Content) > maxSolutionBytes {
		fail(c, http.StatusBadRequest, "题解过长（上限 256KB）")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		fail(c, http.StatusBadRequest, "题解内容不能为空")
		return
	}
	s, err := h.repo.GetSolution(c.Request.Context(), sid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题解不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题解失败")
		return
	}
	if s.UID != uid {
		fail(c, http.StatusForbidden, "只能编辑自己发布的题解")
		return
	}
	if err := h.repo.UpdateSolutionContent(c.Request.Context(), sid, req.Content); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": sid})
}

// DeleteSolution 删除题解：作者本人 ∨ 题目 owner ∨ solution.manage ∨ root。
func (h *Handlers) DeleteSolution(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	pid, okID := parseID(c, "id")
	if !okID {
		return
	}
	sid, okID := parseID(c, "sid")
	if !okID {
		return
	}
	s, err := h.repo.GetSolution(c.Request.Context(), sid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题解不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题解失败")
		return
	}
	p, err := h.repo.GetProblem(c.Request.Context(), pid)
	owner := err == nil && p.CreatedBy == uid
	if s.UID != uid && !owner && !hasPerm(c, repo.PermSolutionManage) {
		fail(c, http.StatusForbidden, "只能删除自己发布的题解")
		return
	}
	if err := h.repo.DeleteSolution(c.Request.Context(), sid); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": sid})
}

// PinSolution 置顶/取消置顶题解（solution.manage 权限位）。
func (h *Handlers) PinSolution(c *gin.Context) {
	if !hasPerm(c, repo.PermSolutionManage) {
		fail(c, http.StatusForbidden, "需要「题解管理」权限")
		return
	}
	sid, okID := parseID(c, "sid")
	if !okID {
		return
	}
	var req struct {
		Pinned *bool `json:"pinned"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Pinned == nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if err := h.repo.SetSolutionPinned(c.Request.Context(), sid, *req.Pinned); err != nil {
		fail(c, http.StatusInternalServerError, "操作失败")
		return
	}
	ok(c, gin.H{"pinned": *req.Pinned})
}
