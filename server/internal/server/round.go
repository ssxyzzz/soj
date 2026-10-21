package server

// 比赛/考试同构 handler（v0.5.4 ① 合并，2026-10-17 差异盘点定稿）。
//
// 差异清单（合并依据，全部差异收敛为四点）：
//  1. 存取集合与文案：kind/name（contests/exams 集合，「比赛」/「考试」）；
//  2. 参赛资格：比赛 = 全体登录用户；考试 = 本班成员（详情/提交/我的提交/榜单多一道成员校验）；
//  3. 创建权限：比赛 = contest.bank.create 权限位；考试 = 班级管理员（classManageable）；
//  4. 详情可见性：比赛挂公开路由（AuthOptional，游客可看）；考试需登录且成员/组织者。
//
// 行为统一（合并时修正的漂移）：提交限速原两处判断不一致（比赛 = rate_exempt 权限位，
// 考试 = role==default 旧判断），统一为 rate_exempt 权限位——自定义角色在考试中提交
// 不再被误限速。另删除 ContestMySubmissions 里 classId!=0 的迁移期遗留死代码。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/lang"
	"soj/internal/repo"
)

type roundScope struct {
	Kind string // repo.KindContest | repo.KindExam
	Name string // 错误与提示文案
}

var (
	scopeContest = roundScope{Kind: repo.KindContest, Name: "比赛"}
	scopeExam    = roundScope{Kind: repo.KindExam, Name: "考试"}
)

// ---------- 共享创建 ----------

// roundCreateReq 创建请求（比赛/考试字段一致，FileIO 见 v0.5.2-3，v0.7 增两个开关）。
type roundCreateReq struct {
	Title      string    `json:"title" binding:"required"`
	Intro      string    `json:"intro"`
	Start      time.Time `json:"start" binding:"required"`
	End        time.Time `json:"end" binding:"required"`
	HiddenDays int       `json:"hidden_days"`
	ProblemIDs []int64   `json:"problem_ids" binding:"required"`
	FileIO     bool      `json:"file_io"`
	Scoreboard *bool     `json:"scoreboard"`  // 缺省 true：开启成绩表
	HideDuring *bool     `json:"hide_during"` // 缺省 true：赛期内对参赛者隐藏结果
}

// validateRoundCreate 校验创建参数；ok=false 时已写响应。
func (h *Handlers) validateRoundCreate(c *gin.Context, s roundScope, req *roundCreateReq) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return false
	}
	req.Title = strings.TrimSpace(req.Title)
	switch {
	case req.Title == "":
		fail(c, http.StatusBadRequest, s.Name+"名称不能为空")
		return false
	case !req.Start.Before(req.End):
		fail(c, http.StatusBadRequest, "开始时间必须早于结束时间")
		return false
	case req.End.Sub(req.Start) > repo.MaxContestDuration:
		fail(c, http.StatusBadRequest, s.Name+"时长最长 7 天")
		return false
	case req.HiddenDays < 0 || req.HiddenDays > int(repo.MaxHiddenDays):
		fail(c, http.StatusBadRequest, "赛后隐藏期最长 7 天")
		return false
	case len(req.Intro) > maxStatementSize:
		fail(c, http.StatusBadRequest, s.Name+"说明过长（上限 256KB）")
		return false
	}
	return true
}

// collectRoundProblems 题目去重与存在性校验；ok=false 时已写响应。
func (h *Handlers) collectRoundProblems(c *gin.Context, s roundScope, ids []int64) ([]repo.ContestProblem, bool) {
	ctx := c.Request.Context()
	seen := map[int64]bool{}
	problems := make([]repo.ContestProblem, 0, len(ids))
	for _, pid := range ids {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if _, err := h.repo.GetProblem(ctx, pid); err != nil {
			fail(c, http.StatusNotFound, "题目不存在: "+strconv.FormatInt(pid, 10))
			return nil, false
		}
		problems = append(problems, repo.ContestProblem{PID: pid, Score: 100})
	}
	return problems, true
}

