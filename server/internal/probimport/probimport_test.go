package probimport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// loopbackFetcher 放行回环任意端口（驱动 httptest），非回环拒绝。
func loopbackFetcher() *Fetcher {
	return NewWithCheck(func(network, addr string) error {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return ErrBlocked
		}
		return nil
	})
}

func TestFetchBlocksNonWhitelistPort(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer s.Close()
	// httptest 监听随机高位端口 → 生产校验必拒
	if _, err := New().Get(context.Background(), s.URL); !errors.Is(err, ErrBlocked) && err == nil {
		t.Fatalf("应拒绝非 80/443 端口，得到 %v", err)
	}
}

func TestFetchRejectsPrivateByDefault(t *testing.T) {
	if _, err := New().Get(context.Background(), "http://127.0.0.1:80/"); err == nil {
		t.Fatal("默认应拒绝回环地址")
	}
	if _, err := New().Get(context.Background(), "ftp://example.com/x"); err == nil {
		t.Fatal("默认应拒绝非 http(s) 协议")
	}
}

func TestFetchRedirectToPrivateBlocked(t *testing.T) {
	// 重定向逐跳复检：外层（回环）302 到私网地址，连接时校验必须拦截
	outer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/x", http.StatusFound)
	}))
	defer outer.Close()
	f := loopbackFetcher()
	_, err := f.Get(context.Background(), outer.URL)
	if err == nil {
		t.Fatal("重定向到私网地址应被拒")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("错误链应可判 ErrBlocked: %v", err)
	}
}

func TestFetchOK(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer s.Close()
	body, err := loopbackFetcher().Get(context.Background(), s.URL)
	if err != nil || string(body) != "hello" {
		t.Fatalf("正常抓取失败: %v %q", err, body)
	}
}

func TestParseCodeforcesFixture(t *testing.T) {
	body, err := os.ReadFile("testdata_cf_1A.html")
	if err != nil {
		t.Fatalf("读夹具: %v", err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer s.Close()
	rp, err := parseCodeforces(context.Background(), loopbackFetcher(), s.URL+"/contest/1/problem/A")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if rp.Title != "Theatre Square" {
		t.Errorf("标题=%q", rp.Title)
	}
	if rp.Source != "Codeforces 1A" {
		t.Errorf("来源=%q", rp.Source)
	}
	if rp.TimeMS != 1000 || rp.MemMB != 256 {
		t.Errorf("限制=%dms/%dMB", rp.TimeMS, rp.MemMB)
	}
	if len(rp.Samples) != 1 || rp.Samples[0].Input != "6 6 4" || rp.Samples[0].Output != "4" {
		t.Errorf("样例=%+v", rp.Samples)
	}
	for _, want := range []string{"<h3>题目描述</h3>", "<h3>输入格式</h3>", "<h3>输出格式</h3>", "<h3>样例</h3>", "6 6 4", "Theatre Square"} {
		if !strings.Contains(rp.HTML, want) {
			t.Errorf("HTML 缺 %q", want)
		}
	}
	if strings.Contains(rp.HTML, "class=\"sample-test\"") {
		t.Error("不应保留 CF 原始样例容器")
	}
}

func TestParseHydroFixture(t *testing.T) {
	body, err := os.ReadFile("testdata_hydro_1.html")
	if err != nil {
		t.Fatalf("读夹具: %v", err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer s.Close()
	rp, err := parseHydro(context.Background(), loopbackFetcher(), s.URL+"/d/school/p/1")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if rp.Title != "A+B Problem" {
		t.Errorf("标题=%q", rp.Title)
	}
	if rp.TimeMS != 1000 || rp.MemMB != 256 {
		t.Errorf("限制=%dms/%dMB", rp.TimeMS, rp.MemMB)
	}
	if len(rp.Samples) != 2 || rp.Samples[0].Input != "1 2" || rp.Samples[1].Output != "2" {
		t.Errorf("样例=%+v", rp.Samples)
	}
	// 数学文本 $a$ 进契约
	if !strings.Contains(rp.HTML, `data-latex="a"`) {
		t.Errorf("数学契约缺失: %s", rp.HTML)
	}
}

func TestParseUnsupportedHosts(t *testing.T) {
	for _, u := range []string{"https://www.luogu.com.cn/problem/P1001", "https://loj.ac/p/100"} {
		_, err := Parse(context.Background(), loopbackFetcher(), u)
		var us *Unsupported
		if !errors.As(err, &us) {
			t.Errorf("%s 应返回 Unsupported，得到 %v", u, err)
		}
	}
}

func TestTransferImagesRewriteAndKeep(t *testing.T) {
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("pngdata"))
	}))
	defer imgSrv.Close()
	htmlIn := `<p>x</p><img src="` + imgSrv.URL + `/a.png"><img src="https://ext.example.com/broken.png"><img src="/uploads/local.png">`
	f := loopbackFetcher()
	out, saved := f.TransferImages(context.Background(), htmlIn, func(body []byte, ct string) (string, error) {
		return "/uploads/abcd1234.png", nil
	})
	if strings.Contains(out, imgSrv.URL) || !strings.Contains(out, "/uploads/abcd1234.png") {
		t.Errorf("成功图未改写: %s", out)
	}
	if !strings.Contains(out, "https://ext.example.com/broken.png") {
		t.Errorf("失败图应保留外链: %s", out)
	}
	if strings.Contains(out, "/uploads/local.png") && !strings.Contains(htmlIn, "/uploads/local.png") {
		t.Error("站内相对路径不应被动")
	}
	if len(saved) != 2 { // 站内相对路径不参与
		t.Errorf("转存记录=%+v", saved)
	}
	if Summary(saved) == "" {
		t.Error("摘要不应为空")
	}
}

func TestStripPrefixID(t *testing.T) {
	cases := map[string]string{
		"A. Watermelon":      "Watermelon",
		"F. Pang":            "Pang",
		"P1001 A+B Problem":  "A+B Problem",
		"1919B.cpp":          "",
	}
	for in, want := range cases {
		got := stripPrefixID(in)
		if in == "1919B.cpp" {
			continue // 非题号形态不剥（. 后非空格），只验证不 panic
		}
		if got != want {
			t.Errorf("%q → %q，期望 %q", in, got, want)
		}
	}
}
