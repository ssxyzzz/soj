package repo

// 判题队列 v0.7.1 单测：优先级领取排序、租约回收、取消两阶段协议与回写竞态。
// 依赖本地 MongoDB（docker compose up -d mongo），不可达时整文件跳过。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// openTestRepo 连独立测试库（每次唯一名，结束即删），Mongo 不可达时跳过。
func openTestRepo(t *testing.T) *Repo {
	t.Helper()
	uri := os.Getenv("SOJ_TEST_MONGO_URI")
	if uri == "" {
		// compose 的 Mongo 是单节点副本集：成员发现会切到容器内主机名 mongo:27017，
		// 宿主机跑测试必须直连不走发现
		uri = "mongodb://127.0.0.1:27017/?directConnection=true"
	}
	cli, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("连接 MongoDB 失败，跳过: %v", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cli.Ping(pingCtx, nil); err != nil {
		t.Skipf("本地 MongoDB 不可达（docker compose up -d mongo 可启用），跳过: %v", err)
	}
	dbName := fmt.Sprintf("soj_queue_test_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		_ = cli.Database(dbName).Drop(dctx)
		_ = cli.Disconnect(dctx)
	})
	r, err := Connect(context.Background(), uri, dbName)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return r
}

// insert 直接落一条提交文档（绕过 CreateSubmission，精确控制队列态字段）。
func insert(t *testing.T, r *Repo, doc bson.M) {
	t.Helper()
	base := bson.M{"uid": int64(1), "uname": "t", "pid": int64(1), "ptitle": "p",
		"lang": "cpp", "code": "", "score": 0, "timeMs": 0, "memKb": 0,
		"priority": PriorityHigh, "createdAt": time.Now()}
	for k, v := range doc {
		base[k] = v
	}
	if _, err := r.Submissions.InsertOne(context.Background(), base); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func getSub(t *testing.T, r *Repo, id int64) *Submission {
	t.Helper()
	s, err := r.GetSubmission(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSubmission #%d: %v", id, err)
	}
	return s
}

func TestClaimPriorityOrder(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	insert(t, r, bson.M{"_id": int64(1), "status": StatusPending, "priority": PriorityLow})
	insert(t, r, bson.M{"_id": int64(2), "status": StatusPending, "priority": PriorityHigh})
	insert(t, r, bson.M{"_id": int64(3), "status": StatusPending, "priority": PriorityMid})
	// 同档 FIFO：两条高档，_id 小者先
	insert(t, r, bson.M{"_id": int64(4), "status": StatusPending, "priority": PriorityHigh})
	insert(t, r, bson.M{"_id": int64(5), "status": StatusAccepted, "priority": PriorityHigh})

	want := []int64{2, 4, 3, 1}
	for _, id := range want {
		s, err := r.ClaimSubmission(ctx, "w1")
		if err != nil || s == nil {
			t.Fatalf("ClaimSubmission 期望领到 #%d，得到 (%v, %v)", id, s, err)
		}
		if s.ID != id {
			t.Fatalf("领取顺序错误：期望 #%d 实际 #%d（优先级降序、同档先来先服务）", id, s.ID)
		}
		if s.Judge != "w1" || s.HeartbeatAt == nil {
			t.Fatalf("领取应写入 judge 与 heartbeatAt，得到 judge=%q heartbeat=%v", s.Judge, s.HeartbeatAt)
		}
	}
	if s, _ := r.ClaimSubmission(ctx, "w1"); s != nil {
		t.Fatalf("队列应已空（Accepted 不可领取），却领到 #%d", s.ID)
	}
}

func TestReclaimExpiredLeases(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	stale := time.Now().Add(-200 * time.Second)
	fresh := time.Now()
	insert(t, r, bson.M{"_id": int64(1), "status": StatusJudging, "judge": "w1", "heartbeatAt": stale, "score": 30})
	insert(t, r, bson.M{"_id": int64(2), "status": StatusJudging, "judge": "w2", "heartbeatAt": fresh})
	insert(t, r, bson.M{"_id": int64(3), "status": StatusCancelRequested, "judge": "w3", "heartbeatAt": stale, "score": 30})
	insert(t, r, bson.M{"_id": int64(4), "status": StatusCancelRequested, "judge": "w4", "heartbeatAt": fresh})

	requeued, cancelled, err := r.ReclaimExpiredLeases(ctx, time.Now().Add(-LeaseStaleAfter))
	if err != nil {
		t.Fatalf("ReclaimExpiredLeases: %v", err)
	}
	if requeued != 1 || cancelled != 1 {
		t.Fatalf("期望 (requeued=1, cancelled=1)，实际 (%d, %d)", requeued, cancelled)
	}
	s1 := getSub(t, r, 1)
	if s1.Status != StatusPending || s1.Judge != "" || s1.HeartbeatAt != nil {
		t.Fatalf("失联 Judging 应重排队并清租约字段：status=%q judge=%q hb=%v", s1.Status, s1.Judge, s1.HeartbeatAt)
	}
	s2 := getSub(t, r, 2)
	if s2.Status != StatusJudging || s2.Judge != "w2" {
		t.Fatalf("心跳正常的 Judging 不应被动：status=%q judge=%q", s2.Status, s2.Judge)
	}
	s3 := getSub(t, r, 3)
	if s3.Status != StatusCancelled || s3.Score != 0 || s3.JudgedAt == nil {
		t.Fatalf("失联 CancelRequested 应直接终态 Cancelled：status=%q score=%d", s3.Status, s3.Score)
	}
	s4 := getSub(t, r, 4)
	if s4.Status != StatusCancelRequested {
		t.Fatalf("心跳正常的 CancelRequested 不应被动：status=%q", s4.Status)
	}
}

func TestCancelProtocol(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	t.Run("Pending 直接终态", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(10), "status": StatusPending,
			"score": 100, "timeMs": 50, "detail": bson.A{bson.M{"index": 1, "status": "AC"}}})
		st, err := r.RequestCancelSubmission(ctx, 10)
		if err != nil || st != StatusCancelled {
			t.Fatalf("期望直接 Cancelled，得到 (%q, %v)", st, err)
		}
		s := getSub(t, r, 10)
		if s.Score != 0 || s.TimeMS != 0 || s.Detail != nil || s.JudgedAt == nil {
			t.Fatalf("Pending 取消应清上一轮结果：score=%d time=%d detail=%v judged=%v",
				s.Score, s.TimeMS, s.Detail, s.JudgedAt)
		}
		// 重复取消 → 已终态
		if _, err := r.RequestCancelSubmission(ctx, 10); !errors.Is(err, ErrNotCancellable) {
			t.Fatalf("终态再取消应返回 ErrNotCancellable，得到 %v", err)
		}
	})

	t.Run("Judging 两阶段", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(11), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now()})
		st, err := r.RequestCancelSubmission(ctx, 11)
		if err != nil || st != StatusCancelRequested {
			t.Fatalf("期望 CancelRequested，得到 (%q, %v)", st, err)
		}
		// 判题循环见标记写终态（仍持租约 → 回写成功）
		if err := r.FinishSubmission(ctx, 11, "w1", StatusCancelled, 10, 20, 0, "已取消评测", nil); err != nil {
			t.Fatalf("持租约的取消回写应成功: %v", err)
		}
		if s := getSub(t, r, 11); s.Status != StatusCancelled {
			t.Fatalf("期望 Cancelled，实际 %q", s.Status)
		}
		// 重复请求（幂等窗口已过后）：终态 → 拒绝
		if _, err := r.RequestCancelSubmission(ctx, 11); !errors.Is(err, ErrNotCancellable) {
			t.Fatalf("终态再取消应返回 ErrNotCancellable，得到 %v", err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		if _, err := r.RequestCancelSubmission(ctx, 999); !errors.Is(err, mongo.ErrNoDocuments) {
			t.Fatalf("期望 ErrNoDocuments，得到 %v", err)
		}
	})
}

