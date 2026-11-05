package server

// 题库整理（v0.43，plan §2.45；v0.49 拆双模式）：AI 打标 + 规则归一化 + 手动直改。
// 两段式铁律（2026-10-31 拍板）：整理任务只产**建议**（逐题差异 + 冲突标记），
// root 在预览表勾选确认后经 TidyApply 走 UpdateProblemMeta 校验链批量写库——
// 不自动写库、不自动建树节点、不动题面、无定时自动整理（纯 root 手动触发）。
// 三项共用单任务骨架（照备份/导入 job 模式：running/done/cancelled/failed + 轮询 + 取消）：
//   difficulty = AI 读题面为主 + 通过率启发式交叉验证（档差 ≥2 判冲突，AI 失败按通过率兜底）
//   knowledge  = AI 从知识树候选集选名（幻觉/歧义/树外名丢弃，不自动建节点）
//   meta       = 纯规则零 AI（ParseSourceHint + MatchMetaTags 对齐重挂 + 树外孤儿剔除）
// v0.49 手动模式（root 拍板）：不经 AI 无建议环节——分页题目表（每页 ≤100 题）行内
// 手改难度/知识点/来源标签，TidyManualApply 收最终值批量写库并按字段对应 kind 打
// tidiedAt 水位（保存过 = 已整理，AI 整理默认跳过，redo 可重跑）。
// 建议快照按页下发（v0.49 验收期调整）：status 端点 page/page_size 切片，页面只持有
// 当前页——全量建议整包进 2s 轮询是 2000 题规模页面卡顿的根源。
// AI 调用走既有并发闸与 aiCalls 审计（kind=tidy_*，cost=0 管理侧不扣玉；不占学生每日上限——
// 该上限保护的是学生侧 AI 功能配额，root 批量整理是后台任务）。

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

	"soj/internal/ai"
	"soj/internal/repo"
)

// tidyKinds 支持的整理类型。
var tidyKinds = map[string]bool{"difficulty": true, "knowledge": true, "meta": true}

// tidyWorkers AI 类任务的并发度（对齐 AI 客户端闸 4 路留余量给在线功能）。
const tidyWorkers = 3

// TidySuggestion 逐题建议（kind 专属字段按需填充，JSON 直接下发渲染预览表）。
type TidySuggestion struct {
	PID   int64  `json:"pid"`
	Title string `json:"title"`
	Kind  string `json:"kind"`

	// difficulty：AI 主建议 + 通过率辅助参考 + 冲突标记
	HasDifficulty     bool    `json:"has_difficulty"`            // 已显式设过难度（预览默认不勾）
	CurDifficulty     int     `json:"cur_difficulty"`            // 现值（0 = 未设置）
	SuggestDifficulty int     `json:"suggest_difficulty"`        // 建议（AI 或兜底）
	RateDifficulty    int     `json:"rate_difficulty"`           // 通过率档位（辅助参考列）
	PassRate          float64 `json:"pass_rate,omitempty"`       // 原始通过率（0–1，展示 %）
	Attempted         int64   `json:"attempted"`                 // 做过人数（0 = 无人做过）
	Conflict          bool    `json:"conflict"`                  // |AI 档 − 通过率档| ≥ 2（置顶 + 默认不勾，root 改判）
	AIError           string  `json:"ai_error,omitempty"`        // 非空 = AI 失败已按通过率兜底的原因

	// knowledge：现有/新增/移除三段差集 + 丢弃名单
	CurTagIDs      []int64  `json:"cur_tag_ids,omitempty"`
	CurTagNames    []string `json:"cur_tag_names,omitempty"`
	AddTagIDs      []int64  `json:"add_tag_ids,omitempty"`
	AddTagNames    []string `json:"add_tag_names,omitempty"`
	RemoveTagIDs   []int64  `json:"remove_tag_ids,omitempty"`
	RemoveTagNames []string `json:"remove_tag_names,omitempty"`
	Dropped        []string `json:"dropped,omitempty"` // 幻觉/歧义/树外名（供 root 核对 AI 行为）

	// meta：规则重挂结果
	CurMetaIDs       []int64  `json:"cur_meta_ids,omitempty"`
	CurMetaNames     []string `json:"cur_meta_names,omitempty"`
	SuggestMetaIDs   []int64  `json:"suggest_meta_ids,omitempty"`
	SuggestMetaNames []string `json:"suggest_meta_names,omitempty"`
	NoClue           bool     `json:"no_clue,omitempty"` // 标题无来源线索（只列出不动）
	Note             string   `json:"note,omitempty"`
}

// tidyJob 一次整理任务的全量状态（自身带锁，供轮询快照与取消）。
type tidyJob struct {
	mu          sync.Mutex
	State       string // running | done | cancelled | failed
	Kind        string
	Total       int
	Done        int
	Excluded    int   // 水位排除数（已整理/已设难度；redo=false 时），快照带出让 root 有感知
	From, To    int64 // 题号范围（0 = 不限），快照带出让 root 记得本次任务边界
	Suggestions []TidySuggestion
	Err         string
	ByName      string
	StartedAt   time.Time
	EndedAt     time.Time
	cancel      context.CancelFunc
}

