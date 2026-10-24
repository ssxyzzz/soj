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
	// JWT 密钥（v0.14）：缺失拒绝启动（公网伪造 token 防线）；开发默认值警告放行
	if cfg.JWTSecret == "" {
		log.Fatal("[web] 未设置 SOJ_JWT_SECRET，拒绝启动（生产安全强制项，见 .env.example / docker-compose.yml）")
	}
	if cfg.JWTSecret == "soj-dev-secret-change-me" || cfg.JWTSecret == "soj-dev-secret" {
		log.Println("[web] 警告: SOJ_JWT_SECRET 为开发默认值，生产环境务必更换为随机长字符串")
	}
	// 邮件通道（v0.16）：smtp 通道开着但配置不全拒绝启动（验证码发不出去等于把
	// 注册/找回功能打成半残，早失败好过线上排查）
	if s, err := deps.Repo.GetSystemSettings(ctx); err == nil {
		if s.MailChannelVal() == repo.MailChannelSMTP && !s.SMTP.Ready() {
			log.Fatal("[web] 邮件通道为 smtp 但配置不完整（主机/端口/发件人），请在后台补全或先切回 log 通道")
		}
	} else {
		log.Printf("[web] 警告: 启动时读取系统设置失败，跳过邮件通道校验: %v", err)
	}
	if os.Getenv("SOJ_DEBUG") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	h := &Handlers{
		cfg:           cfg,
		repo:          deps.Repo,
		sandbox:       deps.Sandbox,
		waker:         deps.Waker,
		rank:          newRankCache(deps.Repo),
		limiter:       newRateLimiter(),
		idents:        newIdentCache(),
		ipLimitGlobal: newIPLimiter(ipRateGlobalPerMin, time.Minute),
		ipLimitAuth:   newIPLimiter(ipRateAuthPerMin, time.Minute),
		leaderCache:   map[string]leaderEntry{},
	}
	r := gin.New()
	// 可信代理（v0.13）：ClientIP() 只从可信直连方解析 X-Forwarded-For，
	// 防 XFF 伪造；Caddy/frp 隧道拓扑下配置后可还原学生真实 IP（v0.14 限流依赖）
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("[web] 可信代理配置无效（SOJ_TRUSTED_PROXIES）: %v", err)
	}
	r.Use(gin.Logger(), gin.Recovery(), CORS(), Gzip())

	// 题面图片公开读取（v0.5.1）：非 /api 前缀，随机文件名 + immutable 缓存
	r.GET("/uploads/:file", h.ServeUpload)

	api := r.Group("/api")
	api.Use(h.rateLimit("global")) // IP 限流（v0.14）：私网来源跳过，公网直连生效
	{
		api.GET("/health", h.Health)

		// 认证类接口（v0.14）叠加更严的 IP 限流（登录/注册是爆破主面）
		authLimit := h.rateLimit("auth")
		api.POST("/auth/register", authLimit, h.Register)
		// 站点公开信息（v0.13 收编原 /auth/register-open）：注册开关 + ICP 备案号，
		// 登录页页签显隐与页脚悬挂共用；v0.16 并入注册验证方式与找回开关
		api.GET("/site", h.GetSiteInfo)
		api.POST("/auth/login", authLimit, h.Login)
		// 本人修改密码（v0.14）：旧密码 + 新密码（前端二次确认），成功后全端下线
		api.POST("/auth/password", AuthRequired(h), h.ChangePassword)
		// 邮箱验证码与自助找回（v0.16）：发码公开（注册/找回），bind 分支要登录态——
		// 挂 AuthOptional 由 handler 按用途校验；发码/重置同属爆破面，纳入认证类 IP 限流
		api.POST("/auth/email/code", authLimit, AuthOptional(h), h.EmailCode)
		api.POST("/auth/password/reset", authLimit, h.PasswordReset)
		api.POST("/auth/email/bind", AuthRequired(h), h.EmailBind)
		api.GET("/me", AuthRequired(h), h.Me)

		api.GET("/problems", AuthOptional(h), h.ListProblems)
		api.GET("/problems/:id", AuthOptional(h), h.GetProblem)
		api.GET("/problems/:id/submissions", AuthRequired(h), h.ListProblemSubmissions)
		api.GET("/problems/:id/my-submissions", AuthRequired(h), h.MyProblemSubmissions)
		// 标签树（v0.5.5）：公开读（题库筛选/详情卡/建题表单共用），写走 /admin/tagtree
		api.GET("/tags/tree", AuthOptional(h), h.GetTagTree)
		// 来源元数据（v0.12）：公开读（题库高级筛选年份选项 + 建题表单 datalist 建议），写走 /admin/sources
		api.GET("/sources/meta", AuthOptional(h), h.SourceMeta)
		// 等级配置（v0.11）：公开读（排行榜/顶栏徽章要名字与特效开关），写走 /admin/levelconfig
		api.GET("/levels", AuthOptional(h), h.GetLevels)

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
			// 题包批量导入（v0.15 统一格式）：独立 /import 页按来源（SOJ/Hydro）适配；problem.create 权限即可
			authed.POST("/import", RequirePerm(repo.PermProblemCreate), h.StartImport)
			authed.GET("/import/status", RequirePerm(repo.PermProblemCreate), h.ImportStatus)
			authed.POST("/import/cancel", RequirePerm(repo.PermProblemCreate), h.ImportCancel)
			authed.PATCH("/problems/:id/meta", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.UpdateProblemMeta)
			// 批量重测（v0.7.1）：按题（题目编辑权限）与按赛（组织者，round.go 注册）
			authed.POST("/problems/:id/rejudge-all", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.RejudgeProblem)
			authed.POST("/problems/:id/testdata", RequirePermAny(repo.PermProblemEditAll, repo.PermProblemEditOwn), h.ReplaceProblemTestdata)
			authed.GET("/problems/:id/testdata/download", RequirePerm(repo.PermProblemDataDownload), h.DownloadProblemTestdata)
			// 多题打包导出（v0.18.1）：权限位与单题下载同源；与 /problems/:id 参数路由
			// 同级静态段，gin 树静态优先匹配
			authed.GET("/problems/export", RequirePerm(repo.PermProblemDataDownload), h.ExportProblems)
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
				// 解绑邮箱（v0.16）：root 硬编码（学生换邮箱场景，不设权限位）
				adminG.DELETE("/users/:id/email", RequireRoot(), h.AdminUnbindEmail)
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
				// 保存被删拦截后的解绑（一键全部/逐题），同 root 专属
				adminG.POST("/tagtree/unbind", RequireRoot(), h.AdminUnbindTags)
				// 重复题扫描（v0.18.1）：按标题+题面双键，只列出供管理员处置
				adminG.GET("/problems/duplicates", RequirePerm(repo.PermProblemEditAll), h.AdminScanDuplicateProblems)

				// 来源库（v0.12，root）：条目 CRUD + 转为标签；公开建议读走 /sources/meta
				adminG.GET("/sources", RequireRoot(), h.AdminListSources)
				adminG.POST("/sources", RequireRoot(), h.AdminCreateSource)
				adminG.PATCH("/sources/:id", RequireRoot(), h.AdminUpdateSource)
				adminG.DELETE("/sources/:id", RequireRoot(), h.AdminDeleteSource)
				adminG.POST("/sources/:id/to-tag", RequireRoot(), h.AdminSourceToTag)

				// 等级面板（v0.11）：名称/阈值/衰减系数/特效开关，仅 root
				adminG.GET("/levelconfig", RequireRoot(), h.AdminGetLevelConfig)
				adminG.PUT("/levelconfig", RequireRoot(), h.AdminUpdateLevelConfig)

				// 安全日志（v0.14）：登录/锁定/改密/重置/限流事件，仅 root
				adminG.GET("/security-logs", RequireRoot(), h.AdminListSecurityLogs)

				// 经验调整（用户管理界面）：查等级 + 加/减经验，用户管理权限位
				adminG.GET("/users/:id/level", RequirePerm(repo.PermAdminUserManage), h.AdminGetUserLevel)
				adminG.POST("/users/:id/xp-adjust", RequirePerm(repo.PermAdminUserManage), h.AdminAdjustUserXp)
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
