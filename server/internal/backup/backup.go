// Package backup 实现全站加密备份（v0.4.9；v0.15.1 分层与流式改造）：
// mongodump 归档 + 测试数据卷/题面图片打包 → 单个 tar → age 加密。
//
// 分层（按数据变化频率设计，面向万题库）：
//   - db     每日：仅 mongodump（用户/提交/成绩/题目元数据，MB 级）→ soj-backup-db-<ts>.v1.tar.age，保留 30 份
//   - media  每周：测试数据卷 + 题面图片（低频变更、体积大头）     → soj-backup-media-<ts>.v1.tar.age，保留 4 份
//   - all    全量（面板手动按钮/兼容旧名）                         → soj-backup-<ts>.v1.tar.age，保留 4 份
//
// 加密为流式（逐文件 io.Copy 进 tar+age 管道）——万题库 testdata 打包后 10GB+，
// 整读内存的旧实现在校内机上必炸；manifest 声明 tier，恢复端按存在性取包。
//
// 密钥模式由密钥卷文件推导（不进 Mongo——私钥入库会随 mongodump 打进备份，加密自欺）：
//   - backup.pub 存在  → Tier 1（离线私钥：服务器只存公钥，私钥仅在启用时一次性展示）
//   - 否则 backup.key 存在 → Tier 0（服务器保管私钥，首次备份自动生成，0600）
//   - 两者皆无 → 未初始化（下次备份自动落入 Tier 0）
//
// 升级 Tier 1 = 生成新身份、只写公钥、删除旧私钥——旧密钥与全部历史密文从此作废；
// 不提供降级（回退 = 删除密钥卷文件重新初始化 + 全量备份）。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"filippo.io/age"
)

const (
	KeyFileName   = "backup.key" // Tier 0 私钥
	PubFileName   = "backup.pub" // Tier 1 公钥
	manifestName  = "manifest.json"
	mongoDumpName = "mongo.archive.gz"
	testdataName  = "testdata.tar.gz"
	uploadsName   = "uploads.tar.gz" // 题面图片（v0.5.1 起；旧归档无此文件，恢复端自动跳过）
	archivePrefix = "soj-backup-"
	archiveSuffix = ".v1.tar.age"
)

type Mode string

const (
	ModeUninitialized Mode = "uninitialized"
	ModeTier0         Mode = "tier0"
	ModeTier1         Mode = "tier1"
)

// Tier 备份分层。
type Tier string

const (
	TierAll   Tier = "all"   // 全量（面板手动/兼容）
	TierDB    Tier = "db"    // 每日：仅数据库
	TierMedia Tier = "media" // 每周：测试数据 + 题面图片
)

// tierKeep 各层保留份数（db 日备 30 天，media/all 周备 4 份）。
var tierKeep = map[Tier]int{TierDB: 30, TierMedia: 4, TierAll: 4}

// ValidTier 合法分层值。
func ValidTier(t string) bool { return t == string(TierAll) || t == string(TierDB) || t == string(TierMedia) }

func normalizeTier(t string) Tier {
	if Tier(t) == TierDB || Tier(t) == TierMedia {
		return Tier(t)
	}
	return TierAll
}

// archiveGlob 各层归档匹配模式；all 层用「时间戳以 2 开头」排除 db/media 前缀。
func archiveGlob(t Tier) string {
	switch t {
	case TierDB:
		return archivePrefix + "db-*" + archiveSuffix
	case TierMedia:
		return archivePrefix + "media-*" + archiveSuffix
	default:
		return archivePrefix + "2*" + archiveSuffix
	}
}

// ErrBusy 同一时刻仅允许一个备份任务（进程内串行，覆盖 HTTP 与 CLI 两条入口）。
var ErrBusy = errors.New("已有备份任务进行中，请稍后再试")

var runMu sync.Mutex

// Options 一次备份所需的全部输入。所有字段均来自服务端配置（环境变量），
// 不含任何用户输入；外部命令以 argv 数组固定调用 mongodump，不经 shell。
type Options struct {
	MongoURI   string    // 传给 mongodump 的连接串（SOJ_MONGO_URI）
	DataDir    string    // 测试数据卷根目录（SOJ_DATA_DIR）
	UploadsDir string    // 题面图片目录（SOJ_UPLOADS_DIR）
	BackupDir  string    // 密文归档输出目录（服务器留存一份）
	KeysDir    string    // 密钥卷目录
	Tier       string    // db | media | all（缺省 all）
	Now        time.Time // 测试注入用；零值取当前时间
}