func (j *tidyJob) finish(state, errMsg string) {
	j.mu.Lock()
	if j.State == "running" {
		j.State = state
		j.Err = errMsg
		j.EndedAt = time.Now()
	}
	j.mu.Unlock()
}

// addSuggestion 收一条建议 + 进度 +1（worker 并发回填，锁内追加；终态前统一定序）。
func (j *tidyJob) addSuggestion(s TidySuggestion) {
	j.mu.Lock()
	j.Suggestions = append(j.Suggestions, s)
	j.Done++
	j.mu.Unlock()
}

func (j *tidyJob) snapshot(page, pageSize int, onlyConflict bool) gin.H {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]TidySuggestion, len(j.Suggestions))
	copy(out, j.Suggestions)
	// 冲突置顶（预览表默认排序）：难度冲突 > 有建议的 > 其余，同类按题号
	sort.SliceStable(out, func(a, b int) bool {
		pa, pb := out[a], out[b]
		if pa.Kind == "difficulty" && pb.Kind == "difficulty" && pa.Conflict != pb.Conflict {
			return pa.Conflict
		}
		return pa.PID < pb.PID
	})
	// 冲突总数与「只看冲突」过滤基于全量建议算好再切页（v0.49 验收期调整：
	// 快照按页下发——2000 题全量建议整包进 2s 轮询响应是页面卡顿根源，
	// 页面上只持有当前页 ≤100 条，跨页勾选随之退役）
	conflictN := 0
	for i := range out {
		if out[i].Kind == "difficulty" && out[i].Conflict {
			conflictN++
		}
	}
	if onlyConflict {
		flt := make([]TidySuggestion, 0, conflictN)
		for _, s := range out {
			if s.Kind == "difficulty" && s.Conflict {
				flt = append(flt, s)
			}
		}
		out = flt
	}
	total := len(out)
	lo := (page - 1) * pageSize
	if lo < 0 {
		lo = 0
	}
	if lo > total {
		lo = total
	}
	hi := lo + pageSize
	if hi > total {
		hi = total
	}
	return gin.H{
		"state": j.State, "kind": j.Kind, "total": j.Total, "done": j.Done,
		"excluded": j.Excluded, "from": j.From, "to": j.To,
		"suggestions": out[lo:hi], "suggestion_total": total, "conflict_total": conflictN,
		"error": j.Err, "by": j.ByName,
		"started_at": j.StartedAt, "ended_at": j.EndedAt,
	}
}

// ---- 路由（root 专属） ----

// TidyRun POST /api/admin/tidy/run {kind, redo?, from?, to?}：启动整理任务（全站单任务，进行中 409）。
// redo=false（默认）排除已整理过的题（tidiedAt.<kind> 水位 + 难度类含已设难度）——
// 断点续整：上次应用过的不再跑，剩未整理的按题号从后继续；redo=true 全量重跑。
// from/to（含边界，v0.45 验收期增强）：圈定题号范围只扫部分库，与水位排除叠加——
// 先按范围圈题，圈内再走水位排除；范围外不进任务也不计 excluded。
func (h *Handlers) TidyRun(c *gin.Context) {
	var req struct {
		Kind string `json:"kind"`
		Redo bool   `json:"redo"`
		From *int64 `json:"from"` // 题号下界（含）；nil = 不限
		To   *int64 `json:"to"`   // 题号上界（含）；nil = 不限
	}
	if err := c.ShouldBindJSON(&req); err != nil || !tidyKinds[req.Kind] {
		fail(c, http.StatusBadRequest, "kind 需为 difficulty / knowledge / meta")
		return
	}
	if (req.From != nil && *req.From < 1) || (req.To != nil && *req.To < 1) ||
		(req.From != nil && req.To != nil && *req.From > *req.To) {
		fail(c, http.StatusBadRequest, "题号范围不合法：需 1 ≤ 起始 ≤ 结束，留空表示全库")
		return
	}
	// AI 类任务前置校验供应商（meta 纯规则不需要）
	if req.Kind != "meta" {
		cfg, _ := h.repo.GetAIConfig(c.Request.Context())
		if cfg.ActiveProvider() == nil {
			fail(c, http.StatusBadRequest, "AI 供应商未配置——请先在「AI 助教」面板配置；来源标签整理（规则）不受影响")
			return
		}
	}
	uid, name, _, _ := currentUser(c)

	var from, to int64
	if req.From != nil {
		from = *req.From
	}
	if req.To != nil {
		to = *req.To
	}

	h.tidyMu.Lock()
	if h.tidyTask != nil && h.tidyTask.State == "running" {
		h.tidyMu.Unlock()
		fail(c, http.StatusConflict, "已有整理任务进行中，请等待完成或先取消")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &tidyJob{State: "running", Kind: req.Kind, From: from, To: to, ByName: name, StartedAt: time.Now(), cancel: cancel}
	h.tidyTask = job
	h.tidyMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				job.finish("failed", fmt.Sprintf("整理任务异常: %v", r))
			}
		}()
		var err error
		switch req.Kind {
		case "difficulty":
			err = h.runTidyDifficulty(ctx, job, uid, req.Redo)
		case "knowledge":
			err = h.runTidyKnowledge(ctx, job, uid, req.Redo)
		case "meta":
			err = h.runTidyMeta(ctx, job, req.Redo)
		}
		if err != nil {
			if ctx.Err() != nil {
				job.finish("cancelled", "")
				return
			}
			job.finish("failed", err.Error())
			return
		}
		job.finish("done", "")
	}()
	ok(c, gin.H{"started": true, "kind": req.Kind})
}

