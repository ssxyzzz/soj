package repo

// 学生等级与题目难度（v0.11）：十阶共用一条难度轴（D1–D10 / Lv.1–10）。
// 难度是题目属性（Problem.Difficulty）；等级是读时实时聚合值、不落库——
// 由用户「各难度 AC 题数」按当前配置算 XP 再定级，老师调难度后全员等级自动跟随。
//
// 经验规则（网游式衰减）：每 AC 一题得 XP = 题目难度分 × 衰减系数。
// 系数按「等级差 = 当前等级 − 题目难度」取值：题目难度 ≥ 当前等级全额；
// 低 1/2/3 级分别乘 Decay1/2/3；低 ≥4 级为 0（打太简单的题不涨经验）。
// 升级阈值 thresholds[i] = 达 Lv.(i+2) 所需累计 XP（默认 100×L×(L−1)，每档同难度题约 200 题）。

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// 等级配置默认值（root 后台可调；名字 2026-10-22 两字段位版定稿：取自王者荣耀段位体系）。
var DefaultLevelNames = [10]string{"青铜", "白银", "黄金", "铂金", "钻石", "星耀", "宗师", "王者", "传奇", "极境"}

// DefaultLevelThresholds 达 Lv.2–10 的累计 XP：100×L×(L−1)。
var DefaultLevelThresholds = [9]int64{200, 600, 1200, 2000, 3000, 4200, 5600, 7200, 9000}

const (
	DefaultDecay1 = 0.5
	DefaultDecay2 = 0.2
	DefaultDecay3 = 0.05
)

// LevelConfig 等级体系配置（settings 集合 system 文档 levelConfig 字段；nil = 全默认）。
type LevelConfig struct {
	Names      [10]string `bson:"names" json:"names"`           // Lv.1–10 名称文字
	Thresholds [9]int64   `bson:"thresholds" json:"thresholds"` // 达 Lv.2–10 的累计 XP，严格递增
	Decay1     float64    `bson:"decay1" json:"decay1"`         // 低 1 级题的分系数
	Decay2     float64    `bson:"decay2" json:"decay2"`         // 低 2 级
	Decay3     float64    `bson:"decay3" json:"decay3"`         // 低 3 级；低 ≥4 级恒 0，不配置
	// 题目难度名（D1–10 档的展示文字，2026-10-22 起 Difficulty 徽章/筛选/建题选择器均显示名字，
	// 不再用「D1~D10」描述）；默认与等级名相同，root 后台独立可改。
	DifficultyNames [10]string `bson:"difficultyNames" json:"difficulty_names"`
	// 特效开关：等级徽章的动画/光效（难度徽章恒静态，不受此控制）。
	// 全局关 = 部署方不想要炫酷效果；逐级关可只保留低档纯色。
	EffectsEnabled bool     `bson:"effectsEnabled" json:"effects_enabled"`
	EffectsTiers   [10]bool `bson:"effectsTiers" json:"effects_tiers"`
}

// DefaultLevelConfig 默认等级配置（深拷贝默认数组；难度名默认与等级名相同）。
func DefaultLevelConfig() *LevelConfig {
	cfg := &LevelConfig{
		Names:           DefaultLevelNames,
		Thresholds:      DefaultLevelThresholds,
		Decay1:          DefaultDecay1,
		Decay2:          DefaultDecay2,
		Decay3:          DefaultDecay3,
		DifficultyNames: DefaultLevelNames,
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
	c.Decay1 = clamp01(c.Decay1)
	c.Decay2 = clamp01(c.Decay2)
	c.Decay3 = clamp01(c.Decay3)
	return ok
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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

// DecayFactor 等级差 gap = level − difficulty 对应的题分系数。
func DecayFactor(cfg *LevelConfig, level, difficulty int) float64 {
	switch level - difficulty {
	case 1:
		return cfg.Decay1
	case 2:
		return cfg.Decay2
	case 3:
		return cfg.Decay3
	default:
		if level-difficulty >= 4 {
			return 0
		}
		return 1
	}
}

// LevelProgress 由「各难度 AC 题数 + 经验调整量」算等级：等级 = 满足 XP(L)+adjust ≥ 阈值(L)
// 的最大 L。XP 随假设等级单调不增（衰减变狠）、阈值严格递增，首个不满足即可停。
// adjust 为管理端加/减经验的合计（可负）；返回 等级、该等级下的 XP（含调整、显示钳非负）、
// 当前级阈值、下一级阈值（Lv.10 时 hasNext=false）。
func LevelProgress(cfg *LevelConfig, counts [10]int64, adjust int64) (level int, xp int64, curThr, nextThr int64, hasNext bool) {
	level = 1
	for L := 1; L <= 10; L++ {
		var xpL int64
		for d := 1; d <= 10; d++ {
			if counts[d-1] > 0 {
				xpL += int64(float64(counts[d-1]*int64(d)) * DecayFactor(cfg, L, d))
			}
		}
		thr := int64(0)
		if L >= 2 {
			thr = cfg.Thresholds[L-2]
		}
		if xpL+adjust >= thr {
			level, xp = L, xpL+adjust
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

// SolvedDifficultyCounts 批量统计用户「各难度 AC 题数」（uid → counts[难度-1]）。
// 口径与总榜一致（status=Accepted ∧ published）；题目已删除的不计。
// 难度缺省按 3（EffectiveDifficulty 同规则）。
func (r *Repo) SolvedDifficultyCounts(ctx context.Context, uids []int64) (map[int64][10]int64, error) {
	out := make(map[int64][10]int64, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	sum := func(d int) bson.M {
		return bson.M{"$sum": bson.M{"$cond": bson.A{
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$p.difficulty", 3}}, d}}, 1, 0}}}
	}
	group := bson.M{"_id": "$_id.uid"}
	for d := 1; d <= 10; d++ {
		group[fmt.Sprintf("c%d", d)] = sum(d)
	}
	cursor, err := r.Submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"uid": bson.M{"$in": uids}, "status": StatusAccepted, "published": true}},
		{"$group": bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}},
		{"$lookup": bson.M{
			"from":         "problems",
			"localField":   "_id.pid",
			"foreignField": "_id",
			"pipeline":     bson.A{bson.M{"$project": bson.M{"difficulty": 1}}},
			"as":           "p",
		}},
		{"$unwind": "$p"},
		{"$group": group},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID  int64 `bson:"_id"`
			C1  int64 `bson:"c1"`
			C2  int64 `bson:"c2"`
			C3  int64 `bson:"c3"`
			C4  int64 `bson:"c4"`
			C5  int64 `bson:"c5"`
			C6  int64 `bson:"c6"`
			C7  int64 `bson:"c7"`
			C8  int64 `bson:"c8"`
			C9  int64 `bson:"c9"`
			C10 int64 `bson:"c10"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = [10]int64{row.C1, row.C2, row.C3, row.C4, row.C5, row.C6, row.C7, row.C8, row.C9, row.C10}
	}
	return out, cursor.Err()
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
		cursor, err := r.Submissions.Aggregate(ctx, []bson.M{
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

	cursor, err := r.Problems.Find(ctx, bson.M{})
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
		if _, err := r.Problems.UpdateOne(ctx, bson.M{"_id": p.ID},
			bson.M{"$set": bson.M{"difficulty": d}}); err != nil {
			return err
		}
		changed++
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	log.Printf("[migrate][difficultyV1] 已按通过率预估 %d 道存量题的难度（D10 仅手工）", changed)
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
