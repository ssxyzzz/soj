package repo

// 题面资产改写纯函数单测（v0.31）：相对引用改写、导出逆改写（互逆往返）、外链提取、
// 文件名形状校验。

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// fakeHex 造一个合法形状的 hex32 资产名（内容随意，形状即契约）。
func fakeHex(seed byte) string {
	sum := sha256.Sum256([]byte{seed})
	return hex.EncodeToString(sum[:16])
}

func TestRewriteRelativeAssetRefs(t *testing.T) {
	x, y := fakeHex(1)+".png", fakeHex(2)+".gif"
	md := "![a](assets/x.png) ![](./assets/y.gif) ![缺](assets/missing.png) 文字里的 assets/x.png 引用"
	out := RewriteRelativeAssetRefs(md, 7, func(ref string) (string, bool) {
		switch ref {
		case "x.png":
			return x, true
		case "y.gif":
			return y, true
		}
		return "", false
	})
	if !strings.Contains(out, "(/problems/7/assets/"+x+")") {
		t.Fatalf("相对引用未改写: %s", out)
	}
	if !strings.Contains(out, "(/problems/7/assets/"+y+")") {
		t.Fatalf("./ 前缀引用未改写: %s", out)
	}
	if !strings.Contains(out, "assets/missing.png") {
		t.Fatalf("未命中的引用应原样保留: %s", out)
	}
	// 纯文本里的 assets/x.png 也随映射改写（同一 token，形态无关）——记录该口径
	if !strings.Contains(out, "/problems/7/assets/"+x) {
		t.Fatalf("纯文本引用也应改写: %s", out)
	}
}

func TestRewriteSiteAssetsToRelative(t *testing.T) {
	own := fakeHex(3) + ".png"
	other := fakeHex(4) + ".webp"
	hosted := fakeHex(5) + ".gif"
	md := "![i](/problems/7/assets/" + own + ") " +
		"![外题](/problems/8/assets/" + other + ") " +
		`<img src="https://soj.example.com/problems/7/assets/` + hosted + `">`
	out := RewriteSiteAssetsToRelative(md, 7)
	if !strings.Contains(out, "(assets/"+own+")") {
		t.Fatalf("本题站内 URL 未逆改写: %s", out)
	}
	if !strings.Contains(out, "assets/"+hosted) || strings.Contains(out, "soj.example.com/problems/7") {
		t.Fatalf("带域名的站内 URL 未逆改写: %s", out)
	}
	if !strings.Contains(out, "/problems/8/assets/"+other) {
		t.Fatalf("他题资产引用不应改写: %s", out)
	}
}

// 导出逆改写与导入改写互逆：assets/名 → 站内 → assets/名（跨实例迁移往返自包含）。
func TestRewriteRoundTrip(t *testing.T) {
	name := fakeHex(6) + ".png"
	orig := "![图](assets/" + name + ")"
	site := RewriteRelativeAssetRefs(orig, 9, func(string) (string, bool) { return name, true })
	if got := RewriteSiteAssetsToRelative(site, 9); got != orig {
		t.Fatalf("往返不一致:\n%q\n%q", orig, got)
	}
}

func TestExtractExternalImageURLs(t *testing.T) {
	md := "![a](https://x.example/1.png)\n" +
		`<img src="https://y.example/2.png" alt="z">` + "\n" +
		"![重复](https://x.example/1.png)\n" +
		"![站内](/problems/1/assets/" + fakeHex(7) + ".png)\n" +
		"![相对](assets/local.png)\n" +
		`<img src="https://z.example/3.jpg">`
	got := ExtractExternalImageURLs(md)
	want := []string{"https://x.example/1.png", "https://y.example/2.png", "https://z.example/3.jpg"}
	if len(got) != len(want) {
		t.Fatalf("外链提取 got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("外链提取顺序/去重: got %v want %v", got, want)
		}
	}
}

func TestValidAssetName(t *testing.T) {
	ok := []string{fakeHex(8) + ".png", fakeHex(9) + ".jpeg", fakeHex(10) + ".webp"}
	for _, n := range ok {
		if !ValidAssetName(n) {
			t.Fatalf("%q 应合法", n)
		}
	}
	bad := []string{
		"x.png", fakeHex(11) + ".svg", fakeHex(12) + ".php", fakeHex(13) + ".png/../x.png",
		"../../etc/passwd.png", fakeHex(14) + ".PNG", strings.Repeat("a", 33) + ".png", "",
	}
	for _, n := range bad {
		if ValidAssetName(n) {
			t.Fatalf("%q 应被拒", n)
		}
	}
}