// TidyStatus GET /api/admin/tidy/status：任务进度与建议结果（无任务 = idle）。
// 建议按页下发（v0.49 验收期调整，page/page_size 走 queryPage 上限 100）：
// only_conflict=true 时先全量过滤难度冲突再切页；suggestion_total/conflict_total
// 为全量口径（翻页器与「只看冲突」计数用），页面只持有当前页切片。
func (h *Handlers) TidyStatus(c *gin.Context) {
	page, pageSize := queryPage(c)
	onlyConflict := c.Query("only_conflict") == "true"
	h.tidyMu.Lock()
	job := h.tidyTask
	h.tidyMu.Unlock()
	if job == nil {
		ok(c, gin.H{"state": "idle"})
		return
	}
	ok(c, job.snapshot(page, pageSize, onlyConflict))
}

// TidyCancel POST /api/admin/tidy/cancel：取消进行中任务（中断在途 AI 调用与后续题）。
func (h *Handlers) TidyCancel(c *gin.Context) {
	h.tidyMu.Lock()
	job := h.tidyTask
	h.tidyMu.Unlock()
	if job != nil && job.State == "running" {
		job.cancel()
	}
	ok(c, gin.H{"cancel_requested": true})
}

// TidyApply POST /api/admin/tidy/apply：把勾选的建议批量写库（root 确认后的第二段）。
// body {kind, items:[{pid, difficulty?, tags?, meta_tags?}]}——字段均为「最终值」
//（root 可改判，不区分建议值与手改值）；逐题走 UpdateProblemMeta + 前置校验链，
// 单题失败跳过出报告，不中断其余。
func (h *Handlers) TidyApply(c *gin.Context) {
	var req struct {
		Kind  string `json:"kind"`
		Items []struct {
			PID        int64    `json:"pid"`
			Difficulty *int     `json:"difficulty,omitempty"`
			Tags       *[]int64 `json:"tags,omitempty"`
			MetaTags   *[]int64 `json:"meta_tags,omitempty"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !tidyKinds[req.Kind] || len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "请求不合法：kind 与非空 items 必填")
		return
	}
	if req.Kind == "meta" {
		// 规则整理的建议集已在生成期剔除树外 ID；apply 侧再校验一次防旧预览提交
		for _, it := range req.Items {
			if it.MetaTags != nil {
				if err := h.repo.ValidateMetaTagRefs(c.Request.Context(), *it.MetaTags); err != nil {
					fail(c, http.StatusBadRequest, fmt.Sprintf("题 #%d 的来源标签含树外节点（预览过期，请重跑整理任务）", it.PID))
					return
				}
			}
		}
	}
	if req.Kind == "knowledge" {
		for _, it := range req.Items {
			if it.Tags != nil {
				if err := h.repo.ValidateTagRefs(c.Request.Context(), *it.Tags); err != nil {
					fail(c, http.StatusBadRequest, fmt.Sprintf("题 #%d 的知识点含树外节点（预览过期，请重跑整理任务）", it.PID))
					return
				}
			}
		}
	}

	ctx := c.Request.Context()
	okN, failN := 0, 0
	var appliedPIDs []int64
	var firstErr string
	for _, it := range req.Items {
		patch := repo.ProblemMetaPatch{}
		switch req.Kind {
		case "difficulty":
			if it.Difficulty == nil || !repo.ValidDifficulty(*it.Difficulty) {
				failN++
				if firstErr == "" {
					firstErr = fmt.Sprintf("题 #%d 难度值非法", it.PID)
				}
				continue
			}
			patch.Difficulty = it.Difficulty
		case "knowledge":
			if it.Tags == nil {
				failN++
				continue
			}
			patch.Tags = it.Tags
		case "meta":
			if it.MetaTags == nil {
				failN++
				continue
			}
			patch.MetaTags = it.MetaTags
		}
		if err := h.repo.UpdateProblemMeta(ctx, it.PID, patch); err != nil {
			failN++
			if firstErr == "" {
				firstErr = fmt.Sprintf("题 #%d 写库失败: %v", it.PID, err)
			}
			continue
		}
		okN++
		appliedPIDs = append(appliedPIDs, it.PID)
	}
	msg := fmt.Sprintf("已应用 %d 题", okN)
	if failN > 0 {
		msg += fmt.Sprintf("，失败 %d（%s）", failN, firstErr)
	}
	// 应用成功 = 已整理水位（下次默认跳过，redo 可重跑）；失败不致命，水位落后仅重复建议
	if err := h.repo.MarkTidied(ctx, req.Kind, appliedPIDs); err != nil {
		ok(c, gin.H{"applied": okN, "failed": failN, "message": msg + "（水位记录失败，下次可能重复建议这些题）"})
		return
	}
	ok(c, gin.H{"applied": okN, "failed": failN, "message": msg})
}

// TidyManualProblems GET /api/admin/tidy/problems?page=&page_size=100&q=：手动整理分页题目表
//（v0.49，root 特权视角零排除——隐藏/重复/锁题均在列）。下发原始难度（0 = 未设置，
// 非 EffDifficulty 的 D3 兜底）与双树标签 ID，编辑控件在前端展开。
func (h *Handlers) TidyManualProblems(c *gin.Context) {
	page, pageSize := queryPage(c)
	filter := repo.ProblemListFilter{Q: strings.TrimSpace(c.Query("q"))}
	probs, total, err := h.repo.ListProblemsPage(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题库失败")
		return
	}
	items := make([]gin.H, 0, len(probs))
	for _, p := range probs {
		items = append(items, gin.H{
			"id":         p.ID,
			"title":      p.Title,
			"difficulty": p.Difficulty,
			"tags":       orEmptyInt64(p.Tags),
			"meta_tags":  orEmptyInt64(p.MetaTags),
		})
	}
	ok(c, gin.H{"list": items, "total": total})
}

// TidyManualApply POST /api/admin/tidy/manual：手动整理批量写库（root 手改最终值，
// v0.49）。body {items:[{pid, difficulty?, tags?, meta_tags?}]}——字段 nil = 不动该字段，
// 非空 = 最终值整体替换（同 TidyApply 语义）；逐题走 UpdateProblemMeta 校验链，
// 单题失败跳过出报告不中断其余。保存成功按 item 携带的字段打水位
//（difficulty→difficulty、tags→knowledge、meta_tags→meta）：手动整理过 = 已整理，
// 下次 AI 整理默认跳过（redo 可重跑）。
func (h *Handlers) TidyManualApply(c *gin.Context) {
	var req struct {
		Items []struct {
			PID        int64    `json:"pid"`
			Difficulty *int     `json:"difficulty,omitempty"`
			Tags       *[]int64 `json:"tags,omitempty"`
			MetaTags   *[]int64 `json:"meta_tags,omitempty"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "请求不合法：非空 items 必填")
		return
	}
	ctx := c.Request.Context()
	// 双树引用预校验（照 TidyApply 口径整批 400）：编辑控件吃树 store，树外引用意味着
	// store 过期（树刚被改），整批拒掉让前端重拉比逐题静默失败更诚实
	for _, it := range req.Items {
		if it.Tags != nil {
			if err := h.repo.ValidateTagRefs(ctx, *it.Tags); err != nil {
				fail(c, http.StatusBadRequest, fmt.Sprintf("题 #%d 的知识点引用过期（%v），请刷新页面后重试", it.PID, err))
				return
			}
		}
		if it.MetaTags != nil {
			if err := h.repo.ValidateMetaTagRefs(ctx, *it.MetaTags); err != nil {
				fail(c, http.StatusBadRequest, fmt.Sprintf("题 #%d 的来源标签引用过期（%v），请刷新页面后重试", it.PID, err))
				return
			}
		}
	}

	okN, failN := 0, 0
	var firstErr string
	var wmPIDs = map[string][]int64{} // kind → 成功写入的题（按字段对应水位）
	for _, it := range req.Items {
		patch := repo.ProblemMetaPatch{}
		if it.Difficulty != nil {
			if !repo.ValidDifficulty(*it.Difficulty) {
				failN++
				if firstErr == "" {
					firstErr = fmt.Sprintf("题 #%d 难度值非法", it.PID)
				}
				continue
			}
			patch.Difficulty = it.Difficulty
		}
		if it.Tags != nil {
			patch.Tags = it.Tags
		}
		if it.MetaTags != nil {
			patch.MetaTags = it.MetaTags
		}
		if patch.Empty() {
			failN++
			if firstErr == "" {
				firstErr = fmt.Sprintf("题 #%d 没有需要更新的字段", it.PID)
			}
			continue
		}
		if err := h.repo.UpdateProblemMeta(ctx, it.PID, patch); err != nil {
			failN++
			if firstErr == "" {
				firstErr = fmt.Sprintf("题 #%d 写库失败: %v", it.PID, err)
			}
			continue
		}
		okN++
		if it.Difficulty != nil {
			wmPIDs["difficulty"] = append(wmPIDs["difficulty"], it.PID)
		}
		if it.Tags != nil {
			wmPIDs["knowledge"] = append(wmPIDs["knowledge"], it.PID)
		}
		if it.MetaTags != nil {
			wmPIDs["meta"] = append(wmPIDs["meta"], it.PID)
		}
	}
	msg := fmt.Sprintf("已保存 %d 题", okN)
	if failN > 0 {
		msg += fmt.Sprintf("，失败 %d（%s）", failN, firstErr)
	}
	for _, kind := range []string{"difficulty", "knowledge", "meta"} {
		if pids := wmPIDs[kind]; len(pids) > 0 {
			if err := h.repo.MarkTidied(ctx, kind, pids); err != nil {
				msg += "（部分水位记录失败，下次 AI 整理可能重复建议这些题）"
				break
			}
		}
	}
	ok(c, gin.H{"applied": okN, "failed": failN, "message": msg})
}