func TestFinishLeaseGuard(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	t.Run("租约被回收后旧判题器回写失效", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(20), "status": StatusJudging, "judge": "w1",
			"heartbeatAt": time.Now().Add(-200 * time.Second)})
		// 回收：重排队并清 judge
		if _, _, err := r.ReclaimExpiredLeases(ctx, time.Now().Add(-LeaseStaleAfter)); err != nil {
			t.Fatalf("回收: %v", err)
		}
		// 迟到的旧判题器回写 → 拒
		err := r.FinishSubmission(ctx, 20, "w1", StatusAccepted, 1, 2, 100, "", nil)
		if !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("旧判题器回写应 ErrLeaseLost，得到 %v", err)
		}
		if s := getSub(t, r, 20); s.Status != StatusPending {
			t.Fatalf("迟到回写不应生效，status=%q", s.Status)
		}
		// 新判题器领取并正常回写 → 成功
		s, _ := r.ClaimSubmission(ctx, "w2")
		if s == nil || s.ID != 20 {
			t.Fatalf("重排队的 #%d 应可被领取", 20)
		}
		if err := r.FinishSubmission(ctx, 20, "w2", StatusAccepted, 1, 2, 100, "", nil); err != nil {
			t.Fatalf("新判题器回写应成功: %v", err)
		}
	})

	t.Run("提交被他人重新领取后旧回写失效", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(21), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now()})
		if err := r.FinishSubmission(ctx, 21, "w0", StatusAccepted, 0, 0, 100, "", nil); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("非持租约者回写应 ErrLeaseLost，得到 %v", err)
		}
	})
}

