package server

// AI 生成作业/考试总结（v0.97）：老师一键把作业/考试的精确统计交给 GLM，产出
// 微信群风格的课堂小结（emoji 分段 + 点名表扬 + 课后作业鼓励），前端弹窗复制即走，
// 不落库（即用即走，重新生成即换一版）。分工铁律：数字与人名全部由服务端实算进
// prompt，AI 只负责行文，系统提示里明令不得编造。外呼复用 tidyAIChat（审计
// kind=summary_*，cost=0，不占学生每日上限——这是教师侧功能）。
//
// 点名表（用户提案：否则 AI 会把建班人当学生表扬、把缺席同学写进完成情况）：
// GET /rollcall 下发全班学生名册（老师集合服务端权威判定后剔除，老师=建班人∨班级
// 管理员∨root/admin 用户角色），默认勾选=至少做出 1 题；生成请求携带勾选 uids，
// 后端无论如何强制剔除老师，题目块的班内统计也只算勾选学生——名单外的人 AI 拿不到
// 数据自然无法提及，系统提示再补一道「名单外不得提及」的明令。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/ai"
	"soj/internal/repo"
)

// 有界参数：prompt 与输出一律有帽（后端性能要求「一切有界」）。
const (
	summaryMaxStudents   = 300  // 学生成绩行上限，超出截断并注明
	summaryNotesMaxRunes = 500  // 教师补充说明上限
	summaryIntroMaxRunes = 800  // 作业/考试说明进 prompt 的上限（Markdown 原文截断）
	summaryOutMaxRunes   = 4000 // 模型输出兜底截断
)

// summaryReq 生成请求体：notes 教师补充说明选填（如今天课上额外强调的点）；
// uids = 点名表勾选名单（空 = 默认口径：非老师且至少做出 1 题 / 考试=有提交的非老师）。
type summaryReq struct {
	Notes string  `json:"notes"`
	UIDs  []int64 `json:"uids"`
}

// summaryProblem 题目块一行所需的全部事实。
type summaryProblem struct {
	PID        int64
	Idx        int    // 场次内序号（T1..Tn，学生行得分明细用同一序号）
	Title      string // 题已删除时以题号兜底
	Difficulty int    // 0 = 未设置，行内省略
	Tags       []string
	FullScore  int
	AttemptN   int // 计入学生中做过人数
	SolvedN    int // 计入学生中满分人数（作业即通过人数，每题满分恒 100）
	AvgScore   int // 计入学生平均分（考试专用，四舍五入）
	ShowAvg    bool
	SiteTotal  int64 // 全网提交数（公开态）
	SiteAC     int64 // 全网 AC 数
}

// line 渲染题目块一行（「｜」分隔的紧凑事实行）。
func (p summaryProblem) line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "T%d《%s》", p.Idx, p.Title)
	if p.Difficulty > 0 {
		fmt.Fprintf(&b, "｜难度D%d", p.Difficulty)
	}
	if len(p.Tags) > 0 {
		fmt.Fprintf(&b, "｜知识点：%s", strings.Join(p.Tags, "、"))
	}
	fmt.Fprintf(&b, "｜满分%d", p.FullScore)
	fmt.Fprintf(&b, "｜班里%d人做过、%d人满分", p.AttemptN, p.SolvedN)
	if p.ShowAvg && p.AttemptN > 0 {
		fmt.Fprintf(&b, "｜平均分%d", p.AvgScore)
	}
	if p.SiteTotal > 0 {
		fmt.Fprintf(&b, "｜全网通过率%.0f%%（%d份提交）", float64(p.SiteAC)/float64(p.SiteTotal)*100, p.SiteTotal)
	}
	return b.String()
}

// summaryDisplayName 展示名：昵称空回退用户名（全站统一口径）。
func summaryDisplayName(nickname, username string) string {
	if nickname != "" {
		return nickname
	}
	return username
}

// summaryTagNameMap 知识树 ID → 名（一次整树遍历建 map，题目块用）。
func summaryTagNameMap(tree []repo.TagNode) map[int64]string {
	m := make(map[int64]string)
	var walk func(ns []repo.TagNode)
	walk = func(ns []repo.TagNode) {
		for _, n := range ns {
			m[n.ID] = n.Name
			walk(n.Children)
		}
	}
	walk(tree)
	return m
}

// summaryScoreNotes 学生行得分明细（只列 >0 分的题，T 序号与题目块对齐）。
func summaryScoreNotes(cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	return "（" + strings.Join(cells, " ") + "）"
}

