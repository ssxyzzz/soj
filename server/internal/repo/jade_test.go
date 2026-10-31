package repo

// 玉地基单测（v0.33-①）：账本防双花、首 AC 幂等与客观题半额、奖池结算数学
//（名额比例/线性递减/并列段位均分/参与池滚入/取整零头）、发布链路集成。
// 依赖本地 MongoDB（openTestRepo 同款，不可达整文件跳过）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// jadeTestProblem 建一道题并返回 pid。
func jadeTestProblem(t *testing.T, r *Repo, objective bool, difficulty int) int64 {
	t.Helper()
	ctx := context.Background()
	pid, err := r.nextSeq(ctx, "problem")
	if err != nil {
		t.Fatalf("nextSeq problem: %v", err)
	}
	p := &Problem{ID: pid, Title: "T", Difficulty: difficulty,
		TimeLimitMS: 1000, MemoryLimitKB: 65536, Visibility: "public"}
	if objective {
		p.Type = TypeObjective
		p.Questions = []ObjectiveQuestion{{}} // IsObjective 要求非空小题集
	}
	if _, err := r.problems.InsertOne(ctx, p); err != nil {
		t.Fatalf("insert problem: %v", err)
	}
	return pid
}

// jadeTestRoundSub 直插一条赛内已判提交（published=false，测发布结算的输入面）。
func jadeTestRoundSub(t *testing.T, r *Repo, kind string, roundID, uid, pid int64, score int) {
	t.Helper()
	sid, err := r.nextSeq(context.Background(), "submission")
	if err != nil {
		t.Fatalf("nextSeq submission: %v", err)
	}
	doc := bson.M{
		"_id": sid, "uid": uid, "uname": "u", "pid": pid, "ptitle": "T",
		"lang": "cpp", "code": "", "status": StatusAccepted, "score": score,
		"published": false, "priority": PriorityHigh, "createdAt": time.Now(),
		"judgedAt": time.Now(),
	}
	if kind == KindExam {
		doc["examId"] = roundID
	} else {
		doc["contestId"] = roundID
	}
	if _, err := r.submissions.InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
}

func jadeBal(t *testing.T, r *Repo, uid int64) int64 {
	t.Helper()
	bal, err := r.JadeBalance(context.Background(), uid)
	if err != nil {
		t.Fatalf("JadeBalance: %v", err)
	}
	return bal
}

func TestJadeCreditSpend(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	if err := r.JadeCredit(ctx, 1, 10, JadeKindAdminAdjust, "测试入账", 0); err != nil {
		t.Fatalf("credit: %v", err)
	}
	if bal := jadeBal(t, r, 1); bal != 10 {
		t.Fatalf("余额 = %d，期望 10", bal)
	}
	if err := r.JadeSpend(ctx, 1, 7, JadeKindAdminAdjust, "测试扣减", 0); err != nil {
		t.Fatalf("spend: %v", err)
	}
	if bal := jadeBal(t, r, 1); bal != 3 {
		t.Fatalf("扣后余额 = %d，期望 3", bal)
	}
	if err := r.JadeSpend(ctx, 1, 4, JadeKindAdminAdjust, "超额扣减", 0); err != ErrInsufficientJade {
		t.Fatalf("超额扣减 err = %v，期望 ErrInsufficientJade", err)
	}
	if bal := jadeBal(t, r, 1); bal != 3 {
		t.Fatalf("拒付后余额 = %d，期望仍 3（不透支）", bal)
	}
	list, total, err := r.JadeLedgerPage(ctx, 1, 1, 10)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("流水 = %d 条（len=%d, err=%v），期望 2", total, len(list), err)
	}
	if list[0].Delta != -7 || list[1].Delta != 10 {
		t.Fatalf("流水顺序（新→旧）= %d,%d，期望 -7,10", list[0].Delta, list[1].Delta)
	}
}

