package server

// 题单 AI 一键整理（v0.71，plan §2.54）：编辑题单页把题库题目按 AI 理解归类进
// 本题单已建好的章/节目录结构——题目难度/知识点/来源标签粒度粗、题单章节细，
// 字符串匹配不可靠，AI 结合题面与章节标题/描述的语义判断落点。
// 两段式铁律同题库整理（tidy.go）：任务只产建议（按叶分组 + 未匹配清单），创建者/root
// 在预览勾选确认后由前端写进编辑草稿（emit dirty），走题单编辑既有整体 PATCH 保存
// 管道——本文件不写 lists 集合。
// 单任务骨架照 tidy（全站单槽 409 互斥 + 2s 轮询 + ctx 取消 + aiCalls 审计 cost=0，
// 复用 tidyAIChat）。AI 批量归类：每批 listOrganizeBatchSize 题（标题/难度/知识点名/
// 来源标签名/题面截断进 prompt，叶结构全量进 prompt），AI 返回 {pid → 叶 key}；
// 未知 key/批外 pid/重复 pid 一律丢弃（落未匹配，幻觉纪律同 tidy knowledge）。
// 范围口径：题库拉取 = 全库未隐藏未重复题（同 SectionProblemsDialog 候选口径），可被
// 难度/知识点/来源标签筛选收窄（维内任一命中、维间 AND，同题库列表页口径）；
// include_mounted=true 时本题单已挂题全量纳入（含隐藏/重复——已在单内 AI 只挪位置，
// 不受筛选收窄），false 时已挂题全部排除（保持原挂载，只加新题）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// listOrganizeBatchSize 单次 AI 调用覆盖的题目数：叶结构全量进 prompt 的前提下
// 25 题批 ≈ 1.5 万字符 prompt 上界，供 3 路并发闸（tidyWorkers）消化；全库 2000 题 ≈ 80 次调用。
const listOrganizeBatchSize = 25

// listOrganizeStatementMaxRunes 单题题面进 prompt 的截断（归类以标题/标签信号为主、
// 题面补细粒度判别；500 runes × 25 题封住批 prompt 体量）。
const listOrganizeStatementMaxRunes = 500

// listOrganizeItem 叶内建议条目（预览行渲染）。
type listOrganizeItem struct {
	PID        int64  `json:"pid"`
	Title      string `json:"title"`
	Difficulty int    `json:"difficulty"`
	Hidden     bool   `json:"hidden,omitempty"` // 应用进草稿时保真红标（重排移动隐藏题不丢徽章）
	Mounted    bool   `json:"mounted,omitempty"` // 发起时已挂在本题单（include_mounted 模式的重排对象）
}

// listOrganizeGroup 叶分组建议（Groups 顺序 = 大纲叶序，前端按序直渲染）。
type listOrganizeGroup struct {
	Key   string             `json:"key"`   // 'ch:i' | 'sec:i-j'（与前端大纲叶口径一致）
	Label string             `json:"label"` // 「第1章 xxx」/「1.2 xxx」短标
	Items []listOrganizeItem `json:"items"`
}

// listOrganizeLeaf 可挂题叶子：章无小节时整章是叶，否则其每个小节是叶（单章不混挂
// 由 ValidateListShape 保证，防御性跳过混挂章的直挂段——同 buildListScope 口径）。
type listOrganizeLeaf struct {
	Key      string
	Label    string
	Title    string
	Desc     string
	Path     string // prompt 用完整路径：节叶 =「章名 › 节名」（上位语境帮 AI 消歧），章叶 = 章名
	ChapDesc string // 节叶所属章的章描述（随路径进 prompt；章叶的描述走 Desc 本身）
}

