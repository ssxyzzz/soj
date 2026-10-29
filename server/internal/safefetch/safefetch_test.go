package safefetch

// SSRF 安全口径单测（v0.31）：内网/保留段与端口拦截、重定向逐跳复检、魔数嗅探。
// httptest 走 NewWithCheck 注入放行回环的校验（生产口径拒回环）。

import (
	"context"
	"errors"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func allowAll(network, addr string) error { return nil }

var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

func TestCheckAddrBlocksPrivateAndPorts(t *testing.T) {
	blocked := []string{
		"192.168.1.1:80", "10.0.0.1:443", "172.16.0.1:80", "127.0.0.1:443",
		"[::1]:80", "169.254.1.1:80", "0.0.0.0:80", "224.0.0.1:80",
		"1.2.3.4:8080", "1.2.3.4:8443", "1.2.3.4:22",
	}
	for _, addr := range blocked {
		if err := checkAddr("tcp", addr); !errors.Is(err, ErrBlocked) {
			t.Fatalf("checkAddr(%q) 应被拦截，得到 %v", addr, err)
		}
	}
	if err := checkAddr("tcp", "1.2.3.4:443"); err != nil {
		t.Fatalf("公网 443 不应拦截: %v", err)
	}
}

func TestCheckURLScheme(t *testing.T) {
	for _, raw := range []string{"ftp://x/1.png", "file:///etc/passwd"} {
		u, _ := url.Parse(raw)
		if err := checkURL(u); !errors.Is(err, ErrBlocked) {
			t.Fatalf("checkURL(%q) 应被拦截，得到 %v", raw, err)
		}
	}
}

func TestGetImageSniffAndRejects(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case "/liar.png": // 声明 png 实为 svg 文本
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<svg onload=alert(1)>"))
		case "/raw": // octet-stream + 真 png（CT 不可信场景，交给魔数终审）
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pngBytes)
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(pngBytes)
		default:
			nethttp.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := NewWithCheck(allowAll)
	ctx := context.Background()

	body, ext, err := f.GetImage(ctx, srv.URL+"/ok.png")
	if err != nil || ext != "png" || len(body) != len(pngBytes) {
		t.Fatalf("合法 png 应通过: ext=%q err=%v", ext, err)
	}
	if _, ext2, err := f.GetImage(ctx, srv.URL+"/raw"); err != nil || ext2 != "png" {
		t.Fatalf("octet-stream 的真 png 应由魔数放行: ext=%q err=%v", ext2, err)
	}
	if _, _, err := f.GetImage(ctx, srv.URL+"/liar.png"); err == nil || !strings.Contains(err.Error(), "无法识别") {
		t.Fatalf("伪装 png 的 svg 应被魔数拒绝，得到 %v", err)
	}
	if _, _, err := f.GetImage(ctx, srv.URL+"/page"); err == nil || !strings.Contains(err.Error(), "非图片") {
		t.Fatalf("html 内容应被 Content-Type 拒绝，得到 %v", err)
	}
	if _, _, err := f.GetImage(ctx, srv.URL+"/404.png"); err == nil {
		t.Fatal("404 应报错")
	}
}

// 重定向逐跳复检：放行首跳端口、拦截重定向目标端口——证明 CheckRedirect 后的拨号
// 依然过 Control（DNS rebinding / 私网跳转不可达）。
func TestRedirectRecheckDialsThroughControl(t *testing.T) {
	target := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer target.Close()

	src := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		nethttp.Redirect(w, r, target.URL+"/x.png", nethttp.StatusFound)
	}))
	defer src.Close()
	srcPort := strconv.Itoa(src.Listener.Addr().(*net.TCPAddr).Port)

	f := NewWithCheck(func(_ string, addr string) error {
		if strings.HasSuffix(addr, ":"+srcPort) {
			return nil // 仅放行首跳
		}
		return checkAddr("tcp", addr) // 生产口径：回环一律拒
	})
	if _, _, err := f.GetImage(context.Background(), src.URL+"/redirect.png"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("重定向到回环目标应被拦截（ErrBlocked），得到 %v", err)
	}
}

func TestDetectImageExt(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want string
	}{
		{"png", pngBytes, "png"},
		{"jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "jpg"},
		{"gif", []byte("GIF89a......"), "gif"},
		{"webp", append([]byte("RIFF....WEBP"), 0, 0), "webp"},
		{"svg 文本", []byte("<svg/>"), ""},
		{"空", nil, ""},
	}
	for _, c := range cases {
		if got := DetectImageExt(c.b); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
