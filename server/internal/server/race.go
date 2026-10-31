package server

// 课堂 PK（v0.42）handler：教师发起/取消，班级成员实时榜轮询。
// 参赛无独立端点——学生从班级卡跳挑战作答（复用 StartChallenge 的 Lv3 闸 +
// staff 豁免），挑战提交由 repo 层自动挂 raceId。

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// CreateRace 发起课堂 PK（POST /classes/:id/race）：manageable 闸；同班同时仅一场。
// body {pid, bonus, countdown}——pid 需公开、可挑战、有数据（隐藏题会向全班露出题面）。
func (h *Handlers) CreateRace(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	if !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可发起课堂 PK")
		return
	}
	var req struct {
		PID       int64 `json:"pid" binding:"required"`
		Bonus     bool  `json:"bonus"`
		Countdown int   `json:"countdown"` // 发起倒计时秒（0 = 即开）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不完整")
		return
	}
	p, err := h.repo.GetProblemMeta(ctx, req.PID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	if p.Hidden || p.Duplicate {
		fail(c, http.StatusBadRequest, "隐藏题/重复题不能用于课堂 PK（会向全班露出题面）")
		return
	}
	if !p.ChallengeEligible() {
		fail(c, http.StatusBadRequest, "该题型不支持挑战，无法作为 PK 题目")
		return
	}
	if len(p.Cases) == 0 {
		fail(c, http.StatusBadRequest, "本题暂无测试数据，无法评测")
		return
	}
	rc, err := h.repo.CreateClassroomRace(ctx, uid, uname, classID, p.ID, p.Title, p.EffChallengeSec(), req.Countdown, req.Bonus)
	if errors.Is(err, repo.ErrRaceActive) {
		fail(c, http.StatusConflict, "已有进行中的课堂 PK，可先取消或等其结束")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "发起课堂 PK 失败")
		return
	}
	slog.Info("发起课堂 PK", "component", "web", "rid", rc.ID, "classId", classID,
		"pid", p.ID, "host", uname, "sec", p.EffChallengeSec(), "bonus", req.Bonus)
	go h.notifyRaceStarted(rc)
	ok(c, raceJSON(ctx, h, rc, uid))
}

// CancelRace 取消进行中的课堂 PK（DELETE /classes/:id/race）：manageable 闸。
func (h *Handlers) CancelRace(c *gin.Context) {
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	if !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级管理员可取消课堂 PK")
		return
	}
	rc, err := h.repo.ActiveClassroomRace(ctx, classID)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取课堂 PK 失败")
		return
	}
	if rc == nil {
		fail(c, http.StatusNotFound, "没有进行中的课堂 PK")
		return
	}
	cancelled, err := h.repo.CancelClassroomRace(ctx, classID, rc.ID)
	if err != nil || !cancelled {
		fail(c, http.StatusInternalServerError, "取消课堂 PK 失败")
		return
	}
	slog.Info("取消课堂 PK", "component", "web", "rid", rc.ID, "classId", classID)
	ok(c, gin.H{"cancelled": true})
}

// GetClassRace 班级 PK 现状（GET /classes/:id/race）：进行中的一场（含实时榜），
// 无进行中时带最近一场（结算名次表）。班级成员或管理员可读。
func (h *Handlers) GetClassRace(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	classID, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	cl, err := h.repo.GetClass(ctx, classID)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "班级不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取班级失败")
		return
	}
	isMember, err := h.repo.IsMember(ctx, classID, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取成员状态失败")
		return
	}
	if !isMember && !h.classManageable(c, cl) {
		fail(c, http.StatusForbidden, "仅班级成员可查看课堂 PK")
		return
	}
	rc, err := h.repo.ActiveClassroomRace(ctx, classID) // 含截止懒结算
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取课堂 PK 失败")
		return
	}
	resp := gin.H{}
	if rc != nil {
		resp["race"] = raceJSON(ctx, h, rc, uid)
	} else if last, _ := h.repo.LatestClassroomRace(ctx, classID); last != nil {
		resp["last"] = raceJSON(ctx, h, last, uid)
	}
	ok(c, resp)
}

