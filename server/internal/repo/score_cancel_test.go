package repo

// 取消成绩/恢复成绩（v0.97.29）单测：归档可逆契约 + 重测拦截 + 批量重测跳过 +
// XP 聚合剔除。依赖本地 MongoDB（docker compose up -d mongo），不可达时整文件跳过。

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// seedSub 落一条提交并回传其 _id（insert 不回值，测试统一显式 _id）。
func seedSub(t *testing.T, r *Repo, doc bson.M) int64 {
	t.Helper()
	insert(t, r, doc)
	return doc["_id"].(int64)
}

func TestCancelScoreRoundTrip(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	sid := seedSub(t, r, bson.M{"_id": int64(4201),
		"status": StatusAccepted, "score": 100, "timeMs": 42, "memKb": 2048,
		"detail":    bson.A{bson.M{"index": 1, "status": "AC", "timeMs": 42, "memoryKb": 2048}},
		"published": true})

	// 取消：状态翻 Cancelled、得分归零，原始判题结果整体归档，操作人/原因落档
	if err := r.CancelScore(ctx, sid, "与网络题解代码雷同", "root"); err != nil {
		t.Fatalf("CancelScore: %v", err)
	}
	s, err := r.GetSubmission(ctx, sid)
	if err != nil {
		t.Fatalf("GetSubmission: %v", err)
	}
	if s.Status != StatusCancelled || s.Score != 0 {
		t.Fatalf("取消后 status=%s score=%d, want Cancelled/0", s.Status, s.Score)
	}
	if !s.ScoreCancelled || s.ScoreCancelledBy != "root" || s.ScoreCancelledReason != "与网络题解代码雷同" {
		t.Fatalf("取消标记/操作人/原因未落档: %+v", s)
	}
	if s.OriginStatus != StatusAccepted || s.OriginScore != 100 || s.OriginTimeMS != 42 || s.OriginMemKB != 2048 ||
		len(s.OriginDetail) != 1 {
		t.Fatalf("归档字段不完整: origin=%s/%d/%d/%d detail=%d",
			s.OriginStatus, s.OriginScore, s.OriginTimeMS, s.OriginMemKB, len(s.OriginDetail))
	}

	// 取消成绩后重测拒绝（防变相恢复），恢复成绩后重测放行
	if err := r.RejudgeSubmission(ctx, sid); !errors.Is(err, ErrScoreCancelled) {
		t.Fatalf("已取消成绩的重测应拒绝, got %v", err)
	}
	if err := r.RestoreScore(ctx, sid); err != nil {
		t.Fatalf("RestoreScore: %v", err)
	}
	s, _ = r.GetSubmission(ctx, sid)
	if s.Status != StatusAccepted || s.Score != 100 || s.TimeMS != 42 || s.MemKB != 2048 || len(s.Detail) != 1 {
		t.Fatalf("恢复后未原样写回: status=%s score=%d time=%d mem=%d detail=%d",
			s.Status, s.Score, s.TimeMS, s.MemKB, len(s.Detail))
	}
	if s.ScoreCancelled || s.OriginStatus != "" {
		t.Fatalf("恢复后取消标记/归档字段未清除: cancelled=%v originStatus=%q", s.ScoreCancelled, s.OriginStatus)
	}
	if err := r.RejudgeSubmission(ctx, sid); err != nil {
		t.Fatalf("恢复后重测应放行, got %v", err)
	}
}

func TestCancelScoreGuards(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 队列态无成绩可取消
	pending := seedSub(t, r, bson.M{"_id": int64(4202), "status": StatusPending})
	if err := r.CancelScore(ctx, pending, "", "root"); !errors.Is(err, ErrNoScore) {
		t.Fatalf("Pending 应 ErrNoScore, got %v", err)
	}
	// 已自取消评测（终态 Cancelled）本就无成绩
	selfCancel := seedSub(t, r, bson.M{"_id": int64(4203), "status": StatusCancelled, "score": 0})
	if err := r.CancelScore(ctx, selfCancel, "", "root"); !errors.Is(err, ErrNoScore) {
		t.Fatalf("自取消应 ErrNoScore, got %v", err)
	}
	// 正常提交取消后再次取消 → ErrNoScore
	sid := seedSub(t, r, bson.M{"_id": int64(4204), "status": StatusAccepted, "score": 100})
	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("首次取消: %v", err)
	}
	if err := r.CancelScore(ctx, sid, "", "root"); !errors.Is(err, ErrNoScore) {
		t.Fatalf("重复取消应 ErrNoScore, got %v", err)
	}
	// 未取消的提交恢复 → ErrNoScore
	normal := seedSub(t, r, bson.M{"_id": int64(4205), "status": StatusWrongAnswer, "score": 40})
	if err := r.RestoreScore(ctx, normal); !errors.Is(err, ErrNoScore) {
		t.Fatalf("未取消提交恢复应 ErrNoScore, got %v", err)
	}
}

