package repo

// 题面资产空间 assets（v0.31）：每题一个 assets/ 目录（数据卷内、与 data/ 平级），
// 题面图成为题目自有资产随题走——落库一律内容寻址名（sha256 截 128 位 hex32 + 规范
// 后缀，防路径穿越、同内容天然去重）；题面引用站内 URL `/problems/:id/assets/<名>`；
// 公开路由按该形状强校验（纵深防御同 uploads）。ReplaceProblemTestdata 只清 data/，
// assets 永不被波及；SeedClear 删题卷根（"."）时一并带走。
//
// 转存口径：批量导入急切转存（相对引用改写 + 外链图下载，改写在 statementHtml 渲染
// 之前）；编辑页保存后异步转存（定点替换双存、不整篇覆盖不重渲染）；失败一律保留
// 外链降级 + 信息性日志，绝不阻塞题目落库。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/safefetch"
	"soj/internal/testdata"
)

const (
	AssetMaxFileBytes   int64 = 5 << 20  // 单图 5MB（与编辑器上传同口径）
	AssetMaxTotalBytes  int64 = 20 << 20 // 单题资产总量 20MB
	AssetMaxTransfer          = 20       // 单次转存图片张数上限（导入/异步各一预算）
	assetTransferBudget       = 60 * time.Second
)

// assetNameRE 资产文件名形状：内容寻址 hex32 + 白名单后缀（svg 明确不收——XSS 载体）。
var assetNameRE = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpe?g|gif|webp)$`)

// ValidAssetName 公开路由的文件名强校验（任何不符合该形状的请求含路径穿越语法
// 在正则层就不可能表达，同 uploads.ServeUpload 的纵深防御）。
func ValidAssetName(name string) bool { return assetNameRE.MatchString(name) }

// AssetSiteURL 资产的站内引用 URL（题面 md / html 里出现的形式）。
func AssetSiteURL(pid int64, name string) string {
	return fmt.Sprintf("/problems/%d/assets/%s", pid, name)
}

// saveAsset 内容寻址落库：sha256 截 128 位 hex32 + 规范后缀（内容寻址非安全用途，
// 128 位防碰撞余量远超所需）；同内容同名字，重复写无害。
func saveAsset(store testdata.Provider, pid int64, content []byte, ext string) (string, error) {
	sum := sha256.Sum256(content)
	name := hex.EncodeToString(sum[:16]) + "." + ext
	if err := store.WriteFile(pid, "assets/"+name, content); err != nil {
		return "", err
	}
	return name, nil
}

// assetsTotalBytes 本题资产目录现有总字节数（预算判定用；目录不存在 = 0）。
func (im *Importer) assetsTotalBytes(pid int64) int64 {
	var total int64
	root := filepath.Join(im.DataDir, strconv.FormatInt(pid, 10), "assets")
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// AssetTransferNote 单张图的转存结果（信息性日志与测试断言用）。
type AssetTransferNote struct {
	URL string // 原始外链 / 包内相对引用
	To  string // 成功时的站内 URL
	Err string // 失败/降级原因
}

// ---- 题面改写（纯函数，单测覆盖） ----

// relAssetRefRe 题面对包内资产的相对引用：`assets/<名>`（容忍 `./` 前缀）。名字字符集
// 排除空白与 md/HTML 定界符，天然止于 `)`、`"`、`>` 等。
var relAssetRefRe = regexp.MustCompile(`(?:\./)?assets/([^\s()\[\]"'<>]+)`)

// RewriteRelativeAssetRefs 把题面里的包内相对引用 `assets/<名>` 改写为站内 URL：
// resolve 命中（该文件已按内容寻址落库）才改写，未命中原样保留。
func RewriteRelativeAssetRefs(md string, pid int64, resolve func(ref string) (storedName string, ok bool)) string {
	return relAssetRefRe.ReplaceAllStringFunc(md, func(m string) string {
		ref := relAssetRefRe.FindStringSubmatch(m)[1]
		name, ok := resolve(ref)
		if !ok {
			return m
		}
		return AssetSiteURL(pid, name)
	})
}

// siteAssetRefRe 题面里的站内资产 URL（导出时逆改写回相对引用）。容忍完整域名前缀
// （用户粘贴绝对地址的形态）；仅当题号与本题一致才改写（引用他题资产不属本包）。
var siteAssetRefRe = regexp.MustCompile(`(?:https?://[^/\s"')]+)?/problems/(\d+)/assets/([0-9a-f]{32}\.(?:png|jpe?g|gif|webp))`)

