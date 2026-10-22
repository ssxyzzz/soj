package server

// 批量导入（v0.15，plan §2.7）：后台「导入题目」页——单一 zip 收单题包/多题包混装，
// 逐目录走三路嗅探（SOJ v2 / Hydro / 裸数据，ImportDir 内归一），题面取包内 statement。
// 全站单任务（进程内互斥，409 拒绝并发）；同步进度快照轮询 + 取消旗标（题间检查，
// 已导入的保留）；单题失败跳过继续，结束出报告。
//
// 入口分工（同批拍板）：建题/编辑 zip = 纯数据/配置（包内题面剔除，见
// stripPackageStatement）；题包（含题面）唯一入口 = 本页。

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

// batchResult 单题导入结果（前端结果流）。
type batchResult struct {
	Name   string `json:"name"`            // 目录名（或 zip 根 = "根目录"）
	Status string `json:"status"`          // ok | skipped | failed
	PID    int64  `json:"pid,omitempty"`   // ok 时的题号
	Title  string `json:"title,omitempty"` // 题目标题（ok/skipped 时带）
	Reason string `json:"reason,omitempty"`
}

// batchImportJob 一次导入任务的全量状态（自身带锁，供轮询快照与取消）。
type batchImportJob struct {
	mu        sync.Mutex
	State     string // running | done | cancelled | failed
	Total     int
	Done      int
	Results   []batchResult
	Cancel    bool
	Err       string
	ByUID     int64
	ByName    string
	StartedAt time.Time
	EndedAt   time.Time
}

func (j *batchImportJob) requestCancel() {
	j.mu.Lock()
	j.Cancel = true
	j.mu.Unlock()
}

func (j *batchImportJob) cancelRequested() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Cancel
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

// snapshot 轮询用只读快照。
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
		"state": j.State, "total": j.Total, "done": j.Done,
		"ok_count": okN, "fail_count": failN, "skip_count": skipN,
		"results": append([]batchResult(nil), j.Results...),
		"error":   j.Err, "by": j.ByName,
		"started_at": j.StartedAt, "ended_at": j.EndedAt,
	}
}

// AdminImportStatus 当前导入任务进度（无任务 = idle）。
func (h *Handlers) AdminImportStatus(c *gin.Context) {
	h.batchMu.Lock()
	j := h.batch
	h.batchMu.Unlock()
	if j == nil {
		ok(c, gin.H{"state": "idle"})
		return
	}
	ok(c, j.snapshot())
}

// AdminImportCancel 请求取消：当前题导入完即停（已导入的保留）。
func (h *Handlers) AdminImportCancel(c *gin.Context) {
	h.batchMu.Lock()
	j := h.batch
	h.batchMu.Unlock()
	if j != nil {
		j.requestCancel()
	}
	ok(c, gin.H{"cancel_requested": true})
}

// AdminImportProblems 上传题包 zip 并启动导入（全站单任务，进行中 409）。
func (h *Handlers) AdminImportProblems(c *gin.Context) {
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
	job := &batchImportJob{State: "running", ByUID: uid, ByName: uname, StartedAt: time.Now()}
	h.batch = job
	h.batchMu.Unlock()

	failJob := func(msg string) {
		job.finish("failed", msg)
		fail(c, http.StatusBadRequest, msg)
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(zipMB)<<20)
	form, err := c.MultipartForm()
	if err != nil {
		failJob("上传失败：请求体过大（整包上限 " + fmt.Sprint(zipMB) + "MB）或格式错误")
		return
	}
	zips := form.File["zip"]
	if len(zips) != 1 {
		failJob("请上传一个题包 zip")
		return
	}
	skipSame := formValue(form, "skip_same_title") == "1"
	defaultHidden := formValue(form, "default_hidden") == "1"

	tmpZip, err := os.CreateTemp("", "soj-batch-*.zip")
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

	go h.runBatchImport(job, tmpZip.Name(), uid, skipSame, defaultHidden, problemMB, zipMB)
	ok(c, gin.H{"started": true, "zip_mb": zipMB, "problem_mb": problemMB})
}

// runBatchImport 后台逐题导入；任何整包级失败（zip/结构）落 job.State=failed。
func (h *Handlers) runBatchImport(job *batchImportJob, zipPath string, uid int64, skipSame, defaultHidden bool, problemMB, zipMB int) {
	defer os.Remove(zipPath)
	defer func() {
		if r := recover(); r != nil {
			job.finish("failed", fmt.Sprintf("导入任务异常: %v", r))
		}
	}()

	tmpDir, err := os.MkdirTemp("", "soj-batch-dir-")
	if err != nil {
		job.finish("failed", "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)

	// 放宽面解压：条目数随整包限额放大（每 MB 8 条、下限 2000），单文件 = 单题限额，
	// 解压总量 = 整包限额 ×2（zip 与解压后体积的常态比）
	maxEntries := zipMB * 8
	if maxEntries < 2000 {
		maxEntries = 2000
	}
	hint := fmt.Sprintf("%dMB", problemMB)
	totalHint := fmt.Sprintf("%dMB", zipMB*2)
	if err := safeExtractZipLimits(zipPath, tmpDir, maxEntries, int64(zipMB)*2<<20, int64(problemMB)<<20, hint, totalHint); err != nil {
		job.finish("failed", "题包不合法："+err.Error())
		return
	}

	dirs, err := splitBatchProblemDirs(tmpDir)
	if err != nil {
		job.finish("failed", err.Error())
		return
	}
	job.setTotal(len(dirs))

	ctx := context.Background()
	for _, dir := range dirs {
		if job.cancelRequested() {
			job.finish("cancelled", "")
			return
		}
		name := filepath.Base(dir)
		if name == "." || name == string(filepath.Separator) {
			name = "根目录"
		}
		title := batchDirTitle(dir)
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
		_ = unwrapSingleRootDir(dir) // 包内再套一层目录时摊平（失败按原样交给嗅探报错）
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

// splitBatchProblemDirs 切分题包目录：zip 根即单题包（任一单题特征命中）返回根本身；
// 否则根下每个一级目录 = 一道题（混格式可）。无目录且无单题特征 → 带所见内容报错。
func splitBatchProblemDirs(root string) ([]string, error) {
	if looksLikeSingleProblem(root) {
		return []string{root}, nil
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
		return nil, fmt.Errorf("无法识别的题包结构（包内：%s）。支持：zip 根即单题包（problem.yaml + cases/ / Hydro testdata/ / 平铺数据对），或根下每个目录一道题（混格式均可）", describeBatchEntries(root))
	}
	sort.Strings(dirs)
	return dirs, nil
}

// looksLikeSingleProblem 单题包特征：problem.yaml / config.yaml / testdata/ / cases/
// 任一存在，或根下有平铺 *.in（与 DetectAndNormalize 的三路判定同口径）。
func looksLikeSingleProblem(dir string) bool {
	for _, name := range []string{"problem.yaml", "config.yaml", "testdata", "cases"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".in") {
			return true
		}
	}
	return false
}

// batchDirTitle 预读目录标题（同标题跳过用）：problem.yaml 的 title 字段，缺省目录名。
func batchDirTitle(dir string) string {
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
	return base
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

// describeBatchEntries 错误信息里「包里实际看到了什么」。
func describeBatchEntries(dir string) string {
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
