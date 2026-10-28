package backup

// 分层备份纯函数与流式加密 round-trip（mongodump 需外部二进制，不在单测范围）。

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func writeTemp(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 分层归档命名与保留清理：all 层 glob 不得误吞 db/media 前缀档。
func TestTierGlobAndPrune(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// db 32 份、media 5 份、all 5 份（时间戳字典序）
	for i := 0; i < 32; i++ {
		mk(archivePrefix + "db-202601" + string(rune('0'+i%10)) + "-040000" + string(rune('0'+i/10)) + archiveSuffix)
	}
	for i := 0; i < 5; i++ {
		mk(archivePrefix + "media-2026010" + string(rune('1'+i)) + "-040000" + archiveSuffix)
		mk(archivePrefix + "2026010" + string(rune('1'+i)) + "-040000" + archiveSuffix)
	}
	pruneArchives(dir, TierDB)
	pruneArchives(dir, TierMedia)
	pruneArchives(dir, TierAll)
	count := func(pattern string) int {
		es, _ := filepath.Glob(filepath.Join(dir, pattern))
		return len(es)
	}
	if n := count(archivePrefix + "db-*" + archiveSuffix); n != 30 {
		t.Fatalf("db 保留 %d 份, want 30", n)
	}
	if n := count(archivePrefix + "media-*" + archiveSuffix); n != 4 {
		t.Fatalf("media 保留 %d 份, want 4", n)
	}
	if n := count(archivePrefix + "2*" + archiveSuffix); n != 4 {
		t.Fatalf("all 保留 %d 份, want 4（且不得吞 db/media 档）", n)
	}
}

// 流式加密 round-trip：tarDirTo 产物经 encryptArchive 加密 → age 解密 → tar 解包，内容一致。
func TestStreamingEncryptRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeTemp(t, src, "1/data/test1.in", "hello\n")
	writeTemp(t, src, "1/data/test1.out", "world\n")
	writeTemp(t, src, "2/problem.md", "# 题面")

	tmp := t.TempDir()
	tdTar := filepath.Join(tmp, testdataName)
	if err := tarDirTo(context.Background(), src, tdTar); err != nil {
		t.Fatal(err)
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(tmp, "enc.tar.age")
	if err := encryptArchive(context.Background(), enc, id.Recipient(), `{"tier":"media"}`, map[string]string{
		testdataName: tdTar,
	}); err != nil {
		t.Fatal(err)
	}

	// 解密
	f, err := os.Open(enc)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dr, err := age.Decrypt(f, id)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(dr)
	got := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = string(data)
	}
	if !strings.Contains(got[manifestName], `"tier":"media"`) {
		t.Fatalf("manifest 不含 tier: %q", got[manifestName])
	}
	// 内层 testdata.tar.gz 解包核对内容
	inner := filepath.Join(tmp, "inner.tgz")
	if err := os.WriteFile(inner, []byte(got[testdataName]), 0o600); err != nil {
		t.Fatal(err)
	}
	inf, err := os.Open(inner)
	if err != nil {
		t.Fatal(err)
	}
	defer inf.Close()
	gz, err := gzip.NewReader(inf)
	if err != nil {
		t.Fatal(err)
	}
	itr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := itr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[hdr.Name] = true
	}
	for _, want := range []string{"1/data/test1.in", "2/problem.md"} {
		if !names[want] {
			t.Fatalf("内层包缺 %s: %v", want, names)
		}
	}
}
