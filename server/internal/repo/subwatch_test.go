package repo

// 逐点进度回写与提交变更监听（v0.98 全站推送）集成测试：本地 Mongo 副本集
// （openTestRepo skip 范式）——① PushCaseResults/SetCaseStage 租约护栏与进度
// 计数；② 终态/取消/重测/回收/领取的瞬态字段清理（不留老账）；③ 领取清场防
// 失联判题器残留叠加成重复点；④ WatchSubmissionChanges 信号过滤（心跳不出
// 事件、状态/逐点推送出事件）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestPushCaseResultsLeaseAndProgress(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	t.Run("租约护栏", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(60), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now()})
		if err := r.PushCaseResults(ctx, 60, "w0", []*CaseResult{{Index: 1, Status: "AC"}}, 1, 3); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("非持租约者推送应 ErrLeaseLost，得到 %v", err)
		}
		if err := r.SetCaseStage(ctx, 60, "w0", "compile", 3); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("非持租约者阶段标记应 ErrLeaseLost，得到 %v", err)
		}
	})

	t.Run("逐点推送与进度计数", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(61), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now()})
		if err := r.SetCaseStage(ctx, 61, "w1", "compile", 10); err != nil {
			t.Fatalf("SetCaseStage: %v", err)
		}
		s := getSub(t, r, 61)
		if s.Stage != "compile" || s.CaseTotal != 10 || s.CaseDone != 0 {
			t.Fatalf("编译期标记不符: %+v", s)
		}
		batch := []*CaseResult{{Index: 1, Status: "AC", TimeMS: 10}, {Index: 2, Status: "WA", TimeMS: 20, Msg: "答案不对"}}
		if err := r.PushCaseResults(ctx, 61, "w1", batch, 2, 10); err != nil {
			t.Fatalf("PushCaseResults: %v", err)
		}
		s = getSub(t, r, 61)
		if len(s.Detail) != 2 || s.CaseDone != 2 || s.CaseTotal != 10 || s.Stage != "run" {
			t.Fatalf("逐点进度不符: done=%d total=%d stage=%q detail=%v", s.CaseDone, s.CaseTotal, s.Stage, s.Detail)
		}
		if s.Detail[1].Status != "WA" || s.Detail[1].Msg != "答案不对" {
			t.Fatalf("推送载荷应保真: %+v", s.Detail[1])
		}
		// 终态整写覆盖 + 瞬态字段清除
		if err := r.FinishSubmission(ctx, 61, "w1", StatusWrongAnswer, 30, 40, 20, "第 1 个测试点: 答案不对", nil); err != nil {
			t.Fatalf("FinishSubmission: %v", err)
		}
		s = getSub(t, r, 61)
		if s.CaseDone != 0 || s.CaseTotal != 0 || s.Stage != "" {
			t.Fatalf("终态应清逐点瞬态字段: %+v", s)
		}
	})

	t.Run("取消与重测清理", func(t *testing.T) {
		insert(t, r, bson.M{"_id": int64(62), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now(),
			"caseDone": 3, "caseTotal": 9, "stage": "run", "detail": bson.A{bson.M{"index": 1, "status": "AC"}}})
		if _, err := r.RequestCancelSubmission(ctx, 62); err != nil {
			t.Fatalf("取消: %v", err)
		}
		if err := r.FinishSubmission(ctx, 62, "w1", StatusCancelled, 0, 0, 0, "已取消评测", nil); err != nil {
			t.Fatalf("取消终态: %v", err)
		}
		s := getSub(t, r, 62)
		if s.CaseDone != 0 || s.CaseTotal != 0 || s.Stage != "" {
			t.Fatalf("取消终态应清瞬态字段: %+v", s)
		}

		insert(t, r, bson.M{"_id": int64(63), "status": StatusAccepted, "caseDone": 5, "caseTotal": 5, "stage": "run"})
		if err := r.RejudgeSubmission(ctx, 63); err != nil {
			t.Fatalf("重测: %v", err)
		}
		s = getSub(t, r, 63)
		if s.CaseDone != 0 || s.CaseTotal != 0 || s.Stage != "" {
			t.Fatalf("重测重置应清瞬态字段: %+v", s)
		}
	})

	t.Run("领取清场防重复点", func(t *testing.T) {
		// 判题器失联被回收：残留部分 detail + 进度字段；重排队后被新判题器领取，
		// 领取即清场——新判题器的逐点推送从零开始不叠加
		insert(t, r, bson.M{"_id": int64(64), "status": StatusJudging, "judge": "w1",
			"heartbeatAt": time.Now().Add(-200 * time.Second),
			"caseDone": 2, "caseTotal": 6, "stage": "run",
			"detail": bson.A{bson.M{"index": 1, "status": "AC"}, bson.M{"index": 2, "status": "AC"}}})
		if _, _, err := r.ReclaimExpiredLeases(ctx, time.Now().Add(-LeaseStaleAfter)); err != nil {
			t.Fatalf("回收: %v", err)
		}
		s := getSub(t, r, 64)
		if s.CaseDone != 0 || s.CaseTotal != 0 || s.Stage != "" {
			t.Fatalf("回收应清瞬态字段: %+v", s)
		}
		claimed, _ := r.ClaimSubmission(ctx, "w2")
		if claimed == nil || claimed.ID != 64 {
			t.Fatalf("重排队的 #%d 应可被领取", 64)
		}
		s = getSub(t, r, 64)
		if len(s.Detail) != 0 || s.CaseDone != 0 || s.CaseTotal != 0 || s.Stage != "" {
			t.Fatalf("领取应清场（旧判题器部分推送是垃圾数据）: %+v", s)
		}
		if err := r.PushCaseResults(ctx, 64, "w2", []*CaseResult{{Index: 1, Status: "AC"}}, 1, 6); err != nil {
			t.Fatalf("新判题器推送: %v", err)
		}
		if s := getSub(t, r, 64); len(s.Detail) != 1 {
			t.Fatalf("清场后推送不应叠加重复点: %v", s.Detail)
		}
	})
}