// ---------- 薄包装：创建（权限语义不同，各自校验后走共享流程） ----------

// CreateContest 创建主题库比赛（contest.bank.create 权限位；班级赛事走考试接口）。
func (h *Handlers) CreateContest(c *gin.Context) {
	if !hasPerm(c, repo.PermContestBankCreate) {
		fail(c, http.StatusForbidden, "需要「创建主题库比赛」权限")
		return
	}
	h.createRound(c, scopeContest, 0)
}

// CreateExam 发起考试（班级管理员）。
func (h *Handlers) CreateExam(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	cl, err := h.repo.GetClass(c.Request.Context(), classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	}
	if err != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可发起考试")
		return
	}
	h.createRound(c, scopeExam, classID)
}

func (h *Handlers) createRound(c *gin.Context, s roundScope, classID int64) {
	uid, _, _, _ := currentUser(c)
	var req roundCreateReq
	if !h.validateRoundCreate(c, s, &req) {
		return
	}
	problems, valid := h.collectRoundProblems(c, s, req.ProblemIDs)
	if !valid {
		return
	}
	ct := &repo.Contest{
		ClassID:    classID,
		Title:      req.Title,
		Intro:      req.Intro,
		Start:      req.Start,
		End:        req.End,
		Problems:   problems,
		CreatedBy:  uid,
		FileIO:     req.FileIO,
		Scoreboard: req.Scoreboard == nil || *req.Scoreboard,
		HideDuring: req.HideDuring == nil || *req.HideDuring,
	}
	if req.HiddenDays > 0 {
		ct.HiddenUntil = req.End.AddDate(0, 0, req.HiddenDays)
	}
	if err := h.repo.CreateRound(c.Request.Context(), s.Kind, ct); err != nil {
		fail(c, http.StatusInternalServerError, "创建"+s.Name+"失败")
		return
	}
	ok(c, gin.H{"id": ct.ID})
}

// ---------- 共享详情 ----------

