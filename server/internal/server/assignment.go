package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/repo"
	"soj/internal/richtext"
)

// assignmentIntroHTML 作业说明富文本（v0.7 起写 introHtml；存量 md 读时计算回退）。
func assignmentIntroHTML(a *repo.Assignment) string {
	if a.IntroHTML != "" {
		return a.IntroHTML
	}
	if a.Intro == "" {
		return ""
	}
	return richtext.MdToHTML(a.Intro)
}

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
		Title      string   `json:"title" binding:"required"`
		Intro      string   `json:"intro"`
		ProblemIDs *[]int64 `json:"problem_ids"` // v0.7.3 创建即选题（可选，对齐比赛/考试形状）
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
	if req.ProblemIDs != nil {
		if err := h.repo.SetAssignmentProblems(c.Request.Context(), a.ID, sanitizePids(*req.ProblemIDs)); err != nil {
			fail(c, http.StatusInternalServerError, "作业已创建，但题目清单保存失败")
			return
		}
	}
	ok(c, gin.H{"id": a.ID, "title": a.Title})
}

// sanitizePids 题目 ID 清单去重保序、剔除非法值。
func sanitizePids(pids []int64) []int64 {
	out := make([]int64, 0, len(pids))
	seen := map[int64]bool{}
	for _, pid := range pids {
		if pid > 0 && !seen[pid] {
			seen[pid] = true
			out = append(out, pid)
		}
	}
	return out
}