func TestWatchSubmissionChanges(t *testing.T) {
	r := openTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seen := make(chan SubSignal, 64)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- r.WatchSubmissionChanges(ctx, func(s SubSignal) { seen <- s })
	}()
	// 变更流建立需要一点时间（首个 getMore 挂起前事件不丢——resume 自当下起，
	// 这里等待流就绪的近似法：先写一条心跳类更新并等它不回响后再开始断言）
	insert(t, r, bson.M{"_id": int64(70), "uid": int64(7), "status": StatusJudging, "judge": "w1", "heartbeatAt": time.Now()})
	waitFor(t, seen, func(s SubSignal) bool { return s.SID == 70 }) // insert 出事件（流已就绪）

	// 心跳-only 更新：不出事件
	if err := r.HeartbeatSubmission(ctx, 70, "w1"); err != nil {
		t.Fatalf("心跳: %v", err)
	}
	select {
	case s := <-seen:
		t.Fatalf("心跳-only 更新不应出事件: %+v", s)
	case <-time.After(700 * time.Millisecond):
	}

	// 逐点推送：出事件，快照带进度计数
	if err := r.PushCaseResults(ctx, 70, "w1", []*CaseResult{{Index: 1, Status: "AC"}}, 1, 4); err != nil {
		t.Fatalf("推送: %v", err)
	}
	s := waitFor(t, seen, func(s SubSignal) bool { return s.SID == 70 && s.CaseDone == 1 })
	if s.CaseTotal != 4 || s.Stage != "run" || s.UID != 7 {
		t.Fatalf("逐点事件快照不符: %+v", s)
	}

	// 终态：出事件，状态可读
	if err := r.FinishSubmission(ctx, 70, "w1", StatusAccepted, 1, 2, 100, "", nil); err != nil {
		t.Fatalf("终态: %v", err)
	}
	s = waitFor(t, seen, func(s SubSignal) bool { return s.SID == 70 && s.Status == StatusAccepted })
	if s.Published {
		t.Fatalf("练习提交快照 published 口径异常: %+v", s)
	}

	// 玉/租约类非信号字段更新（SetSubmissionJade 按 _id 直写 jade 字段）：不出事件
	if _, err := r.submissions.UpdateOne(ctx, bson.M{"_id": int64(70)},
		bson.M{"$set": bson.M{"jadeTotal": 3}}); err != nil {
		t.Fatalf("玉回写: %v", err)
	}
	select {
	case s := <-seen:
		t.Fatalf("非信号字段更新不应出事件: %+v", s)
	case <-time.After(700 * time.Millisecond):
	}

	cancel()
	select {
	case <-watchDone:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后监听应退出")
	}
}

// waitFor 轮询消费事件直到谓词命中（带超时），返回命中信号——变更流是异步的，
// 事件间可能插入别的心跳类噪音（不会，但写法上稳健）。
func waitFor(t *testing.T, ch <-chan SubSignal, pred func(SubSignal) bool) SubSignal {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-ch:
			if pred(s) {
				return s
			}
		case <-deadline:
			t.Fatal("等待变更事件超时")
			return SubSignal{}
		}
	}
}
