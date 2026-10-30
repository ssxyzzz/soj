package server

// 年级晋级管理端点（v0.34）：状态查询（当前学年/下一晋级时刻/各档位人数）。
// 晋级本身全自动（每年 9/1 懒触发，见 repo/grade.go），无手动触发入口
//（2026-10-30 验收调整拍板：手动「立即晋级」移除）。挂在系统设置权限位（root 后台面板）。

import (
	"net/http"
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