// RewriteSiteAssetsToRelative 导出用：站内资产 URL → `assets/<名>` 相对引用，
// 与导入改写互逆——题面 md 跨实例迁移自包含。
func RewriteSiteAssetsToRelative(md string, pid int64) string {
	return siteAssetRefRe.ReplaceAllStringFunc(md, func(m string) string {
		sub := siteAssetRefRe.FindStringSubmatch(m)
		if sub[1] != strconv.FormatInt(pid, 10) {
			return m
		}
		return "assets/" + sub[2]
	})
}

var (
	mdImgURLRe   = regexp.MustCompile(`!\[[^\]]*\]\(\s*(https?://[^\s)]+)`)
	htmlImgSrcRe = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc="(https?://[^"]+)"`)
)

// ExtractExternalImageURLs 题面里的外链图（md 图片语法 + HTML img src），去重保序。
// 站内资产 URL 与相对引用不算外链。
func ExtractExternalImageURLs(md string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		if !seen[u] && !strings.Contains(u, "/problems/") {
			seen[u] = true
			out = append(out, u)
		}
	}
	for _, m := range mdImgURLRe.FindAllStringSubmatch(md, -1) {
		add(m[1])
	}
	for _, m := range htmlImgSrcRe.FindAllStringSubmatch(md, -1) {
		add(m[1])
	}
	return out
}

// ---- 导入链（急切转存） ----

// stageProblemAssets 把题包 assets/ 目录落库到题目资产空间：逐文件校验（白名单后缀 +
// 魔数嗅探 + 单文件 5MB + 单题 20MB 总量）后按内容寻址名写入，返回「原文件名 → 库内名」
// 映射供题面改写。不合规文件跳过并记日志（引用保持相对形态，不阻塞导入）。
func (im *Importer) stageProblemAssets(store testdata.Provider, pid int64, dir string) map[string]string {
	assetsDir := filepath.Join(dir, "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return nil // 无 assets/ = 普通题包
	}
	mapping := map[string]string{}
	total := im.assetsTotalBytes(pid)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || isPackJunk(name) || strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		default:
			slog.Warn("跳过：后缀不在白名单（png/jpg/jpeg/gif/webp）", "component", "assets", "pid", pid, "name", name)
			continue
		}
		content, err := os.ReadFile(filepath.Join(assetsDir, name))
		if err != nil {
			slog.Error("读取资产失败", "component", "assets", "pid", pid, "name", name, "error", err)
			continue
		}
		if int64(len(content)) > AssetMaxFileBytes {
			slog.Warn("跳过：超过单文件 5MB 上限", "component", "assets", "pid", pid, "name", name)
			continue
		}
		// 后缀可伪造，内容不能：按魔数定真实类型（svg 嗅探不出即被拒）。
		real := safefetch.DetectImageExt(content)
		if real == "" {
			slog.Warn("跳过：内容不是白名单图片", "component", "assets", "pid", pid, "name", name)
			continue
		}
		if total+int64(len(content)) > AssetMaxTotalBytes {
			slog.Warn("跳过：单题资产将超 20MB 总量上限", "component", "assets", "pid", pid, "name", name)
			continue
		}
		stored, err := saveAsset(store, pid, content, real)
		if err != nil {
			slog.Error("落库资产失败", "component", "assets", "pid", pid, "name", name, "error", err)
			continue
		}
		total += int64(len(content))
		mapping[name] = stored
	}
	return mapping
}

// transferExternalImages 外链图转存：扫描题面全部外链图，经 SSRF 安全 fetcher 下载、
// 按内容寻址落库，返回改写后的题面与「原 URL → 站内 URL」映射（定点替换用）。
// 单题预算（≤20 张 / 调用方 ctx 控时）；任何失败保留外链降级 + 日志，不返回错误——
// 转存永不阻塞题目落库（plan §2.37 风险备忘）。
func (im *Importer) transferExternalImages(ctx context.Context, store testdata.Provider, pid int64, md string) (string, map[string]string, []AssetTransferNote) {
	urls := ExtractExternalImageURLs(md)
	if len(urls) == 0 {
		return md, nil, nil
	}
	fetcher := im.ImageFetcher
	if fetcher == nil {
		fetcher = safefetch.New()
	}
	total := im.assetsTotalBytes(pid)
	reps := map[string]string{}
	notes := make([]AssetTransferNote, 0, len(urls))
	for i, u := range urls {
		if i >= AssetMaxTransfer {
			notes = append(notes, AssetTransferNote{URL: u, Err: "超过单次 20 张转存上限，保留外链"})
			continue
		}
		content, ext, err := fetcher.GetImage(ctx, u)
		if err != nil {
			slog.Warn("转存失败（保留外链）", "component", "assets", "pid", pid, "url", u, "error", err)
			notes = append(notes, AssetTransferNote{URL: u, Err: err.Error()})
			continue
		}
		if total+int64(len(content)) > AssetMaxTotalBytes {
			slog.Warn("转存中止（保留外链）：单题资产将超 20MB 总量上限", "component", "assets", "pid", pid, "url", u)
			notes = append(notes, AssetTransferNote{URL: u, Err: "单题资产总量超限，保留外链"})
			continue
		}
		stored, err := saveAsset(store, pid, content, ext)
		if err != nil {
			slog.Warn("落库失败（保留外链）", "component", "assets", "pid", pid, "url", u, "error", err)
			notes = append(notes, AssetTransferNote{URL: u, Err: err.Error()})
			continue
		}
		total += int64(len(content))
		to := AssetSiteURL(pid, stored)
		reps[u] = to
		notes = append(notes, AssetTransferNote{URL: u, To: to})
	}
	for old, to := range reps {
		md = strings.ReplaceAll(md, old, to)
	}
	return md, reps, notes
}

