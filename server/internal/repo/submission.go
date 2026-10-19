package repo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 判题状态（submissions.status 存储这些字符串）
const (
	StatusPending      = "Pending"
	StatusJudging      = "Judging"
	StatusAccepted     = "Accepted"
	StatusWrongAnswer  = "Wrong Answer"
	StatusCompileError = "Compile Error"
	StatusTLE          = "Time Limit Exceeded"
	StatusMLE          = "Memory Limit Exceeded"
	StatusRE           = "Runtime Error"
	StatusSystemError  = "System Error"
)

// CaseResult 单个测试点结果（状态缩写: AC/WA/TLE/MLE/RE/OLE/SE）。
type CaseResult struct {
	Index  int    `bson:"index"        json:"index"`
	Status string `bson:"status"       json:"status"`
	TimeMS int    `bson:"timeMs"       json:"time_ms"`
	MemKB  int    `bson:"memKb"        json:"memory_kb"`
	Msg    string `bson:"msg,omitempty" json:"msg,omitempty"` // 失败明细（如 超出时间限制 / 未生成输出文件 x.out），AC 为空
}

type Submission struct {
	ID           int64         `bson:"_id"`
	UID          int64         `bson:"uid"`
	Username     string        `bson:"uname"` // 冗余存储，列表免 JOIN
	ProblemID    int64         `bson:"pid"`
	ProblemTitle string        `bson:"ptitle"` // 冗余存储
	ContestID    int64         `bson:"contestId"`
	ContestTitle string        `bson:"contestTitle,omitempty"`
	ExamID       int64         `bson:"examId,omitempty"`
	ExamTitle    string        `bson:"examTitle,omitempty"`
	Published    bool          `bson:"published,omitempty"` // 比赛/考试提交：隐藏期结束并入公开态
	Language     string        `bson:"lang"`
	Code         string        `bson:"code"`
	Status       string        `bson:"status"`
	Score        int           `bson:"score"`
	TimeMS       int           `bson:"timeMs"`
	MemKB        int           `bson:"memKb"`
	ErrorInfo    string        `bson:"errorInfo,omitempty"`
	Detail       []*CaseResult `bson:"detail,omitempty"`
	Judge        string        `bson:"judge,omitempty"`
	ClaimedAt    time.Time     `bson:"claimedAt,omitempty"`
	CreatedAt    time.Time     `bson:"createdAt"`
	JudgedAt     *time.Time    `bson:"judgedAt,omitempty"`
}

// CreateSubmission 创建提交；contest/exam 非 nil 表示比赛或考试提交（隐藏期结束前不计入公开态）。
func (r *Repo) CreateSubmission(ctx context.Context, uid int64, uname string, p *Problem, lang, code string, contest *Contest, exam *Exam) (int64, error) {
	sid, err := r.nextSeq(ctx, "submission")
	if err != nil {
		return 0, err
	}
	doc := bson.M{
		"_id": sid, "uid": uid, "uname": uname,
		"pid": p.ID, "ptitle": p.Title,
		"lang": lang, "code": code, "status": StatusPending,
		"score": 0, "timeMs": 0, "memKb": 0, "createdAt": time.Now(),
	}
	switch {
	case contest != nil:
		doc["contestId"] = contest.ID
		doc["contestTitle"] = contest.Title
		doc["published"] = false
	case exam != nil:
		doc["examId"] = exam.ID
		doc["examTitle"] = exam.Title
		doc["published"] = false
	default:
		doc["contestId"] = 0
		doc["published"] = true
	}
	if _, err := r.Submissions.InsertOne(ctx, doc); err != nil {
		return 0, err
	}
	return sid, nil
}

