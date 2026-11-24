package repo

// 学生等级与题目难度（v0.11）：十阶共用一条难度轴（D1–D10 / Lv.1–10）。
// 难度是题目属性（Problem.Difficulty）；等级是读时实时聚合值、不落库——
// 由用户「各难度 AC 题数」按当前配置算 XP 再定级，老师调难度后全员等级自动跟随。
//
// 经验规则：每 AC 一题得 XP = 2^(难度−1)（D1=1、D2=2、D3=4 … D10=512），
// 逐档翻倍、不衰减。升级阈值 thresholds 默认 200×(2^(k−1)−1)：题库每档 200 题，
// 清完一档恰好升一级，Lv.10 = D1–D9 九档全清。

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 等级配置默认值（root 后台可调；名字 2026-10-22 两字段位版定稿：取自王者荣耀段位体系）。
var DefaultLevelNames = [10]string{"青铜", "白银", "黄金", "铂金", "钻石", "星耀", "宗师", "王者", "传奇", "极境"}

// DefaultLevelThresholds 达 Lv.2–10 的累计 XP：200×(2^(k−1)−1)——每档 200 题全 AC
// 恰好升一级，Lv.10 = D1–D9 全清（200×511）。
var DefaultLevelThresholds = [9]int64{200, 600, 1400, 3000, 6200, 12600, 25400, 51000, 102200}

// DefaultDifficultyNotes 每档难度说明的默认文案（2026-10-26 定稿：锚点对齐 NOI 大纲与教学进度；
// 空档 = 说明卡不显示该行）。仅对从未保存过等级配置的库生效——后台一旦保存过，以后台内容为准。
var DefaultDifficultyNotes = [10]string{
	"语法入门：顺序/分支/循环",
	"语法基础：数组/字符串/函数/结构体",
	"算法入门：模拟/枚举/排序/贪心/递推/递归",
	"算法基础：二分/前缀和/差分/堆/栈/队列/DFS/BFS",
	"CSP-J：搜索剪枝/记忆化搜索/树基础/基础图论/线性DP",
	"CSP-S入门：并查集/最短路/最小生成树/树状数组/线段树/倍增",
	"CSP-S基础：树形/状压/区间DP/KMP/单调队列优化DP/基础数论",
	"CSP-S进阶：二分图/字符串哈希/AC自动机/组合数学/主席树/网络流",
	"省选级：树链剖分/点分治/高级数论/复杂图论",
	"NOI&IOI：综合构造/LCT/后缀自动机等",
}

// 等级权益闸门（v0.40）：功能解锁阶梯——Lv1 仅本地流程（题面页提交按钮），
// 教职工（root/admin/teacher）豁免；闸门实现在 server 层 requireLevel。
const (
	LevelGateOnline    = 2 // 在线编程 workbench + 在线自测
	LevelGateChallenge = 3 // 挑战模式
)

// LevelConfig 等级体系配置（settings 集合 system 文档 levelConfig 字段；nil = 全默认）。
type LevelConfig struct {
	Names      [10]string `bson:"names" json:"names"`           // Lv.1–10 名称文字
	Thresholds [9]int64   `bson:"thresholds" json:"thresholds"` // 达 Lv.2–10 的累计 XP，严格递增
	// 题目难度名（D1–10 档的展示文字，2026-10-22 起 Difficulty 徽章/筛选/建题选择器均显示名字，
	// 不再用「D1~D10」描述）；默认与等级名相同，root 后台独立可改。
	DifficultyNames [10]string `bson:"difficultyNames" json:"difficulty_names"`
	// 难度说明（2026-10-26）：每档难度各一条自由文案（≤100 字），题库「难度说明」说明卡
	// 逐档展示，建题/编辑页难度页签提示同源；空档 = 不显示该行。默认为定稿文案，
	// root 后台「难度等级」页签编辑。v0.95 起语义收窄为「学生说明」。
	DifficultyNotes [10]string `bson:"difficultyNotes" json:"difficulty_notes"`
	// AI 整理说明（v0.95 双轨拆分）：每档 ≤2000 字的详细知识点与难度锚点，仅作 AI 整理
	// 综合判读的锚点表素材（tidy），不进公开 /levels 视图——学生端不可见。
	// 逐档空则回退学生说明（存量库的详细锚点还在学生说明里，回退保证迁移期锚点不断档）。
	DifficultyAINotes [10]string `bson:"difficultyAiNotes" json:"difficulty_ai_notes"`
	// 特效开关：等级徽章的动画/光效（难度徽章恒静态，不受此控制）。
	// 全局关 = 部署方不想要炫酷效果；逐级关可只保留低档纯色。
	EffectsEnabled bool     `bson:"effectsEnabled" json:"effects_enabled"`
	EffectsTiers   [10]bool `bson:"effectsTiers" json:"effects_tiers"`
}