// listOrganizeLeaves 从章/节树取叶子（key 编号由数组下标决定，前端发送草稿序即叶序）。
func listOrganizeLeaves(chapters []repo.ListEntry) []listOrganizeLeaf {
	out := make([]listOrganizeLeaf, 0, len(chapters))
	for i, ch := range chapters {
		t := strings.TrimSpace(ch.Title)
		if len(ch.Children) == 0 {
			out = append(out, listOrganizeLeaf{
				Key:   fmt.Sprintf("ch:%d", i),
				Label: fmt.Sprintf("第%d章 %s", i+1, t),
				Title: t,
				Desc:  strings.TrimSpace(ch.Desc),
				Path:  t,
			})
			continue
		}
		cd := strings.TrimSpace(ch.Desc)
		for j, sec := range ch.Children {
			st := strings.TrimSpace(sec.Title)
			out = append(out, listOrganizeLeaf{
				Key:      fmt.Sprintf("sec:%d-%d", i, j),
				Label:    fmt.Sprintf("%d.%d %s", i+1, j+1, st),
				Title:    st,
				Desc:     strings.TrimSpace(sec.Desc),
				Path:     fmt.Sprintf("%s › %s", t, st),
				ChapDesc: cd,
			})
		}
	}
	return out
}

// listOrganizeJob 一次整理任务的全量状态（自身带锁，供轮询快照与取消）。
type listOrganizeJob struct {
	mu             sync.Mutex
	State          string // running | done | cancelled | failed
	ListID         int64
	ListTitle      string // 题单标题/简介（v0.83.3）：AI 归类的全局语境——章/节名与题单主题脱节时有全局兜底
	ListIntro      string
	IncludeMounted bool
	From, To       int64 // 题号范围（0 = 不限），快照带出让发起者记得本次任务边界
	Total          int
	Done           int
	FailedBatches  int // AI 调用失败/无法解析的批数（相应题落未匹配，不判整任务失败）
	Leaves         []listOrganizeLeaf
	Keys           map[string]bool
	Scope          []*repo.Problem // 切批前的任务范围（顺序稳定，只读）
	Mounted        map[int64]bool
	Groups         []listOrganizeGroup // 终态装配
	Unmatched      []listOrganizeItem
	Err            string
	ByName         string
	StartedAt      time.Time
	EndedAt        time.Time
	cancel         context.CancelFunc
}

func newListOrganizeJob(listID int64, listTitle, listIntro string, includeMounted bool, from, to int64, leaves []listOrganizeLeaf, scope []*repo.Problem, mounted map[int64]bool, by string, cancel context.CancelFunc) *listOrganizeJob {
	keys := make(map[string]bool, len(leaves))
	for _, lf := range leaves {
		keys[lf.Key] = true
	}
	return &listOrganizeJob{
		State: "running", ListID: listID, ListTitle: listTitle, ListIntro: listIntro,
		IncludeMounted: includeMounted, From: from, To: to,
		Total: len(scope), Leaves: leaves, Keys: keys,
		Scope: scope, Mounted: mounted,
		ByName: by, StartedAt: time.Now(), cancel: cancel,
	}
}

func (j *listOrganizeJob) listID() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.ListID
}

func (j *listOrganizeJob) state() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State
}

func (j *listOrganizeJob) finish(state, errMsg string) {
	j.mu.Lock()
	if j.State == "running" {
		j.State = state
		j.Err = errMsg
		j.EndedAt = time.Now()
	}
	j.mu.Unlock()
}

func (j *listOrganizeJob) statusSnapshot() gin.H {
	j.mu.Lock()
	defer j.mu.Unlock()
	return gin.H{
		"state": j.State, "list_id": j.ListID, "include_mounted": j.IncludeMounted,
		"from": j.From, "to": j.To,
		"total": j.Total, "done": j.Done, "failed_batches": j.FailedBatches,
		"error": j.Err, "by": j.ByName,
		"started_at": j.StartedAt, "ended_at": j.EndedAt,
	}
}