// UpdateAssignment 编辑作业名称/说明（班级管理员/root，v0.7）。
func (h *Handlers) UpdateAssignment(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Title      *string  `json:"title"`
		IntroHTML  *string  `json:"intro_html"`
		ProblemIDs *[]int64 `json:"problem_ids"` // v0.7.3 对齐比赛/考试 PATCH 形状
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
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
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可编辑作业")
		return
	}
	set := bson.M{}
	if req.Title != nil {
		title := trimTo(*req.Title, 64)
		if title == "" {
			fail(c, http.StatusBadRequest, "作业名称不能为空")
			return
		}
		set["title"] = title
	}
	if req.IntroHTML != nil {
		if len(*req.IntroHTML) > maxStatementSize {
			fail(c, http.StatusBadRequest, "作业说明过长（上限 256KB）")
			return
		}
		set["introHtml"] = *req.IntroHTML
	}
	if req.ProblemIDs != nil {
		set["problems"] = sanitizePids(*req.ProblemIDs)
	}
	if len(set) == 0 {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateAssignment(ctx, id, set); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": id})
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

// SetAssignmentProblems 整体保存作业题目清单（顺序即展示顺序）。
// 「调整题目」弹窗一次保存增删与拖动排序；pids 为最终清单。
func (h *Handlers) SetAssignmentProblems(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		Pids []int64 `json:"pids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
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
		fail(c, http.StatusForbidden, "仅班级管理员可调整作业题目")
		return
	}
	// 去重并保持给定顺序
	seen := map[int64]bool{}
	pids := make([]int64, 0, len(req.Pids))
	for _, pid := range req.Pids {
		if pid > 0 && !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
	}
	if err := h.repo.SetAssignmentProblems(ctx, id, pids); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"count": len(pids)})
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
	now := time.Now()
	// 普通题状态按题库口径（仅公开态提交）；轮次来源题用本场次原始提交，
	// 以便关闭「赛期内不可见」时显示实时状态。
	allPids := make([]int64, 0, len(a.Problems))
	for _, pid := range a.Problems {
		allPids = append(allPids, pid)
	}
	bestPub := map[int64]repo.UserBestStatus{}
	if logged {
		if best, err := h.repo.UserBestStatuses(ctx, uid, allPids); err == nil {
			bestPub = best
		}
	}
	type roundSubs struct {
		submitted map[int64]bool
		best      map[int64]repo.UserBestStatus
	}
	rawRounds := map[string]*roundSubs{}
	loadRaw := func(lk *contestLock) *roundSubs {
		key := lk.Kind + ":" + strconv.FormatInt(lk.RefID, 10)
		if rs, ok := rawRounds[key]; ok {
			return rs
		}
		rs := &roundSubs{submitted: map[int64]bool{}, best: map[int64]repo.UserBestStatus{}}
		if logged {
			if subs, err := h.repo.RoundSubmissions(ctx, lk.Kind, lk.RefID, uid); err == nil {
				for _, sub := range subs {
					rs.submitted[sub.ProblemID] = true
					if old, ok := rs.best[sub.ProblemID]; !ok || sub.Score > old.Score {
						rs.best[sub.ProblemID] = repo.UserBestStatus{Status: sub.Status, Score: sub.Score}
					}
				}
			}
		}
		rawRounds[key] = rs
		return rs
	}
	problems := make([]gin.H, 0, len(a.Problems))
	for _, pid := range a.Problems {
		lk := lockMap[pid]
		if lk != nil && lk.Phase != "running" {
			continue // 赛前/隐藏期对该视角隐藏
		}
		p, err := h.repo.GetProblem(ctx, pid)
		if err != nil || p.Hidden {
			continue // 题目已删除或主题库隐藏（备题）
		}
		item := gin.H{"id": p.ID, "title": p.Title}
		resultHidden := false
		if lk != nil {
			if lk.Kind == repo.KindExam {
				item["exam_id"] = lk.RefID
			} else {
				item["contest_id"] = lk.RefID
			}
			r := lk.Round
			resultHidden = !manage && !r.Published &&
				!(r.Status(now) == "running" && !r.HideDuring)
		}
		item["result_hidden"] = resultHidden
		if logged {
			var st repo.UserBestStatus
			var has bool
			submitted := false
			if lk != nil {
				rs := loadRaw(lk)
				submitted = rs.submitted[pid]
				st, has = rs.best[pid]
			} else {
				st, has = bestPub[pid]
				submitted = has
			}
			item["submitted"] = submitted
			if !resultHidden && has {
				item["status"] = st.Status
				item["my_score"] = st.Score
			}
		}
		if !resultHidden {
			item["tags"] = orEmptyInt64(p.Tags)
		}
		problems = append(problems, item)
	}
	if stats, err := h.repo.ListProblemStats(ctx, allPids); err == nil {
		for _, it := range problems {
			if it["result_hidden"] == true {
				continue
			}
			st := stats[it["id"].(int64)]
			it["submit_count"] = st.Total
			it["accepted_count"] = st.Accepted
		}
	}
	resp := gin.H{
		"id": a.ID, "class_id": a.ClassID, "title": a.Title, "intro": a.Intro,
		"intro_html": assignmentIntroHTML(a),
		"problems":   problems, "manageable": manage,
	}
	// 管理员调整题目用完整清单：含赛前/隐藏期/主题库隐藏题，避免保存时悄悄丢失。
	if manage {
		edit := make([]gin.H, 0, len(a.Problems))
		for _, pid := range a.Problems {
			item := gin.H{"id": pid, "title": ""}
			if p, err := h.repo.GetProblem(ctx, pid); err == nil {
				item["title"] = p.Title
				item["hidden"] = p.Hidden
			}
			edit = append(edit, item)
		}
		resp["edit_problems"] = edit
	}
	ok(c, resp)
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
	if !h.assignmentAccessible(ctx, a.ClassID, uid, role, logged) {
		fail(c, http.StatusForbidden, "加入班级后才能查看")
		return
	}
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	manage := h.classManageable(c, cl)

	uids := make([]int64, 0, 8)
	if manage {
		members, err := h.repo.ListMembers(ctx, a.ClassID)
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取成员失败")
			return
		}
		for _, m := range members {
			uids = append(uids, m.UID)
		}
	} else {
		// 学生视角：只统计自己（用于「我的完成进度」）
		uids = append(uids, uid)
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
				probs = append(probs, gin.H{"pid": pid, "tries": 0, "best": 0, "errors": 0, "solved": false})
				continue
			}
			probs = append(probs, gin.H{
				"pid": pid, "tries": cell.Tries, "best": cell.Best,
				"errors": cell.Errors, "solved": cell.Solved,
			})
		}
		items = append(items, gin.H{
			"uid": r.UID, "username": r.Username, "nickname": r.Nickname,
			"attempted": r.Attempt, "solved": r.Solved,
			"total": r.Total, "rank": r.Rank, "problems": probs,
		})
	}
	ok(c, items)
}

// AssignmentUserSubmissions 查看指定用户在某作业某题的全部公开提交（含代码与测试点明细）。
// 门槛：班级管理员/root ∨「查看用户提交明细」权限位；题目可为普通题或比赛/考试来源，
// 统计口径与 AssignmentProgress 一致（published=true，不按轮次过滤）。
func (h *Handlers) AssignmentUserSubmissions(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	pid, okPID := parseID(c, "pid")
	if !okPID {
		return
	}
	targetUID := int64(atoiDefault(c.Query("uid"), 0))
	if targetUID <= 0 {
		fail(c, http.StatusBadRequest, "缺少 uid 参数")
		return
	}
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
		fail(c, http.StatusForbidden, "加入班级后才能查看")
		return
	}
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	if !h.classManageable(c, cl) && !hasPerm(c, repo.PermSubmissionViewUser) {
		fail(c, http.StatusForbidden, "需要「查看用户提交明细」权限或为班级管理员")
		return
	}
	inAssignment := false
	for _, p := range a.Problems {
		if p == pid {
			inAssignment = true
			break
		}
	}
	if !inAssignment {
		fail(c, http.StatusNotFound, "题目不在作业中")
		return
	}
	// 只允许查看本班成员；root 例外（榜单/成绩表管理员视角）
	if targetUID != uid && role != repo.RoleRoot {
		member, _ := h.repo.IsMember(ctx, a.ClassID, targetUID)
		if !member {
			fail(c, http.StatusForbidden, "该用户不在班级中")
			return
		}
	}
	subs, err := h.repo.UserProblemSubmissions(ctx, targetUID, pid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}
	items := make([]gin.H, 0, len(subs))
	for _, s := range subs {
		items = append(items, submissionJSON(s, true))
	}
	ok(c, gin.H{"list": items})
}

// AssignmentExport 作业成绩导出（班级管理员/root，v0.5.6；v0.6.2 起含排名）：
// 列 = 排名/昵称/各题最高分/总分（最高分合计）/通过题数/做过题数，昵称为空回退用户名；
// 行序 = 统一成绩表排名（总分降序 → 通过题数降序 → UID 升序）。
func (h *Handlers) AssignmentExport(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
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
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可导出成绩")
		return
	}
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
	// 展示名统一用昵称（空则回退用户名），不再读取真实姓名
	header := []string{"排名", "昵称"}
	for _, pid := range a.Problems {
		title := ""
		if p, err := h.repo.GetProblem(ctx, pid); err == nil {
			title = p.Title
		}
		if title != "" {
			header = append(header, fmt.Sprintf("P%d %s", pid, title))
		} else {
			header = append(header, fmt.Sprintf("P%d", pid)) // 题目已删除时以题号兜底
		}
	}
	header = append(header, "总分", "通过题数", "做过题数")
	out := [][]string{header}
	for _, r := range rows {
		name := r.Nickname
		if name == "" {
			name = r.Username
		}
		row := []string{strconv.Itoa(r.Rank), name}
		for _, pid := range a.Problems {
			best := 0
			if cell := r.Cells[pid]; cell != nil {
				best = cell.Best
			}
			row = append(row, strconv.Itoa(best))
		}
		row = append(row, strconv.Itoa(r.Total), strconv.Itoa(r.Solved), strconv.Itoa(r.Attempt))
		out = append(out, row)
	}
	serveCSV(c, fmt.Sprintf("%s_%s_%s.csv", csvSafeName(cl.Name), csvSafeName(a.Title), time.Now().Format("2006-01-02")), out)
}
