package repo

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 比赛约束（2026-10-15 定稿：时长与隐藏期上限均 7 天）
const (
	MaxContestDuration = 7 * 24 * time.Hour
	MaxHiddenDays      = 7
)

// 比赛与考试共用同一套规则实现，仅作用域与存储集合不同：
// 比赛（contest）= 主题库级别，contests 集合，root 创建、全体登录用户参赛；
// 考试（exam）= 班级级别，exams 集合，班级管理员创建、仅本班成员参赛。
const (
	KindContest = "contest"
	KindExam    = "exam"
)

// Contest 比赛（主题库）。
type Contest struct {
	ID          int64            `bson:"_id"`
	ClassID     int64            `bson:"classId"` // 恒为 0（历史字段，班级赛事已拆分为 Exam）
	Title       string           `bson:"title"`
	Intro       string           `bson:"intro"`
	Start       time.Time        `bson:"start"`
	End         time.Time        `bson:"end"`
	HiddenUntil time.Time        `bson:"hiddenUntil"` // 零值 = 无赛后隐藏期
	Problems    []ContestProblem `bson:"problems"`
	CreatedBy   int64            `bson:"createdBy"`
	Published   bool             `bson:"published"` // 隐藏期结束，提交/榜单已并入公开态
	FileIO      bool             `bson:"fileIo,omitempty"` // 本场启用文件 IO：场内所有题按各题文件名对以文件读写方式判题（v0.5.2-3）
	CreatedAt   time.Time        `bson:"createdAt"`
}

// Exam 班级考试：与比赛同构（规则见 DEVELOPMENT.md「比赛」节）。
type Exam = Contest

// ContestProblem 比赛引用的题目（Score 为该题满分，默认 100）。
type ContestProblem struct {
	PID   int64 `bson:"pid"`
	Score int   `bson:"score"`
}

// Status 比赛状态：scheduled / running / ended。
func (c *Contest) Status(now time.Time) string {
	switch {
	case now.Before(c.Start):
		return "scheduled"
	case now.Before(c.End):
		return "running"
	default:
		return "ended"
	}
}

// HiddenEnd 隐藏期结束时间（无隐藏期即比赛结束时刻）。
func (c *Contest) HiddenEnd() time.Time {
	if c.HiddenUntil.IsZero() {
		return c.End
	}
	return c.HiddenUntil
}

// roundsCol 按类型返回比赛/考试的集合。
func (r *Repo) roundsCol(kind string) *mongo.Collection {
	if kind == KindExam {
		return r.db.Collection("exams")
	}
	return r.db.Collection("contests")
}

// roundsSeq 比赛与考试独立取号（contests/exams 各自的 _id 序列）。
func roundsSeq(kind string) string {
	if kind == KindExam {
		return "exam"
	}
	return "contest"
}

// ---------- 比赛/考试同构 CRUD（v0.5.4 ① 合并：handler 侧差异见 server/round.go） ----------

// CreateRound 创建比赛或考试（按 kind 入对应集合并独立取号）。
func (r *Repo) CreateRound(ctx context.Context, kind string, c *Contest) error {
	var err error
	c.ID, err = r.nextSeq(ctx, roundsSeq(kind))
	if err != nil {
		return err
	}
	c.CreatedAt = time.Now()
	_, err = r.roundsCol(kind).InsertOne(ctx, c)
	return err
}

