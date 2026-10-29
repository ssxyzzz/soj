// Package mailer 邮件通道（v0.16）：SMTP 与 log 双模式 provider + 短信空接口。
// SMTP 走 Go 标准库 net/smtp（服务器支持 STARTTLS 时自动升级）；log 模式不外发、
// 打服务端日志——开发调试与校内小规模部署（老师人工转告验证码）不依赖外部服务。
package mailer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
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

// SMTP 会话超时（v0.25 万题体检）：验证码发送在 HTTP 请求内同步执行，通知走 goroutine，
// 原 smtp.SendMail 裸拨号无任何超时——SMTP 端口被防火墙 drop 时请求挂起 2 分钟+，
// goroutine 版则无限泄漏。会话各步与 smtp.SendMail 同口径（STARTTLS 自动升级等）。
const (
	smtpDialTimeout  = 10 * time.Second
	smtpSendDeadline = 30 * time.Second // 拨号成功后的会话整体 deadline
)

func (s *SMTP) Send(to, subject, body string) error {
	if !s.cfg.Ready() {
		return fmt.Errorf("SMTP 配置不完整")
	}
	host := s.cfg.Host
	addr := net.JoinHostPort(host, strconv.Itoa(s.cfg.Port))
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
		a = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, host)
	}

	conn, err := net.DialTimeout("tcp", addr, smtpDialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(smtpSendDeadline))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err = c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if a != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("smtp: server doesn't support AUTH")
		}
		if err = c.Auth(a); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ---- log 模式 ----

// Log 打日志模式：验证码不外发，服务端日志可查（人工转告）。
type Log struct{}

func (Log) Send(to, subject, body string) error {
	slog.Info("邮件 log 通道：不外发，人工转告", "component", "mailer", "to", to, "subject", subject, "body", body)
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