// buildSummaryProblems 组装题目块（纯函数）：标题/难度来自题目摘要，知识点名由
// 知识树解析，班内统计由调用方按勾选名单预聚合，全网通过率来自公开态提交聚合。
func buildSummaryProblems(pids []int64, sums map[int64]*repo.ProblemSummary, tagNames map[int64]string,
	site map[int64]repo.ProblemStat, full map[int64]int, attemptN, solvedN map[int64]int, avg map[int64]int, showAvg bool) []summaryProblem {
	out := make([]summaryProblem, 0, len(pids))
	for i, pid := range pids {
		p := summaryProblem{PID: pid, Idx: i + 1, FullScore: 100, ShowAvg: showAvg}
		if full != nil {
			p.FullScore = full[pid]
		}
		if s := sums[pid]; s != nil {
			p.Title, p.Difficulty = s.Title, s.Difficulty
			for _, id := range s.Tags {
				if name := tagNames[id]; name != "" {
					p.Tags = append(p.Tags, name)
				}
			}
		}
		if p.Title == "" {
			p.Title = fmt.Sprintf("P%d", pid) // 题目已删除时以题号兜底（导出同口径）
		}
		p.AttemptN, p.SolvedN = attemptN[pid], solvedN[pid]
		if showAvg {
			p.AvgScore = avg[pid]
		}
		if st := site[pid]; st.Total > 0 {
			p.SiteTotal, p.SiteAC = st.Total, st.Accepted
		}
		out = append(out, p)
	}
	return out
}

// summaryAssignStudentLines 作业学生成绩行（ProgressRow 已按名次排好）。
// 返回行列表与做过题的人数（0 提交 guard 用）。
func summaryAssignStudentLines(rows []*repo.ProgressRow, pids []int64) ([]string, int) {
	attempted := 0
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		name := summaryDisplayName(r.Nickname, r.Username)
		var cells []string
		if r.Attempt > 0 {
			attempted++
			for i, pid := range pids {
				if cell := r.Cells[pid]; cell != nil && cell.Best > 0 {
					cells = append(cells, fmt.Sprintf("T%d:%d", i+1, cell.Best))
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s：总分%d，通过%d/%d题%s",
			r.Rank, name, r.Total, r.Solved, len(pids), summaryScoreNotes(cells)))
	}
	return lines, attempted
}

