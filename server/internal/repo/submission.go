package repo

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 判题状态（submissions.status 存储这些字符串）
const (
	StatusPending         = "Pending"
	StatusJudging         = "Judging"
	StatusCancelRequested = "CancelRequested" // 取消评测两阶段协议的中间态：判题循环见标记即停
	StatusCancelled       = "Cancelled"       // 取消评测终态：得分 0，不加分不进榜
	StatusAccepted        = "Accepted"
	StatusWrongAnswer     = "Wrong Answer"
	StatusCompileError    = "Compile Error"
	StatusTLE             = "Time Limit Exceeded"
	StatusMLE             = "Memory Limit Exceeded"
	StatusRE              = "Runtime Error"
	StatusSystemError     = "System Error"
)

// 队列优先级三档（v0.7.1 内置不对外暴露）：学生新提交 > 单条重测 > 批量重测。
const (
	PriorityHigh = 10 // 学生新提交
	PriorityMid  = 5  // 单条重测
	PriorityLow  = 1  // 批量重测（按题/按赛）
)

// 租约参数：判题循环每 5s 心跳一次，Web 进程每 30s 扫描，
// Judging 且心跳超过 120s 视为判题器失联，回收重排队。
const (
	HeartbeatInterval = 5 * time.Second
	LeaseStaleAfter   = 120 * time.Second
	ReclaimScanEvery  = 30 * time.Second
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
	ID               int64             `bson:"_id"`
	UID              int64             `bson:"uid"`
	Username         string            `bson:"uname"` // 冗余存储，列表免 JOIN
	ProblemID        int64             `bson:"pid"`
	ProblemTitle     string            `bson:"ptitle"` // 冗余存储
	ContestID        int64             `bson:"contestId"`
	ContestTitle     string            `bson:"contestTitle,omitempty"`
	ExamID           int64             `bson:"examId,omitempty"`
	ExamTitle        string            `bson:"examTitle,omitempty"`
	Published        bool              `bson:"published,omitempty"` // 比赛/考试提交：隐藏期结束并入公开态
	Language         string            `bson:"lang"`
	Code             string            `bson:"code"`
	ObjectiveAnswers []ObjectiveAnswer `bson:"objAnswers,omitempty"` // 客观题整卷作答（v0.9.3）
	// 旧单题作答字段：仅迁移读取用（objQuestionsV1 后 $unset，不再写入）
	Answer      []int         `bson:"answer,omitempty"`
	Blanks      []string      `bson:"blanks,omitempty"`
	Status      string        `bson:"status"`
	Score       int           `bson:"score"`
	TimeMS      int           `bson:"timeMs"`
	MemKB       int           `bson:"memKb"`
	ErrorInfo   string        `bson:"errorInfo,omitempty"`
	Detail      []*CaseResult `bson:"detail,omitempty"`
	Judge       string        `bson:"judge,omitempty"`
	ClaimedAt   time.Time     `bson:"claimedAt,omitempty"`
	HeartbeatAt *time.Time    `bson:"heartbeatAt,omitempty"` // 租约心跳（v0.7.1）：判题中周期刷新，超时由回收者重排队
	Priority    int           `bson:"priority"`              // 队列优先级：领取按其降序（v0.7.1）
	ChallengeID int64         `bson:"challengeId,omitempty"` // 挑战模式会话 ID（v0.23；0 = 普通提交）
	CreatedAt   time.Time     `bson:"createdAt"`
	JudgedAt    *time.Time    `bson:"judgedAt,omitempty"`
}

