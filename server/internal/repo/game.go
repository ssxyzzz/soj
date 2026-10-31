package repo

// 游戏感批数据地基（v0.41）：连击/坚持计数器（判题回写维护）、每日一题（确定性
// 选题 + 当日首 AC 掉落 ×2）、每日复仇（错题重战掉落 ×1.5）、成就判定、知识星图、
// 降临横幅、连击/坚持双榜。总原则「钩住不施压」：只记正向数据，无任何惩罚状态。

import (
	"context"
	"hash/fnv"
	"log/slog"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// BeijingZone 北京时区（对外暴露：端点层日界计算与 repo 同源）。
func BeijingZone() *time.Location { return beijingZone }

var beijingZone = time.FixedZone("Asia/Shanghai", 8*3600)

// beijingDay 北京时间日期串（坚持/每日口径的「一天」）。
func beijingDay(t time.Time) string {
	return t.In(beijingZone).Format("2006-01-02")
}

// ---- 连击 / 坚持（判题回写路径维护，user 文档字段） ----

// GameStats 连击与坚持现状（个人主页 / 提交结果 payload 消费）。
type GameStats struct {
	ComboCur   int    `bson:"comboCur"`
	ComboBest  int    `bson:"comboBest"`
	LastAcPID  int64  `bson:"lastAcPid,omitempty"`
	LastAcDay  string `bson:"lastAcDay,omitempty"`
	StreakDays int    `bson:"streakDays"`
	StreakBest int    `bson:"streakBest"`
}

// TrackJudgeOutcome 判题回写后的连击/坚持维护（仅 published 提交调用——比赛期
// 提交不动计数，与玉/XP 口径一致）。连击（v0.45 跨日口径）：连续异题 AC 累计，
// 同题重复 AC 不涨（防重交刷连击）；WA 等未 AC 正式提交清零（Cancelled 主动放弃
// 不清——手滑保护）；断更日（上次 AC 在前天或更早）清零——与火焰/坚持榜「连续」
// 语义统一。坚持按北京时间自然日：当天已有 AC 不变，跨一天 +1，断更重置为 1。
func (r *Repo) TrackJudgeOutcome(ctx context.Context, uid, pid int64, accepted, cancelled bool) (*GameStats, error) {
	var u struct {
		ComboCur   int    `bson:"comboCur"`
		ComboBest  int    `bson:"comboBest"`
		LastAcPID  int64  `bson:"lastAcPid"`
		LastAcDay  string `bson:"lastAcDay"`
		StreakDays int    `bson:"streakDays"`
		StreakBest int    `bson:"streakBest"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{
			"comboCur": 1, "comboBest": 1, "lastAcPid": 1,
			"lastAcDay": 1, "streakDays": 1, "streakBest": 1,
		})).Decode(&u); err != nil {
		if IsNotFound(err) {
			u.LastAcPID = -1 // 区分「无文档」与「首提交」
		} else {
			return nil, err
		}
	}
	today := beijingDay(time.Now())
	yesterday := beijingDay(time.Now().AddDate(0, 0, -1))
	// 断更判定：上次 AC 在前天或更早（今天/昨天有 AC 均算连续）——首提交（空串）不算断
	broke := u.LastAcDay != "" && u.LastAcDay != today && u.LastAcDay != yesterday
	set := bson.M{}
	if accepted {
		if pid != u.LastAcPID {
			combo := u.ComboCur
			if broke {
				combo = 0 // 断更：连击从本次重新起算
			}
			set["comboCur"] = combo + 1
			if combo+1 > u.ComboBest {
				set["comboBest"] = combo + 1
			}
			set["lastAcPid"] = pid
		} else if broke {
			// 同题重交不涨连击，但断更本身要清——连续性度量不残留旧值
			set["comboCur"] = 0
		}
		if today != u.LastAcDay {
			streak := 1
			if u.LastAcDay == yesterday {
				streak = u.StreakDays + 1
			}
			set["streakDays"] = streak
			set["lastAcDay"] = today
			if streak > u.StreakBest {
				set["streakBest"] = streak
			}
		}
	} else if !cancelled {
		// 未 AC：连击清零（Cancelled 不清——主动放弃不算失误）；坚持不受影响
		set["comboCur"] = 0
		set["lastAcPid"] = 0
	}
	if len(set) == 0 {
		return r.GetGameStats(ctx, uid)
	}
	if _, err := r.users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$set": set}); err != nil {
		return nil, err
	}
	return r.GetGameStats(ctx, uid)
}

// GetGameStats 连击/坚持现状（无文档全零）。
func (r *Repo) GetGameStats(ctx context.Context, uid int64) (*GameStats, error) {
	var u struct {
		ComboCur   int    `bson:"comboCur"`
		ComboBest  int    `bson:"comboBest"`
		StreakDays int    `bson:"streakDays"`
		StreakBest int    `bson:"streakBest"`
	}
	err := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{
			"comboCur": 1, "comboBest": 1, "streakDays": 1, "streakBest": 1,
		})).Decode(&u)
	if IsNotFound(err) {
		return &GameStats{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &GameStats{ComboCur: u.ComboCur, ComboBest: u.ComboBest,
		StreakDays: u.StreakDays, StreakBest: u.StreakBest}, nil
}

// ---- 每日一题（确定性选题：按日期 seed 轮转难度，同日全站同题） ----

// DailyProblem 当日每日一题（dailyProblems 文档，_id = 日期串）。
type DailyProblem struct {
	Day        string `bson:"_id"`
	PID        int64  `bson:"pid"`
	Difficulty int    `bson:"difficulty"`
}

// EnsureDailyProblem 取当日每日一题（无则确定性计算并落库，dup 并发回读）。
// 选题：北京日期年积日 → 难度轮转 1–10 → 该难度公开未隐藏、有数据的传统题中
// 按日期 hash 取一本档无题逐档向下兜底。
func (r *Repo) EnsureDailyProblem(ctx context.Context) (*DailyProblem, error) {
	day := beijingDay(time.Now())
	var dp DailyProblem
	err := r.db.Collection("dailyProblems").FindOne(ctx, bson.M{"_id": day}).Decode(&dp)
	if err == nil {
		return &dp, nil
	}
	if !IsNotFound(err) {
		return nil, err
	}
	bt, _ := time.ParseInLocation("2006-01-02", day, beijingZone)
	seed := bt.YearDay()
	for d := seed%10 + 1; d >= 1; d-- {
		filter := bson.M{
			"difficulty": d, "hidden": bson.M{"$ne": true},
			"visibility": "public", "cases.0": bson.M{"$exists": true},
			"$or": bson.A{bson.M{"type": bson.M{"$exists": false}}, bson.M{"type": ""}},
		}
		n, err := r.problems.CountDocuments(ctx, filter)
		if err != nil || n == 0 {
			continue
		}
		h := fnv.New32a()
		h.Write([]byte(day))
		skip := int64(h.Sum32() % uint32(n))
		cur, err := r.problems.Find(ctx, filter,
			options.Find().SetProjection(bson.M{"_id": 1}).
				SetSort(bson.D{{Key: "_id", Value: 1}}).SetSkip(skip).SetLimit(1))
		if err != nil {
			return nil, err
		}
		var p struct {
			ID int64 `bson:"_id"`
		}
		has := cur.Next(ctx)
		cur.Close(ctx)
		if !has {
			continue
		}
		if err := cur.Decode(&p); err != nil {
			return nil, err
		}
		dp = DailyProblem{Day: day, PID: p.ID, Difficulty: d}
		if _, err := r.db.Collection("dailyProblems").InsertOne(ctx, dp); err != nil {
			if mongo.IsDuplicateKeyError(err) { // 并发：回读胜者
				return r.EnsureDailyProblem(ctx)
			}
			return nil, err
		}
		return &dp, nil
	}
	return nil, nil // 全站无可用题（空库）
}

// IsTodayDaily 该题是否今日每日一题（判题发玉路径）。
func (r *Repo) IsTodayDaily(ctx context.Context, pid int64) bool {
	dp, err := r.EnsureDailyProblem(ctx)
	return err == nil && dp != nil && dp.PID == pid
}

// ---- 复仇（错题重战）----

// RevengeRow 错题本/每日复仇行。
type RevengeRow struct {
	PID        int64     `json:"pid"`
	Title      string    `json:"title"`
	Difficulty int       `json:"difficulty"`
	BestScore  int       `json:"best_score"`
	LastAt     time.Time `json:"last_at"`
}

// Wrongbook 错题集：有非取消未 AC 提交且从未 AC 的题（jadeAwards 无记录 = 未 AC 过），
// 聚合每题最好成绩、最近一战时间与最早错误时间；按最久未战优先（lastAt 升序，v0.45 改）。
// minAge > 0 时只保留最早错误距今满 minAge 的题（复仇窗口：当日顺路做对的不进推荐）；
// 错题本全量传 0 不过滤。
func (r *Repo) Wrongbook(ctx context.Context, uid int64, limit int, minAge time.Duration) ([]RevengeRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"uid": uid, "status": bson.M{"$nin": bson.A{StatusAccepted, StatusCancelled}}, "published": true}}},
		{{Key: "$group", Value: bson.M{
			"_id":     "$pid",
			"best":    bson.M{"$max": "$score"},
			"lastAt":  bson.M{"$max": "$createdAt"},
			"firstAt": bson.M{"$min": "$createdAt"},
		}}},
	}
	if minAge > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: bson.M{
			"firstAt": bson.M{"$lte": time.Now().Add(-minAge)},
		}}})
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "lastAt", Value: 1}}}},
		bson.D{{Key: "$limit", Value: limit}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from": "problems", "localField": "_id", "foreignField": "_id", "as": "p",
			"pipeline": bson.A{bson.M{"$project": bson.M{"title": 1, "difficulty": 1}}},
		}}},
		bson.D{{Key: "$unwind", Value: bson.M{"path": "$p", "preserveNullAndEmptyArrays": true}}},
	)
	cur, err := r.submissions.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	rows := make([]RevengeRow, 0, limit)
	for cur.Next(ctx) {
		var row struct {
			PID    int64     `bson:"_id"`
			Best   int       `bson:"best"`
			LastAt time.Time `bson:"lastAt"`
			P      struct {
				Title      string `bson:"title"`
				Difficulty int    `bson:"difficulty"`
			} `bson:"p"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		// 未 AC 过的才留在错题集（jadeAwards 命中 = 已复仇成功）
		if awarded, _ := r.GetJadeAward(ctx, uid, row.PID); awarded != nil {
			continue
		}
		rows = append(rows, RevengeRow{PID: row.PID, Title: row.P.Title,
			Difficulty: row.P.Difficulty, BestScore: row.Best, LastAt: row.LastAt})
	}
	return rows, cur.Err()
}

// （isRevenge 已于 v0.45 由 jade.go revengeState 取代：24h 窗口 + 当日名额 +
// 排除 Cancelled，原「有非 AC 提交即算」口径退役不留。）

// RevengeSlotUsed 今日复仇名额是否已用（v0.45：每日 1 名额，推荐卡完成态展示）。
func (r *Repo) RevengeSlotUsed(ctx context.Context, uid int64) bool {
	var u struct {
		RevengeDay string `bson:"revengeDay"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{"revengeDay": 1})).Decode(&u); err != nil {
		return false
	}
	return u.RevengeDay == beijingDay(time.Now())
}

// ---- 成就（定义表 + 判定）----

// 成就档位。
const (
	AchBronze = "bronze"
	AchSilver = "silver"
	AchGold   = "gold"
	AchColor  = "color"
)

// AchievementDef 成就定义（前端字库同源下发）。
type AchievementDef struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Desc  string `json:"desc"`
	Tier  string `json:"tier"`
	Icon  string `json:"icon"`
}

// AchievementDefs 全部成就定义（判定条件见 EvaluateAchievements 注释）。
var AchievementDefs = []AchievementDef{
	{"first_blood", "初出茅庐", "首次通过一道题", AchBronze, "🩸"},
	{"ac_10", "小试牛刀", "累计通过 10 题", AchBronze, "⚔️"},
	{"ac_50", "渐入佳境", "累计通过 50 题", AchSilver, "🗡️"},
	{"ac_100", "百题斩", "累计通过 100 题", AchGold, "💯"},
	{"ac_500", "五百连城", "累计通过 500 题", AchColor, "🏔️"},
	{"diff_5", "登堂入室", "通过一道 D5 难度题", AchSilver, "🔺"},
	{"diff_8", "攀登者", "通过一道 D8 难度题", AchGold, "🧗"},
	{"diff_10", "问鼎", "通过一道 D10 难度题", AchColor, "👑"},
	{"all_diff", "十阶全通", "一至十档难度全部通关", AchColor, "🌌"},
	{"combo_5", "五连击", "连续通过 5 题不失误", AchBronze, "🔥"},
	{"combo_10", "十连无双", "连续通过 10 题不失误", AchSilver, "⚡"},
	{"combo_20", "二十连传说", "连续通过 20 题不失误", AchGold, "🌟"},
	{"streak_7", "七日之约", "连续 7 天有 AC", AchSilver, "📅"},
	{"streak_30", "三十而立", "连续 30 天有 AC", AchGold, "🗓️"},
	{"nightowl", "夜猫子", "凌晨 0–5 点通过一题", AchBronze, "🦉"},
	{"earlybird", "早起鸟", "清晨 5–8 点通过一题", AchBronze, "🐦"},
	{"revenge_1", "一雪前耻", "首次复仇成功", AchBronze, "🗡️"},
	{"revenge_10", "复仇者", "复仇成功 10 次", AchSilver, "😤"},
	{"daily_1", "每日一题", "完成一次每日一题", AchBronze, "📌"},
	{"daily_7", "周而复始", "完成 7 次每日一题", AchSilver, "🔄"},
	{"perfect_1", "一发入魂", "首次提交即满分", AchSilver, "🎯"},
	{"perfect_10", "弹无虚发", "首发满分 10 次", AchGold, "🏹"},
	{"challenge_1", "初试锋芒", "完成第一次挑战", AchBronze, "⚡"},
	{"challenge_10", "挑战王", "完成 10 次挑战", AchGold, "🏆"},
}

// Achievement unlock 文档。
type Achievement struct {
	ID   int64     `bson:"_id"`
	UID  int64     `bson:"uid"`
	Code string    `bson:"code"`
	SID  int64     `bson:"sid,omitempty"` // 解锁来源提交（payload 查「本次新解锁」）
	At   time.Time `bson:"at"`
}

// EvaluateAchievements 判题回写后的成就判定（仅 AC 且 published 调用）。返回本次
// 新解锁的 code 列表。所有统计走点查/既有计数，无全表聚合。
func (r *Repo) EvaluateAchievements(ctx context.Context, uid, sid, pid int64, score int, judgedAt time.Time, perfectOneShot, isChallenge bool) ([]string, error) {
	// 已解锁集合（小，个人全量拉回）
	cur, err := r.achievements.Find(ctx, bson.M{"uid": uid},
		options.Find().SetProjection(bson.M{"code": 1}))
	if err != nil {
		return nil, err
	}
	var owned []struct {
		Code string `bson:"code"`
	}
	if err := cur.All(ctx, &owned); err != nil {
		return nil, err
	}
	has := map[string]bool{}
	for _, o := range owned {
		has[o.Code] = true
	}

	// 统计（jadeAwards = 去重首 AC 记录，count 即累计解题数）
	totalAC, _ := r.jadeAwards.CountDocuments(ctx, bson.M{"uid": uid})
	dailyCnt, _ := r.jadeAwards.CountDocuments(ctx, bson.M{"uid": uid, "daily": true})
	revengeCnt, _ := r.jadeAwards.CountDocuments(ctx, bson.M{"uid": uid, "revenge": true})
	stats, _ := r.GetGameStats(ctx, uid)
	p, _ := r.GetProblemMeta(ctx, pid)
	diff := 0
	if p != nil {
		diff = p.EffDifficulty()
	}
	challengeCnt := int64(0)
	if isChallenge {
		challengeCnt, _ = r.challenges.CountDocuments(ctx, bson.M{"uid": uid, "status": "finished"})
	}
	hour := judgedAt.In(beijingZone).Hour()

	cand := map[string]bool{
		"first_blood": totalAC >= 1,
		"ac_10":       totalAC >= 10,
		"ac_50":       totalAC >= 50,
		"ac_100":      totalAC >= 100,
		"ac_500":      totalAC >= 500,
		"diff_5":      diff >= 5,
		"diff_8":      diff >= 8,
		"diff_10":     diff >= 10,
		"combo_5":     stats.ComboBest >= 5,
		"combo_10":    stats.ComboBest >= 10,
		"combo_20":    stats.ComboBest >= 20,
		"streak_7":    stats.StreakBest >= 7,
		"streak_30":   stats.StreakBest >= 30,
		"nightowl":    hour < 5,
		"earlybird":   hour >= 5 && hour < 8,
		"revenge_1":   revengeCnt >= 1,
		"revenge_10":  revengeCnt >= 10,
		"daily_1":     dailyCnt >= 1,
		"daily_7":     dailyCnt >= 7,
		"perfect_1":   perfectOneShot,
	}
	if perfectOneShot {
		// 首发满分累计：jadeAwards 无 perfect 标记，用 submissions 抽样成本高——
		// 记在成就计数器（user 文档 perfectCount 判题路径累加）
		pc, _ := r.bumpPerfectCount(ctx, uid)
		cand["perfect_10"] = pc >= 10
	}
	if isChallenge {
		cand["challenge_1"] = challengeCnt >= 1
		cand["challenge_10"] = challengeCnt >= 10
	}
	// 全难度通关：SolvedDifficultyCounts 十档全 ≥1（一次聚合，仅 AC 路径）
	if counts, err := r.SolvedDifficultyCounts(ctx, []int64{uid}); err == nil {
		all := true
		for _, c := range counts[uid] {
			if c == 0 {
				all = false
				break
			}
		}
		cand["all_diff"] = all
	}

	unlocked := make([]string, 0, 2)
	for _, def := range AchievementDefs {
		if !cand[def.Code] || has[def.Code] {
			continue
		}
		id, err := r.nextSeq(ctx, "achievement")
		if err != nil {
			return unlocked, err
		}
		if _, err := r.achievements.InsertOne(ctx, Achievement{
			ID: id, UID: uid, Code: def.Code, SID: sid, At: judgedAt,
		}); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				continue
			}
			return unlocked, err
		}
		unlocked = append(unlocked, def.Code)
		slog.Info("成就解锁", "component", "game", "uid", uid, "code", def.Code)
	}
	return unlocked, nil
}

// bumpPerfectCount user.perfectCount +1 并返回新值。
func (r *Repo) bumpPerfectCount(ctx context.Context, uid int64) (int, error) {
	var doc struct {
		PerfectCount int `bson:"perfectCount"`
	}
	err := r.users.FindOneAndUpdate(ctx, bson.M{"_id": uid},
		bson.M{"$inc": bson.M{"perfectCount": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil && !IsNotFound(err) {
		return 0, err
	}
	return doc.PerfectCount, nil
}

// AchievementsForSubmit 某提交新解锁的成就（提交结果 payload）。
func (r *Repo) AchievementsForSubmit(ctx context.Context, uid, sid int64) ([]string, error) {
	cur, err := r.achievements.Find(ctx, bson.M{"uid": uid, "sid": sid},
		options.Find().SetProjection(bson.M{"code": 1}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Code string `bson:"code"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(rows))
	for _, r := range rows {
		codes = append(codes, r.Code)
	}
	return codes, nil
}

// ---- 知识星图 ----

// StarNode 星图节点：知识点 tag 及点亮进度。
type StarNode struct {
	TagID int64  `json:"tag_id"`
	Name  string `json:"name"`
	Solved int  `json:"solved"` // 去重 AC 题数
	Lit   bool   `json:"lit"`    // ≥3 题点亮
}

// StarMap 知识星图：个人各知识点去重 AC 数（AC 提交 join 题目标签）。
func (r *Repo) StarMap(ctx context.Context, uid int64) ([]StarNode, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"uid": uid, "status": StatusAccepted, "published": true}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}}},
		{{Key: "$lookup", Value: bson.M{
			"from": "problems", "localField": "_id.pid", "foreignField": "_id", "as": "p",
			"pipeline": bson.A{bson.M{"$project": bson.M{"tags": 1}}},
		}}},
		{{Key: "$unwind", Value: bson.M{"path": "$p", "preserveNullAndEmptyArrays": true}}},
		{{Key: "$unwind", Value: bson.M{"path": "$p.tags", "preserveNullAndEmptyArrays": true}}},
		{{Key: "$group", Value: bson.M{"_id": "$p.tags", "solved": bson.M{"$sum": 1}}}},
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$ne": nil}}}},
	}
	cur, err := r.submissions.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	solved := map[int64]int{}
	for cur.Next(ctx) {
		var row struct {
			ID     int64 `bson:"_id"`
			Solved int   `bson:"solved"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		solved[row.ID] = row.Solved
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	// tag 名解析：知识树存 nodes 文档（非逐节点集合），整树拉回走 map
	names := map[int64]string{}
	if tree, terr := r.GetTagTree(ctx); terr == nil {
		var walk func(nodes []TagNode)
		walk = func(nodes []TagNode) {
			for _, n := range nodes {
				names[n.ID] = n.Name
				walk(n.Children)
			}
		}
		walk(tree)
	}
	nodes := make([]StarNode, 0, len(solved))
	for tagID, n := range solved {
		nodes = append(nodes, StarNode{TagID: tagID, Name: names[tagID], Solved: n, Lit: n >= 3})
	}
	// 按点亮与解题数倒序
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && (nodes[j].Solved > nodes[j-1].Solved); j-- {
			nodes[j], nodes[j-1] = nodes[j-1], nodes[j]
		}
	}
	return nodes, nil
}

// ---- 降临横幅（Lv≥7 升级记录）----

// HeraldRow 降临行（jadeBlessings level≥7 倒序）。
type HeraldRow struct {
	UID      int64     `bson:"uid" json:"uid"`
	Level    int       `bson:"level" json:"level"`
	Nickname string    `bson:"nickname" json:"nickname"`
	At       time.Time `bson:"at" json:"at"`
}

// RecentHeralds 最近的大佬降临（前端滚动横幅轮询）。
func (r *Repo) RecentHeralds(ctx context.Context, limit int) ([]HeraldRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"level": bson.M{"$gte": 7}}}},
		{{Key: "$sort", Value: bson.D{{Key: "at", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
		{{Key: "$lookup", Value: bson.M{
			"from": "users", "localField": "uid", "foreignField": "_id", "as": "u",
			"pipeline": bson.A{bson.M{"$project": bson.M{"nickname": 1}}},
		}}},
		{{Key: "$unwind", Value: bson.M{"path": "$u", "preserveNullAndEmptyArrays": true}}},
	}
	cur, err := r.jadeBlessings.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var raw []struct {
		UID      int64     `bson:"uid"`
		Level    int       `bson:"level"`
		At       time.Time `bson:"at"`
		U        struct {
			Nickname string `bson:"nickname"`
		} `bson:"u"`
	}
	if err := cur.All(ctx, &raw); err != nil {
		return nil, err
	}
	rows := make([]HeraldRow, 0, len(raw))
	for _, row := range raw {
		nickname := row.U.Nickname
		if nickname == "" {
			nickname = "用户" + strconv.FormatInt(row.UID, 10)
		}
		rows = append(rows, HeraldRow{UID: row.UID, Level: row.Level, Nickname: nickname, At: row.At})
	}
	return rows, nil
}

// ---- 连击 / 坚持双榜 ----

// GameRankRow 连击/坚持榜单行。
type GameRankRow struct {
	Rank      int    `json:"rank"`
	UID       int64  `json:"user_id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	Value     int    `json:"value"`
	Best      int    `json:"best"`
}

// GameBoard 连击榜（v0.45 双排序：默认按历史 comboBest 倒序——退坑玩家的历史成绩
// 仍在榜；by="cur" 切当前 comboCur）/ 坚持榜（streakDays 倒序）；users 集合字段直查。
// 行 Value = 排序维度值，Best = 另一维度（连击榜两维互为对照）。
func (r *Repo) GameBoard(ctx context.Context, kind, by string, limit int) ([]GameRankRow, error) {
	sortField, curF, bestF := "comboBest", "comboCur", "comboBest"
	if kind == "streak" {
		sortField, curF, bestF = "streakDays", "streakDays", "streakBest"
	} else if by == "cur" {
		sortField = "comboCur"
	}
	filter := bson.M{sortField: bson.M{"$gt": 0}}
	total, err := r.users.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}
	cur, err := r.users.Find(ctx, filter,
		options.Find().SetProjection(bson.M{"uname": 1, "nickname": 1, curF: 1, bestF: 1}).
			SetSort(bson.D{{Key: sortField, Value: -1}, {Key: "_id", Value: 1}}).
			SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	rows := make([]GameRankRow, 0, limit)
	i := 0
	for cur.Next(ctx) {
		var row struct {
			ID       int64  `bson:"_id"`
			Uname    string `bson:"uname"`
			Nickname string `bson:"nickname"`
			Val      int    `bson:"comboCur"`
			Streak   int    `bson:"streakDays"`
			Best     int    `bson:"comboBest"`
			SBest    int    `bson:"streakBest"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		i++
		gr := GameRankRow{Rank: i, UID: row.ID, Username: row.Uname, Nickname: row.Nickname}
		switch {
		case kind == "streak":
			gr.Value, gr.Best = row.Streak, row.SBest
		case by == "cur":
			gr.Value, gr.Best = row.Val, row.Best
		default:
			gr.Value, gr.Best = row.Best, row.Val
		}
		if gr.Nickname == "" {
			gr.Nickname = gr.Username
		}
		rows = append(rows, gr)
	}
	_ = total
	return rows, cur.Err()
}

// GameBoardRank 榜外观察者名次（比我高的人数 +1）；0 = 不在榜。by 口径同 GameBoard。
func (r *Repo) GameBoardRank(ctx context.Context, kind, by string, uid int64) (int, int) {
	field := "comboBest"
	if kind == "streak" {
		field = "streakDays"
	} else if by == "cur" {
		field = "comboCur"
	}
	var u struct {
		Val int `bson:"comboCur"`
		St  int `bson:"streakDays"`
		Bst int `bson:"comboBest"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": uid},
		options.FindOne().SetProjection(bson.M{"comboCur": 1, "streakDays": 1, "comboBest": 1})).Decode(&u); err != nil {
		return 0, 0
	}
	val := u.Bst
	if kind == "streak" {
		val = u.St
	} else if by == "cur" {
		val = u.Val
	}
	if val <= 0 {
		return 0, val
	}
	n, err := r.users.CountDocuments(ctx, bson.M{field: bson.M{"$gt": val}})
	if err != nil {
		return 0, val
	}
	return int(n) + 1, val
}

// CountDailySolved 今日该题的完成人数（jadeAwards 自 dayStart 起，从众软钩）。
func (r *Repo) CountDailySolved(ctx context.Context, pid int64, dayStart time.Time) int64 {
	n, _ := r.jadeAwards.CountDocuments(ctx, bson.M{"pid": pid, "at": bson.M{"$gte": dayStart}})
	return n
}

// RawAchievements 个人成就解锁记录（端点层与定义表 join）。
func (r *Repo) RawAchievements(ctx context.Context, uid int64) ([]Achievement, error) {
	cur, err := r.achievements.Find(ctx, bson.M{"uid": uid}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var rows []Achievement
	return rows, cur.All(ctx, &rows)
}

// GetDailyProblemMeta 当日每日一题 + 题目元数据（标题/难度；无可用题返回 nil, nil, nil）。
func (r *Repo) GetDailyProblemMeta(ctx context.Context) (*DailyProblem, *Problem, error) {
	dp, err := r.EnsureDailyProblem(ctx)
	if err != nil || dp == nil {
		return nil, nil, err
	}
	p, err := r.GetProblemMeta(ctx, dp.PID)
	if err != nil {
		return dp, nil, nil
	}
	return dp, p, nil
}