func TestMaybeAwardJade(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	objPID := jadeTestProblem(t, r, true, 5)  // 客观题 D5：基础 max(1,⌊5/2⌋)=2
	trdPID := jadeTestProblem(t, r, false, 5) // 传统题 D5：基础 5、掉落 ∈{0,5}

	awarded, _, err := r.MaybeAwardJade(ctx, 7, objPID, 0, 0)
	if err != nil || !awarded {
		t.Fatalf("客观题首满分: awarded=%v err=%v", awarded, err)
	}
	a, _ := r.GetJadeAward(ctx, 7, objPID)
	if a.Base != 2 || (a.Drop != 0 && a.Drop != 5) || a.Total != a.Base+a.Drop {
		t.Fatalf("客观题发放 = base %d drop %d total %d，期望 base2 drop∈{0,5}", a.Base, a.Drop, a.Total)
	}
	// 幂等：同题第二次（重交/挑战/再比赛）不再发
	awarded2, total2, err := r.MaybeAwardJade(ctx, 7, objPID, 0, 0)
	if err != nil || awarded2 || total2 != 0 {
		t.Fatalf("重复发放: awarded=%v total=%d err=%v，期望 no-op", awarded2, total2, err)
	}
	if bal := jadeBal(t, r, 7); bal != a.Total {
		t.Fatalf("重复后余额 = %d，期望 %d 不变", bal, a.Total)
	}
	// 传统题另一用户：基础 5
	if _, _, err := r.MaybeAwardJade(ctx, 8, trdPID, 0, 0); err != nil {
		t.Fatalf("传统题发放: %v", err)
	}
	b, _ := r.GetJadeAward(ctx, 8, trdPID)
	if b.Base != 5 || (b.Drop != 0 && b.Drop != 5) {
		t.Fatalf("传统题发放 = base %d drop %d，期望 base5 drop∈{0,5}", b.Base, b.Drop)
	}
	// D1 客观题：⌊1/2⌋=0 → max(1,0)=1
	d1 := jadeTestProblem(t, r, true, 1)
	if _, _, err := r.MaybeAwardJade(ctx, 9, d1, 0, 0); err != nil {
		t.Fatalf("D1 客观题: %v", err)
	}
	c, _ := r.GetJadeAward(ctx, 9, d1)
	if c.Base != 1 {
		t.Fatalf("D1 客观题 base = %d，期望 1", c.Base)
	}
}

// jadeSettleExam 建一场已过隐藏期的考试 + 每用户单题得分，返回场次（含池配置）。
func jadeSettleExam(t *testing.T, r *Repo, scores map[int64]int, pool, partPool int64) *Contest {
	t.Helper()
	pid := jadeTestProblem(t, r, false, 1)
	ct := &Contest{Title: "月考", Start: time.Now().Add(-2 * time.Hour), End: time.Now().Add(-time.Hour),
		HiddenUntil: time.Now().Add(-time.Minute), Published: false,
		Problems: []ContestProblem{{PID: pid, Score: 100}},
		JadePool: pool, JadeParticipatePool: partPool}
	if err := r.CreateRound(context.Background(), KindExam, ct); err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	for uid, score := range scores {
		jadeTestRoundSub(t, r, KindExam, ct.ID, uid, pid, score)
	}
	return ct
}

