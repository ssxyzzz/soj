package server

// 邮箱验证码 handler 层纯函数单测（v0.16）：邮箱归一/格式、发码频控文案。
// 依赖真实 Mongo 的生命周期行为见 repo/verifycode_test.go；SMTP 真发信由部署后 e2e。

import (
	"strings"
	"testing"
	"time"

	"soj/internal/repo"
)

func TestNormalizeEmailAndRegex(t *testing.T) {
	if got := NormalizeEmail("  Student@Example.COM "); got != "student@example.com" {
		t.Fatalf("归一=%q", got)
	}
	if !emailRE.MatchString("a+b-tag@sub.domain.edu.cn") {
		t.Error("合法邮箱应通过")
	}
	for _, bad := range []string{"no-at-sign", "@no-local.com", "user@", "a b@x.com", "user@no-tld"} {
		if emailRE.MatchString(bad) {
			t.Errorf("非法邮箱 %q 不应通过", bad)
		}
	}
}

func TestCodeRateCheck(t *testing.T) {
	base := repo.VerifyCodeRateQuery{}
	if msg := codeRateCheck(&base); msg != "" {
		t.Fatalf("无频控命中应放行，got %q", msg)
	}
	// 冷却期：文案带分钟数且向上取整
	cool := base
	cool.LastAt = time.Now().Add(-20 * time.Second)
	if msg := codeRateCheck(&cool); !strings.Contains(msg, "1 分钟") {
		t.Fatalf("冷却 40s 应提示约 1 分钟，got %q", msg)
	}
	hourly := base
	hourly.Target1h = 5
	if codeRateCheck(&hourly) == "" {
		t.Fatal("小时上限应拦截")
	}
	daily := base
	daily.Target1d = 10
	if codeRateCheck(&daily) == "" {
		t.Fatal("目标日上限应拦截")
	}
	ip := base
	ip.IP1d = 30
	if codeRateCheck(&ip) == "" {
		t.Fatal("IP 日上限应拦截")
	}
	global := base
	global.Global1d = 300
	if codeRateCheck(&global) == "" {
		t.Fatal("全局日上限应拦截")
	}
	// 频控优先级：冷却 > 小时 > 目标日 > IP > 全局（先命中的先报）
	mixed := repo.VerifyCodeRateQuery{LastAt: time.Now(), Target1h: 99, Global1d: 999}
	if msg := codeRateCheck(&mixed); !strings.Contains(msg, "频繁") {
		t.Fatalf("冷却应优先于其他层，got %q", msg)
	}
}

func TestGenCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		c := genCode()
		if len(c) != 6 {
			t.Fatalf("验证码应 6 位，got %q", c)
		}
		for _, ch := range c {
			if ch < '0' || ch > '9' {
				t.Fatalf("验证码应纯数字，got %q", c)
			}
		}
		seen[c] = true
	}
	if len(seen) < 10 {
		t.Fatalf("50 次生成重复度过高（%d 种），疑似熵源问题", len(seen))
	}
}
