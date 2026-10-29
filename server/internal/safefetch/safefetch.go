// Package safefetch：SSRF 安全的受限 HTTP 抓取（v0.31 题面外链图转存的服务端出网口；
// 安全口径自 v0.6.1 probimport 的 Fetcher 原样恢复——probimport 已随 v0.8 编辑器
// 重做退役，此处把抓取内核取回独立成包）。
//
// 安全口径：仅 http/https、端口 80/443；连接时校验目标 IP（net.Dialer.Control 拿到的
// 是已解析 IP，天然免疫 DNS rebinding 的 TOCTOU），拒环回/私网/链路本地/组播/未指定；
// 重定向逐跳复检（每跳重新拨号、Control 再校验）；响应体限量；固定 UA。
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultMaxBytes = 2 << 20 // 通用页面 2MB
	imageMaxBytes   = 5 << 20 // 单图 5MB（题面图上限，与编辑器上传同口径）
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 SOJ-AssetTransfer/1.0"
)

// ErrBlocked 目标被安全策略拒绝（私网地址、非白名单端口/协议等）。
var ErrBlocked = errors.New("目标地址被安全策略拒绝")

// Fetcher SSRF 安全的受限 HTTP 客户端。零值不可用，经 New / NewWithCheck 构造。
type Fetcher struct {
	hc       *nethttp.Client
	maxBytes int64
}

// New 生产口径（拒内网/保留段）。
func New() *Fetcher { return newFetcher(checkAddr) }

// NewWithCheck 测试注入自定义地址校验（如放行回环以驱动 httptest）。
func NewWithCheck(check func(network, addr string) error) *Fetcher { return newFetcher(check) }

func newFetcher(check func(network, addr string) error) *Fetcher {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
		return check(network, address)
	}}
	return &Fetcher{
		hc: &nethttp.Client{
			Timeout:   20 * time.Second,
			Transport: &nethttp.Transport{DialContext: d.DialContext},
			CheckRedirect: func(req *nethttp.Request, via []*nethttp.Request) error {
				if len(via) >= 4 {
					return fmt.Errorf("重定向超过 4 跳")
				}
				return checkURL(req.URL)
			},
		},
		maxBytes: defaultMaxBytes,
	}
}

// checkAddr 连接时校验（拿到的是已解析的 ip:port，天然免疫 DNS rebinding）。
func checkAddr(network, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: 地址无法解析 %q", ErrBlocked, addr)
	}
	if port != "80" && port != "443" {
		return fmt.Errorf("%w: 仅允许 80/443 端口，收到 %q", ErrBlocked, port)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: 目标不是 IP %q", ErrBlocked, host)
	}
	return checkIP(ip)
}

func checkIP(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("%w: 拒绝内网/保留地址 %s", ErrBlocked, ip)
	}
	return nil
}

func checkURL(u *neturl.URL) error {
	if u == nil {
		return fmt.Errorf("%w: 空 URL", ErrBlocked)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: 仅允许 http/https，收到 %q", ErrBlocked, u.Scheme)
	}
	return nil
}

// Get 抓取 URL，返回限长响应体（通用入口；图片转存走 GetImage）。
func (f *Fetcher) Get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 不合法: %w", err)
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.hc.Do(req)
	if err != nil {
		// 错误链保持完整（url.Error → OpError → ErrBlocked），errors.Is 可判拦截原因，
		// 文本自带「拒绝内网/保留地址 x.x.x.x」
		return nil, fmt.Errorf("抓取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		return nil, fmt.Errorf("站点返回 %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if int64(len(body)) > f.maxBytes {
		return nil, fmt.Errorf("响应超过 %s 上限", humanBytes(f.maxBytes))
	}
	return body, nil
}

// GetImage 抓取外链图（题面转存用）：≤5MB、魔数嗅探定真实类型——后缀与声明头都可
// 伪造，内容不能。CT 只做廉价拦截（明确非图的文本/页面类型拒，octet-stream/binary
// 放行交给魔数终审——实测 luogu CDN 的 CT 本就不可信：声明 png 实为 JPEG、或干脆
// octet-stream）。svg 不在白名单（XSS 载体，嗅探不出即被拒）。返回规范后缀
// （png/jpg/gif/webp，不带点）。
func (f *Fetcher) GetImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("URL 不合法: %w", err)
	}
	if err := checkURL(u); err != nil {
		return nil, "", err
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("图片抓取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		return nil, "", fmt.Errorf("图片返回 %s", resp.Status)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.HasPrefix(ct, "image/") &&
		!strings.Contains(ct, "octet-stream") && !strings.Contains(ct, "binary") {
		return nil, "", fmt.Errorf("非图片类型 %q", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, imageMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > imageMaxBytes {
		return nil, "", fmt.Errorf("图片超过 5MB 上限")
	}
	ext := DetectImageExt(body)
	if ext == "" {
		return nil, "", fmt.Errorf("图片内容无法识别（白名单：png/jpg/gif/webp）")
	}
	return body, ext, nil
}

// DetectImageExt 魔数嗅探图片类型（内容不能伪造）：返回规范后缀（png/jpg/gif/webp，
// 不带点），非白名单图片返回空串。
func DetectImageExt(b []byte) string {
	switch {
	case len(b) > 7 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G':
		return "png"
	case len(b) > 2 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	case len(b) > 5 && string(b[:3]) == "GIF":
		return "gif"
	case len(b) > 11 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

func humanBytes(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + "MB"
	}
	return strconv.FormatInt(n>>10, 10) + "KB"
}
