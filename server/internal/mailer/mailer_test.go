package mailer

// 模板渲染与 SMTP 配置就绪判断（v0.16）单测。SMTP 真实发信路径不单测（依赖外部
// 服务），由部署后端到端验证。

import "testing"

func TestRender(t *testing.T) {
	tpl := "【{{site}}】验证码 {{code}}，账号 {{username}} {{action}}"
	got := Render(tpl, map[string]string{
		"code": "123456", "username": "stu", "site": "SOJ", "action": "密码已重置",
	})
	want := "【SOJ】验证码 123456，账号 stu 密码已重置"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
	// 未知占位符与缺失变量保持原样（模板写错不至于发空信）
	keep := Render("a {{nocode}} b", map[string]string{"code": "1"})
	if keep != "a {{nocode}} b" {
		t.Fatalf("缺失变量应保持原样，got %q", keep)
	}
}

func TestSMTPReady(t *testing.T) {
	cases := []struct {
		cfg  SMTPConfig
		want bool
	}{
		{SMTPConfig{Host: "smtp.qq.com", Port: 465, From: "a@b.com"}, true},
		{SMTPConfig{Host: "", Port: 465, From: "a@b.com"}, false},
		{SMTPConfig{Host: "smtp.qq.com", Port: 0, From: "a@b.com"}, false},
		{SMTPConfig{Host: "smtp.qq.com", Port: 465}, false},
	}
	for i, c := range cases {
		if got := c.cfg.Ready(); got != c.want {
			t.Fatalf("#%d Ready=%v want %v", i, got, c.want)
		}
	}
}
