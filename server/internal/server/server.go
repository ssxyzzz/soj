// Package server 组装 SOJ 服务：同一进程提供前端页面与 HTTP API
// （判题线程池由 main 启动，与 HTTP 服务共享 repo/沙箱客户端）。
package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/config"
	"soj/internal/repo"
	"soj/internal/sandbox"
)

// Deps 服务运行依赖。
type Deps struct {
	Cfg     *config.Config
	Repo    *repo.Repo
	Sandbox *sandbox.Client
}

// Run 启动 HTTP 服务（阻塞直至 ctx 取消或监听失败）。
func Run(ctx context.Context, deps *Deps) {
	cfg := deps.Cfg
	if os.Getenv("SOJ_DEBUG") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	h := &Handlers{
		cfg:         cfg,
		repo:        deps.Repo,
		sandbox:     deps.Sandbox,
		rank:        newRankCache(deps.Repo),
		limiter:     newRateLimiter(),
		idents:      newIdentCache(),
		leaderCache: map[string]leaderEntry{},
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), CORS(), Gzip())

	api := r.Group("/api")
	{
		api.GET("/health", h.Health)

		api.POST("/auth/register", h.Register)
		api.POST("/auth/login", h.Login)
		api.GET("/me", AuthRequired(h), h.Me)

		api.GET("/problems", AuthOptional(h), h.ListProblems)
		api.GET("/problems/:id", AuthOptional(h), h.GetProblem)
		api.GET("/problems/:id/submissions", AuthRequired(h), h.ListProblemSubmissions)
		api.GET("/problems/:id/my-submissions", AuthRequired(h), h.MyProblemSubmissions)

		api.POST("/submissions", AuthRequired(h), h.CreateSubmission)
		// 提交记录总表需「查看全部提交」权限位（学生查自己的历史走题目页「我的提交」）
		api.GET("/submissions", AuthRequired(h), RequirePerm(repo.PermSubmissionViewAll), h.ListSubmissions)
		api.GET("/submissions/:id", AuthOptional(h), h.GetSubmission)

		api.GET("/rank", h.Rank)
		api.GET("/notice", h.GetNotice)

		// 比赛
		api.GET("/contests", AuthOptional(h), h.ListContests)
		api.GET("/contests/:id", AuthOptional(h), h.ContestDetail)

		// 班级：列表公开；详情公开（成员/邀请码按身份返回）；其余需登录
		api.GET("/classes", AuthOptional(h), h.ListClasses)
		api.GET("/classes/:id", AuthOptional(h), h.GetClass)
		api.GET("/classes/:id/assignments", AuthOptional(h), h.ListClassAssignments)

		authed := api.Group("", AuthRequired(h))
		{
			authed.GET("/me/classes", h.MyClasses)
			authed.POST("/classes", RequirePerm(repo.PermClassCreate), h.CreateClass)
			authed.POST("/classes/:id/join", h.JoinClass)
			authed.POST("/classes/join-by-code", h.JoinByCode)
			authed.POST("/classes/:id/leave", h.LeaveClass)
			authed.PATCH("/classes/:id/private", h.SetClassPrivate)

			// 班级管理：handler 内按 classManageable 校验（建班人 / 班级管理员 / root）
			managed := authed.Group("/classes/:id")
			{
				managed.POST("/invite", h.RegenInvite)
				managed.POST("/members", h.ImportMembers)
			}
			authed.POST("/classes/:id/members/:uid/role", h.SetMemberRole)

			// 作业：班级内题目清单，即时出分、不锁外班
			authed.POST("/classes/:id/assignments", h.CreateAssignment)
			authed.DELETE("/assignments/:id", h.DeleteAssignment)
			authed.GET("/assignments/:id", h.GetAssignmentDetail)
			authed.POST("/assignments/:id/problems", h.AddAssignmentProblem)
			authed.PUT("/assignments/:id/problems", h.SetAssignmentProblems)
			authed.DELETE("/assignments/:id/problems/:pid", h.RemoveAssignmentProblem)
			authed.GET("/assignments/:id/progress", h.AssignmentProgress)

			// 比赛：创建（主题库=root，班级=班级管理员，handler 内校验）/删除/赛时提交/我的提交/榜单
			authed.POST("/contests", h.CreateContest)
			authed.PATCH("/contests/:id/intro", h.SetContestIntro)
			authed.DELETE("/contests/:id", h.DeleteContest)
			authed.POST("/contests/:id/problems/:pid/submissions", h.SubmitToContest)
			authed.GET("/contests/:id/submissions/mine", h.ContestMySubmissions)
			authed.GET("/contests/:id/leaderboard", h.ContestLeaderboard)

			// 班级考试：发起（班级管理员）/删除/考时提交/我的提交/榜单
			authed.POST("/classes/:id/exams", h.CreateExam)
			authed.GET("/classes/:id/exams", h.ListClassExams)
			authed.DELETE("/exams/:id", h.DeleteExam)
			authed.GET("/exams/:id", h.ExamDetail)
			authed.POST("/exams/:id/problems/:pid/submissions", h.SubmitToExam)
			authed.GET("/exams/:id/submissions/mine", h.ExamMySubmissions)
			authed.GET("/exams/:id/leaderboard", h.ExamLeaderboard)
			authed.PATCH("/exams/:id/intro", h.SetExamIntro)

			// 题目编辑：edit.all 或 edit.own（属主校验在 handler 内）
			authed.PATCH("/problems/:id/hidden", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.SetProblemHidden)
			authed.PATCH("/problems/:id/statement", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.SetProblemStatement)

			// 管理端：按权限位鉴权（可授予自定义角色）；角色管理硬编码 root 不下放
			adminG := authed.Group("/admin")
			{
				adminG.GET("/users", RequirePerm(repo.PermAdminUserManage), h.AdminListUsers)
				adminG.POST("/users", RequirePerm(repo.PermAdminUserManage), h.AdminCreateUser)
				adminG.PATCH("/users/:id", RequirePerm(repo.PermAdminUserManage), h.AdminUpdateUser)
				adminG.GET("/settings", RequirePerm(repo.PermAdminSettings), h.GetSettings)
				adminG.PUT("/settings", RequirePerm(repo.PermAdminSettings), h.UpdateSettings)
				// 角色读取放宽到用户管理员（改角色下拉需要角色清单）；增删改硬编码 root 不下放
				adminG.GET("/roles", RequirePerm(repo.PermAdminUserManage), h.AdminListRoles)
				adminG.POST("/roles", RequireRoot(), h.AdminCreateRole)
				adminG.PATCH("/roles/:id", RequireRoot(), h.AdminUpdateRole)
				adminG.DELETE("/roles/:id", RequireRoot(), h.AdminDeleteRole)
			}
		}
	}

	// 前端静态资源（embed），API 未命中的路径走 SPA 回退
	registerStatic(r)

	log.Printf("[web] SOJ 启动: %s (判题并发 %d, 沙箱 %s)", cfg.Addr, cfg.JudgeWorkers, cfg.SandboxAddr)

	srv := &http.Server{Addr: cfg.Addr, Handler: r}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[web] HTTP 服务异常退出: %v", err)
		}
	}()
	<-ctx.Done()
	log.Println("[web] 收到退出信号，关闭 HTTP 服务...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[web] 强制退出: %v", err)
	}
}