func TestRejudgeProblemSkipsCancelled(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const pid = int64(910)

	normal := seedSub(t, r, bson.M{"_id": int64(4206), "pid": pid, "status": StatusAccepted, "score": 100, "published": true})
	cancelled := seedSub(t, r, bson.M{"_id": int64(4207), "pid": pid, "status": StatusAccepted, "score": 100, "published": true})
	if err := r.CancelScore(ctx, cancelled, "抄袭", "root"); err != nil {
		t.Fatalf("CancelScore: %v", err)
	}

	n, err := r.RejudgeProblem(ctx, pid)
	if err != nil {
		t.Fatalf("RejudgeProblem: %v", err)
	}
	if n != 1 {
		t.Fatalf("批量重测应只重置 1 条（跳过已取消成绩）, got %d", n)
	}
	sn, _ := r.GetSubmission(ctx, normal)
	sc, _ := r.GetSubmission(ctx, cancelled)
	if sn.Status != StatusPending {
		t.Fatalf("正常提交应被重置 Pending, got %s", sn.Status)
	}
	if sc.Status != StatusCancelled || !sc.ScoreCancelled || sc.Score != 0 {
		t.Fatalf("已取消成绩的提交不应被批量重测触碰: status=%s cancelled=%v score=%d",
			sc.Status, sc.ScoreCancelled, sc.Score)
	}
}

// TestCancelScoreXpExclusion 钉「取消成绩 → 等级经验自动回退」：XP 聚合按
// status==Accepted 过滤，取消即剔除、恢复即回归（v0.97.27 纯聚合口径的纪律侧闭环）。
func TestCancelScoreXpExclusion(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const uid, pid, diff = int64(4242), int64(911), 6

	if _, err := r.problems.InsertOne(ctx, bson.M{"_id": pid, "difficulty": diff}); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	sid := seedSub(t, r, bson.M{"_id": int64(4208), "uid": uid, "pid": pid,
		"status": StatusAccepted, "score": 100, "published": true})

	counts := func() [10]int64 {
		m, err := r.SolvedDifficultyCounts(ctx, []int64{uid})
		if err != nil {
			t.Fatalf("SolvedDifficultyCounts: %v", err)
		}
		return m[uid]
	}
	if c := counts(); c[diff-1] != 1 {
		t.Fatalf("取消前 D%d 题数应为 1, got %v", diff, c)
	}
	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("CancelScore: %v", err)
	}
	if c := counts(); c[diff-1] != 0 {
		t.Fatalf("取消后 XP 聚合应剔除该 AC, got %v", c)
	}
	if err := r.RestoreScore(ctx, sid); err != nil {
		t.Fatalf("RestoreScore: %v", err)
	}
	if c := counts(); c[diff-1] != 1 {
		t.Fatalf("恢复后 XP 聚合应回归, got %v", c)
	}
}

// ---- 取消成绩追赃（v0.97.29 拍板：玉+计数型成就随成绩回退，恢复反向补偿，允许负余额） ----

// seedJade 种首 AC 发放记录与余额（绕过 MaybeAwardJade，精确控制金额/标记）。
func seedJade(t *testing.T, r *Repo, uid, pid, total, bal int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.jadeAwards.InsertOne(ctx, bson.M{"uid": uid, "pid": pid, "base": total, "total": total, "at": time.Now()}); err != nil {
		t.Fatalf("seed award: %v", err)
	}
	if _, err := r.jadeBalances.UpdateOne(ctx, bson.M{"_id": uid},
		bson.M{"$set": bson.M{"bal": bal}}, options.UpdateOne().SetUpsert(true)); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
}

// seedAch 种成就解锁记录（显式 _id）。
func seedAch(t *testing.T, r *Repo, id, uid int64, code string) {
	t.Helper()
	if _, err := r.achievements.InsertOne(context.Background(),
		bson.M{"_id": id, "uid": uid, "code": code, "at": time.Now()}); err != nil {
		t.Fatalf("seed achievement %s: %v", code, err)
	}
}

