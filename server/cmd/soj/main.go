// soj 是 SOJ 的唯一入口二进制：
//
//	soj                 启动 HTTP 服务 + 内置判题线程池（默认方式）
//	soj --judge-only    只运行判题线程池（需要独立扩容判题能力时用）
//	soj --import <dir>  导入一个题目目录后退出
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	flag.Parse()

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r, err := repo.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("[boot] %v", err)
	}
	defer r.Close(context.Background())
	if err := r.Bootstrap(ctx); err != nil {
		log.Fatalf("[boot] 初始化失败: %v", err)
	}

	sb := sandbox.New(cfg.SandboxAddr)
	store := testdata.NewLocalFS(cfg.DataDir)

	// 题目导入模式：导入后退出
	if *importDir != "" {
		imp := &repo.Importer{Repo: r, DataDir: cfg.DataDir}
		pid, err := imp.ImportDir(ctx, *importDir)
		if err != nil {
			log.Fatalf("[import] 导入失败: %v", err)
		}
		log.Printf("[import] 已导入题目 #%d（来自 %s）", pid, *importDir)
		return
	}

	// 种子题目：仅 Web 模式导入（多实例时避免重复导入竞争）
	if !*judgeOnly && cfg.SeedDir != "" {
		if err := (&repo.Importer{Repo: r, DataDir: cfg.DataDir}).SeedAll(ctx, cfg.SeedDir); err != nil {
			log.Fatalf("[seed] 种子题目导入失败: %v", err)
		}
	}

	// 隐藏期结束的比赛定期发布：榜单/提交记录并入公开态、题目解锁、历史代码恢复可见
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := r.PublishDueContests(ctx, time.Now()); err != nil {
					log.Printf("[publish] %v", err)
				} else if n > 0 {
					log.Printf("[publish] 已发布 %d 场到期的比赛", n)
				}
			}
		}
	}()

	// 判题线程池
	jd := &judge.Judger{Repo: r, Sandbox: sb, Provider: store, Name: hostname()}
	jd.Pool(ctx, cfg.JudgeWorkers)

	if *judgeOnly {
		log.Printf("[judge] 独立判题进程启动: workers=%d sandbox=%s", cfg.JudgeWorkers, cfg.SandboxAddr)
		<-ctx.Done()
		return
	}
	server.Run(&server.Deps{Cfg: cfg, Repo: r, Sandbox: sb})
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "judge"
	}
	return h
}