// summaryExamStudentLines 考试榜单成绩行（RoundLeaderboard 现算，行序即名次）。
func summaryExamStudentLines(rows []*repo.LeaderRow, pids []int64) []string {
	lines := make([]string, 0, len(rows))
	for i, r := range rows {
		name := summaryDisplayName(r.Nickname, r.Username)
		var cells []string
		for j, pid := range pids {
			if cell := r.Problems[pid]; cell != nil && cell.Score > 0 {
				cells = append(cells, fmt.Sprintf("T%d:%d", j+1, cell.Score))
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s：总分%d%s", i+1, name, r.Total, summaryScoreNotes(cells)))
	}
	return lines
}

// summarySystem 系统提示：微信群小结的排版与风格契约 + 数据铁律。示例里的名字全部虚构。
func summarySystem() string {
	return strings.TrimSpace(`你是一位受学生欢迎的编程班老师，要发一段班级微信群小结。请根据用户消息里给出的「精确数据」，写一段小结正文。

排版铁律（微信群纯文本）：
- 每个段落标题（🌟【…】📖【…】📊【…】📌【…】）都独占一行，内容从下一行开始写
- 段落与段落之间空一行
- 段落里并列的多个内容点用 1、2、3 分点，每点独占一行

结构与风格（可适量 emoji）：
1. 开头：🌟【标题】独占一行——作业固定写「今日课堂小结」，考试固定写「考试小结」，不要自创标题写法；下一行起一句开场（可先给一行从题目内容提炼的主题副题）
2. 📖【课堂重点】（作业）或📊【考情速览】（考试）：优先依据数据里的「作业说明/考试说明」（老师写的介绍）归纳本次知识主题；没有说明就从题目标题与知识点归纳，用通俗的话串讲；多个知识主题分 1、2、3 点
3. 📊【完成情况】：点名表扬——第一名（成绩+名次）、在全网通过率低的题上拿高分的同学、满分/全部完成的同学群像；人多时点名 3~5 个代表，其余用「等 N 名同学」概括；多项亮点分点写
4. 📌【课后作业】（作业）或一句展望（考试）：没做完的题鼓励课后在 OJ 上继续完成；多条安排分点写；末尾再来一句温暖收尾

数据铁律：
- 所有数字（分数、人数、名次、通过率）和人名只能来自给定数据，一个都不许编造
- 学生名单就是本次到场的全部学生；名单之外的人（包括老师、缺席的同学）一律不得提及、点名或评价
- 学生一律用数据里给的展示名；成绩靠后或没做题的不点名批评，只写进课后作业段鼓励
- 全文 300~600 字；纯文本，不要 markdown 标记、不要代码块、不要复述数据清单
- 直接输出小结正文，不要任何前置说明或解释

口吻示例（数据与人名均为虚构，仅示意语气与排版）：
🌟【今日课堂小结】
循环语句初体验——打擂台找最值 + 经典案例
第一次接触循环语句，大家迈出了漂亮的一步～

📖【课堂重点】
1. for 循环三件套：起点、终点、每走一步——像操场跑圈，数着圈数跑到终点就停～
2. 循环变量忘记更新就会「无限跑圈」，调试时先查它～

📊【完成情况】
小星同学 3 题全部完成高居第一！全网通过率仅 15% 的「数星星」她也拿下满分，全班独一份✅ 小明、小刚等 6 名同学各过 2 题，稳步推进～

📌【课后作业】
1. 课上没做完的题就是本周作业，登录 OJ 继续完成哦✍️
2. 卡住了随时找我🤝`)
}

// summaryUser 组用户消息：数据块（场景 + 说明 + 题目事实 + 学生成绩 + 教师补充说明）。
func summaryUser(scene, dateLine, introLabel, intro string, probs []summaryProblem, students []string, studentCount int, notes string) string {
	var b strings.Builder
	b.WriteString("【精确数据】（只能使用以下数据行文）\n")
	b.WriteString(scene + "\n")
	b.WriteString("日期：" + dateLine + "\n")
	if intro = summaryCapRunes(intro, summaryIntroMaxRunes); intro != "" {
		fmt.Fprintf(&b, "%s（老师写的介绍，课堂重点优先依据它归纳）：\n%s\n", introLabel, intro)
	}
	fmt.Fprintf(&b, "题目情况（共%d题）：\n", len(probs))
	for _, p := range probs {
		b.WriteString(p.line() + "\n")
	}
	fmt.Fprintf(&b, "学生成绩（计入%d人，按名次排列）：\n", studentCount)
	for _, l := range students {
		b.WriteString(l + "\n")
	}
	if notes != "" {
		b.WriteString("教师补充说明：" + notes + "\n")
	}
	return b.String()
}

// summaryCapRunes trim + 按字数截断（教师补充说明与作业/考试说明共用）。
func summaryCapRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// summaryNotesIn 归一化教师补充说明（trim + 截断）。
func summaryNotesIn(notes string) string {
	return summaryCapRunes(notes, summaryNotesMaxRunes)
}

// summarizeOutput 归一化模型输出：剥可能的 ``` 围栏 + trim + 兜底截断。
func summarizeOutput(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		lines = lines[1:]
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	r := []rune(s)
	if len(r) > summaryOutMaxRunes {
		r = r[:summaryOutMaxRunes]
	}
	return string(r)
}

// summaryRespond 外呼 + 归一化输出（作业/考试共用出口）。
func (h *Handlers) summaryRespond(c *gin.Context, uid int64, kind, system, user string) {
	resp, err := h.tidyAIChat(c.Request.Context(), uid, kind, 0, system, user, false)
	if err != nil {
		if errors.Is(err, ai.ErrNoProvider) {
			fail(c, http.StatusBadRequest, "AI 未配置：请先在后台「AI 设置」配置可用的提供方")
			return
		}
		fail(c, http.StatusInternalServerError, "AI 生成失败："+truncateTail(err.Error(), 200))
		return
	}
	ok(c, gin.H{"summary": summarizeOutput(resp.Content)})
}

// summaryTeacherSet 老师集合（点名表剔除的权威判定，生成端点强制复用）：
// 建班人 ∨ 班级管理员（class role admin）∨ root/admin 用户角色。users = 成员批量用户表。
func summaryTeacherSet(cl *repo.Class, members []repo.ClassMember, users map[int64]*repo.User) map[int64]bool {
	teachers := make(map[int64]bool, 4)
	teachers[cl.OwnerID] = true
	for _, m := range members {
		if m.Role == repo.ClassRoleAdmin {
			teachers[m.UID] = true
		}
		if u := users[m.UID]; u != nil && (u.Role == repo.RoleRoot || u.Role == repo.RoleAdmin) {
			teachers[m.UID] = true
		}
	}
	return teachers
}

// summaryMemberUsers 成员批量用户表（老师判定与缺席学生展示名共用一次批查）。
func (h *Handlers) summaryMemberUsers(ctx context.Context, members []repo.ClassMember) map[int64]*repo.User {
	uids := make([]int64, 0, len(members))
	for _, m := range members {
		uids = append(uids, m.UID)
	}
	users, _ := h.repo.GetUsersByIDs(ctx, uids) // 失败为 nil：老师判定退化为建班人∨班级管理员
	return users
}

// summaryAssignLoad 作业总结/点名表共用装配：作业 → 班级 → 门禁 → 成员 → 全员完成情况。
// done=true 表示响应已写（404/403/500），调用方直接 return。
func (h *Handlers) summaryAssignLoad(c *gin.Context, id int64) (*repo.Assignment, *repo.Class, []repo.ClassMember, []*repo.ProgressRow, bool) {
	ctx := c.Request.Context()
	a, err := h.repo.GetAssignment(ctx, id)
	if respondRepoErr(c, err, "作业不存在", "读取作业失败") {
		return nil, nil, nil, nil, true
	}
	cl, cerr := h.repo.GetClass(ctx, a.ClassID)
	if cerr != nil || !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可生成总结")
		return nil, nil, nil, nil, true
	}
	members, err := h.repo.ListMembers(ctx, a.ClassID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取成员失败")
		return nil, nil, nil, nil, true
	}
	uids := make([]int64, 0, len(members))
	for _, m := range members {
		uids = append(uids, m.UID)
	}
	rows, err := h.repo.AssignmentProgress(ctx, a, uids)
	if err != nil {
		fail(c, http.StatusInternalServerError, "统计失败")
		return nil, nil, nil, nil, true
	}
	return a, cl, members, rows, false
}

// summaryExamLoad 考试总结/点名表共用装配：考试（组织者/root）→ 现算榜单（不走缓存，
// 导出同口径）→ 班级 → 成员。done=true 表示响应已写。
func (h *Handlers) summaryExamLoad(c *gin.Context, id int64) (*repo.Contest, []*repo.LeaderRow, *repo.Class, []repo.ClassMember, bool) {
	uid, _, role, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return nil, nil, nil, nil, true
	}
	ctx := c.Request.Context()
	ct, err := h.repo.GetRound(ctx, repo.KindExam, id)
	if respondRepoErr(c, err, "考试不存在", "读取考试失败") {
		return nil, nil, nil, nil, true
	}
	if uid != ct.CreatedBy && role != repo.RoleRoot {
		fail(c, http.StatusForbidden, "仅组织者可生成总结")
		return nil, nil, nil, nil, true
	}
	rows, err := h.repo.RoundLeaderboard(ctx, repo.KindExam, ct.ID, roundFullScores(ct), roundPIDs(ct))
	if err != nil {
		fail(c, http.StatusInternalServerError, "计算榜单失败")
		return nil, nil, nil, nil, true
	}
	cl, _ := h.repo.GetClass(ctx, ct.ClassID)
	members, _ := h.repo.ListMembers(ctx, ct.ClassID)
	return ct, rows, cl, members, false
}