// ClaimSubmission 原子领取一条待判提交：Pending -> Judging。
// 数据库原子性保证多个判题线程/进程不会领到同一份提交（见 DEVELOPMENT.md 判题调度）。
// 无待判任务时返回 (nil, nil)。
func (r *Repo) ClaimSubmission(ctx context.Context, judgeName string) (*Submission, error) {
	var s Submission
	err := r.Submissions.FindOneAndUpdate(ctx,
		bson.M{"status": StatusPending},
		bson.M{"$set": bson.M{"status": StatusJudging, "judge": judgeName, "claimedAt": time.Now()}},
		options.FindOneAndUpdate().
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetReturnDocument(options.After),
	).Decode(&s)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FinishSubmission 回写最终结果。
func (r *Repo) FinishSubmission(ctx context.Context, sid int64, status string, timeMS, memKB, score int, errorInfo string, detail []*CaseResult) error {
	now := time.Now()
	set := bson.M{
		"status": status, "timeMs": timeMS, "memKb": memKB,
		"score": score, "judgedAt": now,
	}
	if errorInfo != "" {
		set["errorInfo"] = errorInfo
	} else {
		set["errorInfo"] = ""
	}
	if detail != nil {
		set["detail"] = detail
	}
	_, err := r.Submissions.UpdateOne(ctx, bson.M{"_id": sid}, bson.M{"$set": set})
	return err
}

// ErrJudging 提交正在评测中，不可重测。
var ErrJudging = errors.New("submission is being judged")

// RejudgeSubmission 重测：把一条非评测中的提交重置为 Pending，交还判题线程池自动领取重评。
// 评测中的提交返回 ErrJudging；不存在返回 mongo.ErrNoDocuments。
func (r *Repo) RejudgeSubmission(ctx context.Context, id int64) error {
	err := r.Submissions.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "status": bson.M{"$nin": bson.A{StatusPending, StatusJudging}}},
		bson.M{"$set": bson.M{"status": StatusPending}},
	).Err()
	if err == nil {
		return nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		// 区分「不存在」与「评测中」
		if r.Submissions.FindOne(ctx, bson.M{"_id": id}).Err() == nil {
			return ErrJudging
		}
	}
	return err
}

