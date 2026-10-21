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
	"soj/internal/judge"
	"soj/internal/repo"
	"soj/internal/sandbox"
)

// Deps 服务运行依赖。
type Deps struct {
	Cfg     *config.Config
	Repo    *repo.Repo
	Sandbox *sandbox.Client
	Waker   *judge.Waker // 提交入队即时唤醒判题 worker（判题同进程时非空）
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
		waker:       deps.Waker,
		rank:        newRankCache(deps.Repo),
		limiter:     newRateLimiter(),
		idents:      newIdentCache(),
		leaderCache: map[string]leaderEntry{},
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), CORS(), Gzip())

	// 题面图片公开读取（v0.5.1）：非 /api 前缀，随机文件名 + immutable 缓存
	r.GET("/uploads/:file", h.ServeUpload)

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
		// 标签树（v0.5.5）：公开读（题库筛选/详情卡/建题表单共用），写走 /admin/tagtree
		api.GET("/tags/tree", AuthOptional(h), h.GetTagTree)

		api.POST("/submissions", AuthRequired(h), h.CreateSubmission)
		// 提交记录总表需「查看全部提交」权限位（学生查自己的历史走题目页「我的提交」）
		api.GET("/submissions", AuthRequired(h), RequirePerm(repo.PermSubmissionViewAll), h.ListSubmissions)
		api.GET("/submissions/:id", AuthOptional(h), h.GetSubmission)
		api.POST("/submissions/:id/rejudge", AuthRequired(h), RequirePerm(repo.PermSubmissionViewAll), h.RejudgeSubmission)
		// 取消评测（v0.7.1）：本人 ∨ submission.view.all（handler 内校验）
		api.POST("/submissions/:id/cancel", AuthRequired(h), h.CancelSubmission)

		// 游客可见面收紧（v0.7.3）：仅首页/题库/题目详情公开，其余一律登录后访问
		api.GET("/rank", AuthRequired(h), h.Rank)
		api.GET("/notice", h.GetNotice)

		// 比赛（登录后可见；未开始的比赛对无「题库全量可见」者隐藏，见 ListContests/ContestDetail）
		api.GET("/contests", AuthRequired(h), h.ListContests)
		api.GET("/contests/:id", AuthRequired(h), h.ContestDetail)

		// 班级（登录后可见；成员/邀请码等按身份返回）
		api.GET("/classes", AuthRequired(h), h.ListClasses)
		api.GET("/classes/:id", AuthRequired(h), h.GetClass)
		api.GET("/classes/:id/assignments", AuthRequired(h), h.ListClassAssignments)

		authed := api.Group("", AuthRequired(h))
		{
			authed.GET("/me/classes", h.MyClasses)
			authed.GET("/me/profile", h.GetMyProfile)
			authed.PATCH("/me/profile", h.UpdateMyProfile)
			// 题面图片上传（v0.5.1）：能打开编辑器的人群（题面编辑/系统设置权限位）
			authed.POST("/uploads", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn, repo.PermAdminSettings), h.UploadImages)

			// 网页建题（v0.5.2-1）：problem.create 权限位；元数据编辑沿用 problemEditable 语义
			authed.POST("/problems", RequirePerm(repo.PermProblemCreate), h.CreateProblem)
			authed.PATCH("/problems/:id/meta", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.UpdateProblemMeta)
			// 批量重测（v0.7.1）：按题（题目编辑权限）与按赛（组织者，round.go 注册）
			authed.POST("/problems/:id/rejudge-all", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.RejudgeProblem)
			authed.POST("/problems/:id/testdata", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.ReplaceProblemTestdata)
			authed.GET("/problems/:id/testdata/download", RequirePerm(repo.PermProblemDataDownload), h.DownloadProblemTestdata)
			authed.POST("/problems/:id/checker", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.UpdateChecker)
			authed.POST("/problems/:id/function/:file", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.UpdateFunctionFile) // driver|template（v0.9.1）
			authed.DELETE("/problems/:id/function/:file", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.RemoveFunctionFile)
			authed.POST("/problems/:id/interactor", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.UpdateInteractor) // 交互题（v0.9.2）
			authed.DELETE("/problems/:id/checker", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.RemoveChecker)

			// 题解（v0.5.2-1，多作者）：满分 ∨ 老师群体可看可写；置顶/删他人需 solution.manage
			authed.GET("/problems/:id/solutions", h.ListSolutions)
			authed.POST("/problems/:id/solutions", h.PostSolution)
			authed.PATCH("/problems/:id/solutions/:sid", h.UpdateSolution)
			authed.DELETE("/problems/:id/solutions/:sid", h.DeleteSolution)
			authed.PATCH("/problems/:id/solutions/:sid/pinned", h.PinSolution)

			authed.POST("/classes", RequirePerm(repo.PermClassCreate), h.CreateClass)
			authed.POST("/classes/:id/join", h.JoinClass)
			authed.POST("/classes/join-by-code", h.JoinByCode)
			authed.POST("/classes/:id/leave", h.LeaveClass)
			authed.PATCH("/classes/:id/private", h.SetClassPrivate)
			authed.PATCH("/classes/:id", h.UpdateClass)           // 编辑班级名称/公告（v0.7.3，classManageable）
			authed.POST("/classes/:id/transfer", h.TransferClass) // 转让班级（v0.7.2：建班人 ∨ root）
			authed.DELETE("/classes/:id", h.DeleteClass)          // 解散班级（v0.7.2：建班人 ∨ root）

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
			authed.PATCH("/assignments/:id", h.UpdateAssignment) // v0.7 编辑名称/说明
			authed.POST("/assignments/:id/problems", h.AddAssignmentProblem)
			authed.PUT("/assignments/:id/problems", h.SetAssignmentProblems)
			authed.DELETE("/assignments/:id/problems/:pid", h.RemoveAssignmentProblem)
			authed.GET("/assignments/:id/progress", h.AssignmentProgress)
			authed.GET("/assignments/:id/problems/:pid/submissions", h.AssignmentUserSubmissions)
			authed.GET("/assignments/:id/export", h.AssignmentExport) // 成绩导出 CSV（v0.5.6）

			// 比赛：创建（主题库=root，班级=班级管理员，handler 内校验）/删除/赛时提交/我的提交/榜单
			authed.POST("/contests", h.CreateContest)
			authed.PATCH("/contests/:id", h.UpdateContest) // v0.7 编辑名称/时间/说明/题目/开关
			authed.DELETE("/contests/:id", h.DeleteContest)
			authed.POST("/contests/:id/problems/:pid/submissions", h.SubmitToContest)
			authed.GET("/contests/:id/submissions/mine", h.ContestMySubmissions)
			authed.GET("/contests/:id/problems/:pid/submissions", h.ContestUserSubmissions)
			authed.GET("/contests/:id/leaderboard", h.ContestLeaderboard)
			authed.GET("/contests/:id/export", h.ContestExport) // 成绩导出 CSV（v0.5.6）
			authed.GET("/contests/:id/paper", h.ContestPaper)   // 打印试卷数据（v0.10，organizer/root）

			// 班级考试：发起（班级管理员）/删除/考时提交/我的提交/榜单
			authed.POST("/classes/:id/exams", h.CreateExam)
			authed.GET("/classes/:id/exams", h.ListClassExams)
			authed.DELETE("/exams/:id", h.DeleteExam)
			authed.PATCH("/exams/:id", h.UpdateExam) // v0.7 编辑名称/时间/说明/题目/开关
			authed.GET("/exams/:id", h.ExamDetail)
			authed.POST("/exams/:id/problems/:pid/submissions", h.SubmitToExam)
			authed.GET("/exams/:id/submissions/mine", h.ExamMySubmissions)
			authed.GET("/exams/:id/problems/:pid/submissions", h.ExamUserSubmissions)
			authed.GET("/exams/:id/leaderboard", h.ExamLeaderboard)
			authed.GET("/exams/:id/export", h.ExamExport) // 成绩导出 CSV（v0.5.6）
			authed.GET("/exams/:id/paper", h.ExamPaper)   // 打印试卷数据（v0.10，organizer/root）
			authed.PATCH("/contests/:id/file-io", h.SetContestFileIO)
			authed.PATCH("/exams/:id/file-io", h.SetExamFileIO)
			// 按赛批量重测（v0.7.1）：组织者才可（handler 内校验）
			authed.POST("/contests/:id/rejudge-all", h.RejudgeContest)
			authed.POST("/exams/:id/rejudge-all", h.RejudgeExam)

			// 题目编辑：edit.all 或 edit.own（属主校验在 handler 内）
			authed.PATCH("/problems/:id/hidden", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.SetProblemHidden)
			authed.PATCH("/problems/:id/statement", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.SetProblemStatement)
			// v0.10：客观题小题 YAML 双向 + 单题/整卷学生版试卷打印
			authed.GET("/problems/:id/objective/questions.yaml", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.ExportObjectiveYAML)
			authed.GET("/problems/:id/paper", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.ProblemPaper)
			authed.POST("/objective/questions/parse", RequirePermAny(repo.PermProblemCreate, repo.PermProblemEditAll, repo.PermProblemEditOwn), h.ParseObjectiveYAML)

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

				// 备份面板（v0.4.9）：按权限位鉴权；离线私钥接口的响应含一次性私钥
				adminG.GET("/backup", RequirePerm(repo.PermBackupRun), h.BackupStatus)
				adminG.POST("/backup/run", RequirePerm(repo.PermBackupRun), h.BackupRun)
				adminG.POST("/backup/offline-key", RequirePerm(repo.PermBackupKeys), h.BackupOfflineKey)

				// 标签树（v0.5.5）：仅 root 可改（硬编码不下放，同角色管理先例）；
				// 管理面板读走 /admin/tagtree（附每节点引用数），公开读仍走 /tags/tree
				adminG.GET("/tagtree", RequireRoot(), h.AdminTagTree)
				adminG.PUT("/tagtree", RequireRoot(), h.AdminReplaceTagTree)
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