// roundDetailJSON 组装详情响应与题目列表（参赛资格判定由调用方给出 participant）。
func (h *Handlers) roundDetailJSON(c *gin.Context, s roundScope, ct *repo.Contest, participant bool) {
	ctx := c.Request.Context()
	uid, _, role, logged := currentUser(c)
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	now := time.Now()
	status := ct.Status(now)

	resp := gin.H{
		"id":            ct.ID,
		"title":         ct.Title,
		"intro":         ct.Intro, // 赛事说明（Markdown，v0.8 起单一事实源）
		"class_id":      ct.ClassID,
		"status":        status,
		"start":         ct.Start,
		"end":           ct.End,
		"problem_count": len(ct.Problems),
		"participant":   participant,
		"is_organizer":  isOrganizer,
		"deletable":     isOrganizer && status == "scheduled",
		"published":     ct.Published,
		"file_io":       ct.FileIO,     // 本场启用文件 IO（v0.5.2-3）
		"scoreboard":    ct.Scoreboard, // v0.7：是否开启成绩表
		"hide_during":   ct.HideDuring, // v0.7：赛期内对参赛者隐藏结果
	}
	if !ct.HiddenUntil.IsZero() {
		resp["hidden_until"] = ct.HiddenUntil
	}
	// 结果可见性：组织者恒可见；发布后所有人可见；关闭「赛时隐藏成绩」时赛中参赛者可见。
	// 赛后隐藏期（hidden_until）仍按原 OI 规则隐藏。
	resultHidden := !isOrganizer && !ct.Published && !(status == "running" && !ct.HideDuring)
	resp["result_hidden"] = resultHidden
	// 题目列表：赛前只给组织者；比赛开始后对所有人开放，考试仅本班成员。
	canSeeProblems := isOrganizer || (status != "scheduled" && (participant || s.Kind == repo.KindContest))
	if canSeeProblems {
		// 本人在本场次的提交（原始态，含赛时未发布）：用于「已提交」与关闭隐藏后的实时状态。
		submitted := map[int64]bool{}
		best := map[int64]repo.UserBestStatus{}
		if logged {
			if subs, err := h.repo.RoundSubmissions(ctx, s.Kind, ct.ID, uid); err == nil {
				for _, sub := range subs {
					submitted[sub.ProblemID] = true
					if old, ok := best[sub.ProblemID]; !ok || sub.Score > old.Score {
						best[sub.ProblemID] = repo.UserBestStatus{Status: sub.Status, Score: sub.Score}
					}
				}
			}
		}
		problems := make([]gin.H, 0, len(ct.Problems))
		for _, cp := range ct.Problems {
			p, err := h.repo.GetProblem(ctx, cp.PID)
			if err != nil || p.Hidden {
				continue // 题目已删除或主题库隐藏（备题）：不进入比赛/考试题目表
			}
			item := gin.H{"id": cp.PID, "title": p.Title, "score": cp.Score, "result_hidden": resultHidden}
			// 比赛优先级最高：考试题被主题库比赛收录（隐藏期覆盖赛前/赛中/赛后）时，
			// 行上带 contest_id，前端把入口引导进比赛做题页
			if s.Kind == repo.KindExam {
				if cov := h.bankContestOf(ctx, cp.PID); cov != nil {
					item["contest_id"] = cov.ID
				}
			}
			if logged {
				item["submitted"] = submitted[cp.PID]
			}
			if !resultHidden {
				item["tags"] = orEmptyInt64(p.Tags)
				if st, ok := best[cp.PID]; ok {
					item["status"] = st.Status
					item["my_score"] = st.Score
				}
			}
			problems = append(problems, item)
		}
		// 通过率：公开态提交口径（与题库一致）；结果隐藏时不返回。
		if !resultHidden {
			pids := make([]int64, 0, len(problems))
			for _, it := range problems {
				pids = append(pids, it["id"].(int64))
			}
			if stats, err := h.repo.ListProblemStats(ctx, pids); err == nil {
				for _, it := range problems {
					st := stats[it["id"].(int64)]
					it["submit_count"] = st.Total
					it["accepted_count"] = st.Accepted
				}
			}
		}
		resp["problems"] = problems
	}
	// 组织者编辑用完整清单：包含主题库隐藏题（不进上表，但编辑时不能悄悄丢失）。
	if isOrganizer {
		edit := make([]gin.H, 0, len(ct.Problems))
		for _, cp := range ct.Problems {
			item := gin.H{"id": cp.PID, "score": cp.Score, "title": ""}
			if p, err := h.repo.GetProblem(ctx, cp.PID); err == nil {
				item["title"] = p.Title
				item["hidden"] = p.Hidden
			}
			edit = append(edit, item)
		}
		resp["edit_problems"] = edit
	}
	ok(c, resp)
}

// ContestDetail 比赛详情（需登录；参赛者 = 全体登录用户）。
// 未开始（隐藏期赛前段）的比赛对无「题库全量可见」且非组织者的视角按不存在处理。
func (h *Handlers) ContestDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ct, err := h.repo.GetRound(c.Request.Context(), repo.KindContest, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "比赛不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取比赛失败")
		return
	}
	uid, _, role, logged := currentUser(c)
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	if ct.Status(time.Now()) == "scheduled" && !isOrganizer && !hasPerm(c, repo.PermProblemViewAll) {
		fail(c, http.StatusNotFound, "比赛尚未开始")
		return
	}
	h.roundDetailJSON(c, scopeContest, ct, logged)
}

// ExamDetail 考试详情（需登录；仅本班成员与组织者可见）。
func (h *Handlers) ExamDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	e, err := h.repo.GetRound(c.Request.Context(), repo.KindExam, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "考试不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取考试失败")
		return
	}
	member, _ := h.repo.IsMember(c.Request.Context(), e.ClassID, uid)
	isOrganizer := uid == e.CreatedBy || role == repo.RoleRoot
	if !member && !isOrganizer {
		fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
		return
	}
	h.roundDetailJSON(c, scopeExam, e, member)
}

// ---------- 共享删除 / 说明 / 场景开关（组织者语义一致，仅 kind 与文案不同） ----------

