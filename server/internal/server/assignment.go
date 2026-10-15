package server

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// assignmentAccessible 作业仅本班成员与班级管理员/root 可见。
func (h *Handlers) assignmentAccessible(ctx context.Context, classID, uid int64, role string, logged bool) bool {
	if !logged {
		return false
	}
	if role == repo.RoleRoot {
		return true
	}
	member, _ := h.repo.IsMember(ctx, classID, uid)
	return member
}

// CreateAssignment 创建作业（班级管理员）。
func (h *Handlers) CreateAssignment(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Title string `json:"title" binding:"required"`
		Intro string `json:"intro"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "作业名称不能为空")
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可布置作业")
		return
	}
	a, err := h.repo.CreateAssignment(c.Request.Context(), classID, req.Title, req.Intro)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建作业失败")
		return
	}
	ok(c, gin.H{"id": a.ID, "title": a.Title})
}

// ListClassAssignments 班级作业列表；非本班成员返回空列表。
func (h *Handlers) ListClassAssignments(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, logged := currentUser(c)
	accessible := h.assignmentAccessible(c.Request.Context(), classID, uid, role, logged)
	if !accessible {
		ok(c, gin.H{"visible": false, "list": []gin.H{}})
		return
	}
	list, err := h.repo.ListAssignments(c.Request.Context(), classID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取作业失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, a := range list {
		items = append(items, gin.H{
			"id": a.ID, "title": a.Title, "intro": a.Intro,
			"problem_count": len(a.Problems), "created_at": a.CreatedAt,
		})
	}
	ok(c, gin.H{"visible": true, "list": items})
}

// GetAssignmentDetail 作业详情（题目清单按视角带锁题信息）。
func (h *Handlers) GetAssignmentDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, logged := currentUser(c)
	ctx := c.Request.Context()
	a, err := h.repo.GetAssignment(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "作业不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取作业失败")
		return
	}
	if !h.assignmentAccessible(ctx, a.ClassID, uid, role, logged) {
		fail(c, http.StatusForbidden, "加入班级后才能查看作业")
		return
	}
	manage := false
	cl, err := h.repo.GetClass(ctx, a.ClassID)
	if err == nil {
		manage = h.classManageable(c, cl)
	}

	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	problems := make([]gin.H, 0, len(a.Problems))
	for _, pid := range a.Problems {
		lk := lockMap[pid]
		if lk != nil && lk.Phase != "running" {
			continue // 赛前/隐藏期对该视角隐藏
		}
		p, err := h.repo.GetProblem(ctx, pid)
		if err != nil {
			continue // 题目已删除
		}
		item := gin.H{"id": p.ID, "title": p.Title}
		if lk != nil {
			if lk.Kind == repo.KindExam {
				item["exam_id"] = lk.RefID
			} else {
				item["contest_id"] = lk.RefID
			}
		}
		problems = append(problems, item)
	}
	ok(c, gin.H{
		"id": a.ID, "class_id": a.ClassID, "title": a.Title, "intro": a.Intro,
		"problems": problems, "manageable": manage,
	})
}

// DeleteAssignment 删除作业。
func (h *Handlers) DeleteAssignment(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	a, err := h.repo.GetAssignment(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "作业不存在")
		return
	}
	cl, cerr := h.repo.GetClass(c.Request.Context(), a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可删除作业")
		return
	}
	if err := h.repo.DeleteAssignment(c.Request.Context(), id); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": id})
}

// AddAssignmentProblem 向作业添加题目。
func (h *Handlers) AddAssignmentProblem(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		ProblemID int64 `json:"problem_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	ctx := c.Request.Context()
	a, err := h.repo.GetAssignment(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "作业不存在")
		return
	}
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可编辑作业")
		return
	}
	if _, err := h.repo.GetProblem(ctx, req.ProblemID); repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err := h.repo.AddAssignmentProblem(ctx, id, req.ProblemID); err != nil {
		fail(c, http.StatusInternalServerError, "添加失败")
		return
	}
	ok(c, gin.H{"added": req.ProblemID})
}

// RemoveAssignmentProblem 从作业移除题目。
func (h *Handlers) RemoveAssignmentProblem(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	pid, okPID := parseID(c, "pid")
	if !okPID {
		return
	}
	a, err := h.repo.GetAssignment(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "作业不存在")
		return
	}
	cl, cerr := h.repo.GetClass(c.Request.Context(), a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可编辑作业")
		return
	}
	if err := h.repo.RemoveAssignmentProblem(c.Request.Context(), id, pid); err != nil {
		fail(c, http.StatusInternalServerError, "移除失败")
		return
	}
	ok(c, gin.H{"removed": pid})
}

// AssignmentProgress 作业完成情况（班级管理员/root）：
// 本班每个学生在各题的提交次数 / 最高分 / 是否通过。
func (h *Handlers) AssignmentProgress(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, logged := currentUser(c)
	ctx := c.Request.Context()
	a, err := h.repo.GetAssignment(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "作业不存在")
		return
	}
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可查看完成情况")
		return
	}
	_ = uid
	_ = role
	_ = logged

	members, err := h.repo.ListMembers(ctx, a.ClassID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取成员失败")
		return
	}
	uids := make([]int64, 0, len(members))
	for _, m := range members {
		uids = append(uids, m.UID)
	}
	rows, err := h.repo.AssignmentProgress(ctx, a, uids)
	if err != nil {
		fail(c, http.StatusInternalServerError, "统计失败")
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		probs := make([]gin.H, 0, len(a.Problems))
		for _, pid := range a.Problems {
			cell := r.Cells[pid]
			if cell == nil {
				probs = append(probs, gin.H{"pid": pid, "tries": 0, "best": 0, "solved": false})
				continue
			}
			probs = append(probs, gin.H{"pid": pid, "tries": cell.Tries, "best": cell.Best, "solved": cell.Solved})
		}
		items = append(items, gin.H{
			"uid": r.UID, "username": r.Username, "nickname": r.Nickname,
			"attempted": r.Attempt, "solved": r.Solved, "problems": probs,
		})
	}
	ok(c, items)
}