// CreateSubmission 创建提交；contest/exam 非 nil 表示比赛或考试提交（隐藏期结束前不计入公开态）。
// challengeID 非 0 表示挑战模式提交（AC 后由 judge 管道触发挑战终判）。
func (r *Repo) CreateSubmission(ctx context.Context, uid int64, uname string, p *Problem, lang, code string, contest *Contest, exam *Exam, challengeID int64) (int64, error) {
	sid, err := r.nextSeq(ctx, "submission")
	if err != nil {
		return 0, err
	}
	doc := bson.M{
		"_id": sid, "uid": uid, "uname": uname,
		"pid": p.ID, "ptitle": p.Title,
		"lang": lang, "code": code, "status": StatusPending,
		"score": 0, "timeMs": 0, "memKb": 0, "priority": PriorityHigh, "createdAt": time.Now(),
	}
	if challengeID != 0 {
		doc["challengeId"] = challengeID
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

// CreateObjectiveSubmission 客观题提交（v0.9）：判分是纯函数，不走沙箱、不进队列——
// 提交即写终态。contest/exam 语义与代码提交一致（隐藏期结束前不计入公开态）。
func (r *Repo) CreateObjectiveSubmission(ctx context.Context, uid int64, uname string, p *Problem, answers []ObjectiveAnswer, contest *Contest, exam *Exam) (int64, error) {
	sid, err := r.nextSeq(ctx, "submission")
	if err != nil {
		return 0, err
	}
	status, score, detail := GradeObjectiveSet(p.Questions, answers)
	now := time.Now()
	doc := bson.M{
		"_id": sid, "uid": uid, "uname": uname,
		"pid": p.ID, "ptitle": p.Title,
		"lang": "", "code": "",
		"status": status, "score": score, "judgedAt": now,
		"timeMs": 0, "memKb": 0, "priority": PriorityHigh, "createdAt": now,
	}
	doc["objAnswers"] = answers
	doc["detail"] = detail
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

// RegradeObjective 客观题重测：按存储的作答对当前题目定义重判（题目答案被改后重测生效）。
// 与代码提交的 Pending 重排队不同，这里同步写终态。
func (r *Repo) RegradeObjective(ctx context.Context, id int64) error {
	var s Submission
	if err := r.Submissions.FindOne(ctx, bson.M{"_id": id}).Decode(&s); err != nil {
		return err
	}
	p, err := r.GetProblem(ctx, s.ProblemID)
	if err != nil {
		return err
	}
	status, score, detail := GradeObjectiveSet(p.Questions, s.ObjectiveAnswers)
	_, err = r.Submissions.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"status": status, "score": score, "detail": detail, "judgedAt": time.Now(),
	}})
	return err
}