// assemble 终态装配（收尾单线程）：范围内无叶归属的题 → 未匹配（题号序）；
// 各叶组内难度升序（未设难度殿后、同难按题号）。cancel/部分批失败语义同 tidy：
// 已完成批保留。
func (j *listOrganizeJob) assemble(assigned map[int64]string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	byKey := make(map[string][]listOrganizeItem, len(j.Leaves))
	rest := make([]listOrganizeItem, 0)
	for _, p := range j.Scope {
		it := listOrganizeItem{PID: p.ID, Title: p.Title, Difficulty: p.Difficulty, Hidden: p.Hidden, Mounted: j.Mounted[p.ID]}
		if key := assigned[p.ID]; key != "" {
			byKey[key] = append(byKey[key], it)
		} else {
			rest = append(rest, it)
		}
	}
	sort.Slice(rest, func(a, b int) bool { return rest[a].PID < rest[b].PID })
	j.Unmatched = rest
	j.Groups = make([]listOrganizeGroup, 0, len(j.Leaves))
	for _, lf := range j.Leaves {
		list := byKey[lf.Key]
		if list == nil {
			list = []listOrganizeItem{} // 空叶也给空数组：nil slice 序列化成 null 会炸前端迭代
		}
		sort.Slice(list, func(a, b int) bool {
			da, db := listOrganizeSortDiff(list[a].Difficulty), listOrganizeSortDiff(list[b].Difficulty)
			if da != db {
				return da < db
			}
			return list[a].PID < list[b].PID
		})
		j.Groups = append(j.Groups, listOrganizeGroup{Key: lf.Key, Label: lf.Label, Items: list})
	}
}

// listOrganizeSortDiff 节内排序难度：未设难度（0/越界）殿后。
func listOrganizeSortDiff(d int) int {
	if d < 1 || d > 10 {
		return 99
	}
	return d
}

// listOrganizeGuard status/suggestions/cancel 共用前置：本列表管理者 + 任务属本列表
//（其他列表的任务对此调用方不可见，返回 has=false 不写响应的由调用方定 idle 语义）。
func (h *Handlers) listOrganizeGuard(c *gin.Context) (*listOrganizeJob, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "题单 ID 非法")
		return nil, false
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return nil, false
	}
	if !h.listManageable(c, l) {
		fail(c, http.StatusForbidden, "只有题单创建者可以整理")
		return nil, false
	}
	h.organizeMu.Lock()
	job := h.organizeTask
	h.organizeMu.Unlock()
	if job == nil || job.listID() != id {
		return nil, false
	}
	return job, true
}