// ---- 三类任务体 ----

// runTidyDifficulty 难度整理：AI 读题面（锚点表同尺）为主建议，通过率档位交叉验证，
// 档差 ≥2 判冲突；AI 未配置/失败按通过率启发式兜底（无人做过 D3，启发式最高 D9——
// D10 只由 AI 建议或人工给）。已设难度的题照常出建议但标记 has_difficulty（预览默认不勾）。
func (h *Handlers) runTidyDifficulty(ctx context.Context, job *tidyJob, uid int64, redo bool) error {
	probs, err := h.repo.TidyListProblems(ctx)
	if err != nil {
		return err
	}
	probs = filterRange(probs, job.From, job.To)
	// 水位排除（redo=false）：已整理过（tidiedAt.difficulty）与已显式设难度的题
	// 都不进本轮——后者照常出建议只烧 token（apply 后才有水位），直接不跑
	if !redo {
		kept := probs[:0]
		for _, p := range probs {
			if _, done := p.TidiedAt["difficulty"]; done || (p.Difficulty >= 1 && p.Difficulty <= 10) {
				job.mu.Lock()
				job.Excluded++
				job.mu.Unlock()
				continue
			}
			kept = append(kept, p)
		}
		probs = kept
	}
	rates, attempted, err := h.repo.TidyPassRates(ctx)
	if err != nil {
		return err
	}
	lcfg, _ := h.repo.GetLevelConfig(ctx)
	notes := lcfg.DifficultyNotes

	job.mu.Lock()
	job.Total = len(probs)
	job.mu.Unlock()

	sys := tidyDifficultySystem(notes)

	var wg sync.WaitGroup
	sem := make(chan struct{}, tidyWorkers)
	for _, p := range probs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(p *repo.Problem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s := h.tidySuggestDifficulty(ctx, uid, sys, p, rates[p.ID], attempted[p.ID])
			// 取消后队列里的 worker 会拿到零值建议——不入结果（Done 只计真实建议）
			if ctx.Err() == nil {
				job.addSuggestion(s)
			}
		}(p)
	}
	wg.Wait()
	return ctx.Err()
}