// GetRaceByID 按 id 读一场（GET /races/:id）：ChallengeView ?race= 参数用；
// 班级成员或管理员可读。
func (h *Handlers) GetRaceByID(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	ctx := c.Request.Context()
	rc, err := h.repo.GetClassroomRace(ctx, id)
	if repo.IsNotFound(err) || (err == nil && rc == nil) {
		fail(c, http.StatusNotFound, "课堂 PK 不存在")
		return
	} else if err != nil {
		fail(c, http.StatusInternalServerError, "读取课堂 PK 失败")
		return
	}
	isMember, err := h.repo.IsMember(ctx, rc.ClassID, uid)
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取成员状态失败")
		return
	}
	if !isMember && !h.classManageable(c, &repo.Class{ID: rc.ClassID, OwnerID: rc.HostUID}) {
		fail(c, http.StatusForbidden, "仅班级成员可查看课堂 PK")
		return
	}
	ok(c, raceJSON(ctx, h, rc, uid))
}

// raceJSON 对外视图：race 元数据 + 实时榜 + 观察者名次行（榜外 my_rank=0）。
func raceJSON(ctx context.Context, h *Handlers, rc *repo.ClassroomRace, viewerUID int64) gin.H {
	board, err := h.repo.RaceBoard(ctx, rc.ID, 50)
	if err != nil {
		slog.Warn("race 榜单聚合失败", "component", "race", "rid", rc.ID, "error", err)
	}
	rows := make([]gin.H, 0, len(board))
	myRank, myMS := 0, int64(0)
	for i, row := range board {
		rows = append(rows, gin.H{
			"rank": i + 1, "uid": row.UID, "username": row.Username,
			"duration_ms": row.DurationMS, "at": row.CreatedAt.UnixMilli(),
		})
		if row.UID == viewerUID {
			myRank, myMS = i+1, row.DurationMS
		}
	}
	now := time.Now()
	view := gin.H{
		"id": rc.ID, "class_id": rc.ClassID, "pid": rc.PID, "ptitle": rc.PTitle,
		"host_uid": rc.HostUID, "host_name": rc.HostName, "bonus": rc.Bonus,
		"status": rc.Status,
		"started_at":  rc.StartedAt.UnixMilli(),
		"deadline_at": rc.DeadlineAt.UnixMilli(),
		"board":       rows,
		"my_rank":     myRank, "my_ms": myMS,
	}
	if rc.Status == repo.RaceRunning {
		if remaining := rc.DeadlineAt.Sub(now).Milliseconds(); remaining > 0 {
			view["remaining_ms"] = remaining
		}
		if cd := rc.StartedAt.Sub(now).Milliseconds(); cd > 0 {
			view["countdown_ms"] = cd
		}
	} else if rc.Status == repo.RaceSettled {
		view["winner_uid"], view["winner_name"] = rc.WinnerUID, rc.WinnerName
		view["settled_at"] = rc.SettledAt.UnixMilli()
	}
	return view
}

// notifyRaceStarted 发起广播：全班通知「课堂 PK 开始」（铃铛入口的实时性来源）。
func (h *Handlers) notifyRaceStarted(rc *repo.ClassroomRace) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := h.repo.GetSystemSettings(ctx)
	if err != nil {
		return
	}
	uids, err := h.repo.ClassMemberIDs(ctx, rc.ClassID)
	if err != nil {
		return
	}
	mins := int64(math.Ceil(time.Until(rc.DeadlineAt).Minutes())) // ceil：299s 报 5 分钟（floor 会少报 1 分钟）
	if mins < 1 {
		mins = 1
	}
	_ = h.repo.NotifyUsers(ctx, uids, repo.NotifyInput{
		Typ:   repo.NotifyContest,
		Title: "课堂 PK 开始：" + truncRunes(rc.PTitle, 30),
		Body:  strconv.FormatInt(mins, 10) + " 分钟手速挑战，进入班级页参赛",
		Link:  "/classes/" + strconv.FormatInt(rc.ClassID, 10),
		RefID: rc.ID,
	}, s.NotifyRetentionDaysVal())
}
