// soj 是 SOJ 的唯一入口二进制：
//
//	soj                 启动 HTTP 服务 + 内置判题线程池（默认方式）
//	soj --judge-only    只运行判题线程池（需要独立扩容判题能力时用）
//	soj --import <dir>  导入一个题目目录后退出
//	soj --backup-run    执行一次全站加密备份后退出
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"soj/internal/backup"
	"soj/internal/config"
	"soj/internal/judge"
	"soj/internal/repo"
	"soj/internal/sandbox"
	"soj/internal/server"
	"soj/internal/testdata"
)

// fatal 结构化版 log.Fatalf：Error 级落一条后退出（组件词走 component 属性，可 grep）。
func fatal(component, msg string, args ...any) {
	slog.Error(msg, append([]any{"component", component}, args...)...)
	os.Exit(1)
}

func main() {
	judgeOnly := flag.Bool("judge-only", false, "只运行判题线程池，不启动 HTTP 服务")
	importDir := flag.String("import", "", "导入一个题目目录后退出")
	backupRun := flag.Bool("backup-run", false, "执行一次加密备份后退出")
	backupTier := flag.String("backup-tier", "all", "备份分层：db=每日库备 | media=每周数据卷 | all=全量")
	flag.Parse()

	// slog 一处收口（v0.32）：TextHandler 到 stderr、时间 RFC3339 毫秒；此后全站
	// log.Printf 禁用，组件词统一走 component 属性（grep 口径：component=judge 等）。
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r, err := repo.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		fatal("boot", "MongoDB 连接失败", "error", err)
	}
	defer r.Close(context.Background())
	store := testdata.NewLocalFS(cfg.DataDir)
	if err := r.Bootstrap(ctx, store); err != nil {
		fatal("boot", "初始化失败", "error", err)
	}
	cleanupStaleTemp(cfg)

	sb := sandbox.New(cfg.SandboxAddr)

	// 题目导入模式：导入后退出（题面外链图急切转存，v0.31）
	if *importDir != "" {
		imp := &repo.Importer{Repo: r, DataDir: cfg.DataDir, TransferImages: true}
		pr, err := imp.ImportDir(ctx, *importDir)
		if err != nil {
			fatal("import", "导入失败", "error", err)
		}
		slog.Info("已导入题目", "component", "import", "pid", pr.ID, "dir", *importDir)
		return
	}

	// 备份模式：执行一次全站加密备份后退出（供 scripts/backup.sh 与 cron 调用，
	// 与管理后台 /admin/backup/run 走 internal/backup 同一模块、同一密钥卷）
	if *backupRun {
		if !backup.ValidTier(*backupTier) {
			fatal("backup", "未知备份分层（可用：db / media / all）", "tier", *backupTier)
		}
		path, err := backup.Run(ctx, backup.Options{
			MongoURI:   cfg.MongoURI,
			DataDir:    cfg.DataDir,
			UploadsDir: cfg.UploadsDir,
			BackupDir:  cfg.BackupDir,
			KeysDir:    cfg.KeysDir,
			Tier:       *backupTier,
		})
		if err != nil {
			fatal("backup", "备份失败", "error", err)
		}
		slog.Info("备份已生成", "component", "backup", "path", path)
		return
	}

	// 种子题目：仅 Web 模式导入（多实例时避免重复导入竞争）
	if !*judgeOnly && cfg.SeedDir != "" {
		if err := (&repo.Importer{Repo: r, DataDir: cfg.DataDir}).SeedAll(ctx, cfg.SeedDir); err != nil {
			fatal("seed", "种子题目导入失败", "error", err)
		}
	}

	// 隐藏期结束的比赛与考试定期发布：榜单/提交记录并入公开态、题目解锁、历史代码恢复可见
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := r.PublishDueRounds(ctx, time.Now()); err != nil {
				slog.Error("到期比赛/考试发布失败", "component", "publish", "error", err)
			} else if n > 0 {
				slog.Info("已发布到期的比赛/考试", "component", "publish", "count", n)
			}
			}
		}
	}()

	// 提交入队即时唤醒（v0.7.1）：Web+判题同进程时提交/重测后空闲 worker 近零延迟领取
	waker := judge.NewWaker(cfg.JudgeWorkers)

	// 编译闸容量（快赢批⑤）：默认 1 = 串行；SOJ_COMPILE_CONCURRENCY 可调大
	judge.SetCompileConcurrency(cfg.CompileConcurrency)

	// 判题线程池
	jd := &judge.Judger{Repo: r, Sandbox: sb, Provider: store, Name: hostname(), Waker: waker}
	jd.Pool(ctx, cfg.JudgeWorkers)

	if *judgeOnly {
		slog.Info("独立判题进程启动", "component", "judge", "workers", cfg.JudgeWorkers, "sandbox", cfg.SandboxAddr)
		<-ctx.Done()
		return
	}

	// 判题租约回收者（v0.7.1，Web 进程内每 30s 一扫）：心跳超时的 Judging 重排队
	// （卡死提交不再需要重启进程自愈）、失联的取消请求直接终态 Cancelled。
	go func() {
		ticker := time.NewTicker(repo.ReclaimScanEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				requeued, cancelled, err := r.ReclaimExpiredLeases(ctx, time.Now().Add(-repo.LeaseStaleAfter))
				if err != nil {
					slog.Error("租约回收失败", "component", "judge", "error", err)
					continue
				}
				if requeued+cancelled > 0 {
					slog.Info("租约回收：重排队与转取消", "component", "judge", "requeued", requeued, "cancelled", cancelled)
					waker.Notify()
				}
			}
		}
	}()

	// Web 进程内复用判题器：自测直跑（v0.23）借其编译串行闸与沙箱客户端
	server.Run(ctx, &server.Deps{Cfg: cfg, Repo: r, Sandbox: sb, Waker: waker, Judge: jd})
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "judge"
	}
	return h
}

// cleanupStaleTemp 清扫上次异常退出残留的临时文件（v0.27 长跑体检）：容器被 kill 时
// defer 清理不会执行——导入 zip/解压目录（单次合计最高约 6GB）与备份临时目录（含未
// 加密的 testdata 全量 tar）常驻可写层/备份卷。只清 2 小时前的，避开并行中的任务
// （cron 备份与面板备份互为独立进程）。
func cleanupStaleTemp(cfg *config.Config) {
	const olderThan = 2 * time.Hour
	if n := server.CleanupStaleImportTemp(olderThan); n > 0 {
		slog.Info("已清扫残留导入临时文件", "component", "boot", "count", n)
	}
	if n := backup.CleanupStaleTemp(cfg.BackupDir, olderThan); n > 0 {
		slog.Info("已清扫残留备份临时目录", "component", "boot", "count", n)
	}
}
