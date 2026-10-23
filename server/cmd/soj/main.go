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
	"log"
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

func main() {
	judgeOnly := flag.Bool("judge-only", false, "只运行判题线程池，不启动 HTTP 服务")
	importDir := flag.String("import", "", "导入一个题目目录后退出")
	backupRun := flag.Bool("backup-run", false, "执行一次加密备份后退出")
	backupTier := flag.String("backup-tier", "all", "备份分层：db=每日库备 | media=每周数据卷 | all=全量")
	flag.Parse()

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r, err := repo.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("[boot] %v", err)
	}
	defer r.Close(context.Background())
	store := testdata.NewLocalFS(cfg.DataDir)
	if err := r.Bootstrap(ctx, store); err != nil {
		log.Fatalf("[boot] 初始化失败: %v", err)
	}

	sb := sandbox.New(cfg.SandboxAddr)

	// 题目导入模式：导入后退出
	if *importDir != "" {
		imp := &repo.Importer{Repo: r, DataDir: cfg.DataDir}
		pr, err := imp.ImportDir(ctx, *importDir)
		if err != nil {
			log.Fatalf("[import] 导入失败: %v", err)
		}
		log.Printf("[import] 已导入题目 #%d（来自 %s）", pr.ID, *importDir)
		return
	}

	// 备份模式：执行一次全站加密备份后退出（供 scripts/backup.sh 与 cron 调用，
	// 与管理后台 /admin/backup/run 走 internal/backup 同一模块、同一密钥卷）
	if *backupRun {
		if !backup.ValidTier(*backupTier) {
			log.Fatalf("[backup] 未知分层 %q（可用：db / media / all）", *backupTier)
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
			log.Fatalf("[backup] %v", err)
		}
		log.Printf("[backup] 已生成 %s", path)
		return
	}

	// 种子题目：仅 Web 模式导入（多实例时避免重复导入竞争）
	if !*judgeOnly && cfg.SeedDir != "" {
		if err := (&repo.Importer{Repo: r, DataDir: cfg.DataDir}).SeedAll(ctx, cfg.SeedDir); err != nil {
			log.Fatalf("[seed] 种子题目导入失败: %v", err)
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
					log.Printf("[publish] %v", err)
				} else if n > 0 {
					log.Printf("[publish] 已发布 %d 场到期的比赛/考试", n)
				}
			}
		}
	}()

	// 提交入队即时唤醒（v0.7.1）：Web+判题同进程时提交/重测后空闲 worker 近零延迟领取
	waker := judge.NewWaker(cfg.JudgeWorkers)

	// 判题线程池
	jd := &judge.Judger{Repo: r, Sandbox: sb, Provider: store, Name: hostname(), Waker: waker}
	jd.Pool(ctx, cfg.JudgeWorkers)

	if *judgeOnly {
		log.Printf("[judge] 独立判题进程启动: workers=%d sandbox=%s", cfg.JudgeWorkers, cfg.SandboxAddr)
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
					log.Printf("[judge] 租约回收失败: %v", err)
					continue
				}
				if requeued+cancelled > 0 {
					log.Printf("[judge] 租约回收：重排队 %d 条，转取消 %d 条", requeued, cancelled)
					waker.Notify()
				}
			}
		}
	}()

	server.Run(ctx, &server.Deps{Cfg: cfg, Repo: r, Sandbox: sb, Waker: waker})
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "judge"
	}
	return h
}