// summaryCutTeachers 从 uid 集合中剔除老师（生成端点强制执行，前端漏勾也拦得住）。
func summaryCutTeachers(uids []int64, teachers map[int64]bool) []int64 {
	out := uids[:0:0]
	for _, uid := range uids {
		if !teachers[uid] {
			out = append(out, uid)
		}
	}
	return out
}

// summaryTrimStudents 学生行超帽截断（summaryMaxStudents 防 prompt 无界）。
func summaryTrimStudents(lines []string, total int) []string {
	if len(lines) <= summaryMaxStudents {
		return lines
	}
	return append(lines[:summaryMaxStudents], fmt.Sprintf("……另有%d人成绩从略", total-summaryMaxStudents))
}

// AssignmentSummary AI 生成作业小结（班级管理员/root，与成绩导出同门禁）。
func (h *Handlers) AssignmentSummary(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req summaryReq
	_ = c.ShouldBindJSON(&req)
	notes := summaryNotesIn(req.Notes)

	a, cl, members, rows, done := h.summaryAssignLoad(c, id)
	if done {
		return
	}
	teachers := summaryTeacherSet(cl, members, h.summaryMemberUsers(c.Request.Context(), members))
	// 点名表应用：uids 非空只保留勾选者（老师强制剔除）；空 = 默认非老师且至少做出 1 题
	byUID := make(map[int64]*repo.ProgressRow, len(rows))
	for _, r := range rows {
		byUID[r.UID] = r
	}
	var selected []*repo.ProgressRow
	if len(req.UIDs) > 0 {
		for _, u := range summaryCutTeachers(req.UIDs, teachers) {
			if r := byUID[u]; r != nil {
				selected = append(selected, r)
			}
		}
		if len(selected) == 0 {
			fail(c, http.StatusBadRequest, "点名表中没有可计入的学生")
			return
		}
	} else {
		for _, r := range rows {
			if !teachers[r.UID] && r.Attempt > 0 {
				selected = append(selected, r)
			}
		}
		if len(selected) == 0 {
			fail(c, http.StatusBadRequest, "还没有同学提交，暂无可总结的数据")
			return
		}
	}

	// 班内每题做过/满分人数：只算勾选学生（老师/缺席者的提交不进统计）
	attemptN := make(map[int64]int, len(a.Problems))
	solvedN := make(map[int64]int, len(a.Problems))
	for _, r := range selected {
		for _, pid := range a.Problems {
			if cell := r.Cells[pid]; cell != nil {
				if cell.Attempt {
					attemptN[pid]++
				}
				if cell.Solved {
					solvedN[pid]++
				}
			}
		}
	}
	probs, err := h.summaryProblemBlock(c.Request.Context(), a.Problems, nil, attemptN, solvedN, nil, false)
	if err != nil {
		fail(c, http.StatusInternalServerError, "组装题目数据失败")
		return
	}
	students, _ := summaryAssignStudentLines(selected, a.Problems)
	students = summaryTrimStudents(students, len(selected))
	scene := fmt.Sprintf("班级：%s · 作业「%s」（共%d题，每题满分100；计入学生%d人，名单=老师点名勾选）",
		cl.Name, a.Title, len(a.Problems), len(selected))
	h.summaryRespond(c, uid, "summary_assign", summarySystem(),
		summaryUser(scene, time.Now().Format("2006-01-02"), "作业说明", a.Intro, probs, students, len(selected), notes))
}

