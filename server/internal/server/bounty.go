package server

// 悬赏（v0.33-②）：题内 + 全局讨论发主楼可挂悬赏（发布即冻结托管），发帖人打赏任意
// 回复（可分多次打光），14 天无打赏未退部分自动退回，删帖自动退。金额上下限后台可调
//（默认 5 ~ min(余额, 500)）。打赏走通知（复用 accept 通道：回应被认可）。

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// tipDiscussReply POST /api/discuss/:tid/replies/:rid/tip {amount} —— 发帖人打赏回复。
func (h *Handlers) tipDiscussReply(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tid, okT := parseID(c, "tid")
	rid, okR := parseID(c, "rid")
	if !okT || !okR {
		return
	}
	var req struct {
		Amount int64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount <= 0 {
		fail(c, http.StatusBadRequest, "打赏金额需为正整数")
		return
	}
	ctx := c.Request.Context()
	t, err := h.repo.GetDiscussTopic(ctx, tid)
	if respondRepoErr(c, err, "话题不存在", "读取话题失败") {
		return
	}
	if t.UID != uid {
		fail(c, http.StatusForbidden, "只有悬赏发起人可以打赏")
		return
	}
	if t.Deleted {
		fail(c, http.StatusBadRequest, "话题已删除，悬赏将自动退回")
		return
	}
	remaining := t.Bounty - t.BountyPaid
	if remaining <= 0 {
		fail(c, http.StatusBadRequest, "悬赏已打光或已退回")
		return
	}
	if req.Amount > remaining {
		fail(c, http.StatusBadRequest, "打赏金额超过剩余悬赏")
		return
	}
	rep, err := h.repo.GetDiscussReply(ctx, rid)
	if repo.IsNotFound(err) || (err == nil && rep.TID != tid) {
		fail(c, http.StatusNotFound, "回复不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取回复失败")
		return
	}
	if rep.UID == uid {
		fail(c, http.StatusBadRequest, "不能打赏自己的回复")
		return
	}
	// 托管池内划转：发帖人已冻结（发布时已扣），打赏从托管划给回复作者 + 前移话题活跃
	if err := h.repo.TipDiscussBounty(ctx, tid, rid, rep.UID, req.Amount); err != nil {
		fail(c, http.StatusInternalServerError, "打赏失败")
		return
	}
	s, _ := h.repo.GetSystemSettings(ctx)
	h.notifyOne(ctx, rep.UID, repo.NotifyInput{
		Typ: repo.NotifyAccept, Title: "你的回复收到打赏",
		Body: "悬赏 " + strconv.FormatInt(req.Amount, 10) + " 玉已入账",
		Link: "/discuss/" + strconv.FormatInt(tid, 10), RefID: tid,
	}, s, "")
	ok(c, gin.H{"tipped": req.Amount})
}

// bountyRefundLoop 悬赏自动退款巡检（v0.33-②）：每 10 分钟扫一次——
// 有未退余额 ∧ 14 天无互动（LastActiveAt）的话题，剩余托管退回发帖人（拍板：一律退回，
// 接受「看完答案赖账等退款」的白嫖风险，社交压力兜底）。
func (h *Handlers) bountyRefundLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := h.repo.RefundDueBounties(ctx, 14*24*time.Hour, 50); err != nil {
				slog.Warn("悬赏到期退款巡检失败", "component", "jade", "error", err)
			}
		}
	}
}
