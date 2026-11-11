package server

// 题库整理（v0.43，plan §2.45；v0.49 拆双模式；v0.92 二期）：AI 打标 + 规则归一化 + 手动直改。
// 两段式铁律（2026-10-31 拍板）：整理任务只产**建议**，root 在预览表勾选确认后经
// TidyApply 走 UpdateProblemMeta 校验链批量写库——不自动写库、不自动建树节点、
// 不动题面、无定时自动整理（纯 root 手动触发）。
// 两项共用单任务骨架（照备份/导入 job 模式：running/done/cancelled/failed + 轮询 + 取消）：
//   combo      = 综合 = 难度+知识点一次判读（v0.94，单维难度/知识点 AI 整理同批退役——
//                五源素材共享一次外呼出 {difficulty, tags}，外呼减半；难度锚点表+知识树
//                层级锚点同 prompt；JSON 整体解析失败两维皆无建议，难度越界仅难度侧不
//                采纳；apply 后 difficulty+knowledge 按维双打点水位，手动整理互认）
//   meta       = 纯规则零 AI（ParseSourceHint + MatchMetaTags 对齐重挂 + 树外孤儿剔除）
// v0.94 知识点标注层级纪律（combo）：机械补链（叶子→父链到一级分类自动进新增建议，
// AddAutoIDs 标记联动集）+ prompt 政策（低级可行只标低级压标程高级标签 / 高级主导时
// 基础语法杂音不挂 / 一题多解同档才都标）+ 难度锚点表 D1–D10 作档位校准。
// v0.94.1 用户拍板：单维难度/知识点 AI 整理退役（综合卡全覆盖）；状态端点 page_size
// 上限局部放宽到 1000（预览建议轻量无 statementHtml，其余列表端点维持全局 100 上限）。
// v0.49 手动模式（root 拍板）：不经 AI 无建议环节——分页题目表（每页 ≤100 题）行内
// 手改难度/知识点/来源标签，TidyManualApply 收最终值批量写库并按字段对应 kind 打
// tidiedAt 水位（保存过 = 已整理，AI 整理默认跳过，redo 可重跑）。
// v0.92 整理历史：apply（AI 确认/手动两路）批次留痕 tidyHistory（谁/何时/逐题 old→new，
// 全量留存拍板口径），历史两端点供面板回看。
// 建议快照按页下发（v0.49 验收期调整）：status 端点 page/page_size 切片，页面只持有
// 当前页——全量建议整包进 2s 轮询是 2000 题规模页面卡顿的根源。
// AI 调用走既有并发闸与 aiCalls 审计（kind=tidy_*，cost=0 管理侧不扣玉；不占学生每日上限——
// 该上限保护的是学生侧 AI 功能配额，root 批量整理是后台任务）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/ai"
	"soj/internal/repo"
	"soj/internal/testdata"
)

// tidyKinds 支持的整理类型（v0.94.1：单维 difficulty/knowledge AI 整理退役，综合全覆盖）。
var tidyKinds = map[string]bool{"meta": true, "combo": true}

// tidyWorkers AI 类任务的并发度（对齐 AI 客户端闸 4 路留余量给在线功能）。
const tidyWorkers = 3

