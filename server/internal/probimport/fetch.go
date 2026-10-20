// Package probimport：题目 URL 导入（v0.6.1）——SSRF 安全抓取 + 各站适配器。
//
// 安全口径（plan.md §2.3 验收标准）：仅 http/https、端口 80/443；连接时校验目标 IP
// （net.Dialer.Control，防 DNS rebinding 的 TOCTOU），拒环回/私网/链路本地/组播/未指定；
// 重定向逐跳复检；响应体限量；固定 UA。
package probimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	neturl "net/url"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultMaxBytes = 2 << 20 // 页面 2MB
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 SOJ-ProblemImport/1.0"
)

// Fetcher SSRF 安全的受限 HTTP 客户端。零值不可用，经 New 构造。
type Fetcher struct {
	hc      *http.Client
	maxBytes int64
}

// ErrBlocked 目标被安全策略拒绝（私网地址、非白名单端口/协议等）。
var ErrBlocked = errors.New("目标地址被安全策略拒绝")

// New 构造默认 Fetcher（生产口径）。
func New() *Fetcher {
	return newFetcher(checkAddr)
}

// NewWithCheck 测试注入自定义地址校验（如放行回环以驱动 httptest）。
func NewWithCheck(check func(network, addr string) error) *Fetcher {
	return newFetcher(check)
}

func newFetcher(check func(network, addr string) error) *Fetcher {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
		return check(network, address)
	}}
	return &Fetcher{
		hc: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{DialContext: d.DialContext},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
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

// Get 抓取 URL，返回限长响应体。
func (f *Fetcher) Get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 不合法: %w", err)
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	resp, err := f.hc.Do(req)
	if err != nil {
		// 错误链保持完整（url.Error → OpError → ErrBlocked），errors.Is 可判拦截原因，
		// 文本自带「拒绝内网/保留地址 x.x.x.x」与拨号目标
		return nil, fmt.Errorf("抓取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("站点返回 %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && strings.Contains(ct, "text/html") && resp.ContentLength > f.maxBytes {
		return nil, fmt.Errorf("页面超过 %s 上限", humanBytes(f.maxBytes))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if int64(len(body)) > f.maxBytes {
		return nil, fmt.Errorf("页面超过 %s 上限", humanBytes(f.maxBytes))
	}
	return body, nil
}

// GetImage 抓取图片：Content-Type 必须 image/*，上限 5MB（题面外链图转存用）。
func (f *Fetcher) GetImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("图片抓取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("图片返回 %s", resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", fmt.Errorf("非图片类型 %q", ct)
	}
	imgMax := int64(5 << 20)
	body, err := io.ReadAll(io.LimitReader(resp.Body, imgMax+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > imgMax {
		return nil, "", fmt.Errorf("图片超过 5MB 上限")
	}
	return body, ct, nil
}

func humanBytes(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + "MB"
	}
	return strconv.FormatInt(n>>10, 10) + "KB"
}