// tidySuggestDifficulty 单题难度建议（worker 内执行；AI 失败兜底不报错）。
func (h *Handlers) tidySuggestDifficulty(ctx context.Context, uid int64, sys string, p *repo.Problem, rate float64, attempted int64) TidySuggestion {
	s := TidySuggestion{
		PID: p.ID, Title: p.Title, Kind: "difficulty",
		HasDifficulty: p.Difficulty >= 1 && p.Difficulty <= 10,
		CurDifficulty: p.Difficulty,
		RateDifficulty: rateTier(rate, attempted),
		PassRate:       rate, Attempted: attempted,
	}
	// 启发式兜底值（AI 成功则被覆盖；AI 失败/未用时生效）
	heuristic := s.RateDifficulty

	user := tidyDifficultyUser(p)
	resp, err := h.tidyAIChat(ctx, uid, "tidy_difficulty", p.ID, sys, user)
	if err != nil {
		if ctx.Err() != nil {
			return s // 任务取消中，结果无所谓
		}
		s.SuggestDifficulty = heuristic
		s.AIError = "AI 调用失败，按通过率兜底：" + truncateTail(err.Error(), 120)
		return s
	}
	d, perr := parseTidyDifficulty(resp.Content)
	if perr != nil {
		s.SuggestDifficulty = heuristic
		s.AIError = "AI 输出无法解析，按通过率兜底：" + truncateTail(perr.Error(), 120)
		return s
	}
	s.SuggestDifficulty = d
	s.Conflict = attempted > 0 && abs(d-s.RateDifficulty) >= 2
	return s
}