// ClaimSubmission 原子领取一条待判提交：Pending -> Judging。
// 数据库原子性保证多个判题线程/进程不会领到同一份提交。
// 领取按优先级降序（学生新提交 > 单条重测 > 批量重测），同档按先来先服务。
// 无待判任务时返回 (nil, nil)。
func (r *Repo) ClaimSubmission(ctx context.Context, judgeName string) (*Submission, error) {
	now := time.Now()
	var s Submission
	err := r.Submissions.FindOneAndUpdate(ctx,
		bson.M{"status": StatusPending},
		bson.M{"$set": bson.M{"status": StatusJudging, "judge": judgeName, "claimedAt": now, "heartbeatAt": now}},
		options.FindOneAndUpdate().
			// bson.D 保序：多键排序必须显式有序（bson.M 键序随机）
			SetSort(bson.D{{Key: "priority", Value: -1}, {Key: "_id", Value: 1}}).
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

// ErrLeaseLost 租约已失效（提交被回收者重排队或已转入取消流程），当前判题器的回写被拒。
var ErrLeaseLost = errors.New("submission lease lost")

// FinishSubmission 回写最终结果（v0.7.1 起带租约条件：judge 必须仍是本判题器）。
// 提交被回收者重排队（或已被他人重新领取）后，旧判题器的迟到回写直接失效，防双判。
func (r *Repo) FinishSubmission(ctx context.Context, sid int64, judgeName, status string, timeMS, memKB, score int, errorInfo string, detail []*CaseResult) error {
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
	res, err := r.Submissions.UpdateOne(ctx,
		bson.M{"_id": sid, "judge": judgeName}, bson.M{"$set": set})
	if err == nil && res.MatchedCount == 0 {
		return ErrLeaseLost
	}
	return err
}

// HeartbeatSubmission 刷新判题租约心跳；仅当提交仍由该判题器持有时生效。
func (r *Repo) HeartbeatSubmission(ctx context.Context, sid int64, judgeName string) error {
	_, err := r.Submissions.UpdateOne(ctx,
		bson.M{"_id": sid, "status": StatusJudging, "judge": judgeName},
		bson.M{"$set": bson.M{"heartbeatAt": time.Now()}})
	return err
}

// SubmissionLease 查询提交当前状态与租约归属（判题循环在测试点之间做取消/回收检查）：
// held = judge 字段仍是本判题器（false 表示租约已被回收者收走，应放弃本次判题）。
func (r *Repo) SubmissionLease(ctx context.Context, sid int64, judgeName string) (status string, held bool, err error) {
	var row struct {
		Status string `bson:"status"`
		Judge  string `bson:"judge"`
	}
	if err := r.Submissions.FindOne(ctx, bson.M{"_id": sid},
		options.FindOne().SetProjection(bson.M{"status": 1, "judge": 1})).Decode(&row); err != nil {
		return "", false, err
	}
	return row.Status, row.Judge == judgeName, nil
}

// ReclaimExpiredLeases 回收心跳超时的租约（Web 进程周期调用）：
//   - Judging 失联 -> 重置 Pending 重新排队（重置 judge/claimedAt/heartbeatAt）；
//   - CancelRequested 失联 -> 直接终态 Cancelled（用户已要求取消，不重排队）。
func (r *Repo) ReclaimExpiredLeases(ctx context.Context, staleBefore time.Time) (requeued, cancelled int64, err error) {
	stale := bson.M{"$or": bson.A{
		bson.M{"heartbeatAt": bson.M{"$lt": staleBefore}},
		bson.M{"heartbeatAt": bson.M{"$exists": false}}, // 升级前的存量 Judging（Bootstrap 已兜底重排队）
	}}
	unset := bson.M{"judge": "", "claimedAt": "", "heartbeatAt": ""}
	res, err := r.Submissions.UpdateMany(ctx,
		bson.M{"status": StatusCancelRequested, "$or": stale["$or"]},
		bson.M{"$set": bson.M{
			"status": StatusCancelled, "score": 0, "errorInfo": "已取消评测（判题器失联，由系统回收）",
			"judgedAt": time.Now(),
		}, "$unset": unset})
	if err != nil {
		return 0, 0, err
	}
	cancelled = res.ModifiedCount
	res, err = r.Submissions.UpdateMany(ctx,
		bson.M{"status": StatusJudging, "$or": stale["$or"]},
		bson.M{"$set": bson.M{"status": StatusPending}, "$unset": unset})
	if err != nil {
		return 0, cancelled, err
	}
	return res.ModifiedCount, cancelled, nil
}

// ErrNotCancellable 提交已出结果（终态），无法取消。
var ErrNotCancellable = errors.New("submission is not cancellable")

// RequestCancelSubmission 取消评测两阶段协议（本人 ∨ submission.view.all）：
//   - Pending：直接终态 Cancelled（清掉上一轮遗留的耗时/明细）；
//   - Judging / CancelRequested：置 CancelRequested，判题循环在测试点之间见标记即停
//     并写终态 Cancelled；若判题器失联则由回收者兜底置 Cancelled。
//
// 返回提交进入的状态（StatusCancelled / StatusCancelRequested）。
func (r *Repo) RequestCancelSubmission(ctx context.Context, sid int64) (string, error) {
	err := r.Submissions.FindOneAndUpdate(ctx,
		bson.M{"_id": sid, "status": StatusPending},
		bson.M{"$set": bson.M{
			"status": StatusCancelled, "score": 0, "timeMs": 0, "memKb": 0,
			"errorInfo": "已取消评测", "judgedAt": time.Now(),
		}, "$unset": bson.M{"detail": "", "judge": "", "claimedAt": "", "heartbeatAt": ""}},
	).Err()
	if err == nil {
		return StatusCancelled, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return "", err
	}
	err = r.Submissions.FindOneAndUpdate(ctx,
		bson.M{"_id": sid, "status": bson.M{"$in": bson.A{StatusJudging, StatusCancelRequested}}},
		bson.M{"$set": bson.M{"status": StatusCancelRequested}},
	).Err()
	if err == nil {
		return StatusCancelRequested, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return "", err
	}
	// 非队列态：区分「已出结果」与「不存在」
	if r.Submissions.FindOne(ctx, bson.M{"_id": sid}).Err() == nil {
		return "", ErrNotCancellable
	}
	return "", mongo.ErrNoDocuments
}

// ErrJudging 提交正在评测中，不可重测。
var ErrJudging = errors.New("submission is being judged")

// rejudgeUpdate 重测重置的写入口径：状态回 Pending（按档位给优先级）+ 清空上一次
// 评测结果（分数/耗时/内存/错误信息/测试点明细——重测中保留旧分会被「Judging + 旧分」
// 展示误导，v0.7.1 验收反馈）+ 清掉旧租约痕迹。
func rejudgeUpdate(priority int) bson.M {
	return bson.M{
		"$set": bson.M{
			"status": StatusPending, "priority": priority,
			"score": 0, "timeMs": 0, "memKb": 0, "errorInfo": "", "detail": bson.A{},
		},
		"$unset": bson.M{"judge": "", "claimedAt": "", "heartbeatAt": ""},
	}
}

// RejudgeSubmission 重测：把一条非评测中的提交重置为 Pending（中优先级），交还判题线程池自动领取重评。
// 评测中（含已请求取消、正在收尾）的提交返回 ErrJudging；不存在返回 mongo.ErrNoDocuments。
func (r *Repo) RejudgeSubmission(ctx context.Context, id int64) error {
	err := r.Submissions.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "status": bson.M{"$nin": bson.A{StatusPending, StatusJudging, StatusCancelRequested}}},
		rejudgeUpdate(PriorityMid),
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

// inFlight 队列态全集：批量重测只重置已出结果的提交，避免与在途评测打架。
var inFlight = bson.A{StatusPending, StatusJudging, StatusCancelRequested}

// RejudgeProblem 按题批量重测（低优先级）：本题全部已出结果的提交重置 Pending，
// 只在判题空闲容量里消化，不挤占学生新提交。返回重置条数。
func (r *Repo) RejudgeProblem(ctx context.Context, pid int64) (int64, error) {
	res, err := r.Submissions.UpdateMany(ctx,
		bson.M{"pid": pid, "status": bson.M{"$nin": inFlight}},
		rejudgeUpdate(PriorityLow))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// RejudgeRound 按赛批量重测（低优先级）：一场比赛/考试的全部提交重置 Pending。
// kind 取 KindContest / KindExam。返回重置条数。
func (r *Repo) RejudgeRound(ctx context.Context, kind string, rid int64) (int64, error) {
	field := "contestId"
	if kind == KindExam {
		field = "examId"
	}
	res, err := r.Submissions.UpdateMany(ctx,
		bson.M{field: rid, "status": bson.M{"$nin": inFlight}},
		rejudgeUpdate(PriorityLow))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
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
// SubmissionFilter 提交列表过滤（v0.19 提交记录页 v3 筛选 chips）：零值字段不过滤。
type SubmissionFilter struct {
	UID      int64
	PID      int64
	Status   string
	Language string
	Keyword  string
}

func (r *Repo) ListSubmissions(ctx context.Context, f SubmissionFilter, page, pageSize int) ([]*Submission, int64, error) {
	filter := bson.M{}
	if f.UID > 0 {
		filter["uid"] = f.UID
	}
	if f.PID > 0 {
		filter["pid"] = f.PID
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Language != "" {
		filter["lang"] = f.Language
	}
	if f.Keyword != "" {
		// 纯数字按提交号精确命中；否则按题名子串（不区分大小写）
		if n, err := strconv.ParseInt(f.Keyword, 10, 64); err == nil && n > 0 {
			filter["_id"] = n
		} else {
			filter["ptitle"] = bson.M{"$regex": regexp.QuoteMeta(f.Keyword), "$options": "i"}
		}
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

// MyProblemStats 我的做题统计（v0.19 题库副列 v3 卡）：AC 题数（按题去重）/ 可见题总数 /
// 提交数 / AC 率（提交条数口径）/ 最近 7 天提交分布（[6 天前 .. 今天]，本地时区）。
type MyProblemStats struct {
	Solved      int64   `json:"solved"`
	Total       int64   `json:"total"`
	Submissions int64   `json:"submissions"`
	ACRate      float64 `json:"ac_rate"`
	Recent7     []int64 `json:"recent7"`
}

func (r *Repo) MyProblemStats(ctx context.Context, uid int64, includeHidden bool) (*MyProblemStats, error) {
	st := &MyProblemStats{Recent7: make([]int64, 7)}

	// 可见题总数（2026-10-27 起按观看者视角：root/老师含隐藏题，与题库列表页头口径一致；
	// 学生不含隐藏题——隐藏题对其不存在）
	totalFilter := bson.M{}
	if !includeHidden {
		totalFilter["hidden"] = bson.M{"$ne": true}
	}
	total, err := r.Problems.CountDocuments(ctx, totalFilter)
	if err != nil {
		return nil, err
	}
	st.Total = total

	// 提交数与 AC 条数
	submissions, err := r.Submissions.CountDocuments(ctx, bson.M{"uid": uid})
	if err != nil {
		return nil, err
	}
	st.Submissions = submissions
	if submissions > 0 {
		ac, err := r.Submissions.CountDocuments(ctx, bson.M{"uid": uid, "status": StatusAccepted})
		if err != nil {
			return nil, err
		}
		st.ACRate = float64(ac) / float64(submissions) * 100
	}

	// AC 题数（uid + Accepted 按题去重）
	cur, err := r.Submissions.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"uid": uid, "status": StatusAccepted}}},
		{{Key: "$group", Value: bson.M{"_id": "$pid"}}},
		{{Key: "$count", Value: "n"}},
	})
	if err != nil {
		return nil, err
	}
	var cnt []struct {
		N int64 `bson:"n"`
	}
	if err := cur.All(ctx, &cnt); err != nil {
		return nil, err
	}
	if len(cnt) > 0 {
		st.Solved = cnt[0].N
	}

	// 最近 7 天提交分布：按本地日期字符串分组，再映射回 [6 天前 .. 今天] 下标
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -6)
	cur, err = r.Submissions.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"uid": uid, "createdAt": bson.M{"$gte": dayStart}}}},
		{{Key: "$group", Value: bson.M{
			"_id":   bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt"}},
			"count": bson.M{"$sum": 1},
		}}},
	})
	if err != nil {
		return nil, err
	}
	var byDay []struct {
		ID    string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	if err := cur.All(ctx, &byDay); err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(byDay))
	for _, d := range byDay {
		counts[d.ID] = d.Count
	}
	for i := 0; i < 7; i++ {
		day := dayStart.AddDate(0, 0, i)
		st.Recent7[i] = counts[day.Format("2006-01-02")]
	}
	return st, nil
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
		// 多键排序必须 bson.D：bson.M 是 Go map 键序随机，每次请求可能变成按 _id
		// 取最新提交而非最高分（题库状态列偶发「不是满分」的根因）。
		{"$sort": bson.D{{Key: "pid", Value: 1}, {Key: "score", Value: -1}, {Key: "_id", Value: -1}}},
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
		// bson.D 保序：bson.M 键序随机会让榜单偶尔按 uid 而非解题数排序。
		{{Key: "$sort", Value: bson.D{{Key: "solved", Value: -1}, {Key: "_id", Value: 1}}}},
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

