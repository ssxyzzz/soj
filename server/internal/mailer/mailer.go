// Package mailer 邮件通道（v0.16）：SMTP 与 log 双模式 provider + 短信空接口。
// SMTP 走 Go 标准库 net/smtp（服务器支持 STARTTLS 时自动升级）；log 模式不外发、
// 打服务端日志——开发调试与校内小规模部署（老师人工转告验证码）不依赖外部服务。
package mailer

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

// Provider 邮件发送通道。实现必须可并发调用。
type Provider interface {
	Send(to, subject, body string) error
}

// SmsProvider 短信通道（v0.16 只留接口，手机注册/找回随短信通道接入解锁）。
type SmsProvider interface {
	Send(ctx context.Context, to, body string) error
}

// NoopSms 空实现：UI 不启用手机号，接口占位。
type NoopSms struct{}

func (NoopSms) Send(context.Context, string, string) error {
	return fmt.Errorf("短信通道未接入")
}

// ---- SMTP ----

// SMTPConfig SMTP 连接参数（后台「系统设置」维护，落 settings 集合）。
type SMTPConfig struct {
	Host     string // SMTP 主机，如 smtp.qq.com
	Port     int    // 端口（465/587 常见；0 视为未配置）
	Username string // 登录账号（多为邮箱地址）
	Password string // 授权码（非登录密码）
	From     string // 发件人（显示地址，需与服务商要求一致）
}

// Ready 配置是否足以发信（通道开 smtp 时的启动校验与运行期兜底共用）。
func (c SMTPConfig) Ready() bool {
	return c.Host != "" && c.Port > 0 && c.Port < 65536 && c.From != ""
}

// SMTP SMTP 发件实现。
type SMTP struct {
	cfg SMTPConfig
}

func NewSMTP(cfg SMTPConfig) *SMTP { return &SMTP{cfg: cfg} }

func (s *SMTP) Send(to, subject, body string) error {
	if !s.cfg.Ready() {
		return fmt.Errorf("SMTP 配置不完整")
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	from := s.cfg.From
	// Header 用 UTF-8 base64 编码主题，避免中文主题被投递链路咬掉
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mimeSubject(subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString([]byte(body)),
	}, "\r\n")
	var a smtp.Auth
	if s.cfg.Username != "" {
		a = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}
	return smtp.SendMail(addr, a, from, []string{to}, []byte(msg))
}

// ---- log 模式 ----

// Log 打日志模式：验证码不外发，服务端日志可查（人工转告）。
type Log struct{}

func (Log) Send(to, subject, body string) error {
	log.Printf("[mailer:log] to=%s subject=%q body=%q", to, subject, body)
	return nil
}

// ---- 模板渲染 ----

// Render 模板占位符替换：{{code}}/{{username}}/{{site}}/{{action}}。
// 占位符缺失保持原样（后台模板写错不至于发空信）。
func Render(tpl string, vars map[string]string) string {
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func mimeSubject(subject string) string {
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(subject)) + "?="
}