// runTidyKnowledge 知识点整理：知识树候选名（按一级分类分组）进 prompt，AI 返回名字
// 列表 → 唯一名映射回 ID（同名歧义/幻觉/树外丢弃不挂）；差集 = 新增/移除。
func (h *Handlers) runTidyKnowledge(ctx context.Context, job *tidyJob, uid int64, redo bool) error {
	probs, err := h.repo.TidyListProblems(ctx)
	if err != nil {
		return err
	}
	probs = filterRange(probs, job.From, job.To)
	if !redo {
		probs = excludeTidied(probs, "knowledge", job)
	}
	tree, err := h.repo.GetTagTree(ctx)
	if err != nil {
		return err
	}
	uniq, ambiguous := uniqueTagLookup(tree)
	nameOf := tagNameIDToName(tree)
	candidates := tidyKnowledgeCandidates(tree)

	job.mu.Lock()
	job.Total = len(probs)
	job.mu.Unlock()

	sys := tidyKnowledgeSystem()
	var wg sync.WaitGroup
	sem := make(chan struct{}, tidyWorkers)
	for _, p := range probs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(p *repo.Problem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s := TidySuggestion{PID: p.ID, Title: p.Title, Kind: "knowledge"}
			for _, id := range p.Tags {
				s.CurTagIDs = append(s.CurTagIDs, id)
				if n := nameOf[id]; n != "" {
					s.CurTagNames = append(s.CurTagNames, n)
				}
			}
			cur := map[int64]bool{}
			for _, id := range p.Tags {
				cur[id] = true
			}

			user := tidyKnowledgeUser(candidates, s.CurTagNames, p)
			resp, err := h.tidyAIChat(ctx, uid, "tidy_knowledge", p.ID, sys, user)
			if err != nil {
				if ctx.Err() == nil {
					s.Note = "AI 调用失败：" + truncateTail(err.Error(), 120)
					job.addSuggestion(s)
				}
				return
			}
			names, perr := parseTidyNameList(resp.Content)
			if perr != nil {
				s.Note = "AI 输出无法解析：" + truncateTail(perr.Error(), 120)
				if ctx.Err() == nil {
					job.addSuggestion(s)
				}
				return
			}
			suggested := map[int64]bool{}
			for _, name := range names {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				id, ok := uniq[name]
				if !ok {
					if ambiguous[name] {
						s.Dropped = append(s.Dropped, name+"（同名歧义）")
					} else {
						s.Dropped = append(s.Dropped, name+"（树外名）")
					}
					continue
				}
				suggested[id] = true
				if !cur[id] {
					s.AddTagIDs = append(s.AddTagIDs, id)
					s.AddTagNames = append(s.AddTagNames, name)
				}
			}
			for _, id := range p.Tags {
				if !suggested[id] {
					s.RemoveTagIDs = append(s.RemoveTagIDs, id)
					s.RemoveTagNames = append(s.RemoveTagNames, nameOf[id])
				}
			}
			if ctx.Err() == nil {
				job.addSuggestion(s)
			}
		}(p)
	}
	wg.Wait()
	return ctx.Err()
}

// runTidyMeta 来源标签整理（纯规则零 AI）：全量重跑标题解析 + MatchMetaTags 对齐；
// 建议集 = 现有树内标签 ∪ 标题线索命中（手动挂的来源不丢），树外孤儿剔除
//（apply 整组覆写 meta_tags，孤儿自然清除）；标题无线索的题只列出不动。
func (h *Handlers) runTidyMeta(ctx context.Context, job *tidyJob, redo bool) error {
	probs, err := h.repo.TidyListProblems(ctx)
	if err != nil {
		return err
	}
	probs = filterRange(probs, job.From, job.To)
	if !redo {
		probs = excludeTidied(probs, "meta", job)
	}
	metaTree, err := h.repo.GetMetaTagTree(ctx)
	if err != nil {
		return err
	}
	nameOf := tagNameIDToName(metaTree)
	inTree := map[int64]bool{}
	var walk func(list []repo.TagNode)
	walk = func(list []repo.TagNode) {
		for _, n := range list {
			inTree[n.ID] = true
			walk(n.Children)
		}
	}
	walk(metaTree)

	job.mu.Lock()
	job.Total = len(probs)
	job.mu.Unlock()

	for _, p := range probs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s := TidySuggestion{PID: p.ID, Title: p.Title, Kind: "meta"}
		for _, id := range p.MetaTags {
			s.CurMetaIDs = append(s.CurMetaIDs, id)
			if n := nameOf[id]; n != "" {
				s.CurMetaNames = append(s.CurMetaNames, n)
			}
		}
		hint := repo.ParseSourceHint(p.Title)
		if hint.Source == "" && hint.Year == 0 && hint.Region == "" {
			s.NoClue = true
			s.Note = "标题无来源线索，未生成建议"
			// 无线索题：仍剔除树外孤儿（有则出建议，否则纯列出）
			orphans := false
			for _, id := range p.MetaTags {
				if !inTree[id] {
					orphans = true
					break
				}
			}
			if !orphans {
				job.addSuggestion(s)
				continue
			}
		}
		keep := map[int64]bool{}
		for _, id := range p.MetaTags {
			if inTree[id] {
				keep[id] = true
			}
		}
		if hint.Source != "" || hint.Year != 0 || hint.Region != "" {
			for _, id := range h.repo.MatchMetaTags(ctx, hint) {
				keep[id] = true
			}
		}
		suggest := make([]int64, 0, len(keep))
		for id := range keep {
			suggest = append(suggest, id)
		}
		sort.Slice(suggest, func(a, b int) bool { return suggest[a] < suggest[b] })
		s.SuggestMetaIDs = suggest
		for _, id := range suggest {
			if n := nameOf[id]; n != "" {
				s.SuggestMetaNames = append(s.SuggestMetaNames, n)
			} else {
				s.SuggestMetaNames = append(s.SuggestMetaNames, "#"+strconv.FormatInt(id, 10))
			}
		}
		job.addSuggestion(s)
	}
	return ctx.Err()
}