// achOwned 成就是否在持。
func achOwned(ctx context.Context, t *testing.T, r *Repo, uid int64, code string) bool {
	t.Helper()
	n, err := r.achievements.CountDocuments(ctx, bson.M{"uid": uid, "code": code})
	if err != nil {
		t.Fatalf("count achievement %s: %v", code, err)
	}
	return n > 0
}

// ledgerKinds 流水 kind 计数。
func ledgerKinds(ctx context.Context, t *testing.T, r *Repo, uid int64) map[string]int {
	t.Helper()
	entries, _, err := r.JadeLedgerPage(ctx, uid, 1, 50)
	if err != nil {
		t.Fatalf("ledger page: %v", err)
	}
	m := map[string]int{}
	for _, e := range entries {
		m[e.Kind]++
	}
	return m
}

// TestScoreCancelClawbackRoundTrip 取消唯一 AC → 玉追回+计数型成就撤销（事件型保留）；
// 恢复 → 补回+重授；取消/恢复反复操作幂等（余额最终一致）。
func TestScoreCancelClawbackRoundTrip(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const uid, pid, diff = int64(4301), int64(920), 5
	if _, err := r.problems.InsertOne(ctx, bson.M{"_id": pid, "difficulty": diff}); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	sid := seedSub(t, r, bson.M{"_id": int64(4301), "uid": uid, "pid": pid,
		"status": StatusAccepted, "score": 100, "published": true})
	seedJade(t, r, uid, pid, 9, 20)
	seedAch(t, r, 1, uid, "first_blood")
	seedAch(t, r, 2, uid, "diff_5")
	seedAch(t, r, 3, uid, "combo_5")

	bal := func() int64 {
		b, err := r.JadeBalance(ctx, uid)
		if err != nil {
			t.Fatalf("balance: %v", err)
		}
		return b
	}
	expectBal := func(want int64, stage string) {
		t.Helper()
		if got := bal(); got != want {
			t.Fatalf("%s: 余额 want %d got %d", stage, want, got)
		}
	}

	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	expectBal(11, "取消后")
	a, _ := r.GetJadeAward(ctx, uid, pid)
	if a == nil || !a.ClawedBack {
		t.Fatalf("取消后 awards 应打 clawedBack 标记: %+v", a)
	}
	if !achOwned(ctx, t, r, uid, "combo_5") {
		t.Fatal("事件型成就 combo_5 不应被撤销")
	}
	if achOwned(ctx, t, r, uid, "first_blood") || achOwned(ctx, t, r, uid, "diff_5") {
		t.Fatal("计数型成就 first_blood/diff_5 应被撤销")
	}
	if k := ledgerKinds(ctx, t, r, uid); k["score_clawback"] != 1 {
		t.Fatalf("应恰有一条追回流水, got %v", k)
	}

	if err := r.RestoreScore(ctx, sid); err != nil {
		t.Fatalf("restore: %v", err)
	}
	expectBal(20, "恢复后")
	a, _ = r.GetJadeAward(ctx, uid, pid)
	if a == nil || a.ClawedBack {
		t.Fatalf("恢复后 clawedBack 应清除: %+v", a)
	}
	if !achOwned(ctx, t, r, uid, "first_blood") || !achOwned(ctx, t, r, uid, "diff_5") {
		t.Fatal("恢复后计数型成就应重授")
	}
	if k := ledgerKinds(ctx, t, r, uid); k["score_regrant"] != 1 {
		t.Fatalf("应恰有一条补回流水, got %v", k)
	}

	// 取消→恢复→取消→恢复：clawedBack 幂等标记兜底，余额最终一致
	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("再取消: %v", err)
	}
	if err := r.RestoreScore(ctx, sid); err != nil {
		t.Fatalf("再恢复: %v", err)
	}
	expectBal(20, "反复操作后")
}

