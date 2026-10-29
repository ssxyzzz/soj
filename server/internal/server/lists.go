package server

// 题单（v0.18.3，plan §2.12）：列表页 / 详情（含我的每题状态与难度聚合）/ 完成排名
// / 管理 CRUD / 作业「从题单复制」拍平接口。
// 可见性口径：
//   - 隐藏题单同题目 hidden——非管理者（创建者/root）视角不可见（列表不出现、详情 404）；
//   - 隐藏题在题单内的展示 = 观察者持 problem.view.all 或是该题单管理者（创建者要能
//     看到并打理自己题单里的隐藏题，红标提醒）；其余视角整条剔除、分子分母同步缩减；
//   - 赛时锁题同题库——挂锁徽章、锁期内不可进（分母保留：已完成者保留完成）。
// 排名口径：Accepted ∧ published、按题单可见题去重集合计数、只收有进度用户、同分并列跳号。

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// listManageable 题单管理权：创建者或 root（持 list.manage 也不能改别人的题单，拍板 12）。
func (h *Handlers) listManageable(c *gin.Context, l *repo.ProblemList) bool {
	uid, _, role, logged := currentUser(c)
	return logged && (role == repo.RoleRoot || uid == l.CreatedBy)
}

// listEntryJSON 详情条目：pid + 摘要 + 观察者状态（status/score 来自 UserBestStatuses）。
type listEntryJSON struct {
	PID        int64   `json:"pid"`
	Title      string  `json:"title"`
	Difficulty int     `json:"difficulty"`
	Hidden     bool    `json:"hidden,omitempty"`
	Locked     bool    `json:"locked,omitempty"`
	LockKind   string  `json:"lock_kind,omitempty"`
	Status     *string `json:"status,omitempty"`
	Score      *int    `json:"score,omitempty"`
	Tags       []int64 `json:"tags,omitempty"`
}

// listSectionJSON 详情小节节点（章节树第二层）。
type listSectionJSON struct {
	No       string          `json:"no"`
	Title    string          `json:"title"`
	Desc     string          `json:"desc,omitempty"`
	Problems []listEntryJSON `json:"problems"`
}

// listChapterJSON 详情章节点。
type listChapterJSON struct {
	No       string            `json:"no"`
	Title    string            `json:"title"`
	Desc     string            `json:"desc,omitempty"`
	Problems []listEntryJSON   `json:"problems"`
	Sections []listSectionJSON `json:"sections"`
}

// buildListScope 装配题单章节树（观察者视角）：题目摘要批量查、状态批量查、锁批量查。
// 返回章节树 + 去重可见 pid 集（已剔已删题与无权限隐藏题）。
func (h *Handlers) buildListScope(c *gin.Context, l *repo.ProblemList) ([]listChapterJSON, []int64, error) {
	uid, _, role, logged := currentUser(c)
	manage := h.listManageable(c, l)
	seeAllHidden := manage || hasPerm(c, repo.PermProblemViewAll)

	all := repo.FlattenPIDs(l.Chapters)
	sums, err := h.repo.ProblemSummariesByIDs(c.Request.Context(), all)
	if err != nil {
		return nil, nil, err
	}
	statuses, err := h.repo.UserBestStatuses(c.Request.Context(), uid, all)
	if err != nil {
		return nil, nil, err
	}
	lockMap, err := h.loadLockMapView(c.Request.Context(), uid, role)
	if err != nil {
		return nil, nil, err
	}

	chapters := make([]listChapterJSON, 0, len(l.Chapters))
	seen := map[int64]bool{}
	visible := make([]int64, 0, len(all))
	addEntry := func(pid int64) (listEntryJSON, bool) {
		p := sums[pid]
		if p == nil {
			return listEntryJSON{}, false // 已删除的题：条目剔除，计数不含
		}
		if p.Hidden && !seeAllHidden {
			return listEntryJSON{}, false // 隐藏题：无权限视角整条剔除（分母同步缩减）
		}
		e := listEntryJSON{PID: p.ID, Title: p.Title, Difficulty: p.Difficulty, Hidden: p.Hidden, Tags: p.Tags}
		if lk := lockMap[p.ID]; lk != nil {
			e.Locked = true
			e.LockKind = lk.Kind
		}
		if logged {
			if st, ok := statuses[p.ID]; ok {
				s, sc := st.Status, st.Score
				e.Status, e.Score = &s, &sc
			}
		}
		if !seen[p.ID] {
			seen[p.ID] = true
			visible = append(visible, p.ID)
		}
		return e, true
	}

	for i, ch := range l.Chapters {
		node := listChapterJSON{
			No: strconv.Itoa(i + 1), Title: strings.TrimSpace(ch.Title), Desc: ch.Desc,
			Problems: []listEntryJSON{}, Sections: []listSectionJSON{},
		}
		// 单章不混挂（形状校验保证）：有子节的章不含直挂题，防御性跳过直挂段
		if len(ch.Children) == 0 {
			for _, pid := range ch.Problems {
				if e, ok := addEntry(pid); ok {
					node.Problems = append(node.Problems, e)
				}
			}
		}
		for j, sec := range ch.Children {
			sn := listSectionJSON{
				No: node.No + "." + strconv.Itoa(j+1), Title: strings.TrimSpace(sec.Title),
				Desc: sec.Desc, Problems: []listEntryJSON{},
			}
			for _, pid := range sec.Problems {
				if e, ok := addEntry(pid); ok {
					sn.Problems = append(sn.Problems, e)
				}
			}
			node.Sections = append(node.Sections, sn)
		}
		chapters = append(chapters, node)
	}
	return chapters, visible, nil
}