// TidySuggestion 逐题建议（kind 专属字段按需填充，JSON 直接下发渲染预览表）。
type TidySuggestion struct {
	PID   int64  `json:"pid"`
	Title string `json:"title"`
	Kind  string `json:"kind"`

	// difficulty：五源综合判读建议（v0.92 通过率链路退役——不再有参考档/冲突/兜底）
	HasDifficulty     bool   `json:"has_difficulty"`      // 已显式设过难度（预览默认不勾）
	CurDifficulty     int    `json:"cur_difficulty"`      // 现值（0 = 未设置）
	SuggestDifficulty int    `json:"suggest_difficulty"`  // 建议（0 = 无建议：AI 失败行）
	AIError           string `json:"ai_error,omitempty"`  // 非空 = AI 失败原因（无建议，不出兜底）

	// knowledge/combo：现有/新增/移除三段差集 + 丢弃名单；AddAutoIDs = 机械补链
	//（v0.94 层级纪律：叶子→父链到一级分类自动补的上级，前端默认勾选带「联动」标可取消）
	CurTagIDs      []int64  `json:"cur_tag_ids,omitempty"`
	CurTagNames    []string `json:"cur_tag_names,omitempty"`
	AddTagIDs      []int64  `json:"add_tag_ids,omitempty"`
	AddTagNames    []string `json:"add_tag_names,omitempty"`
	AddAutoIDs     []int64  `json:"add_auto_ids,omitempty"`
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

func (j *tidyJob) snapshot(page, pageSize int) gin.H {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]TidySuggestion, len(j.Suggestions))
	copy(out, j.Suggestions)
	// 题号升序稳定下发（v0.92 难度冲突置顶随通过率链路退役）
	sort.SliceStable(out, func(a, b int) bool { return out[a].PID < out[b].PID })
	// 快照按页下发（v0.49 验收期调整：全量建议整包进 2s 轮询响应是页面卡顿根源，
	// 页面上只持有当前页 ≤100 条，跨页勾选随之退役）
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
		"suggestions": out[lo:hi], "suggestion_total": total,
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
		fail(c, http.StatusBadRequest, "kind 需为 combo / meta")
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
		case "combo":
			err = h.runTidyCombo(ctx, job, uid, req.Redo)
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
// suggestion_total 为全量口径（翻页器用），页面只持有当前页切片。
// tidyStatusMaxPageSize 整理状态端点的页容上限（v0.94.1 局部放宽到 1000）：建议切片
// 为轻量结构（无题面/代码），root 预览要一眼多行；全局 queryPage 100 上限不动（其余
// 列表端点口径不变）。
const tidyStatusMaxPageSize = 1000

func (h *Handlers) TidyStatus(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)
	pageSize := atoiDefault(c.Query("page_size"), 100)
	if pageSize > tidyStatusMaxPageSize {
		pageSize = tidyStatusMaxPageSize
	}
	h.tidyMu.Lock()
	job := h.tidyTask
	h.tidyMu.Unlock()
	if job == nil {
		ok(c, gin.H{"state": "idle"})
		return
	}
	ok(c, job.snapshot(page, pageSize))
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

