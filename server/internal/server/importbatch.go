package server

// 批量导入（v0.15 统一题包格式，import-design.md §2）：题库页「导入题目」→ 独立 /import
// 页选来源（SOJ / Hydro 本期；QDUOJ / HOJ 下期置灰），每种来源独立适配器、不做混合嗅探：
//   - SOJ：zip 根即单题（根有 problem.md）或根下每目录一题（problem.md 定题目目录）；
//   - Hydro：官方导出包——zip 根即单题（根有 testdata/）或根下每数字目录一题，
//     NormalizeHydroDir 归一为 SOJ 格式后走 ImportDir。
// 全站单任务（进行中 409）；秒级进度轮询；**取消立马生效**（context 取消中断在途库操作、
// 后续目录不再识别，已导入保留）；单题失败跳过出报告；同标题跳过 / 默认隐藏开关；
// root 限额（单题数据 / 整包 zip）。

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"gopkg.in/yaml.v3"

	"soj/internal/repo"
)

// importSource 支持的来源适配器。
var importSources = map[string]bool{"soj": true, "hydro": true}

// batchResult 单题导入结果（结果流）。
type batchResult struct {
	Name   string `json:"name"`            // 目录名（zip 根即单题 = "根目录"）
	Status string `json:"status"`          // ok | skipped | failed
	PID    int64  `json:"pid,omitempty"`   // ok 时的题号
	Title  string `json:"title,omitempty"` // 题目标题
	Reason string `json:"reason,omitempty"`
}

// batchImportJob 一次导入任务的全量状态（自身带锁，供轮询快照与取消）。
type batchImportJob struct {
	mu        sync.Mutex
	State     string // running | done | cancelled | failed
	Source    string // soj | hydro
	Total     int
	Done      int
	Results   []batchResult
	Err       string
	ByUID     int64
	ByName    string
	StartedAt time.Time
	EndedAt   time.Time
	cancel    context.CancelFunc // 立马取消：中断在途识别
	cancelled bool
}

func (j *batchImportJob) cancelNow() {
	j.mu.Lock()
	j.cancelled = true
	j.mu.Unlock()
	if j.cancel != nil {
		j.cancel() // 中断在途 ctx 感知操作（库读写等）
	}
}

func (j *batchImportJob) isCancelled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cancelled
}

func (j *batchImportJob) setTotal(n int) {
	j.mu.Lock()
	j.Total = n
	j.mu.Unlock()
}

func (j *batchImportJob) addResult(r batchResult) {
	j.mu.Lock()
	j.Results = append(j.Results, r)
	j.Done++
	j.mu.Unlock()
}

func (j *batchImportJob) finish(state, errMsg string) {
	j.mu.Lock()
	if j.State == "running" {
		j.State = state
		j.Err = errMsg
		j.EndedAt = time.Now()
	}
	j.mu.Unlock()
}

func (j *batchImportJob) snapshot() gin.H {
	j.mu.Lock()
	defer j.mu.Unlock()
	okN, failN, skipN := 0, 0, 0
	for _, r := range j.Results {
		switch r.Status {
		case "ok":
			okN++
		case "failed":
			failN++
		case "skipped":
			skipN++
		}
	}
	return gin.H{
		"state": j.State, "source": j.Source, "total": j.Total, "done": j.Done,
		"ok_count": okN, "fail_count": failN, "skip_count": skipN,
		"results":  append([]batchResult(nil), j.Results...),
		"error":    j.Err, "by": j.ByName,
		"started_at": j.StartedAt, "ended_at": j.EndedAt,
	}
}

// ImportStatus 当前导入任务进度（无任务 = idle）。
func (h *Handlers) ImportStatus(c *gin.Context) {
	h.batchMu.Lock()
	j := h.batch
	h.batchMu.Unlock()
	if j == nil {
		ok(c, gin.H{"state": "idle"})
		return
	}
	ok(c, j.snapshot())
}

// ImportCancel 立马取消：中断当前题的识别、后续目录不再处理（已导入保留）。
func (h *Handlers) ImportCancel(c *gin.Context) {
	h.batchMu.Lock()
	j := h.batch
	h.batchMu.Unlock()
	if j != nil {
		j.cancelNow()
	}
	ok(c, gin.H{"cancel_requested": true})
}