// prepareStatement 导入链的题面终加工：包内相对引用改写（statementHtml 渲染之前——
// importer 是唯一入口，渲染链零改动）+ 外链图急切转存（TransferImages 开关）。
// 任何资产环节失败都只降级不改错——题面文本原样进入渲染。
func (im *Importer) prepareStatement(ctx context.Context, store testdata.Provider, pid int64, dir, md string) string {
	if staged := im.stageProblemAssets(store, pid, dir); len(staged) > 0 {
		md = RewriteRelativeAssetRefs(md, pid, func(ref string) (string, bool) {
			name, ok := staged[ref]
			return name, ok
		})
	}
	if im.TransferImages {
		md, _, _ = im.transferExternalImages(ctx, store, pid, md)
	}
	return md
}

// ---- 编辑链（保存后异步转存） ----

// assetTransferInFlight 同题在途转存去重（进程内；单副本边界既定）。保存连点只在
// 首次扫描起效，新增外链留待下次保存重扫（内容寻址天然幂等，重扫零成本）。
var assetTransferInFlight sync.Map

// TransferStatementAssetsAsync 题目保存落库后的后台转存（前端零按钮零感知）：
// 扫描题面外链图 → 同套 fetcher/预算下载落库 → 对库内当前题面做「旧 URL → 新 URL」
// 定点替换（statement 与 statementHtml 各替换一遍，不整篇覆盖、无需重渲染）。
// 失败静默降级保外链。
func (im *Importer) TransferStatementAssetsAsync(pid int64) {
	if _, busy := assetTransferInFlight.LoadOrStore(pid, struct{}{}); busy {
		return
	}
	go func() {
		defer assetTransferInFlight.Delete(pid)
		ctx, cancel := context.WithTimeout(context.Background(), assetTransferBudget)
		defer cancel()
		if err := im.transferProblemAssets(ctx, pid); err != nil {
			slog.Warn("异步转存失败", "component", "assets", "pid", pid, "error", err)
		}
	}()
}

func (im *Importer) transferProblemAssets(ctx context.Context, pid int64) error {
	p, err := im.Repo.GetProblem(ctx, pid)
	if err != nil {
		return err
	}
	if len(ExtractExternalImageURLs(p.Statement)) == 0 {
		return nil
	}
	store := testdata.NewLocalFS(im.DataDir)
	_, reps, _ := im.transferExternalImages(ctx, store, pid, p.Statement)
	if len(reps) == 0 {
		return nil
	}
	return im.Repo.ReplaceAssetURLs(ctx, pid, reps)
}

// ReplaceAssetURLs 定点替换题面双存里的资产 URL（异步转存回写）：按库内当前值逐 URL
// 替换 statement 与 statementHtml——保存后用户继续编辑零竞态（不整篇覆盖）、URL 在
// html 里与 md 同串出现故无需重渲染。全部未命中不写库。
func (r *Repo) ReplaceAssetURLs(ctx context.Context, pid int64, reps map[string]string) error {
	var doc struct {
		Statement     string `bson:"statement"`
		StatementHtml string `bson:"statementHtml"`
	}
	if err := r.problems.FindOne(ctx, bson.M{"_id": pid},
		options.FindOne().SetProjection(bson.M{"statement": 1, "statementHtml": 1})).Decode(&doc); err != nil {
		return err
	}
	st, html := doc.Statement, doc.StatementHtml
	for old, to := range reps {
		st = strings.ReplaceAll(st, old, to)
		html = strings.ReplaceAll(html, old, to)
	}
	if st == doc.Statement && html == doc.StatementHtml {
		return nil
	}
	_, err := r.problems.UpdateOne(ctx, bson.M{"_id": pid}, bson.M{"$set": bson.M{
		"statement": st, "statementHtml": html, "updatedAt": time.Now()}})
	return err
}