func TestRejudgePriorities(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	t.Run("单条重测中优先级且取消中不可重测", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(30), "status": StatusAccepted})
		if err := r.RejudgeSubmission(ctx, 30); err != nil {
			t.Fatalf("RejudgeSubmission: %v", err)
		}
		if s := getSub(t, r, 30); s.Status != StatusPending || s.Priority != PriorityMid {
			t.Fatalf("单条重测应置 Pending/中优先级：status=%q priority=%d", s.Status, s.Priority)
		}
		insert(t, r, bson.M{"_id": int64(31), "status": StatusCancelRequested, "judge": "w1"})
		if err := r.RejudgeSubmission(ctx, 31); !errors.Is(err, ErrJudging) {
			t.Fatalf("取消收尾中的提交应 ErrJudging，得到 %v", err)
		}
	})

	t.Run("按题批量重测低优先级且不动在途", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(32), "pid": int64(9), "status": StatusAccepted})
		insert(t, r, bson.M{"_id": int64(33), "pid": int64(9), "status": StatusWrongAnswer})
		insert(t, r, bson.M{"_id": int64(34), "pid": int64(9), "status": StatusPending, "priority": PriorityHigh})
		insert(t, r, bson.M{"_id": int64(35), "pid": int64(9), "status": StatusJudging, "judge": "w1"})
		insert(t, r, bson.M{"_id": int64(36), "pid": int64(8), "status": StatusAccepted}) // 别的题
		n, err := r.RejudgeProblem(ctx, 9)
		if err != nil {
			t.Fatalf("RejudgeProblem: %v", err)
		}
		if n != 2 {
			t.Fatalf("期望重置 2 条（在途与别题不动），实际 %d", n)
		}
		if s := getSub(t, r, 32); s.Status != StatusPending || s.Priority != PriorityLow {
			t.Fatalf("批量重测应置 Pending/低优先级：status=%q priority=%d", s.Status, s.Priority)
		}
		if s := getSub(t, r, 34); s.Priority != PriorityHigh {
			t.Fatalf("在途 Pending 的优先级不应被覆盖: %d", s.Priority)
		}
		if s := getSub(t, r, 36); s.Status != StatusAccepted {
			t.Fatalf("别题不应被动: %q", s.Status)
		}
	})

	t.Run("按赛批量重测区分比赛/考试", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(40), "contestId": int64(7), "status": StatusAccepted})
		insert(t, r, bson.M{"_id": int64(41), "examId": int64(7), "status": StatusAccepted})
		n, err := r.RejudgeRound(ctx, KindExam, 7)
		if err != nil {
			t.Fatalf("RejudgeRound: %v", err)
		}
		if n != 1 {
			t.Fatalf("按考试 7 重测应只命中 examId=7，实际 %d", n)
		}
		if s := getSub(t, r, 40); s.Status != StatusAccepted || s.Priority != PriorityHigh {
			t.Fatalf("比赛提交不应被动: status=%q priority=%d", s.Status, s.Priority)
		}
	})
}

// TestListSubmissionsFilter v0.19 提交筛选：status/language/keyword（数字=提交号、文字=题名）。
func TestListSubmissionsFilter(t *testing.T) {
	r := openTestRepo(t)
	insert(t, r, bson.M{"_id": int64(101), "uid": int64(7), "status": "Accepted", "lang": "cpp", "ptitle": "两个数的和"})
	insert(t, r, bson.M{"_id": int64(102), "uid": int64(8), "status": "Wrong Answer", "lang": "python3", "ptitle": "三个数的和"})
	insert(t, r, bson.M{"_id": int64(103), "uid": int64(8), "status": "Accepted", "lang": "cpp", "ptitle": "植树"})

	cases := []struct {
		name   string
		f      SubmissionFilter
		wantID []int64
	}{
		{"无过滤", SubmissionFilter{}, []int64{103, 102, 101}},
		{"状态", SubmissionFilter{Status: "Accepted"}, []int64{103, 101}},
		{"语言", SubmissionFilter{Language: "python3"}, []int64{102}},
		{"用户", SubmissionFilter{UID: 8}, []int64{103, 102}},
		{"组合", SubmissionFilter{UID: 8, Status: "Accepted"}, []int64{103}},
		{"题名子串", SubmissionFilter{Keyword: "数的和"}, []int64{102, 101}},
		{"提交号", SubmissionFilter{Keyword: "102"}, []int64{102}},
		{"空结果", SubmissionFilter{Keyword: "不存在"}, []int64{}},
	}
	for _, tc := range cases {
		list, total, err := r.ListSubmissions(context.Background(), tc.f, 1, 20)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if total != int64(len(tc.wantID)) {
			t.Errorf("%s: total = %d, want %d", tc.name, total, len(tc.wantID))
		}
		got := make([]int64, 0, len(list))
		for _, s := range list {
			got = append(got, s.ID)
		}
		if len(got) != len(tc.wantID) {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.wantID)
		}
		for i := range got {
			if got[i] != tc.wantID[i] {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.wantID)
				break
			}
		}
	}
}