// GetRound 按 kind 取比赛/考试。
func (r *Repo) GetRound(ctx context.Context, kind string, id int64) (*Contest, error) {
	var c Contest
	if err := r.roundsCol(kind).FindOne(ctx, bson.M{"_id": id}).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteRound 仅允许删除未开始的比赛/考试（调用方校验权限与状态）。
func (r *Repo) DeleteRound(ctx context.Context, kind string, id int64) error {
	res, err := r.roundsCol(kind).DeleteOne(ctx,
		bson.M{"_id": id, "start": bson.M{"$gt": time.Now()}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SetRoundIntro 更新比赛/考试说明（Markdown）。
func (r *Repo) SetRoundIntro(ctx context.Context, kind string, id int64, intro string) error {
	res, err := r.roundsCol(kind).UpdateOne(ctx,
		bson.M{"_id": id}, bson.M{"$set": bson.M{"intro": intro}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SetRoundFileIO 设置「本场启用文件 IO」场景开关（创建者/root；中途可改，后续提交按新设置判题）。
func (r *Repo) SetRoundFileIO(ctx context.Context, kind string, id int64, enabled bool) error {
	res, err := r.roundsCol(kind).UpdateOne(ctx,
		bson.M{"_id": id}, bson.M{"$set": bson.M{"fileIo": enabled}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SetContestIntro/SetContestFileIO/SetExamIntro/SetExamFileIO/CreateContest/GetContest/
// DeleteContest/CreateExam/GetExam/DeleteExam 等 kind 双份薄包装已并入上方 Round 系列
// （v0.5.4 ①，调用方全部改走 kind 参数化方法）。

// ListClassExams 某班级的考试列表（按开始时间倒序）。
func (r *Repo) ListClassExams(ctx context.Context, classID int64) ([]*Exam, error) {
	return listRounds(ctx, r.db.Collection("exams"), bson.M{"classId": classID})
}

// ListUnpublishedExams 未发布的考试（锁题判定用，量小）。
func (r *Repo) ListUnpublishedExams(ctx context.Context) ([]*Exam, error) {
	return listRounds(ctx, r.db.Collection("exams"), bson.M{"published": false})
}

// roundsIDField 比赛/考试提交在 submissions 中的引用字段名。
func roundsIDField(kind string) string {
	if kind == KindExam {
		return "examId"
	}
	return "contestId"
}

// ListContests 主题库比赛列表。
func (r *Repo) ListContests(ctx context.Context) ([]*Contest, error) {
	cursor, err := r.db.Collection("contests").Find(ctx,
		bson.M{"classId": 0},
		options.Find().SetSort(bson.D{{Key: "start", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Contest, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ListUnpublishedContests 未发布的主题库比赛（锁题判定用，量小）。
func (r *Repo) ListUnpublishedContests(ctx context.Context) ([]*Contest, error) {
	return listRounds(ctx, r.db.Collection("contests"), bson.M{"published": false})
}

func listRounds(ctx context.Context, col *mongo.Collection, filter bson.M) ([]*Contest, error) {
	cursor, err := col.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Contest, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// PublishDueRounds 把隐藏期已结束的比赛与考试标记为已发布，并将其提交并入公开态。
// 返回本次发布的场数。由主循环周期性调用。
func (r *Repo) PublishDueRounds(ctx context.Context, now time.Time) (int, error) {
	published := 0
	for _, kind := range []string{KindContest, KindExam} {
		col := r.roundsCol(kind)
		idField := roundsIDField(kind)
		var rounds []*Contest
		var err error
		if rounds, err = listRounds(ctx, col, bson.M{"published": false}); err != nil {
			return published, err
		}
		for _, c := range rounds {
			if now.Before(c.HiddenEnd()) {
				continue
			}
			if _, err := col.UpdateOne(ctx,
				bson.M{"_id": c.ID, "published": false},
				bson.M{"$set": bson.M{"published": true}}); err != nil {
				return published, err
			}
			if _, err := r.Submissions.UpdateMany(ctx,
				bson.M{idField: c.ID},
				bson.M{"$set": bson.M{"published": true}}); err != nil {
				return published, err
			}
			published++
		}
	}
	return published, nil
}

// LeaderCell 榜单单元（某人在某题的最好成绩与提交次数）。
type LeaderCell struct {
	Score int
	Tries int
}

// LeaderRow 榜单一行。LastScoring 为最后一次得分提交时间（并列排名依据：早者靠前）。
type LeaderRow struct {
	UID         int64
	Username    string
	Nickname    string
	Total       int
	LastScoring time.Time
	HasScoring  bool
	Problems    map[int64]*LeaderCell
}

// RoundSubmissions 某场比赛/考试的全部提交（榜单计算/组织者查看/选手赛后查看自己的）。
func (r *Repo) RoundSubmissions(ctx context.Context, kind string, roundID int64, uid int64) ([]*Submission, error) {
	filter := bson.M{roundsIDField(kind): roundID}
	if uid > 0 {
		filter["uid"] = uid
	}
	cursor, err := r.Submissions.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Submission, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// RoundUserProblemSubmissions 指定用户在某轮次（比赛/考试）某题的全部提交（含代码与测试点明细），
// 供榜单/进度表的「查看用户提交明细」弹窗使用（门槛由 handler 校验）。
func (r *Repo) RoundUserProblemSubmissions(ctx context.Context, kind string, roundID, uid, pid int64) ([]*Submission, error) {
	filter := bson.M{
		roundsIDField(kind): roundID,
		"uid":               uid,
		"pid":               pid,
	}
	cursor, err := r.Submissions.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]*Submission, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// RoundLeaderboard 计算比赛/考试榜单。
// 排名规则：总分降序 → 最后得分提交时间早者靠前 → 并列。
func (r *Repo) RoundLeaderboard(ctx context.Context, kind string, roundID int64) ([]*LeaderRow, error) {
	cursor, err := r.Submissions.Find(ctx,
		bson.M{roundsIDField(kind): roundID},
		options.Find().SetProjection(bson.M{
			"uid": 1, "uname": 1, "pid": 1, "score": 1, "judgedAt": 1,
		}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var subs []struct {
		UID      int64      `bson:"uid"`
		Username string     `bson:"uname"`
		PID      int64      `bson:"pid"`
		Score    int        `bson:"score"`
		JudgedAt *time.Time `bson:"judgedAt"`
	}
	if err := cursor.All(ctx, &subs); err != nil {
		return nil, err
	}

	rows := map[int64]*LeaderRow{}
	for _, s := range subs {
		row := rows[s.UID]
		if row == nil {
			row = &LeaderRow{UID: s.UID, Username: s.Username, Problems: map[int64]*LeaderCell{}}
			rows[s.UID] = row
		}
		cell := row.Problems[s.PID]
		if cell == nil {
			cell = &LeaderCell{}
			row.Problems[s.PID] = cell
		}
		cell.Tries++
		if s.Score > cell.Score {
			cell.Score = s.Score
		}
		if s.Score > 0 && s.JudgedAt != nil && s.JudgedAt.After(row.LastScoring) {
			row.LastScoring = *s.JudgedAt
			row.HasScoring = true
		}
	}

	// 用户昵称补齐
	out := make([]*LeaderRow, 0, len(rows))
	uids := make([]int64, 0, len(rows))
	for uid := range rows {
		uids = append(uids, uid)
	}
	users, err := r.GetUsersByIDs(ctx, uids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		total := 0
		for _, cell := range row.Problems {
			total += cell.Score
		}
		row.Total = total
		if u := users[row.UID]; u != nil {
			row.Username, row.Nickname = u.Username, u.Nickname
		}
		out = append(out, row)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		// 都没得过分的并列排后面
		ti, tj := out[i].LastScoring, out[j].LastScoring
		if !out[i].HasScoring {
			ti = time.Time{}
		}
		if !out[j].HasScoring {
			tj = time.Time{}
		}
		if out[i].HasScoring != out[j].HasScoring {
			return out[i].HasScoring
		}
		if !out[i].HasScoring {
			return out[i].UID < out[j].UID
		}
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i].UID < out[j].UID
	})
	return out, nil
}

// ContestProblemScore 比赛中某题的满分配置。
func (c *Contest) ProblemScore(pid int64) (int, bool) {
	for _, p := range c.Problems {
		if p.PID == pid {
			if p.Score <= 0 {
				return 100, true
			}
			return p.Score, true
		}
	}
	return 0, false
}