// Status 密钥模式与最近一次备份信息（纯文件推导，不查库）。
type Status struct {
	Mode           Mode
	LastBackupAt   time.Time
	LastBackupFile string
	LastDBAt       time.Time
	LastDBFile     string
	LastMediaAt    time.Time
	LastMediaFile  string
}

// Inspect 汇总密钥卷与备份目录的当前状态（分层各报各的最近一次）。
func Inspect(keysDir, backupDir string) Status {
	st := Status{Mode: modeOf(keysDir)}
	entries, _ := filepath.Glob(filepath.Join(backupDir, archivePrefix+"*"+archiveSuffix))
	for _, f := range entries {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		base := filepath.Base(f)
		switch {
		case fi.ModTime().After(st.LastBackupAt):
			st.LastBackupAt, st.LastBackupFile = fi.ModTime(), base
		}
		if strings.HasPrefix(base, archivePrefix+"db-") && fi.ModTime().After(st.LastDBAt) {
			st.LastDBAt, st.LastDBFile = fi.ModTime(), base
		}
		if strings.HasPrefix(base, archivePrefix+"media-") && fi.ModTime().After(st.LastMediaAt) {
			st.LastMediaAt, st.LastMediaFile = fi.ModTime(), base
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

// Run 执行一次分层备份（串行），返回密文归档路径；成功后按层清理超保留旧档。
func Run(ctx context.Context, o Options) (string, error) {
	if !runMu.TryLock() {
		return "", ErrBusy
	}
	defer runMu.Unlock()

	tier := normalizeTier(o.Tier)
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
	// 磁盘护栏（media/all 体积大）：可用空间 < 最近同层归档 ×2 + 1GB 时拒绝开跑
	if tier != TierDB {
		if err := guardDiskSpace(o.BackupDir, tier); err != nil {
			return "", err
		}
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

	// 按层收集归档内文件（磁盘路径，加密阶段流式读入——不整读内存）
	files := map[string]string{}
	if tier == TierAll || tier == TierDB {
		mongoPath := filepath.Join(tmp, mongoDumpName)
		if err := runMongodump(ctx, o.MongoURI, mongoPath); err != nil {
			return "", err
		}
		files[mongoDumpName] = mongoPath
	}
	if tier == TierAll || tier == TierMedia {
		tdPath := filepath.Join(tmp, testdataName)
		if err := tarDirTo(ctx, o.DataDir, tdPath); err != nil {
			return "", fmt.Errorf("打包测试数据失败: %w", err)
		}
		upPath := filepath.Join(tmp, uploadsName)
		if err := tarDirTo(ctx, o.UploadsDir, upPath); err != nil {
			return "", fmt.Errorf("打包题面图片失败: %w", err)
		}
		files[testdataName] = tdPath
		files[uploadsName] = upPath
	}

	name := archivePrefix + now.Format("20060102-150405") + archiveSuffix
	if tier == TierDB {
		name = archivePrefix + "db-" + now.Format("20060102-150405") + archiveSuffix
	} else if tier == TierMedia {
		name = archivePrefix + "media-" + now.Format("20060102-150405") + archiveSuffix
	}
	out := filepath.Join(o.BackupDir, name)
	tmpOut := out + ".part"
	manifest, err := json.Marshal(map[string]any{
		"format": FormatTag(), "version": FormatVersion(),
		"tier": string(tier), "created_at": now.Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	if err := encryptArchive(ctx, tmpOut, rcpt, string(manifest), files); err != nil {
		os.Remove(tmpOut)
		if ctx.Err() != nil {
			return "", fmt.Errorf("备份已取消")
		}
		return "", fmt.Errorf("加密打包失败: %w", err)
	}
	if err := os.Rename(tmpOut, out); err != nil {
		return "", fmt.Errorf("落盘失败: %w", err)
	}
	pruneArchives(o.BackupDir, tier)
	return out, nil
}

// FormatTag 归档格式标识（restore.sh 按此识别）。
func FormatTag() string { return "soj-backup" }

// ArchivePrefix 归档文件名前缀（备份下载接口的白名单校验用，v0.26 任务化取包通道）。
func ArchivePrefix() string { return archivePrefix }

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

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

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

// ctxReader 每次读前检查取消（v0.26 任务化）：大文件流式拷贝（测试数据/图片 tar、
// 加密打包）也能按 32KB 粒度及时中断，不必等整文件写完。mongodump 阶段保持原样
//（库备体量小、cron 自动备份无取消场景），任务取消只覆盖 tar/加密两个大 pole。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// tarDirTo 把 src 目录打成 gzip 压缩 tar（条目相对 src 根；目录不存在时产出空包）。
// ctx 贯穿遍历与逐文件拷贝：任务取消即中断。
func tarDirTo(ctx context.Context, src, outPath string) error {
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
		if ctx.Err() != nil {
			return ctx.Err() // 任务取消：中断遍历
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
		_, err = io.Copy(tw, &ctxReader{ctx: ctx, r: src})
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("备份已取消")
		}
		return err
	}
	return closeAll()
}

// encryptArchive 流式加密：manifest 在前，其余文件按固定顺序从磁盘 io.Copy 进
// tar+age 管道（万题库 testdata 包 10GB+，整读内存的旧实现会 OOM）。ctx 逐文件检查取消。
func encryptArchive(ctx context.Context, outPath string, rcpt age.Recipient, manifest string, files map[string]string) error {
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
	order := []string{manifestName, mongoDumpName, testdataName, uploadsName}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(manifest))}); err != nil {
		return err
	}
	if _, err := tw.Write([]byte(manifest)); err != nil {
		return err
	}
	for _, name := range order[1:] {
		path, ok := files[name]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: fi.Size()}); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, &ctxReader{ctx: ctx, r: f}); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	if err := tw.Close(); err != nil {
		return err
	}
	// age 尾部标头必须在底层文件关闭前写完
	return w.Close()
}

