package server

// 年级晋级管理端点（v0.34）：状态查询（当前学年/下一晋级时刻/各档位人数）与
// 手动「立即晋级」（错时钟、提前晋级的兜底）。挂在系统设置权限位（root 后台面板）。

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

// AdminGradePromotionInfo GET /api/admin/grade-promotion —— 晋级状态（系统设置面板）。
// 顺带跑一次懒触发：面板打开即对齐（跨 9/1 首次访问面板也能立即晋级，不等读路径）。
func (h *Handlers) AdminGradePromotionInfo(c *gin.Context) {
	if _, err := h.repo.EnsureGradePromotion(c.Request.Context()); err != nil {
		fail(c, http.StatusInternalServerError, "晋级检查失败")
		return
	}
	info, err := h.repo.GradePromotionInfo(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取晋级状态失败")
		return
	}
	grades := make([]gin.H, 0, repo.GradeGraduated+1)
	for no := 0; no <= repo.GradeGraduated; no++ {
		grades = append(grades, gin.H{"no": no, "name": repo.GradeName(no), "count": info.Counts[no]})
	}
	ok(c, gin.H{
		"current_year": info.CurrentYear,                 // 当前学年起始年（2026 = 2026–2027 学年）
		"next_at":      info.NextAt.Format(time.RFC3339), // 下一自动晋级时刻（次年 9/1，北京时间）
		"last_year":    info.LastYear,                    // 已执行到的学年（0 = 从未）
		"grades":       grades,                           // 0 未设置 … 13 已毕业，逐档在档人数
	})
}

// AdminGradePromotionForce POST /api/admin/grade-promotion —— 手动立即晋级一次：
// 1–12 档全体 +1、高三→已毕业，未设置不动；标记对齐当前学年。每调一次都执行，
// 幂等性由前端确认弹窗（带影响人数）把关。
func (h *Handlers) AdminGradePromotionForce(c *gin.Context) {
	promoted, err := h.repo.ForceGradePromotion(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "晋级失败")
		return
	}
	_, uname, _, _ := currentUser(c)
	h.securityLog(c, repo.SecUserImport, 0, uname, "手动年级晋级：影响用户数 "+strconv.FormatInt(promoted, 10))
	ok(c, gin.H{"promoted": promoted})
}