func (h *Handlers) roundOrganizerOnly(c *gin.Context, s roundScope) (*repo.Contest, bool) {
	id, okID := parseID(c, "id")
	if !okID {
		return nil, false
	}
	uid, _, role, _ := currentUser(c)
	ct, err := h.repo.GetRound(c.Request.Context(), s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return nil, false
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return nil, false
	}
	if !(uid == ct.CreatedBy || role == repo.RoleRoot) {
		fail(c, http.StatusForbidden, "只有"+s.Name+"创建者可以操作")
		return nil, false
	}
	return ct, true
}

func (h *Handlers) roundDelete(c *gin.Context, s roundScope) {
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	if ct.Status(time.Now()) != "scheduled" {
		fail(c, http.StatusBadRequest, "只能删除未开始的"+s.Name)
		return
	}
	if err := h.repo.DeleteRound(c.Request.Context(), s.Kind, ct.ID); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": ct.ID})
}

func (h *Handlers) roundSetFileIO(c *gin.Context, s roundScope) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	if err := h.repo.SetRoundFileIO(c.Request.Context(), s.Kind, ct.ID, *req.Enabled); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"file_io": *req.Enabled})
}

// roundUpdate 组织者编辑比赛/考试：名称、起止时间、题目集合与顺序、成绩表/赛时隐藏成绩开关。
// 赛中修改不做后端拦截（前端会二次确认并提示影响），权限沿用 organizer/root。
func (h *Handlers) roundUpdate(c *gin.Context, s roundScope) {
	var req struct {
		Title      *string    `json:"title"`
		Start      *time.Time `json:"start"`
		End        *time.Time `json:"end"`
		Intro      *string    `json:"intro"`
		ProblemIDs *[]int64   `json:"problem_ids"`
		Scoreboard *bool      `json:"scoreboard"`
		HideDuring *bool      `json:"hide_during"`
		FileIO     *bool      `json:"file_io"` // v0.7.4：随编辑弹窗一并保存（同 /file-io 端点语义，只影响之后的提交）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	ctx := c.Request.Context()
	set := bson.M{}
	if req.Title != nil {
		title := trimTo(*req.Title, 64)
		if title == "" {
			fail(c, http.StatusBadRequest, s.Name+"名称不能为空")
			return
		}
		set["title"] = title
	}
	if req.Intro != nil {
		if len(*req.Intro) > maxStatementSize {
			fail(c, http.StatusBadRequest, s.Name+"说明过长（上限 256KB）")
			return
		}
		set["intro"] = *req.Intro
	}
	if req.Start != nil || req.End != nil {
		start, end := ct.Start, ct.End
		if req.Start != nil {
			start = *req.Start
		}
		if req.End != nil {
			end = *req.End
		}
		if !start.Before(end) {
			fail(c, http.StatusBadRequest, "开始时间必须早于结束时间")
			return
		}
		if end.Sub(start) > repo.MaxContestDuration {
			fail(c, http.StatusBadRequest, s.Name+"时长最长 7 天")
			return
		}
		set["start"] = start
		set["end"] = end
		// 原赛后隐藏期若存在，按原相对天数跟到新结束时间，避免出现隐藏期早于结束时间
		if !ct.HiddenUntil.IsZero() {
			days := int(ct.HiddenUntil.Sub(ct.End).Hours() / 24)
			if days > 0 {
				set["hiddenUntil"] = end.AddDate(0, 0, days)
			} else {
				set["hiddenUntil"] = time.Time{}
			}
		}
	}
	if req.ProblemIDs != nil {
		if len(*req.ProblemIDs) == 0 {
			fail(c, http.StatusBadRequest, s.Name+"至少需要一道题目")
			return
		}
		existing := make(map[int64]int, len(ct.Problems))
		for _, cp := range ct.Problems {
			existing[cp.PID] = cp.Score
		}
		seen := map[int64]bool{}
		problems := make([]repo.ContestProblem, 0, len(*req.ProblemIDs))
		for _, pid := range *req.ProblemIDs {
			if pid <= 0 || seen[pid] {
				continue
			}
			seen[pid] = true
			if _, err := h.repo.GetProblem(ctx, pid); err != nil {
				fail(c, http.StatusNotFound, "题目不存在: "+strconv.FormatInt(pid, 10))
				return
			}
			score := existing[pid]
			if score <= 0 {
				score = 100
			}
			problems = append(problems, repo.ContestProblem{PID: pid, Score: score})
		}
		if len(problems) == 0 {
			fail(c, http.StatusBadRequest, s.Name+"至少需要一道题目")
			return
		}
		set["problems"] = problems
	}
	if req.Scoreboard != nil {
		set["scoreboard"] = *req.Scoreboard
	}
	if req.HideDuring != nil {
		set["hideDuring"] = *req.HideDuring
	}
	if req.FileIO != nil {
		set["fileIo"] = *req.FileIO
	}
	if len(set) == 0 {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateRoundFields(ctx, s.Kind, ct.ID, set); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": ct.ID})
}

func (h *Handlers) DeleteContest(c *gin.Context) { h.roundDelete(c, scopeContest) }
func (h *Handlers) DeleteExam(c *gin.Context)    { h.roundDelete(c, scopeExam) }

// SetContestFileIO 设置比赛「本场启用文件 IO」场景开关（创建者/root，中途可改：
// 改动只影响之后的提交，此前提交的结果不回滚——需要时用后续的重测功能）。
func (h *Handlers) SetContestFileIO(c *gin.Context) { h.roundSetFileIO(c, scopeContest) }

// SetExamFileIO 设置考试「本场启用文件 IO」场景开关（语义同比赛）。
func (h *Handlers) SetExamFileIO(c *gin.Context) { h.roundSetFileIO(c, scopeExam) }

// UpdateContest 组织者编辑比赛（v0.7：名称/时间/题目集合与顺序/成绩表与隐藏成绩开关）。
func (h *Handlers) UpdateContest(c *gin.Context) { h.roundUpdate(c, scopeContest) }

// UpdateExam 组织者编辑考试（语义同比赛）。
func (h *Handlers) UpdateExam(c *gin.Context) { h.roundUpdate(c, scopeExam) }

// ---------- 共享提交 ----------

func (h *Handlers) roundSubmit(c *gin.Context, s roundScope) {
	uid, uname, role, _ := currentUser(c)
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	pid, okPID := parseID(c, "pid")
	if !okPID {
		return
	}
	var req struct {
		Language string `json:"language" binding:"required"`
		Code     string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	if len(req.Code) > maxCodeSize {
		fail(c, http.StatusBadRequest, "代码过长（上限 64KB）")
		return
	}

	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	// 考试仅本班成员可交；root 巡场豁免（否则 root 非成员时考试题无路可交）
	if s.Kind == repo.KindExam {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member && role != repo.RoleRoot {
			fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
			return
		}
	}
	if ct.Status(time.Now()) != "running" {
		fail(c, http.StatusBadRequest, s.Name+"未开始或已结束，不能提交")
		return
	}
	if _, inRound := ct.ProblemScore(pid); !inRound {
		fail(c, http.StatusBadRequest, "该题不在本场"+s.Name+"中")
		return
	}
	// 比赛优先级最高（隐藏期含赛前/赛中/赛后）：题目被进行中的主题库比赛收录时，
	// 该题只能从比赛通道提交，考试通道关闭（前端做题页同步重定向）
	if s.Kind == repo.KindExam {
		if covered := h.bankRunningContestOf(ctx, pid); covered != nil {
			fail(c, http.StatusBadRequest,
				"该题处于比赛「"+covered.Title+"」隐藏期，请从比赛页面提交")
			return
		}
	}
	if !lang.Exists(req.Language) {
		fail(c, http.StatusBadRequest, "不支持的语言")
		return
	}

	// 全局限速；持有「提交不限速」权限位者豁免
	if !hasPerm(c, repo.PermSubmissionRateExempt) {
		if st, err := h.repo.GetSystemSettings(ctx); err == nil && !h.limiter.allow(uid, st.RateLimitPerMinute, time.Now()) {
			fail(c, http.StatusTooManyRequests,
				"提交过于频繁：每分钟最多 "+strconv.Itoa(st.RateLimitPerMinute)+" 次")
			return
		}
	}

	p, err := h.repo.GetProblem(ctx, pid)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	// v0.7：主题库隐藏题不从比赛/考试题目表出现，也不接受非特权提交
	if p.Hidden && !hasPerm(c, repo.PermProblemViewAll) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	var ctArg, eArg *repo.Contest
	if s.Kind == repo.KindExam {
		eArg = ct
	} else {
		ctArg = ct
	}
	// 无测试数据的空壳题（v0.6.1 验收反馈）：与前端禁提交同一文案
	if len(p.Cases) == 0 {
		fail(c, http.StatusBadRequest, "本题暂无数据，无法评测。请联系管理员上传数据")
		return
	}
	sid, err := h.repo.CreateSubmission(ctx, uid, uname, p, req.Language, req.Code, ctArg, eArg)
	if err != nil {
		fail(c, http.StatusInternalServerError, "创建提交失败")
		return
	}
	h.wakeJudge() // 即时唤醒空闲 worker（多进程部署靠 1s 轮询兜底）
	ok(c, gin.H{"id": sid})
}

// RejudgeContest / RejudgeExam 按赛批量重测（v0.7.1，组织者）：本场全部已出结果的
// 提交低优先级重排队，不挤占学生新提交。
func (h *Handlers) RejudgeContest(c *gin.Context) { h.roundRejudgeAll(c, scopeContest) }

func (h *Handlers) RejudgeExam(c *gin.Context) { h.roundRejudgeAll(c, scopeExam) }

func (h *Handlers) roundRejudgeAll(c *gin.Context, s roundScope) {
	ct, okR := h.roundOrganizerOnly(c, s)
	if !okR {
		return
	}
	n, err := h.repo.RejudgeRound(c.Request.Context(), s.Kind, ct.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "批量重测失败")
		return
	}
	h.wakeJudge()
	ok(c, gin.H{"count": n})
}

// SubmitToContest 比赛页提交入口（赛时唯一接受该题提交的通道）。
func (h *Handlers) SubmitToContest(c *gin.Context) { h.roundSubmit(c, scopeContest) }

// SubmitToExam 考试页提交入口（考时唯一接受该题提交的通道）。
func (h *Handlers) SubmitToExam(c *gin.Context) { h.roundSubmit(c, scopeExam) }

// ---------- 共享「我的提交」 ----------

func (h *Handlers) roundMySubmissions(c *gin.Context, s roundScope) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if s.Kind == repo.KindExam {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member {
			fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
			return
		}
	}
	revealed := ct.Published || uid == ct.CreatedBy || role == repo.RoleRoot ||
		(!ct.HideDuring && ct.Status(time.Now()) == "running")

	subs, err := h.repo.RoundSubmissions(ctx, s.Kind, ct.ID, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取提交失败")
		return
	}
	items := make([]gin.H, 0, len(subs))
	for _, s := range subs {
		if revealed {
			items = append(items, submissionJSON(s, s.UID == uid))
			continue
		}
		// 隐藏期内不出分（状态/得分不返回），但代码是本人写的，随提交带回去供回看
		items = append(items, gin.H{
			"id": s.ID, "pid": s.ProblemID, "created_at": s.CreatedAt,
			"language": s.Language, "code": s.Code,
		})
	}
	ok(c, gin.H{"revealed": revealed, "list": items})
}

// ContestMySubmissions 我的比赛提交：赛时只显示提交次数（不出分），隐藏期结束或组织者可见全部。
func (h *Handlers) ContestMySubmissions(c *gin.Context) { h.roundMySubmissions(c, scopeContest) }

// ExamMySubmissions 我的考试提交：考时只显示提交次数（不出分），组织者/root 或发布后可见全部。
func (h *Handlers) ExamMySubmissions(c *gin.Context) { h.roundMySubmissions(c, scopeExam) }

// ---------- 共享「用户提交明细」（榜单单元格点看） ----------

// roundUserSubmissions 查看指定用户在某比赛/考试某题的全部提交。
// 门槛：组织者本人 ∨「查看用户提交明细」权限位（root 恒过）。
func (h *Handlers) roundUserSubmissions(c *gin.Context, s roundScope) {
	uid, _, _, logged := currentUser(c)
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
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if uid != ct.CreatedBy && !hasPerm(c, repo.PermSubmissionViewUser) {
		fail(c, http.StatusForbidden, "需要「查看用户提交明细」权限或为该"+s.Name+"组织者")
		return
	}
	subs, err := h.repo.RoundUserProblemSubmissions(ctx, s.Kind, ct.ID, targetUID, pid)
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

// ContestUserSubmissions 比赛「用户提交明细」。
func (h *Handlers) ContestUserSubmissions(c *gin.Context) { h.roundUserSubmissions(c, scopeContest) }

// ExamUserSubmissions 考试「用户提交明细」。
func (h *Handlers) ExamUserSubmissions(c *gin.Context) { h.roundUserSubmissions(c, scopeExam) }

// ---------- 共享榜单 ----------

// roundFullScores 每题满分（pid → score）；未配置/非正数按 100 兜底（与 ProblemScore 口径一致）。
func roundFullScores(ct *repo.Contest) map[int64]int {
	out := make(map[int64]int, len(ct.Problems))
	for _, cp := range ct.Problems {
		if cp.Score > 0 {
			out[cp.PID] = cp.Score
		} else {
			out[cp.PID] = 100
		}
	}
	return out
}

func (h *Handlers) roundLeaderboard(c *gin.Context, s roundScope) {
	uid, _, role, logged := currentUser(c)
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if s.Kind == repo.KindExam {
		if member, _ := h.repo.IsMember(ctx, ct.ClassID, uid); !member {
			fail(c, http.StatusForbidden, "该考试仅限本班成员参加")
			return
		}
	}
	isOrganizer := logged && (uid == ct.CreatedBy || role == repo.RoleRoot)
	if !isOrganizer {
		if !ct.Scoreboard {
			fail(c, http.StatusForbidden, "本场未开启成绩表")
			return
		}
		// 默认 OI：发布后可见；关闭「赛时隐藏成绩」时赛中实时可见（赛后隐藏期仍按发布态）。
		live := ct.Status(time.Now()) == "running" && !ct.HideDuring
		if !ct.Published && !live {
			fail(c, http.StatusForbidden, "榜单暂不公开")
			return
		}
	}

	cacheKey := s.Kind + "-" + strconv.FormatInt(ct.ID, 10)
	h.leaderMu.Lock()
	entry, cached := h.leaderCache[cacheKey]
	h.leaderMu.Unlock()
	if !cached || (!ct.Published && time.Since(entry.at) > 30*time.Second) {
		rows, err := h.repo.RoundLeaderboard(ctx, s.Kind, ct.ID, roundFullScores(ct))
		if err != nil {
			fail(c, http.StatusInternalServerError, "计算榜单失败")
			return
		}
		items := make([]gin.H, 0, len(rows))
		for i, r := range rows {
			probs := make([]gin.H, 0, len(ct.Problems))
			for _, cp := range ct.Problems {
				cell := r.Problems[cp.PID]
				if cell == nil {
					probs = append(probs, gin.H{"pid": cp.PID, "score": 0, "tries": 0, "errors": 0})
					continue
				}
				probs = append(probs, gin.H{"pid": cp.PID, "score": cell.Score, "tries": cell.Tries, "errors": cell.Errors})
			}
			item := gin.H{
				"rank":     i + 1,
				"uid":      r.UID,
				"username": r.Username,
				"nickname": r.Nickname,
				"total":    r.Total,
				"problems": probs,
			}
			if r.HasScoring {
				item["last_scoring_at"] = r.LastScoring
			}
			items = append(items, item)
		}
		entry = leaderEntry{at: time.Now(), published: ct.Published, rows: items}
		h.leaderMu.Lock()
		h.leaderCache[cacheKey] = entry
		h.leaderMu.Unlock()
	}
	ok(c, entry.rows)
}

// ContestLeaderboard 比赛榜单：赛时与隐藏期内仅组织者/root 可见。
func (h *Handlers) ContestLeaderboard(c *gin.Context) { h.roundLeaderboard(c, scopeContest) }

// ExamLeaderboard 考试榜单：考时与隐藏期内仅组织者/root 可见。
func (h *Handlers) ExamLeaderboard(c *gin.Context) { h.roundLeaderboard(c, scopeExam) }

// ---------- 成绩导出 CSV（v0.5.6 老师教学闭环） ----------

// roundExport 比赛/考试成绩单导出（organizer/root）：
// 列 = 排名/昵称/各题得分/总分，行序与榜单一致；昵称为空回退用户名。
// 直接现算榜单不走 leaderCache（导出要的是导出时刻的准确数据）。
func (h *Handlers) roundExport(c *gin.Context, s roundScope) {
	uid, _, role, logged := currentUser(c)
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, s.Kind, id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, s.Name+"不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取"+s.Name+"失败")
		return
	}
	if !logged || (uid != ct.CreatedBy && role != repo.RoleRoot) {
		fail(c, http.StatusForbidden, "仅组织者可导出成绩")
		return
	}
	rows, err := h.repo.RoundLeaderboard(ctx, s.Kind, ct.ID, roundFullScores(ct))
	if err != nil {
		fail(c, http.StatusInternalServerError, "计算榜单失败")
		return
	}
	// 展示名统一用昵称（空则回退用户名），不再读取真实姓名
	header := []string{"排名", "昵称"}
	for _, cp := range ct.Problems {
		title := ""
		if p, err := h.repo.GetProblem(ctx, cp.PID); err == nil {
			title = p.Title
		}
		if title == "" {
			title = fmt.Sprintf("P%d", cp.PID) // 题目已删除时以题号兜底
		}
		header = append(header, title)
	}
	header = append(header, "总分")
	out := [][]string{header}
	for i, r := range rows {
		name := r.Nickname
		if name == "" {
			name = r.Username
		}
		row := []string{strconv.Itoa(i + 1), name}
		for _, cp := range ct.Problems {
			score := 0
			if cell := r.Problems[cp.PID]; cell != nil {
				score = cell.Score
			}
			row = append(row, strconv.Itoa(score))
		}
		row = append(row, strconv.Itoa(r.Total))
		out = append(out, row)
	}
	serveCSV(c, fmt.Sprintf("%s_%s.csv", csvSafeName(ct.Title), time.Now().Format("2006-01-02")), out)
}

// ContestExport 比赛成绩导出。
func (h *Handlers) ContestExport(c *gin.Context) { h.roundExport(c, scopeContest) }

// ExamExport 考试成绩导出。
func (h *Handlers) ExamExport(c *gin.Context) { h.roundExport(c, scopeExam) }

// ---------- 考试列表（班级维度） ----------

// ListClassExams 班级考试列表；非本班成员返回空列表。
func (h *Handlers) ListClassExams(c *gin.Context) {
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
	list, err := h.repo.ListClassExams(c.Request.Context(), classID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取考试失败")
		return
	}
	now := time.Now()
	items := make([]gin.H, 0, len(list))
	for _, e := range list {
		item := gin.H{
			"id":            e.ID,
			"title":         e.Title,
			"intro":         e.Intro,
			"status":        e.Status(now),
			"start":         e.Start,
			"end":           e.End,
			"problem_count": len(e.Problems),
			"is_organizer":  uid == e.CreatedBy,
		}
		if !e.HiddenUntil.IsZero() {
			item["hidden_until"] = e.HiddenUntil
		}
		items = append(items, item)
	}
	ok(c, gin.H{"visible": true, "list": items})
}