// RankMe 观察者位次：与榜单完全同序（solved desc、uid asc 决胜）——
// rank = 比我靠前的人数 + 1，solved = 我的去重解题数。无任何 AC 时 solved=0、
// rank = 全榜人数 + 1（所有有 AC 的用户都排前面）。与 RankTop 同一聚合口径
// （status=accepted && published，按 (uid,pid) 去重计题）。
func (r *Repo) RankMe(ctx context.Context, uid int64) (rank, solved int64, err error) {
	// 我的解题数：AC 且已发布的提交按 pid 去重
	myPipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"uid": uid, "status": StatusAccepted, "published": true}}},
		{{Key: "$group", Value: bson.M{"_id": "$pid"}}},
		{{Key: "$count", Value: "n"}},
	}
	cur, err := r.Submissions.Aggregate(ctx, myPipe)
	if err != nil {
		return 0, 0, err
	}
	var mine []struct {
		N int64 `bson:"n"`
	}
	if err := cur.All(ctx, &mine); err != nil {
		return 0, 0, err
	}
	if len(mine) > 0 {
		solved = mine[0].N
	}
	// 靠前人数：解题更多，或同分但 uid 更小（与榜单排序键一致，同分不用并列名次）
	aheadPipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"status": StatusAccepted, "published": true}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"uid": "$uid", "pid": "$pid"}}}},
		{{Key: "$group", Value: bson.M{"_id": "$_id.uid", "solved": bson.M{"$sum": 1}}}},
		{{Key: "$match", Value: bson.M{"$or": bson.A{
			bson.M{"solved": bson.M{"$gt": solved}},
			bson.M{"solved": solved, "_id": bson.M{"$lt": uid}},
		}}}},
		{{Key: "$count", Value: "n"}},
	}
	cur2, err := r.Submissions.Aggregate(ctx, aheadPipe)
	if err != nil {
		return 0, 0, err
	}
	var ahead []struct {
		N int64 `bson:"n"`
	}
	if err := cur2.All(ctx, &ahead); err != nil {
		return 0, 0, err
	}
	rank = 1
	if len(ahead) > 0 {
		rank = ahead[0].N + 1
	}
	return rank, solved, nil
}

