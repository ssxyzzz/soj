package repo

// 提交集合变更监听（v0.98 全站推送）：web 进程对 submissions 开 Change Stream，
// 作为 judge 容器（逐点回写/终态/raceMs）与 web 进程自身（重测/取消/揭榜翻 published）
// 之外唯一的跨进程信号源——副本集 rs0 开箱可用，不引入额外中间件。
//
// 过滤纪律（一切有界的关键）：判题租约心跳每 5s 刷 heartbeatAt，不过滤会把
// change stream 打成心跳风暴——只有命中信号字段（状态/得分/明细/进度/揭榜等）
// 的变更才做一次投影读并回调；heartbeatAt-only 直接跳过。不持久化 resume token：
// 进程重启从当下开始（事件只是信号，断档由客户端兜底轮询覆盖——轻量取舍）。

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SubSignal 提交变更信号（推送用轻快照：change stream 命中信号字段后的一次投影读）。
// 隐藏期抑制在消费方（web watcher）：场次提交未 published 时整条不发。
type SubSignal struct {
	SID         int64  `bson:"_id"`
	UID         int64  `bson:"uid"`
	Status      string `bson:"status"`
	ContestID   int64  `bson:"contestId"`
	ExamID      int64  `bson:"examId"`
	ChallengeID int64  `bson:"challengeId"`
	RaceID      int64  `bson:"raceId"`
	RaceMS      int64  `bson:"raceMs"`
	Published   bool   `bson:"published"`
	CaseDone    int    `bson:"caseDone"`
	CaseTotal   int    `bson:"caseTotal"`
	Stage       string `bson:"stage"`
}

// subChangeEvent change stream 原始事件的最小解码面（v2 驱动 Current 是 raw BSON）。
type subChangeEvent struct {
	OperationType string `bson:"operationType"`
	DocumentKey   struct {
		ID int64 `bson:"_id"`
	} `bson:"documentKey"`
	UpdateDescription struct {
		UpdatedFields bson.M `bson:"updatedFields"`
	} `bson:"updateDescription"`
}

// subSignalFields 信号字段前缀：updatedFields 命中任一（含数组点路径前缀）才算
// 有效变更。前缀匹配兼容 $push（呈现为 detail / detail.N 两种键形）。
var subSignalFields = []string{
	"status", "score", "timeMs", "memKb", "errorInfo", "detail",
	"judgedAt", "published", "scoreCancelled", "raceMs",
	"caseDone", "caseTotal", "stage",
}

func isSubSignalUpdate(fields bson.M) bool {
	for key := range fields {
		for _, p := range subSignalFields {
			if key == p || strings.HasPrefix(key, p+".") {
				return true
			}
		}
	}
	return false
}

// WatchSubmissionChanges 阻塞监听 submissions 变更并回调轻快照；ctx 取消时返回。
// 流断开（副本集主从切换等）自动重开（2s 退避），函数只在 ctx 结束时返回——
// 由调用方挂在 web 进程生命周期上。
func (r *Repo) WatchSubmissionChanges(ctx context.Context, fn func(SubSignal)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.watchSubmissionsOnce(ctx, fn); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("提交变更流断开，退避重开", "component", "push", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (r *Repo) watchSubmissionsOnce(ctx context.Context, fn func(SubSignal)) error {
	cs, err := r.submissions.Watch(ctx, mongo.Pipeline{})
	if err != nil {
		return err
	}
	defer cs.Close(ctx)
	for cs.Next(ctx) {
		var e subChangeEvent
		if err := bson.Unmarshal(cs.Current, &e); err != nil {
			slog.Warn("变更事件解码失败", "component", "push", "error", err)
			continue
		}
		if e.OperationType == "delete" {
			continue // 提交无删除路径（只增不减），防御性跳过
		}
		if e.OperationType != "insert" && !isSubSignalUpdate(e.UpdateDescription.UpdatedFields) {
			continue // 心跳/租约等非信号变更：零成本跳过（不触发投影读）
		}
		var s SubSignal
		if err := r.submissions.FindOne(ctx, bson.M{"_id": e.DocumentKey.ID}).
			Decode(&s); err != nil {
			continue // 回收竞态下文档可能已被改写，读不到就跳过（信号有兜底轮询）
		}
		fn(s)
	}
	return cs.Err()
}
