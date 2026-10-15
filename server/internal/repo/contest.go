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

// Contest 比赛。ClassID=0 表示主题库比赛（root 创建、全体登录用户可参赛）；
// 否则为班级比赛（建班人创建、仅本班成员参赛）。
type Contest struct {
	ID          int64            `bson:"_id"`
	ClassID     int64            `bson:"classId"` // 0 = 主题库比赛
	Title       string           `bson:"title"`
	Intro       string           `bson:"intro"`
	Start       time.Time        `bson:"start"`
	End         time.Time        `bson:"end"`
	HiddenUntil time.Time        `bson:"hiddenUntil"` // 零值 = 无赛后隐藏期
	Problems    []ContestProblem `bson:"problems"`
	CreatedBy   int64            `bson:"createdBy"`
	Published   bool             `bson:"published"` // 隐藏期结束，提交/榜单已并入公开态
	CreatedAt   time.Time        `bson:"createdAt"`
}

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

func (r *Repo) CreateContest(ctx context.Context, c *Contest) error {
	var err error
	c.ID, err = r.nextSeq(ctx, "contest")
	if err != nil {
		return err
	}
	c.CreatedAt = time.Now()
	_, err = r.db.Collection("contests").InsertOne(ctx, c)
	return err
}

func (r *Repo) GetContest(ctx context.Context, id int64) (*Contest, error) {
	var c Contest
	if err := r.db.Collection("contests").FindOne(ctx, bson.M{"_id": id}).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteContest 仅允许删除未开始的比赛（调用方校验权限与状态）。
func (r *Repo) DeleteContest(ctx context.Context, id int64) error {
	res, err := r.db.Collection("contests").DeleteOne(ctx,
		bson.M{"_id": id, "start": bson.M{"$gt": time.Now()}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// ListContests 主题库比赛 + 指定班级的比赛（classIDs 为空则仅主题库比赛）。
func (r *Repo) ListContests(ctx context.Context, classIDs []int64) ([]*Contest, error) {
	classFilter := bson.A{int64(0)}
	for _, id := range classIDs {
		classFilter = append(classFilter, id)
	}
	cursor, err := r.db.Collection("contests").Find(ctx,
		bson.M{"classId": bson.M{"$in": classFilter}},
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

// ListUnpublishedContests 未发布的比赛（锁题判定用，量小）。
func (r *Repo) ListUnpublishedContests(ctx context.Context) ([]*Contest, error) {
	cursor, err := r.db.Collection("contests").Find(ctx, bson.M{"published": false})
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

// PublishDueContests 把隐藏期已结束的比赛标记为已发布，并将其比赛提交并入公开态。
// 返回本次发布的比赛数。由主循环周期性调用。
func (r *Repo) PublishDueContests(ctx context.Context, now time.Time) (int, error) {
	published := 0
	cursor, err := r.db.Collection("contests").Find(ctx, bson.M{"published": false})
	if err != nil {
		return 0, err
	}
	var contests []*Contest
	if err = cursor.All(ctx, &contests); err != nil {
		cursor.Close(ctx)
		return 0, err
	}
	cursor.Close(ctx)

	for _, c := range contests {
		if now.Before(c.HiddenEnd()) {
			continue
		}
		if _, err := r.db.Collection("contests").UpdateOne(ctx,
			bson.M{"_id": c.ID, "published": false},
			bson.M{"$set": bson.M{"published": true}}); err != nil {
			return published, err
		}
		if _, err := r.Submissions.UpdateMany(ctx,
			bson.M{"contestId": c.ID},
			bson.M{"$set": bson.M{"published": true}}); err != nil {
			return published, err
		}
		published++
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

// ContestLeaderboard 计算比赛榜单（比赛规模小，内存计算足够）。
// 排名规则：总分降序 → 最后得分提交时间早者靠前 → 并列。
func (r *Repo) ContestLeaderboard(ctx context.Context, c *Contest) ([]*LeaderRow, error) {
	cursor, err := r.Submissions.Find(ctx,
		bson.M{"contestId": c.ID},
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