// TidyDiscard POST /api/admin/tidy/discard：弃置终态任务（v0.95.1）——清掉进程内任务槽。
// 此前弃置是纯前端态（面板 status 置空），tidyTask 终态快照恒在、进页恢复一拉即把已弃置
// 的结果复活，与「结果属当次任务当次处置」矛盾。running 态不清理（先走 cancel 优雅取消）；
// 无任务幂等成功。
func (h *Handlers) TidyDiscard(c *gin.Context) {
	h.tidyMu.Lock()
	if h.tidyTask != nil && h.tidyTask.State == "running" {
		h.tidyMu.Unlock()
		fail(c, http.StatusConflict, "任务仍在执行——请先取消整理")
		return
	}
	h.tidyTask = nil
	h.tidyMu.Unlock()
	ok(c, gin.H{"discarded": true})
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
	if req.Kind == "combo" {
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
	uid, name, _, _ := currentUser(c)
	okN, failN := 0, 0
	var appliedPIDs []int64
	var diffApplied, tagApplied []int64 // combo 双水位按维收集（见循环内）
	var firstErr string
	// 落历史（v0.92）：写库前的旧值快照 + 树名映射，逐题 old→new 留痕
	pids := make([]int64, 0, len(req.Items))
	for _, it := range req.Items {
		pids = append(pids, it.PID)
	}
	oldMeta, _ := h.repo.TidyProblemMetas(ctx, pids)
	names := h.tidyKindNames(ctx, req.Kind)
	hist := &repo.TidyHistory{
		At: time.Now(), ByUID: uid, ByName: name, Mode: "ai", Kinds: []string{req.Kind},
		Items: make([]repo.TidyHistoryItem, 0, len(req.Items)),
	}
	// 生成建议任务的题号范围（apply 与 run 非同一请求，取当前驻留任务同 kind 的边界）
	h.tidyMu.Lock()
	if h.tidyTask != nil && h.tidyTask.Kind == req.Kind {
		hist.From, hist.To = h.tidyTask.From, h.tidyTask.To
	}
	h.tidyMu.Unlock()

	for _, it := range req.Items {
		patch := repo.ProblemMetaPatch{}
		item := repo.TidyHistoryItem{PID: it.PID}
		if old := oldMeta[it.PID]; old != nil {
			item.Title = old.Title
			switch req.Kind {
			case "combo":
				item.OldDifficulty = old.Difficulty
				item.OldTagNames = tidyIDNames(old.Tags, names)
			case "meta":
				item.OldMetaNames = tidyIDNames(old.MetaTags, names)
			}
		}
		var itemErr string
		switch req.Kind {
		case "combo":
			// 两维任一有终值即合法应用项；缺哪个维度 = 该维度本次不动
			if it.Difficulty != nil {
				item.NewDifficulty = *it.Difficulty
				if !repo.ValidDifficulty(*it.Difficulty) {
					itemErr = "难度值非法"
				} else {
					patch.Difficulty = it.Difficulty
				}
			}
			if itemErr == "" && it.Tags != nil {
				patch.Tags = it.Tags
				item.NewTagNames = tidyIDNames(*it.Tags, names)
			}
			if itemErr == "" && it.Difficulty == nil && it.Tags == nil {
				itemErr = "缺少难度/知识点字段"
			}
		case "meta":
			if it.MetaTags == nil {
				itemErr = "缺少来源标签字段"
			} else {
				patch.MetaTags = it.MetaTags
				item.NewMetaNames = tidyIDNames(*it.MetaTags, names)
			}
		}
		if itemErr == "" {
			if err := h.repo.UpdateProblemMeta(ctx, it.PID, patch); err != nil {
				itemErr = fmt.Sprintf("写库失败: %v", err)
			}
		}
		if itemErr != "" {
			failN++
			if firstErr == "" {
				firstErr = fmt.Sprintf("题 #%d %s", it.PID, itemErr)
			}
			item.Err = itemErr
		} else {
			okN++
			item.OK = true
			appliedPIDs = append(appliedPIDs, it.PID)
			// combo 双水位按维精确打点：难度只记真带了难度终值的项、知识点只记真带
			// 了 tags 终值的项——某维没勾 = 该维本次未整理，不打点（下次单维 redo 会再来）
			if req.Kind == "combo" {
				if it.Difficulty != nil {
					diffApplied = append(diffApplied, it.PID)
				}
				if it.Tags != nil {
					tagApplied = append(tagApplied, it.PID)
				}
			}
		}
		hist.Items = append(hist.Items, item)
	}
	hist.Applied, hist.Failed = okN, failN
	msg := fmt.Sprintf("已应用 %d 题", okN)
	if failN > 0 {
		msg += fmt.Sprintf("，失败 %d（%s）", failN, firstErr)
	}
	// 应用成功 = 已整理水位（下次默认跳过，redo 可重跑）；失败不致命，水位落后仅重复建议
	if req.Kind == "combo" {
		// combo = difficulty+knowledge 双打点（单维整理互认水位，不重复烧钱）
		if err := h.repo.MarkTidied(ctx, "difficulty", diffApplied); err != nil {
			msg += "（水位记录失败，下次可能重复建议这些题）"
		}
		if err := h.repo.MarkTidied(ctx, "knowledge", tagApplied); err != nil {
			msg += "（水位记录失败，下次可能重复建议这些题）"
		}
	} else if err := h.repo.MarkTidied(ctx, req.Kind, appliedPIDs); err != nil {
		msg += "（水位记录失败，下次可能重复建议这些题）"
	}
	hist.Message = msg
	if err := h.repo.InsertTidyHistory(ctx, hist); err != nil {
		msg += "（历史记录失败）"
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

	uid, name, _, _ := currentUser(c)
	okN, failN := 0, 0
	var firstErr string
	var wmPIDs = map[string][]int64{} // kind → 成功写入的题（按字段对应水位）
	// 落历史（v0.92）：旧值快照 + 双树名映射（手动批可混合三字段，names 双树都备）
	pids := make([]int64, 0, len(req.Items))
	for _, it := range req.Items {
		pids = append(pids, it.PID)
	}
	oldMeta, _ := h.repo.TidyProblemMetas(ctx, pids)
	tagNames := h.tidyTagNames(ctx, false)
	metaNames := h.tidyTagNames(ctx, true)
	hist := &repo.TidyHistory{
		At: time.Now(), ByUID: uid, ByName: name, Mode: "manual",
		Items: make([]repo.TidyHistoryItem, 0, len(req.Items)),
	}
	for _, it := range req.Items {
		patch := repo.ProblemMetaPatch{}
		item := repo.TidyHistoryItem{PID: it.PID}
		if old := oldMeta[it.PID]; old != nil {
			item.Title = old.Title
			item.OldDifficulty = old.Difficulty
			item.OldTagNames = tidyIDNames(old.Tags, tagNames)
			item.OldMetaNames = tidyIDNames(old.MetaTags, metaNames)
		}
		var itemErr string
		if it.Difficulty != nil {
			if !repo.ValidDifficulty(*it.Difficulty) {
				itemErr = "难度值非法"
			} else {
				patch.Difficulty = it.Difficulty
				item.NewDifficulty = *it.Difficulty
			}
		}
		if it.Tags != nil {
			patch.Tags = it.Tags
			item.NewTagNames = tidyIDNames(*it.Tags, tagNames)
		}
		if it.MetaTags != nil {
			patch.MetaTags = it.MetaTags
			item.NewMetaNames = tidyIDNames(*it.MetaTags, metaNames)
		}
		if itemErr == "" && patch.Empty() {
			itemErr = "没有需要更新的字段"
		}
		if itemErr == "" {
			if err := h.repo.UpdateProblemMeta(ctx, it.PID, patch); err != nil {
				itemErr = fmt.Sprintf("写库失败: %v", err)
			}
		}
		if itemErr != "" {
			failN++
			if firstErr == "" {
				firstErr = fmt.Sprintf("题 #%d %s", it.PID, itemErr)
			}
			item.Err = itemErr
		} else {
			okN++
			item.OK = true
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
		hist.Items = append(hist.Items, item)
	}
	hist.Applied, hist.Failed = okN, failN
	msg := fmt.Sprintf("已保存 %d 题", okN)
	if failN > 0 {
		msg += fmt.Sprintf("，失败 %d（%s）", failN, firstErr)
	}
	for _, kind := range []string{"difficulty", "knowledge", "meta"} {
		if pids := wmPIDs[kind]; len(pids) > 0 {
			hist.Kinds = append(hist.Kinds, kind)
			if err := h.repo.MarkTidied(ctx, kind, pids); err != nil {
				msg += "（部分水位记录失败，下次 AI 整理可能重复建议这些题）"
				break
			}
		}
	}
	hist.Message = msg
	if err := h.repo.InsertTidyHistory(ctx, hist); err != nil {
		msg += "（历史记录失败）"
	}
	ok(c, gin.H{"applied": okN, "failed": failN, "message": msg})
}

// ---- 整理历史（v0.92）----

// TidyHistoryList GET /api/admin/tidy/history?page=&page_size=：apply 批次列表
//（倒序分页，不含逐题明细）。
func (h *Handlers) TidyHistoryList(c *gin.Context) {
	page, pageSize := queryPage(c)
	list, total, err := h.repo.ListTidyHistory(c.Request.Context(), page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取整理历史失败")
		return
	}
	if list == nil {
		list = []*repo.TidyHistory{}
	}
	ok(c, gin.H{"list": list, "total": total})
}

// TidyHistoryDetail GET /api/admin/tidy/history/:id：单批全量（逐题 old→new 明细）。
func (h *Handlers) TidyHistoryDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		fail(c, http.StatusBadRequest, "历史批次 id 不合法")
		return
	}
	hist, err := h.repo.GetTidyHistory(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取整理历史失败")
		return
	}
	if hist == nil {
		fail(c, http.StatusNotFound, "历史批次不存在")
		return
	}
	ok(c, hist)
}

// tidyKindNames 按 kind 取对应树 ID→名映射（历史明细名字快照用）。
func (h *Handlers) tidyKindNames(ctx context.Context, kind string) map[int64]string {
	switch kind {
	case "combo":
		return h.tidyTagNames(ctx, false)
	case "meta":
		return h.tidyTagNames(ctx, true)
	}
	return nil
}

// tidyTagNames 双树任一的 ID→名映射（读失败给空映射，明细名缺席不废历史落库）。
func (h *Handlers) tidyTagNames(ctx context.Context, meta bool) map[int64]string {
	var tree []repo.TagNode
	var err error
	if meta {
		tree, err = h.repo.GetMetaTagTree(ctx)
	} else {
		tree, err = h.repo.GetTagTree(ctx)
	}
	if err != nil {
		return map[int64]string{}
	}
	return tagNameIDToName(tree)
}

// tidyIDNames ID 集 → 名字集（未知 ID 跳过；旧值空集 → nil 与「清空」区分）。
func tidyIDNames(ids []int64, nameOf map[int64]string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := nameOf[id]; n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ---- 任务体 ----

// applyTagSuggestion AI 名单 → 树内差集（combo）：逐字一致映射、歧义/
// 树外进丢弃列、差集出新增/移除，再按 v0.94 层级纪律做父链机械补链（自动补入的上级
// 进 AddAutoIDs 联动集；已在现挂里的联动祖先不出行但仍计入建议集防误移除）。
func applyTagSuggestion(s *TidySuggestion, names []string, uniq map[string]int64, ambiguous map[string]bool, parentOf map[int64]int64, nameOf map[int64]string) {
	cur := map[int64]bool{}
	for _, id := range s.CurTagIDs {
		cur[id] = true
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
	for _, id := range tagParentClosure(parentOf, suggested) {
		if !cur[id] {
			s.AddTagIDs = append(s.AddTagIDs, id)
			s.AddTagNames = append(s.AddTagNames, nameOf[id])
			s.AddAutoIDs = append(s.AddAutoIDs, id)
		}
	}
	for _, id := range s.CurTagIDs {
		if !suggested[id] {
			s.RemoveTagIDs = append(s.RemoveTagIDs, id)
			s.RemoveTagNames = append(s.RemoveTagNames, nameOf[id])
		}
	}
}

// runTidyCombo 综合整理（v0.94）：难度+知识点一次判读——五源素材共享一次外呼出
// {difficulty, tags}（搜索一次），难度锚点表+知识树层级锚点同 prompt；水位排除口径 =
// difficulty 与 knowledge 都整理过才跳过（任一维度未整理即进任务，已设难度照常出建议
// 靠 has_difficulty 默认不勾）；JSON 整体解析失败两维皆无建议，难度越界仅难度侧不采纳；
// apply 后 difficulty+knowledge 双打点水位（见 TidyApply），单维 redo 互认。
func (h *Handlers) runTidyCombo(ctx context.Context, job *tidyJob, uid int64, redo bool) error {
	probs, err := h.repo.TidyListProblems(ctx)
	if err != nil {
		return err
	}
	probs = filterRange(probs, job.From, job.To)
	if !redo {
		kept := probs[:0]
		for _, p := range probs {
			_, d := p.TidiedAt["difficulty"]
			_, k := p.TidiedAt["knowledge"]
			if d && k {
				job.mu.Lock()
				job.Excluded++
				job.mu.Unlock()
				continue
			}
			kept = append(kept, p)
		}
		probs = kept
	}
	tree, err := h.repo.GetTagTree(ctx)
	if err != nil {
		return err
	}
	uniq, ambiguous := uniqueTagLookup(tree)
	nameOf := tagNameIDToName(tree)
	parentOf := tagParentOf(tree)
	candidates := tidyKnowledgeCandidates(tree)
	lcfg, _ := h.repo.GetLevelConfig(ctx)
	stdStore := testdata.NewLocalFS(h.cfg.DataDir)

	job.mu.Lock()
	job.Total = len(probs)
	job.mu.Unlock()

	sys := tidyComboSystem(effectiveDifficultyNotes(lcfg))
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
			s := TidySuggestion{
				PID: p.ID, Title: p.Title, Kind: "combo",
				HasDifficulty: p.Difficulty >= 1 && p.Difficulty <= 10,
				CurDifficulty: p.Difficulty,
			}
			for _, id := range p.Tags {
				s.CurTagIDs = append(s.CurTagIDs, id)
				if n := nameOf[id]; n != "" {
					s.CurTagNames = append(s.CurTagNames, n)
				}
			}

			// 五源素材采集（有则用无则缺席；单源查询失败按缺席降级不废整题判读）
			sols, solErr := h.repo.TidyProblemSolutions(ctx, p.ID, tidySolutionLimit)
			acs, acErr := h.repo.TidyACCodes(ctx, p.ID)
			std, stdErr := readTidyStd(stdStore, p.ID)

			user := tidyComboUser(candidates, p, s.CurTagNames, sols, acs, std)
			resp, err := h.tidyAIChat(ctx, uid, "tidy_combo", p.ID, sys, user, true)
			if err != nil {
				if ctx.Err() == nil {
					s.AIError = "AI 调用失败：" + truncateTail(err.Error(), 120)
					job.addSuggestion(s)
				}
				return
			}
			d, names, perr := parseTidyCombo(resp.Content)
			if perr != nil {
				// JSON 整体解析失败 = 两维皆无建议（一次判读的固有粒度）
				s.AIError = "AI 输出无法解析：" + truncateTail(perr.Error(), 120)
				if ctx.Err() == nil {
					job.addSuggestion(s)
				}
				return
			}
			var notes []string
			if !repo.ValidDifficulty(d) {
				notes = append(notes, fmt.Sprintf("难度建议 %d 越界，未采纳（知识点侧照常）", d))
			} else {
				s.SuggestDifficulty = d
			}
			applyTagSuggestion(&s, names, uniq, ambiguous, parentOf, nameOf)
			// 素材采集告警进 note（源缺席不拦判读，root 备注列可见）
			if solErr != nil {
				notes = append(notes, "题解源读取失败，已按缺席判读："+truncateTail(solErr.Error(), 80))
			} else if acErr != nil {
				notes = append(notes, "AC 代码源读取失败，已按缺席判读："+truncateTail(acErr.Error(), 80))
			} else if stdErr != nil {
				notes = append(notes, "数据标程读取失败，已按缺席判读："+truncateTail(stdErr.Error(), 80))
			}
			s.Note = strings.Join(notes, "；")
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
// search=true 时难度五源走 ChatSearch（GLM web_search 工具；非 bigmodel 端点自动降级），
// 搜索外呼预算放宽到 110s（搜索 + 大 prompt 慢于纯对话的 60s）。
func (h *Handlers) tidyAIChat(ctx context.Context, uid int64, kind string, pid int64, system, user string, search bool) (*ai.Response, error) {
	cfg, _ := h.repo.GetAIConfig(ctx)
	p := cfg.ActiveProvider()
	if p == nil {
		return nil, ai.ErrNoProvider
	}
	budget := 60 * time.Second
	if search {
		budget = 110 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	client := ai.NewClient(p)
	var resp *ai.Response
	var err error
	if search {
		resp, err = client.ChatSearch(cctx, system, user)
	} else {
		resp, err = client.Chat(cctx, system, user)
	}
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

// ---- prompt 组装 ----

// 五源素材预算（v0.92 定口径，combo 同源）：题面 6000 + 题解 ≤2 篇×3000 +
// AC 代码 ≤6 份×6000 + 数据标程 6000，单题 prompt 有界（≈54K 字符），后台批量任务可承受。
const (
	tidyStatementMaxRunes = 6000
	tidySolutionLimit     = int64(2)
	tidySolutionMaxRunes  = 3000
	tidyACCodeMaxRunes    = 6000
	tidyStdCodeMaxRunes   = 6000
	// tidyStdMaxBytes 标程文件读取硬上限（128KB）：现库 std.cpp 样本最大 ≈5KB，
	// 上限只防异常大文件拖爆内存，截断到 6000 rune 由 rune 预算兜底。
	tidyStdMaxBytes = 128 << 10
)

// tidySourceBlock 五源素材块（v0.94 抽取共用，combo user prompt 素材源）——
// 题号/标题/类型/时限/现挂知识点 + 【题面】【数据标程】【站内题解 n】【历史通过代码 n】；
// 有则用无则缺席，不伪造空源段落。
func tidySourceBlock(p *repo.Problem, tagNames []string, sols []*repo.Solution, acs []repo.TidyACCode, stdCode string) string {
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
	if len(tagNames) > 0 {
		fmt.Fprintf(&b, "现挂知识点：%s\n", strings.Join(tagNames, "、"))
	}
	fmt.Fprintf(&b, "\n【题面】\n%s\n", truncateTail(p.Statement, tidyStatementMaxRunes))
	if stdCode != "" {
		fmt.Fprintf(&b, "\n【数据标程】\n%s\n", truncateTail(stdCode, tidyStdCodeMaxRunes))
	}
	for i, s := range sols {
		fmt.Fprintf(&b, "\n【站内题解 %d】\n%s\n", i+1, truncateTail(s.Content, tidySolutionMaxRunes))
	}
	for i, a := range acs {
		fmt.Fprintf(&b, "\n【历史通过代码 %d】（%s，耗时 %dms）\n%s\n",
			i+1, a.Lang, a.TimeMS, truncateTail(a.Code, tidyACCodeMaxRunes))
	}
	return b.String()
}

// tidyKnowledgeCandidates 知识树候选锚点按层级缩进渲染（v0.94：平铺名单改层级树——
// AI 看得见上下级才判得了标注层级纪律；纯叶子组并名一行省字符）。
func tidyKnowledgeCandidates(tree []repo.TagNode) string {
	var b strings.Builder
	for _, top := range tree {
		if len(top.Children) == 0 {
			continue
		}
		b.WriteString("【" + top.Name + "】\n")
		writeTagLevels(&b, top.Children, "  ")
	}
	return b.String()
}

func writeTagLevels(b *strings.Builder, nodes []repo.TagNode, indent string) {
	for _, n := range nodes {
		switch {
		case len(n.Children) == 0:
			b.WriteString(indent + "- " + n.Name + "\n")
		case allTagLeaf(n.Children):
			names := make([]string, 0, len(n.Children))
			for _, c := range n.Children {
				names = append(names, c.Name)
			}
			b.WriteString(indent + "- " + n.Name + "：" + strings.Join(names, "、") + "\n")
		default:
			b.WriteString(indent + "- " + n.Name + "\n")
			writeTagLevels(b, n.Children, indent+"  ")
		}
	}
}

func allTagLeaf(nodes []repo.TagNode) bool {
	for _, n := range nodes {
		if len(n.Children) > 0 {
			return false
		}
	}
	return true
}

// tidyTagDiscipline 知识点标注层级纪律（v0.94，combo system；档位校准 = 下方难度锚点表）：
//   1. 逐字一致 + 只出最具体层级（上级由机械补链保证，AI 不必重复给上级）
//   2. 低级可行则只标低级（压标程/AC 的高级解法标签——标注刻画题目本质考点与最低门槛）
//   3. 高级主导时基础语法杂音不挂（小数处理这类），除非正是本题考点
//   4. 一题多解同档才都标，跨档只标最低档那套
//   5. 现挂知识点无关的不包含（差集出移除建议）
func tidyTagDiscipline() string {
	return `知识点标注层级纪律：
1. 只能使用候选列表中出现过的名字，逐字一致；列表外、自造、同义改写的名字一律不要。给出最具体层级的知识点即可，其全部上级由系统自动补齐。
2. 低级可行则只标低级：若存在更基础知识点构成的正确解法，只标那套——标程/通过代码用了更高级算法不代表要标，知识标注刻画的是题目本质考点与最低门槛。
3. 高级主导时低级杂音不挂：题目核心是算法知识时，基础语法类标签（小数处理、简单输出这类）不要标，除非它正是本题考点。
4. 一题多解同档才都标：对照下方难度锚点表判断知识点档位——多个解法档位相同才都标，跨档只标最低档的那套。
5. 现挂知识点中与题目无关的不要包含（将按差集作为移除建议）。`
}

// effectiveDifficultyNotes AI 整理锚点表文案（v0.95 双轨拆分）：逐档 AI 整理说明优先，
// 空档回退学生说明——存量库的详细锚点还在学生说明里，回退保证配置迁移期锚点不断档。
func effectiveDifficultyNotes(cfg *repo.LevelConfig) [10]string {
	var out [10]string
	for i := range out {
		if s := strings.TrimSpace(cfg.DifficultyAINotes[i]); s != "" {
			out[i] = cfg.DifficultyAINotes[i]
		} else {
			out[i] = cfg.DifficultyNotes[i]
		}
	}
	return out
}

// writeDifficultyAnchors 难度锚点表渲染（combo system；空档跳过）。
func writeDifficultyAnchors(b *strings.Builder, notes [10]string) {
	b.WriteString("\n难度锚点表（知识点档位校准参考）：\n")
	for i, n := range notes {
		if strings.TrimSpace(n) == "" {
			continue
		}
		fmt.Fprintf(b, "D%d：%s\n", i+1, n)
	}
}

// tidyComboSystem 综合整理（难度+知识点一次判读）system（v0.94；notes = 难度锚点表文案，
// v0.95 起传 effectiveDifficultyNotes 的产物——AI 说明优先、空档回退学生说明）。
func tidyComboSystem(notes [10]string) string {
	var b strings.Builder
	b.WriteString("你是信息学竞赛题目的整理专家，一次完成难度评估与知识点标注。综合题面、现挂知识点、数据标程、站内题解、历史通过代码判断题目难度档位与考查的核心知识点，知识点从候选列表（按层级缩进）中选名；如具备联网搜索能力，可搜索该题的公开资料（原题出处、网上题解）作参考。\n")
	b.WriteString("难度判读要点：算法思维深度与实现复杂度是主要依据（时间复杂度档位、代码量、数据结构的实现成本）；若提供多份通过代码，最短实现最能代表算法本质复杂度；不要依据通过率或做题人数评估难度（本站样本小且为教学场景，噪声大）。\n")
	b.WriteString(tidyTagDiscipline())
	b.WriteString("\n6. 宁缺毋滥：没有把握的知识点不要选，通常 1–4 个；数据标程是最可靠的判据——它就是该题的标准解法。\n")
	writeDifficultyAnchors(&b, notes)
	b.WriteString("\n要求：只输出 JSON，格式为 {\"difficulty\": <1-10 的整数>, \"tags\": [\"知识点1\", \"知识点2\"]}，不要输出任何其他内容。")
	return b.String()
}

// tidyComboUser 综合整理素材组装（层级树候选锚点 + 五源块）。
func tidyComboUser(candidates string, p *repo.Problem, tagNames []string, sols []*repo.Solution, acs []repo.TidyACCode, stdCode string) string {
	var b strings.Builder
	b.WriteString("候选知识点（按层级缩进；选中下层时其全部上级由系统自动补齐）：\n")
	b.WriteString(candidates)
	b.WriteString("\n")
	b.WriteString(tidySourceBlock(p, tagNames, sols, acs, stdCode))
	b.WriteString("\n请综合上述素材（可联网搜索补充公开资料），判定难度档位并选出核心知识点，输出综合 JSON。")
	return b.String()
}

// parseTidyCombo 综合输出解析：{difficulty, tags} 结构体一次解析；难度取值合法性由
// 调用方校验（越界只废难度侧不废知识点侧）。
func parseTidyCombo(content string) (int, []string, error) {
	var out struct {
		Difficulty int      `json:"difficulty"`
		Tags       []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &out); err != nil {
		return 0, nil, fmt.Errorf("%w: %s", err, truncateTail(content, 80))
	}
	return out.Difficulty, out.Tags, nil
}

// tagParentOf 知识树子→父映射（机械补链用；根节点无父不入表）。
func tagParentOf(tree []repo.TagNode) map[int64]int64 {
	m := map[int64]int64{}
	var walk func(list []repo.TagNode, parent int64)
	walk = func(list []repo.TagNode, parent int64) {
		for _, n := range list {
			if parent > 0 {
				m[n.ID] = parent
			}
			walk(n.Children, n.ID)
		}
	}
	walk(tree, 0)
	return m
}

// tagParentClosure v0.94 层级纪律机械补链：对建议集做父链闭包——每个命中节点沿
// parentOf 补齐全部上级到一级分类；返回自动补入的上级 ID（原集已含的不重复计，
// 按节点 ID 升序稳定输出）。从每个原集节点直接走到根，新增节点的祖先必然在链上被覆盖。
func tagParentClosure(parentOf map[int64]int64, suggested map[int64]bool) []int64 {
	var auto []int64
	for id := range suggested {
		p, ok := parentOf[id]
		for ok {
			if !suggested[p] {
				suggested[p] = true
				auto = append(auto, p)
			}
			p, ok = parentOf[p]
		}
	}
	sort.Slice(auto, func(a, b int) bool { return auto[a] < auto[b] })
	return auto
}

// readTidyStd 数据标程素材（v0.93 五源之一）：读数据卷 data/std.cpp（数据包附属标程，
// 现库命名统一）。无数据卷/无标程返回空串按缺席降级不报错；128KB 读取硬上限 +
// 6000 rune 截断双闸有界。
func readTidyStd(store *testdata.LocalFS, pid int64) (string, error) {
	f, err := store.Open(pid, "data/std.cpp")
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err = io.CopyN(&buf, f, tidyStdMaxBytes); err != nil && err != io.EOF {
		return "", err
	}
	return buf.String(), nil
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

// ---- 通过率分档链路已随 v0.92 五源判读退役（参考列/冲突/兜底全数移除）----