// pruneArchives 按层保留最近 N 份（文件名含时间戳，字典序即时间序），更旧删除。
func pruneArchives(backupDir string, tier Tier) {
	entries, err := filepath.Glob(filepath.Join(backupDir, archiveGlob(tier)))
	if err != nil || len(entries) <= tierKeep[tier] {
		return
	}
	sort.Strings(entries) // 时间戳字典序
	for _, old := range entries[:len(entries)-tierKeep[tier]] {
		_ = os.Remove(old)
	}
}

// guardDiskSpace 磁盘护栏：可用空间低于「最近同层归档 ×2 + 1GB」时拒绝开跑
//（防大备份把盘写爆；首次备份（无历史）跳过）。
// CleanupStaleTemp 清扫 BackupDir 下残留的 .tmp-backup-* 临时目录（v0.27 长跑体检）：
// 进程被 kill 时 Run 的 defer RemoveAll 不会执行，残留目录含未加密的 testdata 全量
// tar。只清 olderThan 之前创建的，避开恰好并行中的备份任务（cron 备份与面板备份是
// 两个进程，runMu 互不互斥）。返回清理数量。
func CleanupStaleTemp(backupDir string, olderThan time.Duration) int {
	matches, err := filepath.Glob(filepath.Join(backupDir, ".tmp-backup-*"))
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil || !fi.IsDir() || fi.ModTime().After(cutoff) {
			continue
		}
		if os.RemoveAll(m) == nil {
			removed++
		}
	}
	return removed
}

func guardDiskSpace(backupDir string, tier Tier) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(backupDir, &stat); err != nil {
		return nil // 拿不到就不拦
	}
	free := int64(stat.Bavail) * int64(stat.Bsize)
	entries, _ := filepath.Glob(filepath.Join(backupDir, archiveGlob(tier)))
	if len(entries) == 0 {
		return nil
	}
	sort.Strings(entries)
	fi, err := os.Stat(entries[len(entries)-1])
	if err != nil {
		return nil
	}
	need := fi.Size()*2 + 1<<30
	if free < need {
		return fmt.Errorf("磁盘空间不足：可用 %s，本层最近一份 %s、按 ×2 护栏需 %s——请清理旧档或扩盘后再备份",
			humanBytes(free), humanBytes(fi.Size()), humanBytes(need))
	}
	return nil
}

func humanBytes(n int64) string {
	const unit = 1 << 30
	if n >= unit {
		return fmt.Sprintf("%.1fGB", float64(n)/unit)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
}

func mustRead(p string) []byte {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return data
}