// GetLists 题单列表页（登录可见）：可见题单 + 每卡「N 题 · M 章」与我的进度。
func (h *Handlers) GetLists(c *gin.Context) {
	uid, _, role, logged := currentUser(c)
	all, err := h.repo.ListProblemLists(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题单失败")
		return
	}
	seeAll := hasPerm(c, repo.PermProblemViewAll)
	type cardPrep struct {
		l      *repo.ProblemList
		manage bool
		pids   []int64 // 该题单对观察者可见的去重题集
		diffs  []int   // 难度分布（可见集，D1–D10；卡内柱状图用）
	}
	preps := make([]cardPrep, 0, len(all))
	uniqAll := map[int64]bool{}
	var allPIDs []int64
	for _, l := range all {
		manage := logged && (role == repo.RoleRoot || uid == l.CreatedBy)
		if l.Hidden && !manage {
			continue // 隐藏题单对学生不可见
		}
		p := cardPrep{l: l, manage: manage, diffs: make([]int, 10)}
		// 卡片题数按观察者可见口径：全量可见位或创建者看全量，否则剔隐藏题
		seeHidden := manage || seeAll
		sums, err := h.repo.ProblemSummariesByIDs(c.Request.Context(), repo.FlattenPIDs(l.Chapters))
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取题单失败")
			return
		}
		uniq := map[int64]bool{}
		for _, pid := range repo.FlattenPIDs(l.Chapters) {
			s := sums[pid]
			if s == nil || (s.Hidden && !seeHidden) {
				continue
			}
			if !uniq[pid] {
				uniq[pid] = true
				p.pids = append(p.pids, pid)
				p.diffs[s.Difficulty-1]++
			}
			if !uniqAll[pid] {
				uniqAll[pid] = true
				allPIDs = append(allPIDs, pid)
			}
		}
		preps = append(preps, p)
	}
	statuses, err := h.repo.UserBestStatuses(c.Request.Context(), uid, allPIDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取进度失败")
		return
	}
	// 参与人数：(uid, pid) 参与对 → 每卡在自身可见题集内数去重用户（尝试即算参与）
	pairs, err := h.repo.ListParticipantPairs(c.Request.Context(), allPIDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "统计参与人数失败")
		return
	}
	type listCard struct {
		ID           int64     `json:"id"`
		Title        string    `json:"title"`
		Intro        string    `json:"intro"`
		Hidden       bool      `json:"hidden,omitempty"`
		CreatedAt    time.Time `json:"created_at"`
		CreatedBy    int64     `json:"created_by"`
		ChapterCount int       `json:"chapter_count"`
		ProblemCount int       `json:"problem_count"`
		Solved       int       `json:"solved"`
		Difficulties []int     `json:"difficulties"`
		Participants int       `json:"participants"`
		Manageable   bool      `json:"manageable"`
	}
	items := make([]listCard, 0, len(preps))
	for _, p := range preps {
		card := listCard{
			ID: p.l.ID, Title: p.l.Title, Intro: p.l.Intro, Hidden: p.l.Hidden,
			CreatedAt: p.l.CreatedAt, CreatedBy: p.l.CreatedBy,
			ChapterCount: len(p.l.Chapters), ProblemCount: len(p.pids), Manageable: p.manage,
			Difficulties: p.diffs,
		}
		pidSet := make(map[int64]bool, len(p.pids))
		for _, pid := range p.pids {
			pidSet[pid] = true
		}
		uids := make(map[int64]bool)
		for _, row := range pairs {
			if pidSet[row.PID] && !uids[row.UID] {
				uids[row.UID] = true
				card.Participants++
			}
		}
		for _, pid := range p.pids {
			if st, ok := statuses[pid]; ok && st.Status == repo.StatusAccepted {
				card.Solved++
			}
		}
		items = append(items, card)
	}
	ok(c, gin.H{"items": items, "can_create": hasPerm(c, repo.PermListManage)})
}

