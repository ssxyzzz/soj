package server

import (
	"testing"
	"time"
)

// passwordProblem（v0.14）：长度 8-64、弱口令黑名单。
// （原「不得与用户名相同/包含用户名」已按需求移除，保留用例锁定该口径）
func TestPasswordProblem(t *testing.T) {
	cases := []struct {
		name    string
		pwd     string
		wantMsg bool // 是否应拒绝
	}{
		{"过短", "a1b2c3d", true},
		{"恰 8 位通过", "a1b2c3d4", false},
		{"超长", string(make([]byte, 65)), true},
		{"弱口令", "12345678", true},
		{"弱口令大小写不敏感", "Password1", true},
		{"正常", "xk9!vq2m", false},
		{"与用户名相同已允许", "alicealice", false},
		{"包含用户名已允许", "xxalice99", false},
	}
	for _, tc := range cases {
		msg := passwordProblem(tc.pwd)
		if (msg != "") != tc.wantMsg {
			t.Errorf("%s: passwordProblem(%q) = %q, 期望拒绝=%v", tc.name, tc.pwd, msg, tc.wantMsg)
		}
	}
}

// ipLimiter 滑动窗口：达到上限拒绝、窗口滑出后恢复。
func TestIPLimiter(t *testing.T) {
	l := newIPLimiter(3, time.Minute)
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4", now) {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if l.allow("1.2.3.4", now) {
		t.Fatal("超上限应拒绝")
	}
	if !l.allow("5.6.7.8", now) {
		t.Fatal("不同 IP 独立计数")
	}
	if !l.allow("1.2.3.4", now.Add(time.Minute+time.Second)) {
		t.Fatal("窗口滑出后应恢复放行")
	}
}

// isPrivateIP：私网/回环跳过限流（frp 隧道共享出口场景），公网 IP 计数。
func TestIsPrivateIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "172.18.0.1", "192.168.1.5", "10.0.0.2", "169.254.1.1", "not-an-ip"} {
		if !isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%q) 应为 true", ip)
		}
	}
	for _, ip := range []string{"223.5.5.5", "8.8.8.8", "114.114.114.114"} {
		if isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%q) 应为 false", ip)
		}
	}
}
