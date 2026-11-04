// 压测工具（v0.66 批①）：考试场景基线采集——50 并发登录风暴 → 预热浏览 →
// 赛中提交+榜单轮询（重叠，考试真实负载形态）→ 自测洪峰；产出 markdown 基线报告，
// 供批④按证据排修复优先级、后续版本作「性能第六绿」比对基线。
//
// 用法：go run ./cmd/loadtest -target http://127.0.0.1 [-users 50] [-out 报告.md] [-cleanup]
// 生产保护：目标仅允许 loopback，其他地址必须显式 -force（严禁对线上域名压测）。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/repo"
)

func main() {
	target := flag.String("target", "http://127.0.0.1", "被测站点（默认本机 compose 栈；仅允许 loopback）")
	// 宿主连 compose Mongo 必须直连：单节点副本集成员发现会切到容器内主机名
	// mongo:27017（宿主解析不了），与 repo 测试同口径（submission_test.go 注释）
	mongoURI := flag.String("mongo", "mongodb://127.0.0.1:27017/?directConnection=true", "Mongo 连接（种子用户与清理用）")
	dbName := flag.String("db", "soj", "Mongo 库名")
	nUsers := flag.Int("users", 50, "并发虚拟用户数")
	uidBase := flag.Int64("uid-base", 990000, "压测用户 uid 基号（组织者=基号，选手=基号+1..N）")
	prefix := flag.String("prefix", "ldt", "压测用户名前缀")
	out := flag.String("out", "loadtest-report.md", "基线报告输出路径")
	doCleanup := flag.Bool("cleanup", false, "跑完删除压测数据文档（用户/提交/比赛/题目；testdata 卷内小文件残留无害）")
	force := flag.Bool("force", false, "允许非 loopback 目标（自担风险）")
	flag.Parse()

	if err := run(context.Background(), cfg{
		target: *target, mongoURI: *mongoURI, dbName: *dbName,
		nUsers: *nUsers, uidBase: *uidBase, prefix: *prefix,
		out: *out, cleanup: *doCleanup, force: *force,
	}); err != nil {
		log.Fatal(err)
	}
}

type cfg struct {
	target   string
	mongoURI string
	dbName   string
	nUsers   int
	uidBase  int64
	prefix   string
	out      string
	cleanup  bool
	force    bool
}

func run(ctx context.Context, c cfg) error {
	// 生产保护：默认只打 loopback
	u, err := url.Parse(c.target)
	if err != nil {
		return fmt.Errorf("target 解析失败: %w", err)
	}
	host := u.Hostname()
	if !c.force && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("拒绝：目标 %s 不是 loopback（生产保护）；确需压测内网/其他地址请加 -force", host)
	}
	base := u.String()

	// 压测用户口令走环境变量（不进源码）；跨次复用同一值保证种子用户幂等可复登
	pass := os.Getenv("SOJ_LDT_PASSWORD")
	if pass == "" {
		return fmt.Errorf("缺 SOJ_LDT_PASSWORD：压测用户口令自行设定（例：export SOJ_LDT_PASSWORD=xxx），跨次复用同一值")
	}

	hc := &http.Client{Timeout: 60 * time.Second}

	// 健康与版本（报告头回显，基线与二进制版本对齐）
	version := ""
	if r, _, err := callJSON(hc, http.MethodGet, base, "/api/health", "", nil); err == nil && r.Code == 0 {
		var d struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(r.Data, &d)
		version = d.Version
	} else {
		return fmt.Errorf("/api/health 不可达，确认本地栈已启动：%v", err)
	}

	// Mongo：repo 种子用户（bcrypt 同源）+ 自连 client 供清理
	rp, err := repo.Connect(ctx, c.mongoURI, c.dbName)
	if err != nil {
		return fmt.Errorf("连接 Mongo 失败: %w", err)
	}
	cli, err := mongo.Connect(options.Client().ApplyURI(c.mongoURI))
	if err != nil {
		return fmt.Errorf("连接 Mongo 失败: %w", err)
	}

	// ---- 造数（幂等：用户存在即复用；题目/比赛每跑新建）----
	us := make([]user, c.nUsers+1) // us[0]=组织者，us[1..N]=选手
	for i := range us {
		us[i] = user{uid: c.uidBase + int64(i), uname: unameAt(c.prefix, i), pass: pass}
	}
	log.Printf("种子用户 %s_u000..%s_u%03d（uid %d-%d，已存在则复用）",
		c.prefix, c.prefix, c.nUsers, c.uidBase, c.uidBase+int64(c.nUsers))
	if err := seedUsers(ctx, rp, cli, c.dbName, c.prefix, c.uidBase, c.nUsers, pass); err != nil {
		return err
	}
	var pid, cid int64
	if err := loginOne(hc, base, &us[0]); err != nil {
		return fmt.Errorf("组织者登录失败: %w", err)
	}
	if pid, err = createProblem(hc, base, us[0].token,
		"LDT A+B "+time.Now().Format("0102-1504")); err != nil {
		return fmt.Errorf("建题失败: %w", err)
	}
	if cid, err = createContest(hc, base, us[0].token, pid); err != nil {
		return fmt.Errorf("建赛失败: %w", err)
	}
	log.Printf("就绪：题目 pid=%d，比赛 cid=%d（running，hide_during=false，scoreboard=true）", pid, cid)

	// ---- 场景 ----
	m := newMetrics()
	log.Printf("[1/4] 登录风暴：%d 用户同时登录", c.nUsers)
	if err := loginAll(hc, base, us, m); err != nil {
		return err
	}
	log.Printf("[2/4] 预热浏览：me / site/stats / problems / contest detail")
	runBrowse(hc, base, us, cid, m)
	log.Printf("[3/4] 赛中负载：提交 + 轮询提交状态 + 榜单轮询（重叠）")
	examRes, board := runExam(hc, base, us, cid, pid, m)
	log.Printf("[4/4] 自测洪峰：10 用户 × 2 次")
	runSelftest(hc, base, us, pid, m)

	// ---- 报告 ----
	rep := buildReport(reportInput{
		at: time.Now(), target: base, version: version, nUsers: c.nUsers,
		uidBase: c.uidBase, prefix: c.prefix, pid: pid, cid: cid,
		m: m, exam: examRes, board: board,
	})
	if err := os.WriteFile(c.out, []byte(rep), 0o644); err != nil {
		return fmt.Errorf("写报告失败: %w", err)
	}
	log.Printf("基线报告已写入 %s", c.out)

	if c.cleanup {
		if err := cleanupAll(ctx, cli, c.dbName, c.uidBase, c.nUsers, cid, pid); err != nil {
			return fmt.Errorf("清理失败: %w", err)
		}
		log.Printf("压测数据已清理（testdata 卷内 <1KB 数据文件残留，无害）")
	}
	return nil
}