// GetListDetail 题单详情：章节树（含我的状态）+ 总进度 + 难度分布。
func (h *Handlers) GetListDetail(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return
	}
	manage := h.listManageable(c, l)
	if l.Hidden && !manage {
		fail(c, http.StatusNotFound, "题单不存在") // 隐藏题单对非管理者同「不存在」
		return
	}
	chapters, visible, err := h.buildListScope(c, l)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题单失败")
		return
	}
	// 总进度与难度分布（可见题去重集合；重复挂载的题只计一次，与 total 同口径；进度只认 AC）
	uid, _, _, _ := currentUser(c)
	statuses, _ := h.repo.UserBestStatuses(c.Request.Context(), uid, visible)
	solved, difficulties := 0, make([]int, 10)
	seen := map[int64]bool{}
	countEntry := func(e listEntryJSON) {
		if seen[e.PID] {
			return // 同题重复挂载：进度与难度分布均只计一次
		}
		seen[e.PID] = true
		difficulties[e.Difficulty-1]++
		if st, ok := statuses[e.PID]; ok && st.Status == repo.StatusAccepted {
			solved++
		}
	}
	for _, ch := range chapters {
		for _, e := range ch.Problems {
			countEntry(e)
		}
		for _, sec := range ch.Sections {
			for _, e := range sec.Problems {
				countEntry(e)
			}
		}
	}
	ok(c, gin.H{
		"id": l.ID, "title": l.Title, "intro": l.Intro, "intro_html": l.IntroHtml, "hidden": l.Hidden,
		"created_at": l.CreatedAt, "manageable": manage,
		"total": len(seen), "solved": solved, "difficulties": difficulties,
		"chapters": chapters,
	})
}

// listRankJump 计算全榜第 i 行的并列跳号名次（同分取第一名位次；rows 已按 solved 降序）。
func listRankJump(rows []repo.ListRankRow, i int) int {
	rank := i + 1
	if i > 0 && rows[i].Solved == rows[i-1].Solved {
		rank = listRankJump(rows, i-1) // 同分继承上一位名次（递归深度 = 同分组长度，榜单规模小无虞）
	}
	return rank
}

// GetListRank 题单完成排名（分页每页 100；同分并列同名次跳号；榜外自己补算 me）。
func (h *Handlers) GetListRank(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return
	}
	if l.Hidden && !h.listManageable(c, l) {
		fail(c, http.StatusNotFound, "题单不存在")
		return
	}
	_, visible, err := h.buildListScope(c, l)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题单失败")
		return
	}
	rows, err := h.repo.ListRankRows(c.Request.Context(), visible)
	if err != nil {
		fail(c, http.StatusInternalServerError, "统计排名失败")
		return
	}
	total := len(rows)
	page, pageSize := 1, 100
	if v, err := strconv.Atoi(c.Query("page")); err == nil && v >= 1 {
		page = v
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	slice := rows[start:end]
	uids := make([]int64, 0, len(slice))
	for _, r := range slice {
		uids = append(uids, r.UID)
	}
	users, _ := h.repo.GetUsersByIDs(c.Request.Context(), uids)
	levels := h.levelMapFor(c, uids) // 失败为 nil，行内省略等级
	type rankItem struct {
		Rank     int    `json:"rank"`
		UID      int64  `json:"uid"`
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Solved   int64  `json:"solved"`
		Level    int    `json:"level,omitempty"`
	}
	items := make([]rankItem, 0, len(slice))
	for i, r := range slice {
		it := rankItem{Rank: listRankJump(rows, start+i), UID: r.UID, Solved: r.Solved}
		if u := users[r.UID]; u != nil {
			it.Username, it.Nickname = u.Username, u.Nickname
		}
		if levels != nil {
			it.Level = levels[r.UID]
		}
		items = append(items, it)
	}
	// 榜外自己补算（同排行榜 me 口径）：0 进度 → me = null
	uid, _, _, _ := currentUser(c)
	var me gin.H
	for i, r := range rows {
		if r.UID == uid {
			me = gin.H{"rank": listRankJump(rows, i), "solved": r.Solved}
			break
		}
	}
	ok(c, gin.H{"total": total, "page": page, "page_size": pageSize, "items": items, "me": me})
}

// CreateList 新建题单（list.manage；初始排到队尾）。
func (h *Handlers) CreateList(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len([]rune(title)) > 64 {
		fail(c, http.StatusBadRequest, "题单名需为 1-64 字")
		return
	}
	maxSort, _ := h.repo.MaxListSort(c.Request.Context())
	l := &repo.ProblemList{Title: title, Sort: maxSort + 1, CreatedBy: uid, Chapters: []repo.ListEntry{}}
	if err := h.repo.CreateProblemList(c.Request.Context(), l); err != nil {
		fail(c, http.StatusInternalServerError, "创建失败")
		return
	}
	ok(c, gin.H{"id": l.ID})
}