func (r *Repo) GetSubmission(ctx context.Context, id int64) (*Submission, error) {
	var s Submission
	if err := r.Submissions.FindOne(ctx, bson.M{"_id": id}).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSubmissions 按可选条件（uid/pid，0 表示不过滤）分页查询，不含代码字段。
// 仅供提交记录总表使用：路由已按「查看全部提交」权限位把门，不再过滤隐藏期赛事提交
// （2026-10-16 定稿：隐藏期可见性并入权限位）。
func (r *Repo) ListSubmissions(ctx context.Context, uid, pid int64, page, pageSize int) ([]*Submission, int64, error) {
	filter := bson.M{}
	if uid > 0 {
		filter["uid"] = uid
	}
	if pid > 0 {
		filter["pid"] = pid
	}
	total, err := r.Submissions.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.Submissions.Find(ctx, filter,
		options.Find().
			SetProjection(bson.M{"code": 0}).
			SetSort(bson.D{{Key: "_id", Value: -1}}).
			SetLimit(int64(pageSize)).
			SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*Submission, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// HasSolved 用户是否在该题拿到过满分（代码可见性解锁判定）。
func (r *Repo) HasSolved(ctx context.Context, uid, pid int64) (bool, error) {
	n, err := r.Submissions.CountDocuments(ctx,
		bson.M{"uid": uid, "pid": pid, "score": 100})
	return n > 0, err
}

// UserBestStatus 该用户在某题的代表性提交：最高分那条的状态与得分（题库状态列，v0.5.6 升级）。
type UserBestStatus struct {
	Status string `bson:"status"`
	Score  int    `bson:"score"`
}

// UserBestStatuses 批量查某用户在一组题目的最高分提交状态（题库状态列，v0.5.6 升级）：
// 每题取 published 提交中得分最高的一条（并列取最新），未出现 = 无公开提交。
// 口径与通过率统计/总榜一致（published=true）：比赛/考试隐藏期内的提交不计，
// 防 OI 赛制下从状态列推测赛中判题结果。
func (r *Repo) UserBestStatuses(ctx context.Context, uid int64, pids []int64) (map[int64]UserBestStatus, error) {
	out := make(map[int64]UserBestStatus, len(pids))
	if uid <= 0 || len(pids) == 0 {
		return out, nil
	}
	cursor, err := r.Submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"uid": uid, "pid": bson.M{"$in": pids}, "published": true}},
		{"$sort": bson.M{"pid": 1, "score": -1, "_id": -1}},
		{"$group": bson.M{
			"_id":    "$pid",
			"status": bson.M{"$first": "$status"},
			"score":  bson.M{"$first": "$score"},
		}},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID     int64  `bson:"_id"`
			Status string `bson:"status"`
			Score  int    `bson:"score"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = UserBestStatus{Status: row.Status, Score: row.Score}
	}
	return out, cursor.Err()
}

// ListPidSubmissions 题目维提交列表（含代码，分页）。
// uid > 0 时只看该用户的（我的提交，最新在前）；uid = 0 为所有代码视图：
// sort 为空或 recent（默认，最近提交在前）/ time（耗时升序、并列按内存）/ mem（内存升序、并列按耗时），
// lang 非空时按语言过滤。published=true 过滤掉隐藏期内比赛/考试的提交。
func (r *Repo) ListPidSubmissions(ctx context.Context, pid int64, uid int64, page, pageSize int, sort, lang string) ([]*Submission, int64, error) {
	filter := bson.M{"pid": pid, "published": true}
	if uid > 0 {
		filter["uid"] = uid
	}
	if lang != "" {
		filter["lang"] = lang
	}
	total, err := r.Submissions.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	sortSpec := bson.D{{Key: "_id", Value: -1}} // 我的提交 / 所有代码默认：最近提交在前
	if uid == 0 && sort == "time" {
		sortSpec = bson.D{{Key: "timeMs", Value: 1}, {Key: "memKb", Value: 1}}
	} else if uid == 0 && sort == "mem" {
		sortSpec = bson.D{{Key: "memKb", Value: 1}, {Key: "timeMs", Value: 1}}
	}
	cursor, err := r.Submissions.Find(ctx, filter,
		options.Find().SetSort(sortSpec).
			SetLimit(int64(pageSize)).
			SetSkip(int64((page-1)*pageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	list := make([]*Submission, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ProblemStat 单题提交统计（题库通过率展示用）。
type ProblemStat struct {
	Total    int64
	Accepted int64
}

// ListProblemStats 聚合各题的提交数与 AC 数（仅公开态提交，与排行榜口径一致）。
func (r *Repo) ListProblemStats(ctx context.Context, pids []int64) (map[int64]ProblemStat, error) {
	out := make(map[int64]ProblemStat, len(pids))
	if len(pids) == 0 {
		return out, nil
	}
	cursor, err := r.Submissions.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"pid": bson.M{"$in": pids}, "published": true}},
		{"$group": bson.M{
			"_id":      "$pid",
			"total":    bson.M{"$sum": 1},
			"accepted": bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$status", StatusAccepted}}, 1, 0}}},
		}},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID       int64 `bson:"_id"`
			Total    int64 `bson:"total"`
			Accepted int64 `bson:"accepted"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = ProblemStat{Total: row.Total, Accepted: row.Accepted}
	}
	return out, cursor.Err()
}

// RankRow 排行榜一行。
type RankRow struct {
	UID      int64  `bson:"_id"`
	Solved   int64  `bson:"solved"`
	Username string `bson:"username"`
	Nickname string `bson:"nickname"`
}

// RankTop 解题数前 limit 名：先按 (uid,pid) 去重 Accepted，再统计聚合。
// 比赛提交在隐藏期结束（published）后才计入总榜，防赛中推测成绩。
func (r *Repo) RankTop(ctx context.Context, limit int) ([]RankRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"status": StatusAccepted, "published": true}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}}},
		{{Key: "$group", Value: bson.M{"_id": "$_id.uid", "solved": bson.M{"$sum": 1}}}},
		{{Key: "$sort", Value: bson.M{"solved": -1, "_id": 1}}},
		{{Key: "$limit", Value: limit}},
		{{Key: "$lookup", Value: bson.M{
			"from": "users", "localField": "_id", "foreignField": "_id", "as": "u",
		}}},
		{{Key: "$unwind", Value: bson.M{"path": "$u", "preserveNullAndEmptyArrays": true}}},
		{{Key: "$project", Value: bson.M{
			"solved":   1,
			"username": "$u.uname",
			"nickname": "$u.nickname",
		}}},
	}
	cursor, err := r.Submissions.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]RankRow, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
