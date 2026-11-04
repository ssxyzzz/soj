package main

// 基线报告：端点延迟分位表 + 判题链路 + 榜单轮询特征 + 口径注记。

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type reportInput struct {
	at      time.Time
	target  string
	version string
	nUsers  int
	uidBase int64
	prefix  string
	pid     int64
	cid     int64
	m       *metrics
	exam    []subOutcome
	board   []boardPoint
}

func buildReport(in reportInput) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		"# SOJ 考试场景压测基线（v0.66 批①）\n\n"+
			"- 时间：%s\n- 目标：%s（version `%s`）\n"+
			"- 虚拟用户：%d（uid %d-%d，%s_u001..%s_u%03d，admin 角色）\n"+
			"- 题目 pid=%d，比赛 cid=%d（running / hide_during=false / scoreboard=true）\n",
		in.at.Format("2006-01-02 15:04:05"), in.target, in.version, in.nUsers,
		in.uidBase+1, in.uidBase+int64(in.nUsers), in.prefix, in.prefix, in.nUsers, in.pid, in.cid))

	b.WriteString("\n## 端点延迟（毫秒）\n\n| 端点 | n | 失败 | p50 | p95 | p99 | max |\n|---|---|---|---|---|---|---|\n")
	for _, ep := range in.m.endpoints() {
		s, errs := in.m.snapshot(ep)
		if len(s) == 0 {
			continue
		}
		sort.Float64s(s)
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %.0f | %.0f | %.0f | %.0f |\n",
			ep, len(s), errs, percentile(s, 0.5), percentile(s, 0.95), percentile(s, 0.99), s[len(s)-1]))
	}

	b.WriteString("\n## 判题链路（提交→终态，含排队）\n\n")
	var judge []float64
	finals := map[string]int{}
	var fails []string
	for _, o := range in.exam {
		if o.uname == "" {
			continue
		}
		if o.errText != "" {
			fails = append(fails, o.uname+": "+o.errText)
			continue
		}
		finals[o.final]++
		judge = append(judge, o.judgeSec)
	}
	sort.Float64s(judge)
	b.WriteString(fmt.Sprintf("- 终态分布：%s\n- 提交→终态耗时（秒）：p50 %.1f / p95 %.1f / max %.1f\n",
		dist(finals), percentile(judge, 0.5), percentile(judge, 0.95), lastOf(judge)))
	if len(fails) > 0 {
		n := min(5, len(fails))
		b.WriteString(fmt.Sprintf("- 失败 %d 例（前 %d）：%s\n", len(fails), n, strings.Join(fails[:n], "；")))
	}

	if len(in.board) > 0 {
		vals := make([]float64, len(in.board))
		for i, p := range in.board {
			vals[i] = p.ms
		}
		sort.Float64s(vals)
		b.WriteString(fmt.Sprintf(
			"\n## 榜单轮询（进行中场次，30s TTL 缓存无 single-flight）\n\n"+
				"- n=%d，p50 %.0f / p95 %.0f / p99 %.0f / max %.0f ms\n",
			len(vals), percentile(vals, 0.5), percentile(vals, 0.95), percentile(vals, 0.99), vals[len(vals)-1]))
		top := append([]boardPoint(nil), in.board...)
		sort.Slice(top, func(i, j int) bool { return top[i].ms > top[j].ms })
		b.WriteString("- 最慢 5 拍（偏移秒/毫秒；偏移邻近 30 的倍数 = 缓存到期重算特征）：")
		for i := 0; i < len(top) && i < 5; i++ {
			b.WriteString(fmt.Sprintf("%.0fs/%.0fms ", top[i].offSec, top[i].ms))
		}
		b.WriteString("\n")
	}

	b.WriteString(
		"\n## 口径注记\n\n" +
			"- 全部流量来自 loopback：IP 限流（全站 600/min、认证 30/min）对私网豁免；公网同 IP 齐发登录会先撞 429——本基线测的是无 429 干扰的裸链路。\n" +
			"- 榜单权限模型下学生（default 角色）无 contest.score.view，考试类（本班成员）才天然可看榜；本基线用 contest + admin 角色近似，榜单计算路径同构。\n" +
			"- 判题 workers=2、编译串行闸=1、自测并发闸=2 为默认配置；提交→终态耗时含编译排队。\n")
	return b.String()
}

func dist(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, m[k]))
	}
	return strings.Join(parts, "，")
}

func lastOf(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[len(sorted)-1]
}
