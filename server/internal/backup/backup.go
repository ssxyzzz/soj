// Package backup 实现全站加密备份（v0.4.9）：
// mongodump 归档 + 测试数据卷打包 → 单个 tar → age 加密，
// 产物 soj-backup-<时间戳>.v1.tar.age（文件名自带格式版本号，恢复按版本识别）。
//
// 密钥模式由密钥卷文件推导（不进 Mongo——私钥入库会随 mongodump 打进备份文件，加密自欺）：
//   - backup.pub 存在  → Tier 1（离线私钥：服务器只存公钥，私钥仅在启用时一次性展示）
//   - 否则 backup.key 存在 → Tier 0（服务器保管私钥，首次备份自动生成，0600）
//   - 两者皆无 → 未初始化（下次备份自动落入 Tier 0）
//
// 升级 Tier 1 = 生成新身份、只写公钥、删除私钥——旧密钥与全部历史密文从此作废；
// 不提供降级（回退 = 删除密钥卷文件重新初始化 + 全量备份）。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
)

const (
	KeyFileName   = "backup.key" // Tier 0 私钥
	PubFileName   = "backup.pub" // Tier 1 公钥
	manifestName  = "manifest.json"
	mongoDumpName = "mongo.archive.gz"
	testdataName  = "testdata.tar.gz"
	uploadsName   = "uploads.tar.gz" // 题面图片（v0.5a 起；旧归档无此文件，恢复端自动跳过）
	archivePrefix = "soj-backup-"
	archiveSuffix = ".v1.tar.age"
)

type Mode string

const (
	ModeUninitialized Mode = "uninitialized"
	ModeTier0         Mode = "tier0"
	ModeTier1         Mode = "tier1"
)

// ErrBusy 同一时刻仅允许一个备份任务（进程内串行，覆盖 HTTP 与 CLI 两条入口）。
var ErrBusy = errors.New("已有备份任务进行中，请稍后再试")

var runMu sync.Mutex

// Options 一次备份所需的全部输入。所有字段均来自服务端配置（环境变量），
// 不含任何用户输入；外部命令以 argv 数组固定调用 mongodump，不经 shell。
type Options struct {
	MongoURI   string    // 传给 mongodump 的连接串（SOJ_MONGO_URI）
	DataDir    string    // 测试数据卷根目录（SOJ_DATA_DIR）
	UploadsDir string    // 题面图片目录（SOJ_UPLOADS_DIR，v0.5a 起）
	BackupDir  string    // 密文归档输出目录（服务器留存一份）
	KeysDir    string    // 密钥卷目录
	Now        time.Time // 测试注入用；零值取当前时间
}

// Status 密钥模式与最近一次备份信息（纯文件推导，不查库）。
type Status struct {
	Mode           Mode
	LastBackupAt   time.Time
	LastBackupFile string
}

// Inspect 汇总密钥卷与备份目录的当前状态。
func Inspect(keysDir, backupDir string) Status {
	st := Status{Mode: modeOf(keysDir)}
	entries, _ := filepath.Glob(filepath.Join(backupDir, archivePrefix+"*"+archiveSuffix))
	for _, f := range entries {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		if fi.ModTime().After(st.LastBackupAt) {
			st.LastBackupAt = fi.ModTime()
			st.LastBackupFile = filepath.Base(f)
		}
	}
	return st
}

// UpgradeTier1 升级为离线私钥模式：生成新身份、只写公钥、删除旧私钥。
// 返回的私钥仅此一次出现机会，调用方不得落日志/落库。
func UpgradeTier1(keysDir string) (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", fmt.Errorf("生成密钥对失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(keysDir, PubFileName), []byte(id.Recipient().String()+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("写入公钥失败: %w", err)
	}
	_ = os.Remove(filepath.Join(keysDir, KeyFileName)) // 旧私钥作废（不存在则忽略）
	return id.String(), nil
}