// TestScoreCancelMultiACNoClawback 多次 AC 只取消一条：题目仍算通过，玉/成就都不动。
func TestScoreCancelMultiACNoClawback(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const uid, pid, diff = int64(4302), int64(921), 3
	if _, err := r.problems.InsertOne(ctx, bson.M{"_id": pid, "difficulty": diff}); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	seedSub(t, r, bson.M{"_id": int64(4302), "uid": uid, "pid": pid,
		"status": StatusAccepted, "score": 100, "published": true})
	sid2 := seedSub(t, r, bson.M{"_id": int64(4303), "uid": uid, "pid": pid,
		"status": StatusAccepted, "score": 100, "published": true})
	seedJade(t, r, uid, pid, 9, 20)
	seedAch(t, r, 11, uid, "first_blood")

	if err := r.CancelScore(ctx, sid2, "", "root"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if b, _ := r.JadeBalance(ctx, uid); b != 20 {
		t.Fatalf("仍有有效 AC 不应追玉, 余额 want 20 got %d", b)
	}
	a, _ := r.GetJadeAward(ctx, uid, pid)
	if a == nil || a.ClawedBack {
		t.Fatalf("不应打 clawedBack 标记: %+v", a)
	}
	if !achOwned(ctx, t, r, uid, "first_blood") {
		t.Fatal("仍有有效 AC 不应撤销成就")
	}
}

// TestScoreCancelClawbackNegativeBalance 余额不足追回时透支为负（拍板允许负余额）。
func TestScoreCancelClawbackNegativeBalance(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const uid, pid = int64(4304), int64(922)
	if _, err := r.problems.InsertOne(ctx, bson.M{"_id": pid, "difficulty": 1}); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	sid := seedSub(t, r, bson.M{"_id": int64(4304), "uid": uid, "pid": pid,
		"status": StatusAccepted, "score": 100, "published": true})
	seedJade(t, r, uid, pid, 9, 3)

	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if b, _ := r.JadeBalance(ctx, uid); b != -6 {
		t.Fatalf("余额应透支为 -6, got %d", b)
	}
}

// TestScoreCancelLedgerReason 追回/补回流水说明带「#题号《题名》」（v0.98.1，与首 AC 发放
// 流水同款写法）；题目已删除时题名取不到 → 退化为仅题号，账照记。
func TestScoreCancelLedgerReason(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	const uid, pid = int64(4305), int64(923)
	if _, err := r.problems.InsertOne(ctx,
		bson.M{"_id": pid, "difficulty": 5, "title": "追回测试题"}); err != nil {
		t.Fatalf("seed problem: %v", err)
	}
	sid := seedSub(t, r, bson.M{"_id": int64(4305), "uid": uid, "pid": pid,
		"status": StatusAccepted, "score": 100, "published": true})
	seedJade(t, r, uid, pid, 9, 20)

	// lastReason 最近一条该 kind 流水的说明（JadeLedgerPage 新→旧）
	lastReason := func(kind string) string {
		t.Helper()
		entries, _, err := r.JadeLedgerPage(ctx, uid, 1, 50)
		if err != nil {
			t.Fatalf("ledger page: %v", err)
		}
		for _, e := range entries {
			if e.Kind == kind {
				return e.Reason
			}
		}
		t.Fatalf("流水缺少 %s", kind)
		return ""
	}
	expectReason := func(kind, want, stage string) {
		t.Helper()
		if got := lastReason(kind); got != want {
			t.Fatalf("%s流水说明 = %q, want %q", stage, got, want)
		}
	}
	expectBal := func(want int64, stage string) {
		t.Helper()
		if b, _ := r.JadeBalance(ctx, uid); b != want {
			t.Fatalf("%s: 余额 want %d got %d", stage, want, b)
		}
	}

	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	expectReason("score_clawback", "成绩取消追回 #923《追回测试题》首 AC 玉", "取消后追回")
	expectBal(11, "取消后")
	if err := r.RestoreScore(ctx, sid); err != nil {
		t.Fatalf("restore: %v", err)
	}
	expectReason("score_regrant", "成绩恢复补回 #923《追回测试题》首 AC 玉", "恢复后补回")
	expectBal(20, "恢复后")

	// 题目删除后再取消/恢复：题名取不到 → 仅题号，照常入账
	if _, err := r.problems.DeleteOne(ctx, bson.M{"_id": pid}); err != nil {
		t.Fatalf("delete problem: %v", err)
	}
	if err := r.CancelScore(ctx, sid, "", "root"); err != nil {
		t.Fatalf("cancel after delete: %v", err)
	}
	expectReason("score_clawback", "成绩取消追回 #923 首 AC 玉", "题目删除后追回")
	expectBal(11, "题目删除后取消")
	if err := r.RestoreScore(ctx, sid); err != nil {
		t.Fatalf("restore after delete: %v", err)
	}
	expectReason("score_regrant", "成绩恢复补回 #923 首 AC 玉", "题目删除后补回")
	expectBal(20, "题目删除后恢复")
}