// DefaultLevelConfig 默认等级配置（深拷贝默认数组；难度名默认与等级名相同，难度说明默认为
// 定稿文案；AI 整理说明默认全空——逐档回退学生说明，不另备一份默认文案）。
func DefaultLevelConfig() *LevelConfig {
	cfg := &LevelConfig{
		Names:           DefaultLevelNames,
		Thresholds:      DefaultLevelThresholds,
		DifficultyNames: DefaultLevelNames,
		DifficultyNotes: DefaultDifficultyNotes,
		EffectsEnabled:  true,
	}
	for i := range cfg.EffectsTiers {
		cfg.EffectsTiers[i] = true
	}
	return cfg
}

// Normalize 校验并兜底：名字空档回默认、阈值非正/不严格递增回默认、系数夹取 [0,1]。
// 返回是否可保存（结构本身总是可用；调用方据此决定是否报参数错误）。
func (c *LevelConfig) Normalize() bool {
	ok := true
	for i := range c.Names {
		if len([]rune(c.Names[i])) == 0 || len([]rune(c.Names[i])) > 12 {
			c.Names[i] = DefaultLevelNames[i]
			ok = false
		}
	}
	for i := range c.DifficultyNames {
		if len([]rune(c.DifficultyNames[i])) == 0 || len([]rune(c.DifficultyNames[i])) > 12 {
			c.DifficultyNames[i] = DefaultLevelNames[i]
			ok = false
		}
	}
	prev := int64(0)
	for i := range c.Thresholds {
		if c.Thresholds[i] <= prev {
			c.Thresholds = DefaultLevelThresholds
			ok = false
			break
		}
		prev = c.Thresholds[i]
	}
	return ok
}

// GetLevelConfig 读等级配置，未设置/字段缺失处回默认。
func (r *Repo) GetLevelConfig(ctx context.Context) (*LevelConfig, error) {
	var doc struct {
		LevelConfig *LevelConfig `bson:"levelConfig"`
	}
	err := r.db.Collection("settings").FindOne(ctx, bson.M{"_id": "system"}).Decode(&doc)
	if IsNotFound(err) || doc.LevelConfig == nil {
		return DefaultLevelConfig(), nil
	}
	if err != nil {
		return nil, err
	}
	cfg := doc.LevelConfig
	// 存量文档缺数组字段时解码为零值数组——整体兜底到默认，避免渲染出空名字
	if len(cfg.Names) != 10 || len(cfg.Thresholds) != 9 || len(cfg.EffectsTiers) != 10 || len(cfg.DifficultyNames) != 10 {
		def := DefaultLevelConfig()
		if len(cfg.Names) != 10 {
			cfg.Names = def.Names
		}
		if len(cfg.Thresholds) != 9 {
			cfg.Thresholds = def.Thresholds
		}
		if len(cfg.EffectsTiers) != 10 {
			cfg.EffectsTiers = def.EffectsTiers
		}
		if len(cfg.DifficultyNames) != 10 {
			cfg.DifficultyNames = def.DifficultyNames
		}
	}
	cfg.Normalize()
	return cfg, nil
}