// TestMyProblemStats v0.19 我的做题统计：AC 去重题数 / 提交数 / AC 率 / 七天分布。
func TestMyProblemStats(t *testing.T) {
	r := openTestRepo(t)
	now := time.Now()
	// 题目两条（一条隐藏不计入 total）
	if _, err := r.Problems.InsertOne(context.Background(), bson.M{"_id": int64(1), "title": "a", "hidden": false}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Problems.InsertOne(context.Background(), bson.M{"_id": int64(2), "title": "b", "hidden": false}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Problems.InsertOne(context.Background(), bson.M{"_id": int64(3), "title": "c", "hidden": true}); err != nil {
		t.Fatal(err)
	}
	// 提交：题1 两条 AC（去重 1 题）+ 一条 WA；题2 一条 AC；另加一条 8 天前的 AC（不进七天窗）
	insert(t, r, bson.M{"_id": int64(201), "uid": int64(9), "pid": int64(1), "status": "Accepted"})
	insert(t, r, bson.M{"_id": int64(202), "uid": int64(9), "pid": int64(1), "status": "Accepted"})
	insert(t, r, bson.M{"_id": int64(203), "uid": int64(9), "pid": int64(1), "status": "Wrong Answer"})
	insert(t, r, bson.M{"_id": int64(204), "uid": int64(9), "pid": int64(2), "status": "Accepted"})
	insert(t, r, bson.M{"_id": int64(205), "uid": int64(9), "pid": int64(1), "status": "Accepted", "createdAt": now.AddDate(0, 0, -8)})

	st, err := r.MyProblemStats(context.Background(), 9, true)
	if err != nil {
		t.Fatalf("MyProblemStats: %v", err)
	}
	if st.Solved != 2 {
		t.Errorf("Solved = %d, want 2", st.Solved)
	}
	// 全量可见视角（root/老师）：隐藏题计入分母，与题库列表口径一致
	if st.Total != 3 {
		t.Errorf("Total(includeHidden) = %d, want 3", st.Total)
	}
	// 学生视角：隐藏题不计入分母
	st2, err := r.MyProblemStats(context.Background(), 9, false)
	if err != nil {
		t.Fatalf("MyProblemStats(student): %v", err)
	}
	if st2.Total != 2 {
		t.Errorf("Total(student) = %d, want 2（隐藏题不计）", st2.Total)
	}
	if st2.Solved != 2 {
		t.Errorf("Solved = %d, want 2", st2.Solved)
	}
	if st.Submissions != 5 {
		t.Errorf("Submissions = %d, want 5", st.Submissions)
	}
	if st.ACRate != 80 {
		t.Errorf("ACRate = %v, want 80", st.ACRate)
	}
	// 七天窗内 4 条（8 天前那条不计），全部落在今天或近六天之一
	var sum int64
	for _, n := range st.Recent7 {
		sum += n
	}
	if sum != 4 {
		t.Errorf("Recent7 合计 = %d, want 4（8 天前不计入）", sum)
	}
	if len(st.Recent7) != 7 {
		t.Errorf("Recent7 长度 = %d, want 7", len(st.Recent7))
	}
}

// RankMe 名次计算：与榜单同序（solved desc、uid asc 决胜）；无 AC 用户 = 全榜人数 + 1。
func TestRankMe(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	// 榜单序：11 与 12 同分 2 题（uid 决胜 11 先），13 一题，14 无 AC
	for _, s := range []struct{ uid, pid int64 }{
		{11, 1}, {11, 2},
		{12, 1}, {12, 2},
		{13, 1},
	} {
		insert(t, r, bson.M{"uid": s.uid, "pid": s.pid, "status": StatusAccepted, "published": true})
	}
	cases := []struct {
		uid    int64
		rank   int64
		solved int64
	}{
		{11, 1, 2},
		{12, 2, 2},
		{13, 3, 1},
		{14, 4, 0},
	}
	for _, c := range cases {
		rank, solved, err := r.RankMe(ctx, c.uid)
		if err != nil {
			t.Fatalf("RankMe(uid=%d): %v", c.uid, err)
		}
		if rank != c.rank || solved != c.solved {
			t.Errorf("RankMe(uid=%d) = (%d, %d), want (%d, %d)", c.uid, rank, solved, c.rank, c.solved)
		}
	}
}