// AssignmentRollcall 点名表（班级管理员/root）：全班学生名册 + 默认勾选。
// 老师集合服务端判定后整行剔除；默认勾选 = 至少做出 1 题（做过 0 题的到课与否由老师手勾）。
func (h *Handlers) AssignmentRollcall(c *gin.Context) {
	if _, _, _, logged := currentUser(c); !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	a, cl, members, rows, done := h.summaryAssignLoad(c, id)
	if done {
		return
	}
	teachers := summaryTeacherSet(cl, members, h.summaryMemberUsers(c.Request.Context(), members))
	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		if teachers[r.UID] {
			continue
		}
		items = append(items, gin.H{
			"uid": r.UID, "name": summaryDisplayName(r.Nickname, r.Username),
			"hint":    fmt.Sprintf("已做%d/%d题，通过%d题", r.Attempt, len(a.Problems), r.Solved),
			"checked": r.Attempt > 0,
		})
	}
	ok(c, gin.H{"list": items})
}

// ExamSummary AI 生成考试小结（组织者/root，与成绩导出同门禁）。
func (h *Handlers) ExamSummary(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	var req summaryReq
	_ = c.ShouldBindJSON(&req)
	notes := summaryNotesIn(req.Notes)

	ct, rows, cl, members, done := h.summaryExamLoad(c, id)
	if done {
		return
	}
	pids := roundPIDs(ct)
	full := roundFullScores(ct)
	teachers := summaryTeacherSet(cl, members, h.summaryMemberUsers(c.Request.Context(), members))
	// 点名表应用：uids 非空只保留勾选者（老师强制剔除）；空 = 默认非老师且有提交者
	byUID := make(map[int64]*repo.LeaderRow, len(rows))
	for _, r := range rows {
		byUID[r.UID] = r
	}
	var selected []*repo.LeaderRow
	if len(req.UIDs) > 0 {
		for _, u := range summaryCutTeachers(req.UIDs, teachers) {
			if r := byUID[u]; r != nil {
				selected = append(selected, r)
			}
		}
		if len(selected) == 0 {
			fail(c, http.StatusBadRequest, "点名表中没有可计入的学生")
			return
		}
	} else {
		for _, r := range rows {
			if !teachers[r.UID] {
				selected = append(selected, r)
			}
		}
		if len(selected) == 0 {
			fail(c, http.StatusBadRequest, "还没有同学提交，暂无可总结的数据")
			return
		}
	}

	// 班内每题做过/满分人数与平均分：只算勾选学生
	attemptN := make(map[int64]int, len(pids))
	solvedN := make(map[int64]int, len(pids))
	sum := make(map[int64]int64, len(pids))
	for _, r := range selected {
		for _, pid := range pids {
			if cell := r.Problems[pid]; cell != nil {
				attemptN[pid]++
				sum[pid] += int64(cell.Score)
				if cell.Score >= full[pid] {
					solvedN[pid]++
				}
			}
		}
	}
	avg := make(map[int64]int, len(pids))
	for pid, n := range attemptN {
		if n > 0 {
			avg[pid] = int((sum[pid] + int64(n)/2) / int64(n)) // 四舍五入
		}
	}
	students := summaryTrimStudents(summaryExamStudentLines(selected, pids), len(selected))
	probs, err := h.summaryProblemBlock(c.Request.Context(), pids, full, attemptN, solvedN, avg, true)
	if err != nil {
		fail(c, http.StatusInternalServerError, "组装题目数据失败")
		return
	}
	className := ""
	if cl != nil {
		className = cl.Name
	}
	scene := fmt.Sprintf("班级：%s · 考试「%s」（%s ~ %s，共%d题；计入学生%d人，名单=老师点名勾选）", className, ct.Title,
		ct.Start.Format("2006-01-02 15:04"), ct.End.Format("2006-01-02 15:04"), len(pids), len(selected))
	h.summaryRespond(c, uid, "summary_exam", summarySystem(),
		summaryUser(scene, time.Now().Format("2006-01-02"), "考试说明", ct.Intro, probs, students, len(selected), notes))
}