// UpdateLevelConfig 覆盖保存等级配置（root；调用方先 Normalize）。
func (r *Repo) UpdateLevelConfig(ctx context.Context, cfg *LevelConfig) error {
	if _, err := r.db.Collection("settings").InsertOne(ctx, bson.M{"_id": "system"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	_, err := r.db.Collection("settings").UpdateOne(ctx,
		bson.M{"_id": "system"}, bson.M{"$set": bson.M{"levelConfig": cfg}})
	return err
}

// DifficultyXP 每道难度题的经验分：D1=1 起逐档翻倍（D2=2、D3=4 … D10=512）。
func DifficultyXP(d int) int64 { return int64(1) << (d - 1) }

// LevelProgress 由「各难度 AC 题数 + 经验调整量」算等级：XP = Σ(题数 × 2^(难度−1))，
// 与等级无关（不衰减）；等级 = 满足 XP ≥ 阈值(L) 的最大 L，阈值严格递增、首个
// 不满足即可停。adjust 为管理端加/减经验的合计（可负）；返回 等级、XP（含调整、
// 显示钳非负）、当前级阈值、下一级阈值（Lv.10 时 hasNext=false）。
func LevelProgress(cfg *LevelConfig, counts [10]int64, adjust int64) (level int, xp int64, curThr, nextThr int64, hasNext bool) {
	xp = adjust
	for d := 1; d <= 10; d++ {
		xp += counts[d-1] * DifficultyXP(d)
	}
	level = 1
	for L := 2; L <= 10; L++ {
		if xp >= cfg.Thresholds[L-2] {
			level = L
		} else {
			break
		}
	}
	if xp < 0 {
		xp = 0
	}
	curThr = 0
	if level >= 2 {
		curThr = cfg.Thresholds[level-2]
	}
	if level < 10 {
		nextThr, hasNext = cfg.Thresholds[level-1], true
	}
	return level, xp, curThr, nextThr, hasNext
}

// UserTier 用户当前等级 1–10（今日挑战个性化选题用，v0.97.12）：定级三件套
//（配置 + 各难度 AC 数 + 经验调整）单点组装——口径与 server 层 levelAgg/排行榜
// 一致；配置或 AC 数失败回 1（青铜），经验调整失败按全 0 不拖垮。
func (r *Repo) UserTier(ctx context.Context, uid int64) int {
	cfg, err := r.GetLevelConfig(ctx)
	if err != nil {
		return 1
	}
	counts, err := r.SolvedDifficultyCounts(ctx, []int64{uid})
	if err != nil {
		return 1
	}
	adjs, _ := r.SumXpAdjust(ctx, []int64{uid})
	lv, _, _, _, _ := LevelProgress(cfg, counts[uid], adjs[uid])
	if lv < 1 || lv > 10 {
		return 1
	}
	return lv
}

// ---- 「各难度 AC 题数」两步法（v0.100）----
//
// 先聚合出每人的去重 AC 题集，再只对涉及的题号取难度，计数在 Go 内存里做。原实现是
// 单管道里逐行 $lookup 题目表——每行一次子查询游标（约 70µs/行），耗时随历史 AC 线性
// 上涨（2000 人 / 约 6 万条 AC 对的量级一次 4.8s，两步法 122ms）。全站「题集 → 难度计数」
// 只有这一份实现：等级徽章、成绩表、等级闸、判题回写链的成就判定与排行榜快照共用。

// rankInChunk $in 批量取数的单批上限（题号/用户号分批，单条命令体积有界）。
const rankInChunk = 10000

// solvedSets 用户的去重 AC 题集（uid → pid 列表）：status=Accepted ∧ published 的提交按
// (uid,pid) 去重，口径与总榜一致。uids 为空取全站；limit > 0 时按题集大小降序只取前
// limit 人（防病态规模的上限）。前段只走覆盖索引 {status,published,uid,pid}，不读文档。
func (r *Repo) solvedSets(ctx context.Context, uids []int64, limit int) (map[int64][]int64, error) {
	match := bson.M{"status": StatusAccepted, "published": true}
	if len(uids) > 0 {
		match["uid"] = bson.M{"$in": uids}
	}
	pipeline := []bson.M{
		{"$match": match},
		{"$group": bson.M{"_id": "$uid", "pids": bson.M{"$addToSet": "$pid"}}},
	}
	if limit > 0 {
		pipeline = append(pipeline,
			bson.M{"$project": bson.M{"pids": 1, "n": bson.M{"$size": "$pids"}}},
			bson.M{"$sort": bson.D{{Key: "n", Value: -1}, {Key: "_id", Value: 1}}},
			bson.M{"$limit": limit},
		)
	}
	cursor, err := r.submissions.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := map[int64][]int64{}
	for cursor.Next(ctx) {
		var row struct {
			ID   int64   `bson:"_id"`
			Pids []int64 `bson:"pids"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = row.Pids
	}
	return out, cursor.Err()
}

// allPIDs 一组题集涉及的全部题号（去重；顺序无关，仅作 $in 取难度用）。
func allPIDs(sets map[int64][]int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(sets))
	for _, pids := range sets {
		for _, pid := range pids {
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			out = append(out, pid)
		}
	}
	return out
}

// problemDifficulties 取一批题目的难度（pid → 生效难度：未设置/越界按 D3，
// EffectiveDifficulty 同规则）。题号查不到 = 题目已删除，不入返回表——调用方据此把
// 已删题排除出解题数与等级。入参重复无妨。
func (r *Repo) problemDifficulties(ctx context.Context, pids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(pids))
	for start := 0; start < len(pids); start += rankInChunk {
		end := start + rankInChunk
		if end > len(pids) {
			end = len(pids)
		}
		cursor, err := r.problems.Find(ctx, bson.M{"_id": bson.M{"$in": pids[start:end]}},
			options.Find().SetProjection(bson.M{"difficulty": 1}))
		if err != nil {
			return nil, err
		}
		var rows []struct {
			ID         int64 `bson:"_id"`
			Difficulty int   `bson:"difficulty"`
		}
		if err := cursor.All(ctx, &rows); err != nil {
			cursor.Close(ctx)
			return nil, err
		}
		cursor.Close(ctx)
		for _, row := range rows {
			d := row.Difficulty
			if !ValidDifficulty(d) {
				d = 3
			}
			out[row.ID] = d
		}
	}
	return out, nil
}

// countDifficulties 题集按难度计数：只数难度表里查得到的题（已删题目不计），
// 返回 counts[难度-1] 与其中现存题数（即解题数）。
func countDifficulties(pids []int64, diff map[int64]int) (counts [10]int64, solved int64) {
	for _, pid := range pids {
		d, ok := diff[pid]
		if !ok {
			continue
		}
		counts[d-1]++
		solved++
	}
	return counts, solved
}

// SolvedDifficultyCounts 批量统计用户「各难度 AC 题数」（uid → counts[难度-1]）。
// 口径与总榜一致（status=Accepted ∧ published）；题目已删除的不计。
// 难度缺省按 3（EffectiveDifficulty 同规则）。没有任何有效 AC 的用户不入返回表。
func (r *Repo) SolvedDifficultyCounts(ctx context.Context, uids []int64) (map[int64][10]int64, error) {
	out := make(map[int64][10]int64, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	sets, err := r.solvedSets(ctx, uids, 0)
	if err != nil {
		return nil, err
	}
	diff, err := r.problemDifficulties(ctx, allPIDs(sets))
	if err != nil {
		return nil, err
	}
	for uid, pids := range sets {
		if counts, solved := countDifficulties(pids, diff); solved > 0 {
			out[uid] = counts
		}
	}
	return out, nil
}

// ---- 存量难度迁移（difficultyV1，v0.11 一次性） ----

// migrateDifficultyEstimate 给未设难度的存量题按通过率启发式预估（用户对照锚点表逐题核对）。
// 通过率 = 去重用户口径：做过题的不同用户中满分用户占比（抗重复提交灌水）。
// 分档：≥0.9→D1 ≥0.8→D2 ≥0.5→D3 ≥0.3→D4 ≥0.2→D5 ≥0.1→D6 ≥0.05→D7 ≥0.02→D8 其余→D9；
// 无人做过的题 D3（表单默认档）。D10 只手工给。已显式设过难度（1–10）的题不动。
func (r *Repo) migrateDifficultyEstimate(ctx context.Context) error {
	type cntRow struct {
		ID int64 `bson:"_id"`
		N  int64 `bson:"n"`
	}
	distinctUsers := func(match bson.M) (map[int64]int64, error) {
		cursor, err := r.submissions.Aggregate(ctx, []bson.M{
			{"$match": match},
			{"$group": bson.M{"_id": bson.M{"pid": "$pid", "uid": "$uid"}}},
			{"$group": bson.M{"_id": "$_id.pid", "n": bson.M{"$sum": 1}}},
		})
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)
		m := map[int64]int64{}
		for cursor.Next(ctx) {
			var row cntRow
			if err := cursor.Decode(&row); err != nil {
				return nil, err
			}
			m[row.ID] = row.N
		}
		return m, cursor.Err()
	}
	solved, err := distinctUsers(bson.M{"score": 100})
	if err != nil {
		return err
	}
	attempted, err := distinctUsers(bson.M{"status": bson.M{"$ne": StatusCancelled}})
	if err != nil {
		return err
	}

	cursor, err := r.problems.Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	changed := 0
	for cursor.Next(ctx) {
		var p Problem
		if err := cursor.Decode(&p); err != nil {
			return err
		}
		if p.Difficulty >= 1 && p.Difficulty <= 10 {
			continue
		}
		d := 3
		if att := attempted[p.ID]; att > 0 {
			rate := float64(solved[p.ID]) / float64(att)
			switch {
			case rate >= 0.9:
				d = 1
			case rate >= 0.8:
				d = 2
			case rate >= 0.5:
				d = 3
			case rate >= 0.3:
				d = 4
			case rate >= 0.2:
				d = 5
			case rate >= 0.1:
				d = 6
			case rate >= 0.05:
				d = 7
			case rate >= 0.02:
				d = 8
			default:
				d = 9
			}
		}
		if _, err := r.problems.UpdateOne(ctx, bson.M{"_id": p.ID},
			bson.M{"$set": bson.M{"difficulty": d}}); err != nil {
			return err
		}
		changed++
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	if changed > 0 {
		slog.Info("已按通过率预估存量题的难度（D10 仅手工）", "component", "migrate", "count", changed)
	}
	return nil
}

// ---- 经验调整（管理端对指定用户加/减经验） ----
// 等级是读时聚合值（AC 难度分），没有可改的存储经验——调整以流水形式落 xpAdjust
// 集合 {uid, delta, reason, operatorUid, at}，聚合时按 uid 求和并入 LevelProgress：
// 可加可减、天然留审计。delta 正 = 加经验，负 = 减。

// AddXpAdjust 记一笔经验调整（调用方校验 delta ≠ 0 与权限）。
func (r *Repo) AddXpAdjust(ctx context.Context, uid, delta int64, reason string, operatorUID int64) error {
	id, err := r.nextSeq(ctx, "xpAdjust")
	if err != nil {
		return err
	}
	_, err = r.db.Collection("xpAdjust").InsertOne(ctx, bson.M{
		"_id": id, "uid": uid, "delta": delta,
		"reason": reason, "operatorUid": operatorUID, "at": time.Now(),
	})
	return err
}

// SumXpAdjust 批量汇总若干用户的经验调整合计（uid → delta 合计，无记录 = 0）。
func (r *Repo) SumXpAdjust(ctx context.Context, uids []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	cursor, err := r.db.Collection("xpAdjust").Aggregate(ctx, []bson.M{
		{"$match": bson.M{"uid": bson.M{"$in": uids}}},
		{"$group": bson.M{"_id": "$uid", "n": bson.M{"$sum": "$delta"}}},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID int64 `bson:"_id"`
			N  int64 `bson:"n"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = row.N
	}
	return out, cursor.Err()
}