// ---- AI 调用与解析 ----

// tidyAIChat 整理任务的 AI 外呼（job ctx 取消即中断；审计 kind=tidy_*，cost=0，
// 不占学生每日上限——该上限保护的是学生侧 AI 配额，root 批量整理是后台任务）。
func (h *Handlers) tidyAIChat(ctx context.Context, uid int64, kind string, pid int64, system, user string) (*ai.Response, error) {
	cfg, _ := h.repo.GetAIConfig(ctx)
	p := cfg.ActiveProvider()
	if p == nil {
		return nil, ai.ErrNoProvider
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := ai.NewClient(p).Chat(cctx, system, user)
	rec := &repo.AiCall{
		UID: uid, PID: pid, Kind: kind, Cost: 0, Provider: p.Name, Model: p.Model,
		OK: err == nil, At: time.Now(),
	}
	if resp != nil {
		rec.TokensTotal = resp.TotalTokens
		rec.PromptChars = resp.PromptChars
	}
	if err != nil {
		rec.Err = truncateTail(err.Error(), 300)
	}
	// 审计写库用独立短超时 ctx：任务取消路径上 job ctx 已取消，在途调用被中断的
	// 错误记录若沿用原 ctx 写库会静默丢失（v0.71 生产日志实锤——取消瞬间的报错
	// 无审计可查），脱离原 ctx 才能留下「这批为何没产出建议」的痕迹
	actx, acancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer acancel()
	h.repo.RecordAiCall(actx, rec)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// extractJSON 从模型回复中提取 JSON（剥 ```json 围栏 / 前后闲话，取首 { 到尾 }）。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func parseTidyDifficulty(content string) (int, error) {
	var out struct {
		Difficulty int `json:"difficulty"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &out); err != nil {
		return 0, fmt.Errorf("%w: %s", err, truncateTail(content, 80))
	}
	if !repo.ValidDifficulty(out.Difficulty) {
		return 0, fmt.Errorf("难度 %d 越界（1–10）", out.Difficulty)
	}
	return out.Difficulty, nil
}

func parseTidyNameList(content string) ([]string, error) {
	var out struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &out); err != nil {
		return nil, fmt.Errorf("%w: %s", err, truncateTail(content, 80))
	}
	return out.Tags, nil
}

// ---- prompt 组装 ----

// tidyDifficultySystem 难度评估 system（锚点表 = root 后台当前 DifficultyNotes，与
// 站内难度说明卡同一把尺子）。
func tidyDifficultySystem(notes [10]string) string {
	var b strings.Builder
	b.WriteString("你是信息学竞赛题目的难度评估专家。根据题目内容，对照下面的难度锚点表，输出 1–10 的难度档位。\n\n难度锚点表：\n")
	for i, n := range notes {
		if strings.TrimSpace(n) == "" {
			continue
		}
		fmt.Fprintf(&b, "D%d：%s\n", i+1, n)
	}
	b.WriteString("\n要求：只输出 JSON，格式为 {\"difficulty\": <1-10 的整数>}，不要输出任何其他内容。")
	return b.String()
}

const tidyStatementMaxRunes = 6000

func tidyDifficultyUser(p *repo.Problem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "题号：%d\n标题：%s\n", p.ID, p.Title)
	switch p.Type {
	case "objective":
		b.WriteString("类型：客观题（选择/判断/填空）\n")
	case "function":
		b.WriteString("类型：函数实现题\n")
	case "interactive":
		b.WriteString("类型：交互题\n")
	}
	fmt.Fprintf(&b, "时限：%dms  内存：%dKB\n", p.TimeLimitMS, p.MemoryLimitKB)
	fmt.Fprintf(&b, "时间复杂度档位与代码量是主要依据；样例与数据范围见题面。\n\n题面：\n%s\n\n请输出难度 JSON。",
		truncateTail(p.Statement, tidyStatementMaxRunes))
	return b.String()
}

// tidyKnowledgeCandidates 知识树候选集按一级分类分组（供 prompt 与前端展示同源）。
func tidyKnowledgeCandidates(tree []repo.TagNode) string {
	var b strings.Builder
	for _, top := range tree {
		names := collectSubtreeNames(top)
		if len(names) == 0 {
			continue
		}
		fmt.Fprintf(&b, "【%s】%s\n", top.Name, strings.Join(names, "、"))
	}
	return b.String()
}

func collectSubtreeNames(n repo.TagNode) []string {
	out := []string{}
	var walk func(t repo.TagNode)
	walk = func(t repo.TagNode) {
		out = append(out, t.Name)
		for _, c := range t.Children {
			walk(c)
		}
	}
	for _, c := range n.Children {
		walk(c)
	}
	return out
}

func tidyKnowledgeSystem() string {
	return `你是信息学竞赛题目的知识点标注专家。根据题目内容，从给定的候选知识点列表中选出该题考查的核心知识点。
规则：
1. 只能使用候选列表中出现过的名字，逐字一致；列表外、自造、同义改写的名字一律不要。
2. 宁缺毋滥：没有把握的知识点不要选，通常 1–4 个。
3. 只输出 JSON，格式为 {"tags": ["知识点1", "知识点2"]}，不要输出任何其他内容。`
}

func tidyKnowledgeUser(candidates string, curNames []string, p *repo.Problem) string {
	var b strings.Builder
	b.WriteString("候选知识点（按分类分组）：\n")
	b.WriteString(candidates)
	if len(curNames) > 0 {
		fmt.Fprintf(&b, "\n该题现挂知识点：%s（若其中某些与题目无关，结果中不要包含它们——将按差集作为移除建议）\n",
			strings.Join(curNames, "、"))
	} else {
		b.WriteString("\n该题现挂知识点：无\n")
	}
	fmt.Fprintf(&b, "\n标题：%s\n题面：\n%s\n\n请输出知识点 JSON。",
		p.Title, truncateTail(p.Statement, tidyStatementMaxRunes))
	return b.String()
}

// filterRange 题号范围过滤（from/to 0 = 不限）。范围外的题不计入 excluded——
// excluded 只表达「水位/已设难度排除」，范围是 root 主动圈定的任务边界本身。
func filterRange(probs []*repo.Problem, from, to int64) []*repo.Problem {
	if from <= 0 && to <= 0 {
		return probs
	}
	kept := probs[:0]
	for _, p := range probs {
		if (from > 0 && p.ID < from) || (to > 0 && p.ID > to) {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// excludeTidied 水位过滤（redo=false 共用）：tidiedAt.<kind> 已打点的题跳过并计数。
func excludeTidied(probs []*repo.Problem, kind string, job *tidyJob) []*repo.Problem {
	kept := probs[:0]
	for _, p := range probs {
		if _, done := p.TidiedAt[kind]; done {
			job.mu.Lock()
			job.Excluded++
			job.mu.Unlock()
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// ---- 树工具 ----

// uniqueTagLookup 全树唯一名映射；同名歧义名单独返回（AI 返回的歧义名一律丢弃）。
func uniqueTagLookup(tree []repo.TagNode) (uniq map[string]int64, ambiguous map[string]bool) {
	count := map[string]int{}
	first := map[string]int64{}
	var walk func(list []repo.TagNode)
	walk = func(list []repo.TagNode) {
		for _, n := range list {
			count[n.Name]++
			if count[n.Name] == 1 {
				first[n.Name] = n.ID
			}
			walk(n.Children)
		}
	}
	walk(tree)
	uniq, ambiguous = map[string]int64{}, map[string]bool{}
	for name, c := range count {
		if c == 1 {
			uniq[name] = first[name]
		} else {
			ambiguous[name] = true
		}
	}
	return uniq, ambiguous
}

// tagNameIDToName 树内 ID→名字映射（同名取先出现者，展示用）。
func tagNameIDToName(tree []repo.TagNode) map[int64]string {
	m := map[int64]string{}
	var walk func(list []repo.TagNode)
	walk = func(list []repo.TagNode) {
		for _, n := range list {
			if _, ok := m[n.ID]; !ok {
				m[n.ID] = n.Name
			}
			walk(n.Children)
		}
	}
	walk(tree)
	return m
}

// ---- 通过率分档（与 migrateDifficultyEstimate 同线）----

// rateTier 通过率 → 难度档位：≥0.9→1 ≥0.8→2 ≥0.5→3 ≥0.3→4 ≥0.2→5 ≥0.1→6
// ≥0.05→7 ≥0.02→8 其余→9；无人做过 → D3（表单默认档）。D10 只手工给。
func rateTier(rate float64, attempted int64) int {
	if attempted == 0 {
		return 3
	}
	switch {
	case rate >= 0.9:
		return 1
	case rate >= 0.8:
		return 2
	case rate >= 0.5:
		return 3
	case rate >= 0.3:
		return 4
	case rate >= 0.2:
		return 5
	case rate >= 0.1:
		return 6
	case rate >= 0.05:
		return 7
	case rate >= 0.02:
		return 8
	default:
		return 9
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