func TestMaybeAwardJadeBlessing(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	pid := jadeTestProblem(t, r, false, 4) // 传统题 D4：基础 4、掉落 ∈{0,4}

	// 未祝福：base=4、倍率 100（无加成）
	if _, _, err := r.MaybeAwardJade(ctx, 21, pid, 0, 0); err != nil {
		t.Fatalf("未祝福发放: %v", err)
	}
	a, _ := r.GetJadeAward(ctx, 21, pid)
	if a.Base != 4 || a.MultPct != 100 {
		t.Fatalf("未祝福 base = %d multPct = %d，期望 base4 multPct100", a.Base, a.MultPct)
	}

	// 祝福期：blessUntil 未来 → 记录 base/drop 保持原值，Total = (base+drop)×2（upsert 建用户文档）
	if _, err := r.users.UpdateOne(ctx, bson.M{"_id": int64(22)},
		bson.M{"$set": bson.M{"blessedLevel": 5, "blessUntil": time.Now().Add(time.Hour)}},
		options.UpdateOne().SetUpsert(true)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.MaybeAwardJade(ctx, 22, pid, 0, 0); err != nil {
		t.Fatalf("祝福发放: %v", err)
	}
	b, _ := r.GetJadeAward(ctx, 22, pid)
	if b.Base != 4 || (b.Drop != 0 && b.Drop != 4) {
		t.Fatalf("祝福发放 = base %d drop %d，期望 base4 drop∈{0,4}（记录不放大）", b.Base, b.Drop)
	}
	if b.MultPct != 200 || b.MultFrom == "" {
		t.Fatalf("祝福 multPct = %d from = %q，期望 200/升维祝福", b.MultPct, b.MultFrom)
	}
	if b.Total != (b.Base+b.Drop)*2 {
		t.Fatalf("祝福 total = %d ≠ (base+drop)×2 = %d", b.Total, (b.Base+b.Drop)*2)
	}
	// 流水说明取 max 语义（2026-10-31 验收期修复）：单一来源 × 单一倍率，
	// 不得出现「×2 ×2」连乘假象；无加成流水不带倍率段
	entries, _, _ := r.JadeLedgerPage(ctx, 22, 1, 5)
	if len(entries) == 0 {
		t.Fatal("祝福发放未落流水")
	}
	reason := entries[0].Reason
	if !strings.Contains(reason, "升维祝福 ×2") || strings.Contains(reason, "×2 ×2") {
		t.Fatalf("祝福流水说明 = %q，期望含「升维祝福 ×2」且无连乘", reason)
	}

	// EnsureLevelBlessing：CAS 幂等——同档二次不重开祝福
	u := int64(23)
	if _, err := r.users.UpdateOne(ctx, bson.M{"_id": u},
		bson.M{"$set": bson.M{"uname": "blesstest"}}, options.UpdateOne().SetUpsert(true)); err != nil {
		t.Fatal(err)
	}
	if err := r.EnsureLevelBlessing(ctx, u, 5); err != nil {
		t.Fatalf("首次祝福: %v", err)
	}
	var doc1 struct {
		BlessUntil time.Time `bson:"blessUntil"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": u}).Decode(&doc1); err != nil {
		t.Fatal(err)
	}
	if doc1.BlessUntil.Before(time.Now()) {
		t.Fatal("首次祝福未生效")
	}
	time.Sleep(1100 * time.Millisecond) // 保证两次 blessUntil 时间戳可分
	if err := r.EnsureLevelBlessing(ctx, u, 5); err != nil {
		t.Fatalf("同档重复: %v", err)
	}
	var doc2 struct {
		BlessUntil time.Time `bson:"blessUntil"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": u}).Decode(&doc2); err != nil {
		t.Fatal(err)
	}
	if !doc2.BlessUntil.Equal(doc1.BlessUntil) {
		t.Fatal("同档重复祝福被重开（CAS 失效）")
	}
	// 更高档再开：允许
	if err := r.EnsureLevelBlessing(ctx, u, 6); err != nil {
		t.Fatalf("升档祝福: %v", err)
	}
	var doc3 struct {
		BlessedLevel int `bson:"blessedLevel"`
	}
	if err := r.users.FindOne(ctx, bson.M{"_id": u}).Decode(&doc3); err != nil {
		t.Fatal(err)
	}
	if doc3.BlessedLevel != 6 {
		t.Fatalf("blessedLevel = %d，期望 6", doc3.BlessedLevel)
	}
}

func TestSettleRoundJadePools(t *testing.T) {
	t.Run("名次+参与+零头归榜首", func(t *testing.T) {
		r := openTestRepo(t)
		// N=4（score>0），比例默认 50% → K=2，权重 2:1（和 3）
		// P=100：第 1 名 66、第 2 名 33、零头 1 归第 1 → 67/33；Q=30：u3/u4 各 15；u5 零分无份
		ct := jadeSettleExam(t, r, map[int64]int{1: 100, 2: 60, 3: 40, 4: 10, 5: 0}, 100, 30)
		r.settleRoundJadePools(context.Background(), KindExam, ct)
		for uid, want := range map[int64]int64{1: 67, 2: 33, 3: 15, 4: 15, 5: 0} {
			if bal := jadeBal(t, r, uid); bal != want {
				t.Fatalf("u%d 余额 = %d，期望 %d", uid, bal, want)
			}
		}
		// 幂等闸：再结一次不重复入账
		r.settleRoundJadePools(context.Background(), KindExam, ct)
		if bal := jadeBal(t, r, 1); bal != 67 {
			t.Fatalf("重复结算后 u1 = %d，期望 67", bal)
		}
	})
	t.Run("并列段位均分不超发", func(t *testing.T) {
		r := openTestRepo(t)
		// N=3 全 100 并列；K=2：并列段占 1..2 位（段权重=3=权重和）→ 段额 100 三人均分 33，
		// 零头 1 归榜首 → 34/33/33，总 100 不超发
		ct := jadeSettleExam(t, r, map[int64]int{1: 100, 2: 100, 3: 100}, 100, 0)
		r.settleRoundJadePools(context.Background(), KindExam, ct)
		b1, b2, b3 := jadeBal(t, r, 1), jadeBal(t, r, 2), jadeBal(t, r, 3)
		if b1+b2+b3 != 100 {
			t.Fatalf("并列总发放 = %d，期望 100 不超发", b1+b2+b3)
		}
		if b1 < 33 || b2 < 33 || b3 < 33 {
			t.Fatalf("并列各得 = %d/%d/%d，期望均 ≥33", b1, b2, b3)
		}
	})
	t.Run("唯一得分者参与池滚入名次", func(t *testing.T) {
		r := openTestRepo(t)
		// N=1（u2 零分不算参赛）→ K=1；无人可领参与奖 → Q=20 滚入 P=30：u1 全拿 50
		ct := jadeSettleExam(t, r, map[int64]int{1: 100, 2: 0}, 30, 20)
		r.settleRoundJadePools(context.Background(), KindExam, ct)
		if bal := jadeBal(t, r, 1); bal != 50 {
			t.Fatalf("u1 余额 = %d，期望 50（Q 滚入名次池）", bal)
		}
		if bal := jadeBal(t, r, 2); bal != 0 {
			t.Fatalf("u2 余额 = %d，期望 0（零分非真正参赛）", bal)
		}
	})
	t.Run("全员零分不发", func(t *testing.T) {
		r := openTestRepo(t)
		ct := jadeSettleExam(t, r, map[int64]int{1: 0, 2: 0}, 100, 30)
		r.settleRoundJadePools(context.Background(), KindExam, ct)
		if bal := jadeBal(t, r, 1) + jadeBal(t, r, 2); bal != 0 {
			t.Fatalf("全员零分仍发放 %d", bal)
		}
	})
}

func TestPublishDueRoundsJadeIntegration(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	pid := jadeTestProblem(t, r, false, 5)
	ct := &Contest{Title: "月赛", Start: time.Now().Add(-3 * time.Hour), End: time.Now().Add(-2 * time.Hour),
		HiddenUntil: time.Now().Add(-time.Minute), Published: false,
		Problems: []ContestProblem{{PID: pid, Score: 100}},
		JadePool: 100, JadeParticipatePool: 30}
	if err := r.CreateRound(ctx, KindContest, ct); err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	// u1 满分（赛内首 AC + 名次奖），u2 半分（参与奖），u3 零分（无奖）
	jadeTestRoundSub(t, r, KindContest, ct.ID, 1, pid, 100)
	jadeTestRoundSub(t, r, KindContest, ct.ID, 2, pid, 50)
	jadeTestRoundSub(t, r, KindContest, ct.ID, 3, pid, 0)

	if _, err := r.PublishDueRounds(ctx, time.Now()); err != nil {
		t.Fatalf("PublishDueRounds: %v", err)
	}
	a, err := r.GetJadeAward(ctx, 1, pid)
	if err != nil || a == nil {
		t.Fatalf("赛内首 AC 未补发: %v %v", a, err)
	}
	if a.Base != 5 || (a.Drop != 0 && a.Drop != 5) {
		t.Fatalf("补发金额 = base %d drop %d，期望 base5", a.Base, a.Drop)
	}
	// N=2（u1/u2 score>0），K=1：u1 名次奖 100 + 首AC(base+drop)；u2 参与奖 30；u3 零
	if bal := jadeBal(t, r, 1); bal != a.Total+100 {
		t.Fatalf("u1 余额 = %d，期望 首AC(%d)+名次奖100", bal, a.Total)
	}
	if bal := jadeBal(t, r, 2); bal != 30 {
		t.Fatalf("u2 余额 = %d，期望 30 参与奖", bal)
	}
	if bal := jadeBal(t, r, 3); bal != 0 {
		t.Fatalf("u3 余额 = %d，期望 0（零分不算真正参赛）", bal)
	}
	// 再触发一轮发布：无新增（场次已发布，幂等）
	if _, err := r.PublishDueRounds(ctx, time.Now()); err != nil {
		t.Fatalf("PublishDueRounds 二次: %v", err)
	}
	if bal := jadeBal(t, r, 1); bal != a.Total+100 {
		t.Fatalf("二次发布后 u1 = %d，期望不变", bal)
	}
}

// TestJadeMultRule 倍率总则（v0.45）：全部倍率平级取最高不连乘（峰值 ×2）；
// 复仇倍率被更高倍率压制时名额顺延不扣、被采用时消耗且当日第二次复仇无加成。
func TestJadeMultRule(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// ① 祝福 + 每日一题并列 → ×2 不 ×4
	pid1 := jadeTestProblem(t, r, false, 4) // base 4、drop∈{0,4}
	if _, err := r.db.Collection("dailyProblems").InsertOne(ctx, bson.M{
		"_id": beijingDay(time.Now()), "pid": pid1, "difficulty": 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.users.UpdateOne(ctx, bson.M{"_id": int64(31)},
		bson.M{"$set": bson.M{"blessedLevel": 5, "blessUntil": time.Now().Add(time.Hour)}},
		options.UpdateOne().SetUpsert(true)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.MaybeAwardJade(ctx, 31, pid1, 0, 0); err != nil {
		t.Fatalf("祝福+每日: %v", err)
	}
	a, _ := r.GetJadeAward(ctx, 31, pid1)
	if a.MultPct != 200 || a.Total != (a.Base+a.Drop)*2 {
		t.Fatalf("祝福+每日一题应取 max ×2 不连乘：multPct=%d total=%d base=%d drop=%d",
			a.MultPct, a.Total, a.Base, a.Drop)
	}

	// ② 复仇被祝福压制 → 顺延：revenge 旗标不记、名额不扣
	pid2 := jadeTestProblem(t, r, false, 2)
	jadeTestSeedWrong(t, r, 31, pid2) // 25h 前的 WA（满 24h 窗口）
	if _, _, err := r.MaybeAwardJade(ctx, 31, pid2, 0, 0); err != nil {
		t.Fatalf("复仇被压制: %v", err)
	}
	b, _ := r.GetJadeAward(ctx, 31, pid2)
	if b.Revenge {
		t.Fatal("复仇倍率被祝福压制时 revenge 旗标不应记（顺延）")
	}
	if r.RevengeSlotUsed(ctx, 31) {
		t.Fatal("复仇名额不应被消耗")
	}

	// ③ 无祝福：复仇 ×1.5 生效 + 名额消耗 + 当日第二次复仇无加成
	pid3 := jadeTestProblem(t, r, false, 4)
	jadeTestSeedWrong(t, r, 32, pid3)
	if _, _, err := r.MaybeAwardJade(ctx, 32, pid3, 0, 0); err != nil {
		t.Fatalf("复仇独立: %v", err)
	}
	c, _ := r.GetJadeAward(ctx, 32, pid3)
	if !c.Revenge || c.MultPct != 150 {
		t.Fatalf("复仇应 ×1.5：revenge=%v multPct=%d", c.Revenge, c.MultPct)
	}
	if want := ((c.Base+c.Drop)*3 + 1) / 2; c.Total != want {
		t.Fatalf("复仇 total = %d ≠ ceil((base+drop)×1.5) = %d", c.Total, want)
	}
	if !r.RevengeSlotUsed(ctx, 32) {
		t.Fatal("复仇名额应已消耗")
	}
	pid4 := jadeTestProblem(t, r, false, 4)
	jadeTestSeedWrong(t, r, 32, pid4)
	if _, _, err := r.MaybeAwardJade(ctx, 32, pid4, 0, 0); err != nil {
		t.Fatalf("二次复仇: %v", err)
	}
	d, _ := r.GetJadeAward(ctx, 32, pid4)
	if d.Revenge || d.MultPct != 100 {
		t.Fatalf("当日第二次复仇应无加成：revenge=%v multPct=%d", d.Revenge, d.MultPct)
	}

	// ④ 连击 50（×1.5）撞祝福 ×2 → 取 ×2
	pid5 := jadeTestProblem(t, r, false, 4)
	if _, _, err := r.MaybeAwardJade(ctx, 31, pid5, 0, 50); err != nil {
		t.Fatalf("连击+祝福: %v", err)
	}
	e, _ := r.GetJadeAward(ctx, 31, pid5)
	if e.MultPct != 200 {
		t.Fatalf("连击 50 撞祝福 ×2 应取 ×2，multPct = %d", e.MultPct)
	}
	// ⑤ race 第 1 名 ×2 与祝福并列 → ×2；无倍率 → 100
	pid6 := jadeTestProblem(t, r, false, 4)
	if _, _, err := r.MaybeAwardJade(ctx, 32, pid6, 200, 0); err != nil {
		t.Fatalf("PK 加成: %v", err)
	}
	f, _ := r.GetJadeAward(ctx, 32, pid6)
	if f.MultPct != 200 {
		t.Fatalf("PK 第 1 名 ×2 应参与 max，multPct = %d", f.MultPct)
	}
}

// jadeTestSeedWrong 直插一条 25h 前的未 AC 提交（复仇资格输入面，非取消）。
func jadeTestSeedWrong(t *testing.T, r *Repo, uid, pid int64) {
	t.Helper()
	sid, err := r.nextSeq(context.Background(), "submission")
	if err != nil {
		t.Fatalf("nextSeq: %v", err)
	}
	doc := bson.M{
		"_id": sid, "uid": uid, "uname": "u", "pid": pid, "ptitle": "T",
		"lang": "cpp", "code": "", "status": StatusWrongAnswer, "score": 0,
		"published": true, "priority": PriorityHigh,
		"createdAt": time.Now().Add(-25 * time.Hour), "judgedAt": time.Now().Add(-25 * time.Hour),
	}
	if _, err := r.submissions.InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("insert wrong sub: %v", err)
	}
}
