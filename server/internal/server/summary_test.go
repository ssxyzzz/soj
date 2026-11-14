package server

// AI 生成作业/考试总结（v0.97）契约测试：纯函数（题目块事实行 / 学生成绩行 / 输出与
// 补充说明归一化 / 系统提示契约）+ handler 门禁链（作业=班级管理员、考试=组织者/root、
// 0 提交 guard、AI 未配置路径——fake aiCfg 无启用档即命中 ErrNoProvider 分支）。

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/repo"
)

// fakeRepo 补作业总结方法（本文件局部覆盖）：题目摘要从 f.problems 派生，
// 考试榜单从 f.roundSubs 派生（每 (uid,pid) 取最高分，总分降序）。
func (f *fakeRepo) GetAssignment(_ context.Context, id int64) (*repo.Assignment, error) {
	if a, ok := f.assignments[id]; ok {
		return a, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) AssignmentProgress(_ context.Context, _ *repo.Assignment, _ []int64) ([]*repo.ProgressRow, error) {
	return f.progressRows, nil
}

func (f *fakeRepo) ProblemSummariesByIDs(_ context.Context, pids []int64) (map[int64]*repo.ProblemSummary, error) {
	out := map[int64]*repo.ProblemSummary{}
	for _, pid := range pids {
		if p, ok := f.problems[pid]; ok {
			out[pid] = &repo.ProblemSummary{ID: p.ID, Title: p.Title, Difficulty: p.Difficulty, Tags: p.Tags}
		}
	}
	return out, nil
}

func (f *fakeRepo) RoundLeaderboard(_ context.Context, kind string, roundID int64, _ map[int64]int, _ []int64) ([]*repo.LeaderRow, error) {
	if kind != repo.KindExam {
		return nil, nil
	}
	best := map[[2]int64]int{}
	for _, s := range f.roundSubs {
		if s.ExamID != roundID {
			continue
		}
		key := [2]int64{s.UID, s.ProblemID}
		if s.Score > best[key] {
			best[key] = s.Score
		}
	}
	totals := map[int64]int{}
	seen := map[int64]bool{}
	for key, score := range best {
		totals[key[0]] += score
		seen[key[0]] = true
	}
	var rows []*repo.LeaderRow
	for uid := range seen {
		r := &repo.LeaderRow{UID: uid, Username: "stu", Total: totals[uid], Problems: map[int64]*repo.LeaderCell{}}
		if u := f.users[uid]; u != nil {
			r.Username, r.Nickname = u.Username, u.Nickname
		}
		for key, score := range best {
			if key[0] == uid {
				r.Problems[key[1]] = &repo.LeaderCell{Score: score}
			}
		}
		rows = append(rows, r)
	}
	for i := 0; i < len(rows); i++ { // 小数据量插入排序：总分降序
		for j := i + 1; j < len(rows); j++ {
			if rows[j].Total > rows[i].Total {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	return rows, nil
}

func summaryEngine(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api", AuthRequired(h))
	authed.POST("/assignments/:id/summary", h.AssignmentSummary)
	authed.POST("/exams/:id/summary", h.ExamSummary)
	return r
}

func TestSummaryProblemBlockLine(t *testing.T) {
	pids := []int64{11, 22}
	sums := map[int64]*repo.ProblemSummary{
		11: {ID: 11, Title: "打擂台找最值", Difficulty: 3, Tags: []int64{5}},
		// 22 缺项 = 题已删除
	}
	tagNames := map[int64]string{5: "分支"}
	site := map[int64]repo.ProblemStat{11: {Total: 100, Accepted: 20}}
	attemptN := map[int64]int{11: 30, 22: 5}
	solvedN := map[int64]int{11: 10}

	probs := buildSummaryProblems(pids, sums, tagNames, site, nil, attemptN, solvedN, nil, false)
	if len(probs) != 2 {
		t.Fatalf("题目块行数 = %d，期望 2", len(probs))
	}
	l1 := probs[0].line()
	if probs[0].Title != "打擂台找最值" || !strings.Contains(l1, "难度D3") || !strings.Contains(l1, "知识点：分支") {
		t.Fatalf("题目事实行缺标题/难度/知识点：%s", l1)
	}
	if !strings.Contains(l1, "班里30人做过、10人满分") || !strings.Contains(l1, "全网通过率20%（100份提交）") {
		t.Fatalf("题目事实行缺班内/全网统计：%s", l1)
	}
	if probs[1].Title != "P22" || strings.Contains(probs[1].line(), "全网通过率") {
		t.Fatalf("已删除题应以题号兜底且全网无数据不出通过率：%s", probs[1].line())
	}
	// 小样本不给比例只给事实数（防「通过率100%（1份提交）」误导）
	probsSmall := buildSummaryProblems([]int64{33}, map[int64]*repo.ProblemSummary{33: {ID: 33, Title: "小样本题"}},
		nil, map[int64]repo.ProblemStat{33: {Total: 1, Accepted: 1}}, nil, nil, nil, nil, false)
	ls := probsSmall[0].line()
	if !strings.Contains(ls, "全网仅1份提交、通过1份") || strings.Contains(ls, "通过率") {
		t.Fatalf("小样本应只给事实数不出比例：%s", ls)
	}

	// 考试形态：满分 50 + 平均分展示
	probsExam := buildSummaryProblems([]int64{22}, map[int64]*repo.ProblemSummary{22: {ID: 22, Title: "牛吃草"}},
		nil, nil, map[int64]int{22: 50}, attemptN, solvedN, map[int64]int{22: 43}, true)
	l := probsExam[0].line()
	if !strings.Contains(l, "满分50") || !strings.Contains(l, "平均分43") {
		t.Fatalf("考试题目行缺自定义满分/平均分：%s", l)
	}
}

func TestSummaryStudentLines(t *testing.T) {
	pids := []int64{11, 22}
	rows := []*repo.ProgressRow{
		{UID: 1, Username: "ua", Nickname: "昵称A", Rank: 1, Total: 190, Solved: 2, Attempt: 2,
			Cells: map[int64]*repo.ProgressCell{11: {Attempt: true, Best: 100, Solved: true}, 22: {Attempt: true, Best: 90}}},
		{UID: 2, Username: "ub", Rank: 2, Cells: map[int64]*repo.ProgressCell{}}, // 没动笔
	}
	lines, attempted := summaryAssignStudentLines(rows, pids)
	if attempted != 1 {
		t.Fatalf("做过题人数 = %d，期望 1", attempted)
	}
	if !strings.Contains(lines[0], "1. 昵称A：总分190，通过2/2题（T1:100 T2:90）") {
		t.Fatalf("学生行名次应在名单内重新编号且带得分明细：%s", lines[0])
	}
	if !strings.Contains(lines[1], "ub") || strings.Contains(lines[1], "（T") {
		t.Fatalf("昵称应回退用户名且未动笔无明细：%s", lines[1])
	}

	leader := []*repo.LeaderRow{
		{UID: 1, Username: "ua", Nickname: "昵称A", Total: 140, Problems: map[int64]*repo.LeaderCell{11: {Score: 100}, 22: {Score: 40}}},
		{UID: 2, Username: "ub", Total: 0, Problems: map[int64]*repo.LeaderCell{}},
	}
	el := summaryExamStudentLines(leader, pids)
	if !strings.Contains(el[0], "1. 昵称A：总分140（T1:100 T2:40）") || !strings.Contains(el[1], "2. ub：总分0") {
		t.Fatalf("考试学生行渲染不符：%v", el)
	}
}

func TestSummaryOutputAndNotes(t *testing.T) {
	if got := summarizeOutput("  你好呀  "); got != "你好呀" {
		t.Fatalf("trim 失效：%q", got)
	}
	if got := summarizeOutput("```\n小结正文\n```"); got != "小结正文" {
		t.Fatalf("围栏剥离失效：%q", got)
	}
	if got := summarizeOutput(strings.Repeat("字", summaryOutMaxRunes+5)); len([]rune(got)) != summaryOutMaxRunes {
		t.Fatalf("输出截断失效：len=%d", len([]rune(got)))
	}
	if got := summaryNotesIn("  提醒  "); got != "提醒" {
		t.Fatalf("补充说明 trim 失效：%q", got)
	}
	if got := summaryNotesIn(strings.Repeat("补", summaryNotesMaxRunes+1)); len([]rune(got)) != summaryNotesMaxRunes {
		t.Fatalf("补充说明截断失效：len=%d", len([]rune(got)))
	}
}

func TestSummarySystemContract(t *testing.T) {
	// 风格枚举：5 个、ID/Label 齐全、默认回退
	if len(summaryStyles) != 5 {
		t.Fatalf("风格数 = %d，期望 5", len(summaryStyles))
	}
	seen := map[string]bool{}
	for _, s := range summaryStyles {
		if s.ID == "" || s.Label == "" || s.Persona == "" {
			t.Fatalf("风格 %q 字段缺失：%+v", s.ID, s)
		}
		if seen[s.ID] {
			t.Fatalf("风格 ID 重复：%s", s.ID)
		}
		seen[s.ID] = true
	}
	if _, ok := seen[summaryStyleDefault]; !ok {
		t.Fatalf("默认风格 %q 不在枚举里", summaryStyleDefault)
	}
	if got := summaryStyleByID("nope"); got.ID != summaryStyleDefault {
		t.Fatalf("未知风格应回默认，got %s", got.ID)
	}

	// 共享骨架：排版/数据铁律/点名表纪律在各风格下都在
	for _, want := range []string{"只能来自给定数据", "名单之外的人", "300~600", "纯文本", "展示名", "课后作业", "独占一行", "分点", "作业说明", "今日课堂小结", "考试小结", "做题就说「做」", "家长"} {
		for _, s := range summaryStyles {
			if !strings.Contains(summarySystem(s), want) {
				t.Fatalf("风格 %s 的系统提示缺关键契约 %q", s.ID, want)
			}
		}
	}
	// 风格人设差异落进各自 prompt
	if got := summarySystem(summaryStyleByID("lively")); !strings.Contains(got, "活泼亲切") {
		t.Fatal("lively 风格人设未注入")
	}
	if got := summarySystem(summaryStyleByID("formal")); strings.Contains(got, "活泼亲切") {
		t.Fatal("formal 不应带活泼人设")
	}
	// 无说明/无补充时不应出现对应数据行；有说明时整块注入（课堂重点依据它归纳）
	u := summaryUser("班级：九班 · 作业「分支」", "2026-11-14", "作业说明", "", nil, nil, 0, "")
	if strings.Contains(u, "教师补充说明") || strings.Contains(u, "作业说明") {
		t.Fatal("空说明不应出现作业说明行，空补充不应出现教师补充说明行")
	}
	u = summaryUser("班级：九班", "2026-11-14", "作业说明", "本次目标：1）掌握输入输出格式；2）按公式分步计算", nil, nil, 0, "强调 else if")
	if !strings.Contains(u, "本次目标：1）掌握输入输出格式") || !strings.Contains(u, "教师补充说明：强调 else if") {
		t.Fatalf("说明与补充应整块注入：%s", u)
	}
	// 说明超长截断（800 帽；用专属字符计数，避免与标签文案撞字）
	long := strings.Repeat("囍", summaryIntroMaxRunes+10)
	if got := summaryUser("班级", "2026-11-14", "作业说明", long, nil, nil, 0, ""); strings.Count(got, "囍") != summaryIntroMaxRunes {
		t.Fatalf("说明应按 800 字截断，got %d", strings.Count(got, "囍"))
	}
}

func TestSummaryTeacherSetAndCut(t *testing.T) {
	users := map[int64]*repo.User{
		1: {ID: 1, Username: "owner", Role: repo.RoleDefault},   // 建班人（非特权角色）
		2: {ID: 2, Username: "monitor", Role: repo.RoleDefault}, // 班级管理员
		3: {ID: 3, Username: "rootish", Role: repo.RoleRoot},    // root 用户角色（普通成员）
		4: {ID: 4, Username: "adminy", Role: repo.RoleAdmin},    // admin 用户角色（普通成员）
		5: {ID: 5, Username: "stu", Role: repo.RoleDefault},     // 学生
	}
	members := []repo.ClassMember{
		{UID: 1}, {UID: 2, Role: repo.ClassRoleAdmin}, {UID: 3}, {UID: 4}, {UID: 5},
	}
	teachers := summaryTeacherSet(&repo.Class{OwnerID: 1}, members, users)
	for _, uid := range []int64{1, 2, 3, 4} {
		if !teachers[uid] {
			t.Fatalf("uid %d 应判为老师", uid)
		}
	}
	if teachers[5] {
		t.Fatal("普通学生不应判为老师")
	}
	got := summaryCutTeachers([]int64{1, 3, 5}, teachers)
	if len(got) != 1 || got[0] != 5 {
		t.Fatalf("老师剔除后应只剩学生，got %v", got)
	}
}

func TestAssignmentRollcallPerms(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault, Perms: []string{repo.PermClassEditOwn}}
	// 老师 = 建班人 100（class role admin 兼）∪ root 用户角色 103（普通成员挂班）；
	// 学生 = 101（做过题，默认勾）∪ 102（0 题未做，默认不勾）
	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 100, InviteCode: "ab12"}
	f.memberRoles[[2]int64{9, 100}] = repo.ClassRoleAdmin
	f.memberRoles[[2]int64{9, 101}] = repo.ClassRoleMember
	f.memberRoles[[2]int64{9, 102}] = repo.ClassRoleMember
	f.memberRoles[[2]int64{9, 103}] = repo.ClassRoleMember
	f.assignments[5] = &repo.Assignment{ID: 5, ClassID: 9, Title: "分支练习", Problems: []int64{301, 302}}
	f.progressRows = []*repo.ProgressRow{
		{UID: 100, Username: "teacher", Nickname: "超越", Rank: 1, Total: 100, Solved: 1, Attempt: 1,
			Cells: map[int64]*repo.ProgressCell{301: {Attempt: true, Best: 100, Solved: true}}},
		{UID: 101, Username: "stu101", Nickname: "小甲", Rank: 2, Total: 90, Solved: 0, Attempt: 1,
			Cells: map[int64]*repo.ProgressCell{301: {Attempt: true, Best: 90}}},
		{UID: 102, Username: "stu102", Rank: 3, Cells: map[int64]*repo.ProgressCell{}},
	}
	h := newTestHandlers(f)
	r := gin.New()
	gin.SetMode(gin.TestMode)
	authed := r.Group("/api", AuthRequired(h))
	authed.GET("/assignments/:id/rollcall", h.AssignmentRollcall)
	owner := f.addUser(h, 100, "teacher", repo.RoleDefault, 0)
	member := f.addUser(h, 101, "stu101", repo.RoleDefault, 0)

	// 普通成员（非班级管理员）403：与生成同门禁
	if code, _ := doJSON(r, "GET", "/api/assignments/5/rollcall", member, ""); code != http.StatusForbidden {
		t.Fatalf("普通成员点名表 = %d，期望 403", code)
	}
	code, body := doJSON(r, "GET", "/api/assignments/5/rollcall", owner, "")
	if code != http.StatusOK {
		t.Fatalf("建班人点名表 = %d %v，期望 200", code, body)
	}
	list := body["data"].(map[string]any)["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("点名表应剔除老师只剩 2 名学生，got %d: %v", len(list), body["data"])
	}
	byUID := map[int64]map[string]any{}
	for _, it := range list {
		m := it.(map[string]any)
		byUID[int64(m["uid"].(float64))] = m
	}
	if _, ok := byUID[100]; ok {
		t.Fatal("建班人不应出现在点名表（即便他做过题）")
	}
	if _, ok := byUID[103]; ok {
		t.Fatal("root 用户角色不应出现在点名表")
	}
	if byUID[101]["checked"] != true || byUID[101]["name"] != "小甲" || byUID[101]["hint"] != "已做1/2题，通过0题" {
		t.Fatalf("做过题学生应默认勾选且带做题提示：%v", byUID[101])
	}
	if byUID[102]["checked"] != false || byUID[102]["hint"] != "已做0/2题，通过0题" {
		t.Fatalf("0 题学生应默认不勾：%v", byUID[102])
	}
}

func TestExamRollcallAndUIDFilter(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	// 老师 = 建班人 200 ∪ 班级管理员 203；学生 = 201（有提交，默认勾）∪ 202（未提交，默认不勾）
	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 200}
	f.memberRoles[[2]int64{9, 201}] = repo.ClassRoleMember
	f.memberRoles[[2]int64{9, 202}] = repo.ClassRoleMember
	f.memberRoles[[2]int64{9, 203}] = repo.ClassRoleAdmin
	f.rounds = []*repo.Contest{
		{ID: 7, ClassID: 9, Title: "期中考试", CreatedBy: 200, Published: true,
			Start: time.Now().Add(-2 * time.Hour), End: time.Now().Add(-time.Hour),
			Problems: []repo.ContestProblem{{PID: 301, Score: 100}}},
	}
	f.problems[301] = &repo.Problem{ID: 301, Title: "智能计算器", Difficulty: 4}
	f.roundSubs = []*repo.Submission{
		{ID: 1, UID: 201, Username: "stu201", ExamID: 7, ProblemID: 301, Score: 95, Status: repo.StatusAccepted, Published: true},
	}

	h := newTestHandlers(f)
	r := summaryEngine(h)
	organizerTok := f.addUser(h, 200, "teacher2", repo.RoleDefault, 0)
	outsiderTok := f.addUser(h, 204, "outsider", repo.RoleDefault, 0)

	// rollcall 路由单挂（summaryEngine 只挂 summary）
	rr := gin.New()
	gin.SetMode(gin.TestMode)
	rrAuthed := rr.Group("/api", AuthRequired(h))
	rrAuthed.GET("/exams/:id/rollcall", h.ExamRollcall)

	// 非组织者 403（rollcall 与 summary 同门禁）
	if code, _ := doJSON(rr, "GET", "/api/exams/7/rollcall", outsiderTok, ""); code != http.StatusForbidden {
		t.Fatalf("非组织者点名表 = %d，期望 403", code)
	}
	code, body := doJSON(rr, "GET", "/api/exams/7/rollcall", organizerTok, "")
	if code != http.StatusOK {
		t.Fatalf("组织者点名表 = %d %v，期望 200", code, body)
	}
	list := body["data"].(map[string]any)["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("点名表应剔除老师（建班人/班级管理员）只剩 2 名学生，got %d: %v", len(list), body["data"])
	}
	first, second := list[0].(map[string]any), list[1].(map[string]any)
	if first["uid"].(float64) != 201 || first["checked"] != true || first["hint"] != "总分95" {
		t.Fatalf("有提交学生应默认勾选且带总分提示：%v", first)
	}
	if second["uid"].(float64) != 202 || second["checked"] != false || second["hint"] != "未提交" {
		t.Fatalf("未提交学生应默认不勾：%v", second)
	}

	// 生成端点 uids 路径：只传老师 uid → 剔除后为空 → 点名表报错；传学生 → 过门禁进 AI 分支
	if code, body := doJSON(r, "POST", "/api/exams/7/summary", organizerTok, `{"uids":[200,203]}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "点名表中没有可计入的学生") {
		t.Fatalf("uids 全为老师 = %d %v，期望 400 点名表报错", code, body)
	}
	if code, body := doJSON(r, "POST", "/api/exams/7/summary", organizerTok, `{"uids":[201]}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "AI 未配置") {
		t.Fatalf("uids 学生 = %d %v，期望过门禁命中 AI 未配置", code, body)
	}
	// 老师混在勾选名单里也被强制剔除（201 学生兜底过 guard）
	if code, body := doJSON(r, "POST", "/api/exams/7/summary", organizerTok, `{"uids":[201,200,203]}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "AI 未配置") {
		t.Fatalf("uids 混入老师 = %d %v，期望剔除后照常过 guard", code, body)
	}
}

func TestAssignmentSummaryPerms(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault, Perms: []string{repo.PermClassEditOwn}}
	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 100, InviteCode: "ab12"}
	f.memberRoles[[2]int64{9, 101}] = repo.ClassRoleMember
	f.assignments[5] = &repo.Assignment{ID: 5, ClassID: 9, Title: "分支练习", Problems: []int64{301, 302}}
	f.problems[301] = &repo.Problem{ID: 301, Title: "智能计算器", Difficulty: 4, Tags: []int64{5}}
	f.problems[302] = &repo.Problem{ID: 302, Title: "直角三角形判断", Difficulty: 5}
	f.tagTreeNodes = []repo.TagNode{{ID: 5, Name: "分支"}}
	f.stats = map[int64]repo.ProblemStat{301: {Total: 100, Accepted: 20}}
	f.progressRows = []*repo.ProgressRow{
		{UID: 101, Username: "stu101", Rank: 1, Total: 190, Solved: 1, Attempt: 2,
			Cells: map[int64]*repo.ProgressCell{301: {Attempt: true, Best: 100, Solved: true}, 302: {Attempt: true, Best: 90}}},
	}

	h := newTestHandlers(f)
	r := summaryEngine(h)
	teacher := f.addUser(h, 100, "teacher", repo.RoleDefault, 0)
	member := f.addUser(h, 101, "stu101", repo.RoleDefault, 0)

	// 未登录 401（AuthRequired 中间件链）
	if code, _ := doJSON(r, "POST", "/api/assignments/5/summary", "", `{}`); code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，期望 401", code)
	}
	// 普通成员 403：仅班级管理员可生成总结
	if code, body := doJSON(r, "POST", "/api/assignments/5/summary", member, `{}`); code != http.StatusForbidden {
		t.Fatalf("普通成员 = %d %v，期望 403", code, body)
	}
	// 建班人（持 edit.own）过门禁；fake aiCfg 无启用档 → AI 未配置分支
	code, body := doJSON(r, "POST", "/api/assignments/5/summary", teacher, `{"notes":"今天强调了 else if"}`)
	if code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "AI 未配置") {
		t.Fatalf("过门禁后应命中 AI 未配置 = %d %v", code, body)
	}
	// uids 路径：老师 uid 强制剔除 → 空 → 点名表报错；学生 uid → 照常进 AI 分支
	if code, body := doJSON(r, "POST", "/api/assignments/5/summary", teacher, `{"uids":[100]}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "点名表中没有可计入的学生") {
		t.Fatalf("uids 全为老师 = %d %v，期望 400 点名表报错", code, body)
	}
	if code, body := doJSON(r, "POST", "/api/assignments/5/summary", teacher, `{"uids":[101]}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "AI 未配置") {
		t.Fatalf("uids 学生 = %d %v，期望过门禁命中 AI 未配置", code, body)
	}

	// 0 提交 guard：全班没人动笔 → 400，未到 AI 外呼
	f.progressRows = []*repo.ProgressRow{{UID: 101, Username: "stu101", Rank: 1, Cells: map[int64]*repo.ProgressCell{}}}
	if code, body := doJSON(r, "POST", "/api/assignments/5/summary", teacher, `{}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "还没有同学提交") {
		t.Fatalf("0 提交 = %d %v，期望 400 还没有同学提交", code, body)
	}
	// 作业不存在 404
	if code, _ := doJSON(r, "POST", "/api/assignments/99/summary", teacher, `{}`); code != http.StatusNotFound {
		t.Fatalf("作业不存在 = %d，期望 404", code)
	}
}

func TestExamSummaryPerms(t *testing.T) {
	f := baseFake()
	f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
	f.classes[9] = &repo.Class{ID: 9, Name: "九班", OwnerID: 200}
	f.rounds = []*repo.Contest{
		{ID: 7, ClassID: 9, Title: "期中考试", CreatedBy: 200, Published: true,
			Start: time.Now().Add(-2 * time.Hour), End: time.Now().Add(-time.Hour),
			Problems: []repo.ContestProblem{{PID: 301, Score: 100}, {PID: 302, Score: 50}}},
		{ID: 8, ClassID: 9, Title: "空场考试", CreatedBy: 200, Published: true,
			Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Hour),
			Problems: []repo.ContestProblem{{PID: 301, Score: 100}}},
	}
	f.problems[301] = &repo.Problem{ID: 301, Title: "智能计算器", Difficulty: 4}
	f.roundSubs = []*repo.Submission{
		{ID: 1, UID: 201, Username: "stu201", ExamID: 7, ProblemID: 301, Score: 100, Status: repo.StatusAccepted, Published: true},
		{ID: 2, UID: 201, Username: "stu201", ExamID: 7, ProblemID: 302, Score: 40},
	}

	h := newTestHandlers(f)
	r := summaryEngine(h)
	organizer := f.addUser(h, 200, "teacher2", repo.RoleDefault, 0)
	outsider := f.addUser(h, 201, "stu201", repo.RoleDefault, 0)

	// 非组织者（哪怕是在考学生）403
	if code, body := doJSON(r, "POST", "/api/exams/7/summary", outsider, `{}`); code != http.StatusForbidden {
		t.Fatalf("非组织者 = %d %v，期望 403", code, body)
	}
	// 组织者过门禁 → 榜单非空 → AI 未配置分支
	code, body := doJSON(r, "POST", "/api/exams/7/summary", organizer, `{}`)
	if code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "AI 未配置") {
		t.Fatalf("组织者应命中 AI 未配置 = %d %v", code, body)
	}
	// 0 提交 guard：无任何提交的考试
	if code, body := doJSON(r, "POST", "/api/exams/8/summary", organizer, `{}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "还没有同学提交") {
		t.Fatalf("0 提交考试 = %d %v，期望 400 还没有同学提交", code, body)
	}
	// 考试不存在 404
	if code, _ := doJSON(r, "POST", "/api/exams/99/summary", organizer, `{}`); code != http.StatusNotFound {
		t.Fatalf("考试不存在 = %d，期望 404", code)
	}
}