// BoardRow 题内效率榜行（v0.23）：Value = timeMs（用时榜）或 memKb（内存榜）。
type BoardRow struct {
	UID          int64
	Value        int
	Lang         string
	SubmissionID int64
	CreatedAt    time.Time // 该条最优提交的时刻（同值并列时的决胜与展示）
}

// ProblemEfficiencyBoard 题内用时/内存榜：Accepted∧published 每人取最优——
// 用时榜按 timeMs↑（同值 memKb↑、再按先达成），内存榜按 memKb↑（同值 timeMs↑、再按先达成）；
// Mongo 排序 + Go 侧按 uid 去重，截前 limit 名。班级规模毫秒级，无需预聚合。
func (r *Repo) ProblemEfficiencyBoard(ctx context.Context, pid int64, byMem bool, limit int) ([]BoardRow, error) {
	if limit <= 0 {
		limit = 50
	}
	sortKeys := bson.D{{Key: "timeMs", Value: 1}, {Key: "memKb", Value: 1}}
	if byMem {
		sortKeys = bson.D{{Key: "memKb", Value: 1}, {Key: "timeMs", Value: 1}}
	}
	sortKeys = append(sortKeys, bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}...)
	cur, err := r.Submissions.Find(ctx,
		bson.M{"pid": pid, "status": StatusAccepted, "published": true},
		options.Find().
			SetSort(sortKeys).
			SetProjection(bson.M{"uid": 1, "timeMs": 1, "memKb": 1, "lang": 1, "createdAt": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []BoardRow
	seen := map[int64]bool{}
	for cur.Next(ctx) {
		var s Submission
		if err := cur.Decode(&s); err != nil {
			return nil, err
		}
		if seen[s.UID] {
			continue
		}
		seen[s.UID] = true
		v := s.TimeMS
		if byMem {
			v = s.MemKB
		}
		rows = append(rows, BoardRow{UID: s.UID, Value: v, Lang: s.Language, SubmissionID: s.ID, CreatedAt: s.CreatedAt})
		if len(rows) >= limit {
			break
		}
	}
	return rows, cur.Err()
}