// StartImport 上传题包 zip 并启动导入（全站单任务，进行中 409）。
func (h *Handlers) StartImport(c *gin.Context) {
	uid, uname, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	st, err := h.repo.GetSystemSettings(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取设置失败")
		return
	}
	zipMB := st.BatchImportZipMBVal()
	problemMB := st.BatchImportProblemMBVal()

	// 单任务排队：已有 running 任务直接拒绝（前端轮询其进度）
	h.batchMu.Lock()
	if h.batch != nil && h.batch.State == "running" {
		h.batchMu.Unlock()
		fail(c, http.StatusConflict, "已有导入任务进行中，请等待完成或先取消")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &batchImportJob{State: "running", ByUID: uid, ByName: uname, StartedAt: time.Now(), cancel: cancel}
	h.batch = job
	h.batchMu.Unlock()

	failJob := func(msg string) {
		cancel()
		job.finish("failed", msg)
		fail(c, http.StatusBadRequest, msg)
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(zipMB)<<20)
	form, err := c.MultipartForm()
	if err != nil {
		failJob("上传失败：请求体过大（整包上限 " + fmt.Sprint(zipMB) + "MB）或格式错误")
		return
	}
	source := strings.TrimSpace(formValue(form, "source"))
	job.mu.Lock()
	job.Source = source
	job.mu.Unlock()
	if !importSources[source] {
		failJob("暂不支持该来源（本期支持：SOJ、Hydro；QDUOJ / HOJ 下期开放）")
		return
	}
	zips := form.File["zip"]
	if len(zips) != 1 {
		failJob("请上传一个题包 zip")
		return
	}
	skipSame := formValue(form, "skip_same_title") == "1"
	defaultHidden := formValue(form, "default_hidden") == "1"

	tmpZip, err := os.CreateTemp("", "soj-import-*.zip")
	if err != nil {
		failJob("服务器临时文件创建失败")
		return
	}
	zf, err := zips[0].Open()
	if err != nil {
		tmpZip.Close()
		os.Remove(tmpZip.Name())
		failJob("读取上传文件失败")
		return
	}
	if _, err = io.Copy(tmpZip, zf); err != nil {
		zf.Close()
		tmpZip.Close()
		os.Remove(tmpZip.Name())
		failJob("读取上传文件失败")
		return
	}
	zf.Close()
	tmpZip.Close()

	go h.runImportJob(ctx, job, tmpZip.Name(), uid, skipSame, defaultHidden, problemMB, zipMB)
	ok(c, gin.H{"started": true, "source": source, "zip_mb": zipMB, "problem_mb": problemMB})
}

// runImportJob 后台逐题导入；整包级失败（zip/结构）落 job.State=failed。
func (h *Handlers) runImportJob(ctx context.Context, job *batchImportJob, zipPath string, uid int64, skipSame, defaultHidden bool, problemMB, zipMB int) {
	defer os.Remove(zipPath)
	defer func() {
		if r := recover(); r != nil {
			job.finish("failed", fmt.Sprintf("导入任务异常: %v", r))
		}
	}()

	tmpDir, err := os.MkdirTemp("", "soj-import-dir-")
	if err != nil {
		job.finish("failed", "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)

	// 放宽面解压：条目数随整包限额放大，单文件 = 单题限额，总量 = 整包 ×2
	maxEntries := zipMB * 8
	if maxEntries < 2000 {
		maxEntries = 2000
	}
	if err := safeExtractZipLimits(zipPath, tmpDir, maxEntries, int64(zipMB)*2<<20, int64(problemMB)<<20,
		fmt.Sprintf("%dMB", problemMB), fmt.Sprintf("%dMB", zipMB*2)); err != nil {
		job.finish("failed", "题包不合法："+err.Error())
		return
	}

	dirs, err := splitImportDirs(tmpDir, job.Source)
	if err != nil {
		job.finish("failed", err.Error())
		return
	}
	job.setTotal(len(dirs))

	for _, dir := range dirs {
		if job.isCancelled() || ctx.Err() != nil {
			job.finish("cancelled", "")
			return
		}
		name := filepath.Base(dir)
		if name == "." || name == string(filepath.Separator) {
			name = "根目录"
		}
		// Hydro 目录先归一为 SOJ 格式（失败 = 该题失败跳过）
		if job.Source == "hydro" {
			if err := repo.NormalizeHydroDir(dir); err != nil {
				job.addResult(batchResult{Name: name, Status: "failed", Title: dirTitleFallbackRepo(name), Reason: err.Error()})
				continue
			}
		}
		title := importDirTitle(dir)
		if skipSame && title != "" {
			if n, err := h.repo.Problems.CountDocuments(ctx, bson.M{"title": title}); err == nil && n > 0 {
				job.addResult(batchResult{Name: name, Status: "skipped", Title: title, Reason: "题库已存在同标题题，按开关跳过"})
				continue
			}
		}
		if size, err := dirSize(dir); err == nil && size > int64(problemMB)<<20 {
			job.addResult(batchResult{Name: name, Status: "failed", Title: title, Reason: fmt.Sprintf("单题数据超过 %dMB 上限，请拆分后单独传", problemMB)})
			continue
		}
		if err := unwrapSingleRootDir(dir); err != nil {
			job.addResult(batchResult{Name: name, Status: "failed", Title: title, Reason: err.Error()})
			continue
		}
		pr, err := (&repo.Importer{
			Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid,
			IgnoreDirPID: true, DefaultHidden: defaultHidden,
		}).ImportDir(ctx, dir)
		if err != nil {
			job.addResult(batchResult{Name: name, Status: "failed", Title: title, Reason: err.Error()})
			continue
		}
		job.addResult(batchResult{Name: name, Status: "ok", PID: pr.ID, Title: pr.Title})
	}
	job.finish("done", "")
}

// dirTitleFallbackRepo 目录名剥前导数字（repo.dirTitleFallback 的 server 侧同名语义）。
func dirTitleFallbackRepo(name string) string {
	name = strings.TrimLeft(name, "0123456789")
	return strings.TrimLeft(name, "_- .")
}

// splitImportDirs 按来源切分题目录：
//   - SOJ：根有 problem.md → [根]；否则根下每目录 = 一题（无 problem.md 的目录也进入列表，
//     由 ImportDir 报「缺 problem.md」失败跳过——报告里能看见）；
//   - Hydro：根有 testdata/（Hydro 单题导出）→ [根]；否则根下每目录一题。
func splitImportDirs(root, source string) ([]string, error) {
	switch source {
	case "soj":
		if fileExists(filepath.Join(root, "problem.md")) {
			return []string{root}, nil
		}
	case "hydro":
		if fi, err := os.Stat(filepath.Join(root, "testdata")); err == nil && fi.IsDir() {
			return []string{root}, nil
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取题包目录失败: %w", err)
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !isPackagingJunk(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("无法识别的题包结构（%s 包内：%s）。支持：zip 根即单题，或根下每个目录一道题", sourceLabel(source), describeImportEntries(root))
	}
	sort.Strings(dirs)
	return dirs, nil
}

func sourceLabel(source string) string {
	if source == "hydro" {
		return "Hydro"
	}
	return "SOJ"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// importDirTitle 预读目录标题（同标题跳过用）：problem.yaml 的 title（Hydro 原生 yaml
// 同样有 title），缺省目录名剥前导数字。
func importDirTitle(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml"))
	if err == nil {
		var probe struct {
			Title string `yaml:"title"`
		}
		if yaml.Unmarshal(raw, &probe) == nil && strings.TrimSpace(probe.Title) != "" {
			return strings.TrimSpace(probe.Title)
		}
	}
	base := filepath.Base(dir)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return dirTitleFallbackRepo(base)
}

// dirSize 目录内全部常规文件字节数。
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total, err
}

// describeImportEntries 错误信息里「包里实际看到了什么」。
func describeImportEntries(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "无法读取"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if isPackagingJunk(e.Name()) || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			names = append(names, e.Name()+"/")
		} else {
			names = append(names, e.Name())
		}
		if len(names) >= 8 {
			break
		}
	}
	if len(names) == 0 {
		return "空目录"
	}
	return strings.Join(names, ", ")
}