// ExamRollcall 点名表（组织者/root）：全班学生名册 + 默认勾选。
// 有提交的（榜单行）默认勾选，无提交的列出但默认不勾（到课与否由老师手勾）；老师整行剔除。
// 展示名：榜单行自带昵称回退，缺席学生用成员用户表昵称回退（同一次批查的结果）。
func (h *Handlers) ExamRollcall(c *gin.Context) {
	if _, _, _, logged := currentUser(c); !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	_, rows, cl, members, done := h.summaryExamLoad(c, id)
	if done {
		return
	}
	if cl == nil {
		cl = &repo.Class{}
	}
	users := h.summaryMemberUsers(c.Request.Context(), members)
	teachers := summaryTeacherSet(cl, members, users)
	participant := make(map[int64]*repo.LeaderRow, len(rows))
	for _, r := range rows {
		participant[r.UID] = r
	}
	checked := make([]gin.H, 0, len(rows))
	unchecked := make([]gin.H, 0, len(members))
	for _, m := range members {
		if teachers[m.UID] {
			continue
		}
		if r := participant[m.UID]; r != nil {
			checked = append(checked, gin.H{
				"uid": m.UID, "name": summaryDisplayName(r.Nickname, r.Username),
				"hint": fmt.Sprintf("总分%d", r.Total), "checked": true,
			})
		} else {
			name := m.Uname
			if u := users[m.UID]; u != nil {
				name = summaryDisplayName(u.Nickname, u.Username)
			}
			unchecked = append(unchecked, gin.H{
				"uid": m.UID, "name": name, "hint": "未提交", "checked": false,
			})
		}
	}
	items := append(checked, unchecked...)
	ok(c, gin.H{"list": items})
}

// summaryProblemBlock 组题目块：题目摘要 + 知识点名 + 全网通过率（作业/考试共用）。
func (h *Handlers) summaryProblemBlock(ctx context.Context, pids []int64, full map[int64]int,
	attemptN, solvedN map[int64]int, avg map[int64]int, showAvg bool) ([]summaryProblem, error) {
	sums, err := h.repo.ProblemSummariesByIDs(ctx, pids)
	if err != nil {
		return nil, err
	}
	tree, err := h.repo.GetTagTree(ctx)
	if err != nil {
		return nil, err
	}
	site, err := h.repo.ListProblemStats(ctx, pids)
	if err != nil {
		return nil, err
	}
	return buildSummaryProblems(pids, sums, summaryTagNameMap(tree), site, full, attemptN, solvedN, avg, showAvg), nil
}