// ListOrganizeRun POST /api/lists/:id/organize/run：启动整理任务（全站单任务，进行中 409）。
// body {chapters, include_mounted?, difficulty?, tags?, meta?}——chapters 传编辑草稿当前值
//（未保存的结构也参与整理），形状校验同保存口径。
func (h *Handlers) ListOrganizeRun(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "题单 ID 非法")
		return
	}
	var req struct {
		Chapters       *[]repo.ListEntry `json:"chapters"`
		IncludeMounted bool              `json:"include_mounted"`
		From           *int64            `json:"from"` // 题号下界（含）；nil = 不限
		To             *int64            `json:"to"`   // 题号上界（含）；nil = 不限
		Difficulty     []int             `json:"difficulty"`
		Tags           []int64           `json:"tags"`
		Meta           []int64           `json:"meta"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Chapters == nil {
		fail(c, http.StatusBadRequest, "chapters 不能为空")
		return
	}
	chapters := *req.Chapters
	if len(chapters) == 0 {
		fail(c, http.StatusBadRequest, "题单还没有章节，先建章/节再整理")
		return
	}
	if err := repo.ValidateListShape(chapters); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	for _, d := range req.Difficulty {
		if !repo.ValidDifficulty(d) {
			fail(c, http.StatusBadRequest, "难度筛选需为 D1–D10")
			return
		}
	}
	if (req.From != nil && *req.From < 1) || (req.To != nil && *req.To < 1) ||
		(req.From != nil && req.To != nil && *req.From > *req.To) {
		fail(c, http.StatusBadRequest, "题号范围不合法：需 1 ≤ 起始 ≤ 结束，留空表示不限")
		return
	}
	l, err := h.repo.GetProblemList(c.Request.Context(), id)
	if respondRepoErr(c, err, "题单不存在", "读取题单失败") {
		return
	}
	if !h.listManageable(c, l) {
		fail(c, http.StatusForbidden, "只有题单创建者可以整理")
		return
	}
	// AI 类任务前置校验供应商（同 TidyRun）
	cfg, _ := h.repo.GetAIConfig(c.Request.Context())
	if cfg.ActiveProvider() == nil {
		fail(c, http.StatusBadRequest, "AI 供应商未配置——请先在管理后台「AI 助教」面板配置")
		return
	}
	uid, name, _, _ := currentUser(c)

	leaves := listOrganizeLeaves(chapters)
	if len(leaves) == 0 {
		fail(c, http.StatusBadRequest, "没有可挂题的章/节")
		return
	}
	mounted := map[int64]bool{}
	for _, pid := range repo.FlattenPIDs(chapters) {
		mounted[pid] = true
	}

	// 范围：全库未隐藏未重复题按筛选收窄（题号/难度/知识点/来源同为题库拉取侧条件）；
	// 已挂题按 include_mounted 二选一处理（不受题号范围与筛选收窄）。
	var from, to int64
	if req.From != nil {
		from = *req.From
	}
	if req.To != nil {
		to = *req.To
	}
	probs, err := h.repo.TidyListProblems(c.Request.Context())
	if respondRepoErr(c, err, "", "读取题库失败") {
		return
	}
	scope := make([]*repo.Problem, 0, len(probs))
	byID := make(map[int64]*repo.Problem, len(probs))
	for _, p := range probs {
		byID[p.ID] = p
		if mounted[p.ID] {
			continue // 已挂题统一后置处理
		}
		if p.Hidden || p.Duplicate || !inPIDRange(p.ID, from, to) ||
			!listOrganizeScopeHit(p, req.Difficulty, req.Tags, req.Meta) {
			continue
		}
		scope = append(scope, p)
	}
	if req.IncludeMounted {
		for pid := range mounted {
			if p := byID[pid]; p != nil {
				scope = append(scope, p)
			}
		}
	}
	if len(scope) == 0 {
		fail(c, http.StatusBadRequest, "没有符合范围的题目")
		return
	}

	h.organizeMu.Lock()
	if h.organizeTask != nil && h.organizeTask.State == "running" {
		h.organizeMu.Unlock()
		fail(c, http.StatusConflict, "已有整理任务进行中，请等待完成或先取消")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := newListOrganizeJob(id, l.Title, l.Intro, req.IncludeMounted, from, to, leaves, scope, mounted, name, cancel)
	h.organizeTask = job
	h.organizeMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				job.finish("failed", fmt.Sprintf("整理任务异常: %v", r))
			}
		}()
		h.runListOrganize(ctx, job, uid)
	}()
	ok(c, gin.H{"started": true})
}

// ListOrganizeStatus GET /api/lists/:id/organize/status：任务进度（无本列表任务 = idle）。
// 轻量快照——建议不在轮询响应里（v0.49 教训：全量建议整包进 2s 轮询是卡顿根源），
// 完成后前端一次性拉 suggestions。
func (h *Handlers) ListOrganizeStatus(c *gin.Context) {
	job, has := h.listOrganizeGuard(c)
	if !has {
		ok(c, gin.H{"state": "idle"})
		return
	}
	ok(c, job.statusSnapshot())
}

// ListOrganizeSuggestions GET /api/lists/:id/organize/suggestions：终态建议全量一次拉
//（done 全量 / cancelled 部分保留）；running 时 409 提示等待。
func (h *Handlers) ListOrganizeSuggestions(c *gin.Context) {
	job, has := h.listOrganizeGuard(c)
	if !has {
		fail(c, http.StatusNotFound, "没有整理任务")
		return
	}
	job.mu.Lock()
	state, groups, unmatched, total, done, failedN := job.State, job.Groups, job.Unmatched, job.Total, job.Done, job.FailedBatches
	job.mu.Unlock()
	switch state {
	case "running":
		fail(c, http.StatusConflict, "整理任务进行中，请等待完成")
		return
	case "failed":
		fail(c, http.StatusConflict, "整理任务失败，请重新发起")
		return
	}
	ok(c, gin.H{
		"state": state, "total": total, "done": done, "failed_batches": failedN,
		"groups": groups, "unmatched": unmatched,
	})
}

// ListOrganizeCancel POST /api/lists/:id/organize/cancel：取消进行中任务（中断在途
// AI 调用与后续批，已完成批保留——预览可看部分结果，语义同 tidy）。
func (h *Handlers) ListOrganizeCancel(c *gin.Context) {
	job, has := h.listOrganizeGuard(c)
	if has && job.state() == "running" {
		job.cancel()
	}
	ok(c, gin.H{"cancel_requested": true})
}

// runListOrganize 任务体：双树名字解析 → 切批 → 3 路并发 AI 归类 → 终态装配。
func (h *Handlers) runListOrganize(ctx context.Context, job *listOrganizeJob, uid int64) {
	knTree, err := h.repo.GetTagTree(ctx)
	if err != nil {
		job.finish("failed", "读取知识树失败: "+truncateTail(err.Error(), 120))
		return
	}
	metaTree, err := h.repo.GetMetaTagTree(ctx)
	if err != nil {
		job.finish("failed", "读取来源标签树失败: "+truncateTail(err.Error(), 120))
		return
	}
	knNames := tagNameIDToName(knTree)
	metaNames := tagNameIDToName(metaTree)

	leafText := listOrganizeLeavesPrompt(job.Leaves)
	sys := listOrganizeSystem()
	// 题单全局语境（v0.83.3）：标题/简介进每批 prompt，AI 结合整体主题归类
	listCtx := listOrganizeContext(job.ListTitle, job.ListIntro)

	var batches [][]*repo.Problem
	for lo := 0; lo < len(job.Scope); lo += listOrganizeBatchSize {
		hi := lo + listOrganizeBatchSize
		if hi > len(job.Scope) {
			hi = len(job.Scope)
		}
		batches = append(batches, job.Scope[lo:hi])
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, tidyWorkers)
	results := make([]map[int64]string, len(batches))
	var firstBatchErr string
	for bi := range batches {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(bi int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			batch := batches[bi]
			user := listOrganizeUser(listCtx, leafText, batch, knNames, metaNames)
			resp, err := h.tidyAIChat(ctx, uid, "list_organize", 0, sys, user, false)
			if err == nil {
				var m map[int64]string
				m, err = parseListOrganizeAssign(resp.Content, batch, job.Keys)
				if err == nil {
					results[bi] = m
				}
			}
			job.mu.Lock()
			job.Done += len(batch) // 批无论成败都算已处理（失败批的题落未匹配）
			if err != nil && ctx.Err() == nil {
				job.FailedBatches++
				if firstBatchErr == "" {
					firstBatchErr = truncateTail(err.Error(), 120)
				}
			}
			job.mu.Unlock()
		}(bi)
	}
	wg.Wait()

	assigned := map[int64]string{}
	for _, m := range results {
		for pid, key := range m {
			assigned[pid] = key
		}
	}
	job.assemble(assigned)

	if ctx.Err() != nil {
		job.finish("cancelled", "")
		return
	}
	if n, total := job.batchFailStats(), len(batches); n > 0 && n == total {
		job.finish("failed", "全部批次 AI 调用失败："+firstBatchErr)
		return
	}
	job.finish("done", "")
}

// batchFailStats 失败批数快照。
func (j *listOrganizeJob) batchFailStats() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.FailedBatches
}

// inPIDRange 题号范围过滤（from/to 0 = 不限，含边界；同题库整理 from/to 口径）。
func inPIDRange(pid, from, to int64) bool {
	if from > 0 && pid < from {
		return false
	}
	if to > 0 && pid > to {
		return false
	}
	return true
}

// listOrganizeScopeHit 题库拉取筛选（维内任一命中、维间 AND，同题库列表页口径；
// 只作用于题库拉取，include_mounted 纳入的已挂题不受收窄）。
func listOrganizeScopeHit(p *repo.Problem, difficulty []int, tags, meta []int64) bool {
	if len(difficulty) > 0 {
		hit := false
		for _, d := range difficulty {
			if p.Difficulty == d {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(tags) > 0 && !anyIDHit(p.Tags, tags) {
		return false
	}
	if len(meta) > 0 && !anyIDHit(p.MetaTags, meta) {
		return false
	}
	return true
}

func anyIDHit(ids, sel []int64) bool {
	for _, s := range sel {
		for _, id := range ids {
			if id == s {
				return true
			}
		}
	}
	return false
}

// ---- prompt 组装与解析 ----

func listOrganizeSystem() string {
	return `你是信息学竞赛题单的结构化整理专家。给你一个题单的章节结构（若干可挂题的章/小节，均有唯一 key）和一批题目，请把每道题归入语义上最合适的一个章/小节。
规则：
1. 每道题恰好输出一条归类；leaf 只能取给出的叶子 key 或 "unmatched"。
2. 以题目内容（标题、题面）与章节完整语境（章名 › 节名、章/节描述）的语义匹配为主，并结合题单整体主题（标题/简介，如有给出）判断大类方向；难度、知识点、来源标签作参考。
3. 章节分得细时归入语义最贴切的那一个；确实不属于任何章/小节的题输出 "unmatched"，宁缺毋滥不要硬塞。
4. 只输出 JSON，格式为 {"assign":[{"pid":<题号>,"leaf":"<叶子key或unmatched>"}]}，不要输出任何其他内容。`
}

func listOrganizeLeavesPrompt(leaves []listOrganizeLeaf) string {
	var b strings.Builder
	b.WriteString("章节结构（叶子 key 完整路径 [本叶描述] （所属章描述））：\n")
	for _, lf := range leaves {
		line := lf.Key + " " + lf.Path
		if lf.Desc != "" {
			line += " [" + lf.Desc + "]"
		}
		if lf.ChapDesc != "" {
			line += "（章：" + lf.ChapDesc + "）"
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// listOrganizeContext 题单全局语境（v0.83.3）：标题/简介进 prompt，AI 先知整体主题再归
// 章节——章/节名与题单主题脱节时有全局兜底。两者均空返回空串（prompt 不加空段）；简介
// 截 200 runes 封住体量（章节描述校验 ≤128 字，简介 Markdown 可能更长）。
func listOrganizeContext(title, intro string) string {
	t, d := strings.TrimSpace(title), strings.TrimSpace(intro)
	if t == "" && d == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("题单主题：")
	if t != "" {
		b.WriteString("《" + t + "》")
	}
	if d != "" {
		b.WriteString(" " + truncateTail(d, 200))
	}
	b.WriteString("\n")
	return b.String()
}

func listOrganizeUser(listCtx, leafText string, batch []*repo.Problem, knNames, metaNames map[int64]string) string {
	var b strings.Builder
	if listCtx != "" {
		b.WriteString(listCtx)
		b.WriteString("\n")
	}
	b.WriteString(leafText)
	b.WriteString("\n本批题目：\n")
	for _, p := range batch {
		fmt.Fprintf(&b, "#%d %s\n", p.ID, p.Title)
		if p.Difficulty >= 1 && p.Difficulty <= 10 {
			fmt.Fprintf(&b, "难度：D%d\n", p.Difficulty)
		}
		if kn := joinTagNames(p.Tags, knNames); kn != "" {
			fmt.Fprintf(&b, "知识点：%s\n", kn)
		}
		if mt := joinTagNames(p.MetaTags, metaNames); mt != "" {
			fmt.Fprintf(&b, "来源：%s\n", mt)
		}
		if st := strings.TrimSpace(p.Statement); st != "" {
			fmt.Fprintf(&b, "题面：%s\n", truncateTail(st, listOrganizeStatementMaxRunes))
		}
		b.WriteString("---\n")
	}
	b.WriteString("\n请输出归类 JSON。")
	return b.String()
}

func joinTagNames(ids []int64, nameOf map[int64]string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := nameOf[id]; n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, "、")
}

// parseListOrganizeAssign 解析 AI 批量归类输出：仅接受本批 pid 与已知叶 key——
// 未知 key（幻觉）/批外 pid/重复 pid 一律丢弃；"unmatched" 显式不映射即未匹配。
func parseListOrganizeAssign(content string, batch []*repo.Problem, keys map[string]bool) (map[int64]string, error) {
	var out struct {
		Assign []struct {
			PID  int64  `json:"pid"`
			Leaf string `json:"leaf"`
		} `json:"assign"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &out); err != nil {
		return nil, fmt.Errorf("%w: %s", err, truncateTail(content, 80))
	}
	inBatch := map[int64]bool{}
	for _, p := range batch {
		inBatch[p.ID] = true
	}
	m := map[int64]string{}
	for _, a := range out.Assign {
		if !inBatch[a.PID] {
			continue
		}
		if _, dup := m[a.PID]; dup {
			continue
		}
		if a.Leaf == "unmatched" || !keys[a.Leaf] {
			continue
		}
		m[a.PID] = a.Leaf
	}
	return m, nil
}
