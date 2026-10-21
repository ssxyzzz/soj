package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
	"soj/internal/testdata"
)

// maxStatementSize 题面/说明类富文本的体积上限（v0.6 起直接作用 HTML 字段——HTML 与最终
// 渲染 DOM 同量级，防病态内容卡死页面的口径不变）。超限直接拒绝。
const maxStatementSize = 256 << 10 // 256KB

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

// hasAnyTag 题目标签是否命中任一选中 ID（并集、精确匹配）。
func hasAnyTag(tags []int64, sel map[int64]bool) bool {
	for _, id := range tags {
		if sel[id] {
			return true
		}
	}
	return false
}

// orEmptyInt64 nil 切片转空切片，JSON 输出 [] 而非 null。
func orEmptyInt64(s []int64) []int64 {
	if s == nil {
		return []int64{}
	}
	return s
}

// ListProblems 题库列表（按视角过滤）：
//   - 题目隐藏（hidden）对无「题库全量可见」的视角一律不出现（v0.7 起任何场景均隐藏，比赛/考试也不可见）；
//   - 被赛事收录（隐藏期=赛前/赛中/赛后未发布）的题：非 running 阶段对受影响视角不出现，
//     running 阶段带锁标记由前端引导进赛事做题页；
//   - admin/root 可见全部并带标记；
//   - tags=1,5,9 标签筛选：并集、精确匹配节点 ID（v0.5.5）。
func (h *Handlers) ListProblems(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	page, pageSize := queryPage(c)
	privileged := hasPerm(c, repo.PermProblemViewAll)
	// 题库搜索：标题包含（忽略大小写）或 ID 精确匹配
	q := strings.TrimSpace(c.Query("q"))
	ql := strings.ToLower(q)
	tagSel, err := parseTagFilter(c.Query("tags"))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	selSet := map[int64]bool{}
	for _, id := range tagSel {
		selSet[id] = true
	}

	ctx := c.Request.Context()
	// 生效语义（非特权可见性过滤）与展示视角分开：view 版仅用于锁定标记字段，
	// root/admin 的全量锁不得参与隐藏过滤
	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	viewMap, err := h.loadLockMapView(ctx, uid, role)
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
		if q != "" && strconv.FormatInt(p.ID, 10) != q && !strings.Contains(strings.ToLower(p.Title), ql) {
			continue
		}
		if len(selSet) > 0 && !hasAnyTag(p.Tags, selSet) {
			continue
		}
		lk := lockMap[p.ID]
		if !privileged {
			// 题目隐藏：任何场景都对无权限视角隐藏（备题用，比赛/考试题目表同理）
			// 赛事隐藏期的非 running 阶段（赛前/赛后未发布）同理不出现，running 交给前端引导
			if p.Hidden || (lk != nil && lk.Phase != "running") {
				continue
			}
		}
		item := gin.H{"id": p.ID, "title": p.Title, "hidden": p.Hidden, "tags": orEmptyInt64(p.Tags), "has_cases": len(p.Cases) > 0}
		// 题型徽章（v0.9）：客观题另带小类；函数/交互题只给 type（空 = 传统题不返回）
		if p.Type != "" {
			item["type"] = p.Type
		}
		if p.IsObjective() {
			item["kind"] = repo.SetKindSummary(p.Questions)
		}
		if vk := viewMap[p.ID]; vk != nil {
			item["locked"] = true
			item["lock_kind"] = vk.Kind
			if vk.Kind == repo.KindExam {
				item["exam_id"] = vk.RefID
			} else {
				item["contest_id"] = vk.RefID
			}
			item["lock_phase"] = vk.Phase
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
	pageItems := items[start:end]
	// 通过率统计：只聚合当前页题目的公开态提交；失败不阻塞列表（前端显示 -）
	pids := make([]int64, 0, len(pageItems))
	for _, it := range pageItems {
		pids = append(pids, it["id"].(int64))
	}
	stats, err := h.repo.ListProblemStats(ctx, pids)
	if err != nil {
		stats = map[int64]repo.ProblemStat{}
	}
	for _, it := range pageItems {
		st := stats[it["id"].(int64)]
		it["submit_count"] = st.Total
		it["accepted_count"] = st.Accepted
	}
	// 状态列（v0.5.6 升级）：登录用户按当前页批查自己本题最高分提交的状态与得分；
	// 失败不阻塞列表（该列回退未做）
	if logged {
		if bests, err := h.repo.UserBestStatuses(ctx, uid, pids); err == nil {
			for _, it := range pageItems {
				if st := bests[it["id"].(int64)]; st.Status != "" {
					it["status"] = st.Status
					it["my_score"] = st.Score
				}
			}
		}
	}
	ok(c, gin.H{"list": pageItems, "total": total})
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

	uid, _, role, logged := currentUser(c)
	privileged := hasPerm(c, repo.PermProblemViewAll)
	canViewOthers := privileged
	if logged && !privileged {
		if solved, err := h.repo.HasSolved(ctx, uid, id); err == nil && solved {
			canViewOthers = true
		}
	}
	// 题解可见门槛与 solutionAllowed 同规则：老师群体（题面编辑权）∨ 该题满分
	canViewSolutions := logged && h.solutionAllowed(c, uid, id)
	lockMap, err := h.loadLockMap(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	lk := lockMap[p.ID]
	// 展示视角（v0.7.2 验收反馈）：root/admin 详情响应带全量锁标记（比赛中/考试中 + 赛事指引），
	// 上面的 404 生效判定仍用原语义 lockMap
	viewMap, err := h.loadLockMapView(ctx, uid, role)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛状态失败")
		return
	}
	vk := viewMap[p.ID]

	// 隐藏题：对无「题库全量可见」的任何视角一律隐藏（比赛/考试中也不可见）
	if p.Hidden && !privileged {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	// 隐藏期（赛前/赛后未发布）：对受影响视角返回 404 + 锁信息，前端凭 failData
	// 重定向进比赛/考试做题页；持「题库全量可见」者响应 200 带锁标记，由前端同规则重定向
	if lk != nil && lk.Phase != "running" && !privileged {
		what := "比赛"
		data := gin.H{"locked": true, "lock_kind": lk.Kind, "contest_id": lk.RefID}
		if lk.Kind == repo.KindExam {
			what = "考试"
			data = gin.H{"locked": true, "lock_kind": lk.Kind, "exam_id": lk.RefID}
		}
		failData(c, http.StatusNotFound, "该题处于"+what+"隐藏期", data)
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
		"id":                 p.ID,
		"type":               p.Type, // 空 = 传统题（前端按 default 处理）
		"title":              p.Title,
		"description":        p.Statement, // 题面（Markdown，v0.8 起单一事实源）
		"time_limit_ms":      p.TimeLimitMS,
		"memory_limit_kb":    p.MemoryLimitKB,
		"compare":            p.Compare,
		"float_tolerance":    p.FloatTolerance,
		"has_checker":        p.CheckerFile != "",
		"has_cases":          len(p.Cases) > 0, // 无测试数据空壳题（v0.6.1 验收反馈）：前端禁提交+详情提示
		"samples":            sampleItems,
		"has_subtasks":       len(p.Subtasks) > 0,
		"hidden":             p.Hidden,
		"can_set_hidden":     h.problemEditable(c, p),
		"can_edit_statement": h.problemEditable(c, p),
		"can_view_others":    canViewOthers,    // 满分解锁后可在本页查看他人代码
		"can_view_solutions": canViewSolutions, // 满分 ∨ 老师群体：题解弹窗入口提示用
		"file_io_input":      p.FileIOInput,    // 文件名对：编辑配置用；场景 warning 展示实际文件名
		"file_io_output":     p.FileIOOutput,
		"tags":               orEmptyInt64(p.Tags), // 节点 ID，名字由前端标签树映射
	}
	// 客观题（v0.9.3 多小题）：对外只回脱敏视图；正确答案与解析在满分 ∨ 老师群体门槛下附给
	if p.IsObjective() {
		resp["questions"] = repo.PublicSetView(p.Questions)
		if canViewSolutions || h.problemEditable(c, p) {
			resp["questions_full"] = p.Questions // 含答案/解析（编辑回填 + 满分解锁展示）
		}
	}
	// 交互题（v0.9.2）：交互器配置状态（做题页展示交互说明卡）
	if p.IsInteractive() {
		resp["interactive"] = gin.H{"has_interactor": p.InteractorFile != ""}
	}
	// 函数题（v0.9.1）：driver 配置状态 + 学生端模板内容（提交页/在线编程预填）
	if p.IsFunction() {
		fn := gin.H{"has_driver": p.DriverFile != "", "has_template": p.TemplateFile != ""}
		if p.TemplateFile != "" {
			if src, err := testdata.ReadAll(h.provider(), id, p.TemplateFile); err == nil {
				fn["template_code"] = string(src)
			}
		}
		resp["function"] = fn
	}
	// 统计（题目信息卡）：公开态提交聚合；失败降级 0，不阻塞详情
	if stats, err := h.repo.ListProblemStats(ctx, []int64{id}); err == nil {
		resp["submit_count"] = stats[id].Total
		resp["accepted_count"] = stats[id].Accepted
	} else {
		resp["submit_count"] = int64(0)
		resp["accepted_count"] = int64(0)
	}
	if vk != nil {
		resp["locked"] = true
		resp["lock_kind"] = vk.Kind
		if vk.Kind == repo.KindExam {
			resp["exam_id"] = vk.RefID
			resp["exam_title"] = vk.Round.Title
		} else {
			resp["contest_id"] = vk.RefID
			resp["contest_title"] = vk.Round.Title
		}
	}
	ok(c, resp)
}

// SetProblemStatement 编辑题面（Markdown；权限同隐藏设置：root 任意题；admin 仅自己导入的题）。
func (h *Handlers) SetProblemStatement(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req struct {
		MD string `json:"md" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "题面不能为空")
		return
	}
	if len(req.MD) > maxStatementSize {
		fail(c, http.StatusBadRequest, "题面过长（上限 256KB）")
		return
	}
	_, _, _, _ = currentUser(c)
	ctx := c.Request.Context()
	p, err := h.repo.GetProblem(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if !h.problemEditable(c, p) {
		fail(c, http.StatusForbidden, "需要「编辑任意题目」权限，或仅编辑自己导入的题目")
		return
	}
	if err := h.repo.SetProblemStatement(ctx, id, req.MD); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"id": id})
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
	_, _, _, _ = currentUser(c)
	ctx := c.Request.Context()
	p, err := h.repo.GetProblem(ctx, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if !h.problemEditable(c, p) {
		fail(c, http.StatusForbidden, "需要「编辑任意题目」权限，或仅编辑自己导入的题目")
		return
	}
	if err := h.repo.SetProblemHidden(ctx, id, *req.Hidden); err != nil {
		fail(c, http.StatusInternalServerError, "设置失败")
		return
	}
	ok(c, gin.H{"id": id, "hidden": *req.Hidden})
}

// problemEditable 题目编辑判定：edit.all 任意题；edit.own 仅题目导入者。
func (h *Handlers) problemEditable(c *gin.Context, p *repo.Problem) bool {
	if hasPerm(c, repo.PermProblemEditAll) {
		return true
	}
	if !hasPerm(c, repo.PermProblemEditOwn) {
		return false
	}
	uid, _, _, logged := currentUser(c)
	return logged && p.CreatedBy == uid
}