// Run 执行一次完整备份（串行），返回密文归档路径（同时为服务器留存件）。
func Run(ctx context.Context, o Options) (string, error) {
	if !runMu.TryLock() {
		return "", ErrBusy
	}
	defer runMu.Unlock()

	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := os.MkdirAll(o.BackupDir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	if err := os.MkdirAll(o.KeysDir, 0o700); err != nil {
		return "", fmt.Errorf("创建密钥目录失败: %w", err)
	}
	rcpt, err := ensureRecipient(o.KeysDir) // 未初始化自动落入 Tier 0
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp(o.BackupDir, ".tmp-backup-")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmp)

	mongoPath := filepath.Join(tmp, mongoDumpName)
	if err := runMongodump(ctx, o.MongoURI, mongoPath); err != nil {
		return "", err
	}
	tdPath := filepath.Join(tmp, testdataName)
	if err := tarDirTo(o.DataDir, tdPath); err != nil {
		return "", fmt.Errorf("打包测试数据失败: %w", err)
	}
	upPath := filepath.Join(tmp, uploadsName)
	if err := tarDirTo(o.UploadsDir, upPath); err != nil {
		return "", fmt.Errorf("打包题面图片失败: %w", err)
	}

	out := filepath.Join(o.BackupDir, archivePrefix+now.Format("20060102-150405")+archiveSuffix)
	tmpOut := out + ".part"
	manifest := fmt.Sprintf(`{"format":%q,"version":%d,"created_at":%q}`,
		FormatTag(), FormatVersion(), now.Format(time.RFC3339))
	mongoData, err := os.ReadFile(mongoPath)
	if err != nil {
		return "", fmt.Errorf("读取 mongodump 归档失败: %w", err)
	}
	tdData, err := os.ReadFile(tdPath)
	if err != nil {
		return "", fmt.Errorf("读取测试数据包失败: %w", err)
	}
	upData, err := os.ReadFile(upPath)
	if err != nil {
		return "", fmt.Errorf("读取题面图片包失败: %w", err)
	}
	if err := encryptArchive(tmpOut, rcpt, map[string][]byte{
		manifestName:  []byte(manifest),
		mongoDumpName: mongoData,
		testdataName:  tdData,
		uploadsName:   upData,
	}); err != nil {
		os.Remove(tmpOut)
		return "", fmt.Errorf("加密打包失败: %w", err)
	}
	if err := os.Rename(tmpOut, out); err != nil {
		return "", fmt.Errorf("落盘失败: %w", err)
	}
	return out, nil
}

// FormatTag 归档格式标识（restore.sh 按此识别）。
func FormatTag() string { return "soj-backup" }

// FormatVersion 归档格式版本号（文件名 .v1 与 manifest 双重携带）。
func FormatVersion() int { return 1 }

// ---- 内部实现 ----

func modeOf(keysDir string) Mode {
	switch {
	case fileExists(filepath.Join(keysDir, PubFileName)):
		return ModeTier1
	case fileExists(filepath.Join(keysDir, KeyFileName)):
		return ModeTier0
	default:
		return ModeUninitialized
	}
}

func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }

// ensureRecipient 返回当前模式的加密公钥；未初始化时生成密钥对落入 Tier 0。
func ensureRecipient(keysDir string) (age.Recipient, error) {
	switch modeOf(keysDir) {
	case ModeTier1:
		pubs, err := age.ParseRecipients(strings.NewReader(string(mustRead(filepath.Join(keysDir, PubFileName)))))
		if err != nil || len(pubs) == 0 {
			return nil, fmt.Errorf("解析离线公钥失败: %w", err)
		}
		return pubs[0], nil
	case ModeTier0:
		id, err := age.ParseX25519Identity(strings.TrimSpace(string(mustRead(filepath.Join(keysDir, KeyFileName)))))
		if err != nil {
			return nil, fmt.Errorf("解析服务器私钥失败（密钥卷可能损坏）: %w", err)
		}
		return id.Recipient(), nil
	default:
		id, err := age.GenerateX25519Identity()
		if err != nil {
			return nil, fmt.Errorf("生成 Tier 0 密钥对失败: %w", err)
		}
		if err := os.WriteFile(filepath.Join(keysDir, KeyFileName), []byte(id.String()+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("写入私钥失败: %w", err)
		}
		return id.Recipient(), nil
	}
}

// runMongodump 执行 mongodump --archive --gzip 输出到 outPath。
// 命令名固定字面量、参数走 argv 数组（不经 shell）；uri 来自服务端配置而非用户输入。
func runMongodump(ctx context.Context, uri, outPath string) error {
	cmd := exec.Command("mongodump", "--uri", uri, "--archive", "--gzip")
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建 mongodump 输出失败: %w", err)
	}
	defer f.Close()
	cmd.Stdout = f
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mongodump 失败: %v：%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// tarDirTo 把 src 目录打成 gzip 压缩 tar（条目相对 src 根；目录不存在时产出空包）。
func tarDirTo(src, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	closeAll := func() error {
		if err := tw.Close(); err != nil {
			return err
		}
		return gw.Close()
	}

	if _, err := os.Stat(src); err != nil {
		return closeAll() // 空卷：产出空包
	}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == src {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if err != nil {
		return err
	}
	return closeAll()
}

// encryptArchive 把 files（归档内名 → 内容字节）打包进单个 tar 并 age 加密写入 outPath。
func encryptArchive(outPath string, rcpt age.Recipient, files map[string][]byte) error {
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	w, err := age.Encrypt(out, rcpt)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)

	// 固定写入顺序：manifest 在前，保证恢复端可先读元数据
	for _, name := range []string{manifestName, mongoDumpName, testdataName, uploadsName} {
		data, ok := files[name]
		if !ok {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	// age 尾部标头必须在底层文件关闭前写完
	return w.Close()
}

func mustRead(p string) []byte {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return data
}