// UpdateList 编辑题单（创建者/root）：meta 指针语义 + chapters 整体覆盖（形状校验）。
func (h *Handlers) UpdateList(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return
	}
	if !h.listManageable(c, l) {
		fail(c, http.StatusForbidden, "只有题单创建者可以编辑")
		return
	}
	var req struct {
		Title    *string           `json:"title"`
		Intro    *string           `json:"intro"`
		Hidden   *bool             `json:"hidden"`
		Sort     *int64            `json:"sort"`
		Chapters *[]repo.ListEntry `json:"chapters"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	var patch repo.ListPatch
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" || len([]rune(title)) > 64 {
			fail(c, http.StatusBadRequest, "题单名需为 1-64 字")
			return
		}
		patch.Title = &title
	}
	if req.Intro != nil {
		if len(*req.Intro) > 64<<10 {
			fail(c, http.StatusBadRequest, "简介过长（上限 64KB）")
			return
		}
		intro := strings.TrimSpace(*req.Intro)
		patch.Intro = &intro
	}
	if req.Hidden != nil {
		patch.Hidden = req.Hidden
	}
	if req.Sort != nil {
		patch.Sort = req.Sort
	}
	if req.Chapters != nil {
		if err := repo.ValidateListShape(*req.Chapters); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		if *req.Chapters == nil {
			ch := make([]repo.ListEntry, 0)
			req.Chapters = &ch
		}
		patch.Chapters = req.Chapters
	}
	if patch.Empty() {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateProblemList(c.Request.Context(), id, patch); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": 1})
}

// DeleteList 删除题单：创建者、root 或持「删除任意题单」位（list.delete，默认仅 root）。
func (h *Handlers) DeleteList(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return
	}
	uid, _, role, logged := currentUser(c)
	canDelete := logged && (role == repo.RoleRoot || uid == l.CreatedBy || hasPerm(c, repo.PermListDelete))
	if !canDelete {
		fail(c, http.StatusForbidden, "没有删除该题单的权限")
		return
	}
	if err := h.repo.DeleteProblemList(c.Request.Context(), id); err != nil {
		fail(c, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(c, gin.H{"deleted": 1})
}

// ReorderLists 列表页手动排序（list.manage）：按传入 id 顺序写 sort = 下标。
func (h *Handlers) ReorderLists(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	for i, id := range req.IDs {
		sort := int64(i)
		if err := h.repo.UpdateProblemList(c.Request.Context(), id, repo.ListPatch{Sort: &sort}); err != nil && !repo.IsNotFound(err) {
			fail(c, http.StatusInternalServerError, "排序保存失败")
			return
		}
	}
	ok(c, gin.H{"updated": len(req.IDs)})
}

// GetListFlat 作业「从题单复制」拍平接口：去重保序、一律跳过隐藏题与已删题
//（复制进班级作业不带隐藏题，与观察者权限无关——拍板 7）。
// 可选查询参数圈定范围（2026-10-26 拍板）：chapter=章序号（详情接口的 no，1 起）、
// section=小节 no（如 2.1，需与 chapter 同给）——只复制该章 / 该节，其余拍平全单。
func (h *Handlers) GetListFlat(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return
	}
	if l.Hidden && !h.listManageable(c, l) {
		fail(c, http.StatusNotFound, "题单不存在")
		return
	}
	chapters := l.Chapters
	if q := c.Query("chapter"); q != "" {
		ci, err := strconv.Atoi(q)
		if err != nil || ci < 1 || ci > len(l.Chapters) {
			fail(c, http.StatusBadRequest, "章节不存在")
			return
		}
		ch := l.Chapters[ci-1]
		if s := c.Query("section"); s != "" {
			dot := strings.LastIndex(s, ".")
			si, err := strconv.Atoi(s[dot+1:])
			if err != nil || si < 1 || si > len(ch.Children) {
				fail(c, http.StatusBadRequest, "小节不存在")
				return
			}
			chapters = []repo.ListEntry{{Title: ch.Title, Children: []repo.ListEntry{ch.Children[si-1]}}}
		} else {
			chapters = []repo.ListEntry{ch}
		}
	}
	sums, err := h.repo.ProblemSummariesByIDs(c.Request.Context(), repo.FlattenPIDs(chapters))
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题单失败")
		return
	}
	items := make([]gin.H, 0, len(sums))
	skippedHidden := 0
	seen := map[int64]bool{}
	for _, pid := range repo.FlattenPIDs(chapters) {
		p := sums[pid]
		if p == nil {
			continue
		}
		if p.Hidden {
			skippedHidden++
			continue
		}
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		items = append(items, gin.H{"id": p.ID, "title": p.Title})
	}
	ok(c, gin.H{"title": l.Title, "items": items, "skipped_hidden": skippedHidden})
}
