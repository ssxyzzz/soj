package server

// 网页建题与元数据编辑（v0.5b-1）：
//   - POST /api/problems：multipart 建题（元数据字段 + 题面 + 测试数据 zip），
//     zip 只需 cases/<N>/in|ans 结构，problem.yaml / statement.md 由表单生成；
//   - PATCH /api/problems/:id/meta：编辑标题/时限/内存/比对模式（problemEditable 校验）。
// zip 安全面三道闸：请求体上限、条目数与解压总量上限、条目名穿越成分拒绝。

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"gopkg.in/yaml.v3"

	"soj/internal/repo"
)

const (
	maxCreateRequest   = 64 << 20  // 建题请求体上限（zip + 表单）
	maxZipEntries      = 2000      // zip 条目数上限
	maxZipUncompressed = 200 << 20 // zip 解压总量上限（防炸弹）
	maxZipFile         = 20 << 20  // zip 单文件上限
)

func (h *Handlers) CreateProblem(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRequest)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败：请求体过大或格式错误（测试数据上限 64MB）")
		return
	}

	title := strings.TrimSpace(formValue(form, "title"))
	if title == "" || len([]rune(title)) > 64 {
		fail(c, http.StatusBadRequest, "标题需在 1-64 字符")
		return
	}
	timeLimit := atoiDefault(formValue(form, "time_limit_ms"), 1000)
	if timeLimit < 100 || timeLimit > 600000 {
		fail(c, http.StatusBadRequest, "时限需在 100-600000 毫秒")
		return
	}
	memLimit := atoiDefault(formValue(form, "memory_limit_kb"), 262144)
	if memLimit < 16 || memLimit > 1048576 {
		fail(c, http.StatusBadRequest, "内存限制需在 16-1048576 MB")
		return
	}
	compare := formValue(form, "compare")
	if compare == "" {
		compare = repo.CompareExact
	}
	if !repo.ValidCompare(compare) {
		fail(c, http.StatusBadRequest, "比对模式需为 exact / tolerant")
		return
	}
	tol := 0.0
	if compare == repo.CompareTolerant {
		tol = parsePositiveFloat(formValue(form, "float_tolerance"), repo.DefaultFloatTolerance)
	}
	statement := formValue(form, "statement")
	if len(statement) > 256<<10 {
		fail(c, http.StatusBadRequest, "题面过长（上限 256KB）")
		return
	}
	zips := form.File["zip"]
	if len(zips) != 1 {
		fail(c, http.StatusBadRequest, "请上传一个测试数据 zip（含 cases/<N>/in|ans 结构）")
		return
	}

	// zip 落临时文件后做安全解压
	tmpZip, err := os.CreateTemp("", "soj-create-*.zip")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时文件创建失败")
		return
	}
	defer os.Remove(tmpZip.Name())
	zf, err := zips[0].Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取上传文件失败")
		return
	}
	if _, err = io.Copy(tmpZip, zf); err != nil {
		tmpZip.Close()
		fail(c, http.StatusBadRequest, "读取上传文件失败")
		return
	}
	tmpZip.Close()

	tmpDir, err := os.MkdirTemp("", "soj-create-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	if err = safeExtractZip(tmpZip.Name(), tmpDir); err != nil {
		fail(c, http.StatusBadRequest, "测试数据包不合法："+err.Error())
		return
	}

	// problem.yaml / statement.md 由表单生成（zip 里若带了同名文件，以表单为准）
	yamlBytes, _ := yaml.Marshal(map[string]int{
		"timeLimitMs":   timeLimit,
		"memoryLimitKb": memLimit,
	})
	setup := map[string][]byte{
		"problem.yaml": append([]byte("title: "+quoteYaml(title)+"\n"), yamlBytes...),
		"statement.md": []byte(statement),
	}
	for name, data := range setup {
		if err := os.WriteFile(filepath.Join(tmpDir, name), data, 0o644); err != nil {
			fail(c, http.StatusInternalServerError, "写题目文件失败")
			return
		}
	}

	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid}).ImportDir(c.Request.Context(), tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败："+err.Error())
		return
	}
	// 比对模式：zip 内 config.yaml 已声明的以 config 为准；否则用表单值
	set := bson.M{}
	if pr.Compare == "" {
		set["compare"] = compare
		if compare == repo.CompareTolerant {
			set["floatTolerance"] = tol
		}
	}
	// 表单可选上传校验器（zip 内没有 checker 文件时）
	if len(form.File["checker"]) == 1 && pr.CheckerFile == "" {
		if cf, err := form.File["checker"][0].Open(); err == nil {
			data, err := io.ReadAll(cf)
			cf.Close()
			name := form.File["checker"][0].Filename
			if err == nil && len(data) <= 256<<10 && strings.HasSuffix(strings.ToLower(name), ".cpp") {
				if err := h.store.WriteFile(pr.ID, "checker.cpp", data); err == nil {
					set["checkerFile"] = "checker.cpp"
					set["checkerSha"] = fmt.Sprintf("%x", sha256.Sum256(data))
				}
			}
		}
	}
	// 文件 IO 文件名对与说明（v0.5b-3）：zip 内 config.yaml 已声明的以 config 为准；
	// 名字缺省 problem.in / problem.out（FileIONames 兜底），说明为 Markdown
	if pr.FileIOInput == "" {
		if v := formValue(form, "file_io_input"); v != "" {
			set["fileIoInput"] = v
		}
	}
	if pr.FileIOOutput == "" {
		if v := formValue(form, "file_io_output"); v != "" {
			set["fileIoOutput"] = v
		}
	}
	if v := formValue(form, "file_io_desc"); v != "" {
		if len(v) > 256<<10 {
			fail(c, http.StatusBadRequest, "文件 IO 说明过长（上限 256KB）")
			return
		}
		set["fileIoDesc"] = v
	}
	if len(set) > 0 {
		if err := h.repo.UpdateProblemMeta(c.Request.Context(), pr.ID, set); err != nil {
			fail(c, http.StatusInternalServerError, "建题成功但写入配置失败")
			return
		}
	}
	ok(c, gin.H{"id": pr.ID})
}

// UpdateChecker 上传/替换题目校验器（C++ 源文件，problemEditable 语义）。
// 替换后清空编译产物缓存，判题时懒编译生效。
func (h *Handlers) UpdateChecker(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	p, err := h.repo.GetProblem(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	if !h.problemEditable(c, p) {
		fail(c, http.StatusForbidden, "需要「编辑任意题目」权限，或仅编辑自己导入的题目")
		return
	}
	fh, err := c.FormFile("checker")
	if err != nil {
		fail(c, http.StatusBadRequest, "请上传 checker 源文件（.cpp）")
		return
	}
	if !strings.HasSuffix(strings.ToLower(fh.Filename), ".cpp") {
		fail(c, http.StatusBadRequest, "校验器需为 C++ 源文件（.cpp）")
		return
	}
	if fh.Size > 256<<10 {
		fail(c, http.StatusBadRequest, "校验器过大（上限 256KB）")
		return
	}
	f, err := fh.Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取校验器失败")
		return
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取校验器失败")
		return
	}
	if err := h.store.WriteFile(id, "checker.cpp", data); err != nil {
		fail(c, http.StatusInternalServerError, "保存校验器失败")
		return
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{
		"checkerFile": "checker.cpp", "checkerSha": sha, "checkerFileId": "",
	}); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"checker": "checker.cpp", "sha": sha})
}

// RemoveChecker 移除题目校验器（恢复按比对模式判定）。
func (h *Handlers) RemoveChecker(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	p, err := h.repo.GetProblem(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	if !h.problemEditable(c, p) {
		fail(c, http.StatusForbidden, "需要「编辑任意题目」权限，或仅编辑自己导入的题目")
		return
	}
	_ = h.store.Delete(id, "checker.cpp")
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{
		"checkerFile": "", "checkerSha": "", "checkerFileId": "",
	}); err != nil {
		fail(c, http.StatusInternalServerError, "移除失败")
		return
	}
	ok(c, gin.H{"removed": true})
}

// UpdateProblemMeta 编辑题目元数据（标题/时限/内存/比对模式；题面走既有 statement 接口）。
func (h *Handlers) UpdateProblemMeta(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	p, err := h.repo.GetProblem(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	if !h.problemEditable(c, p) {
		fail(c, http.StatusForbidden, "需要「编辑任意题目」权限，或仅编辑自己导入的题目")
		return
	}
	var req struct {
		Title          *string  `json:"title"`
		TimeLimitMS    *int     `json:"time_limit_ms"`
		MemoryLimitKB  *int     `json:"memory_limit_kb"`
		Compare        *string  `json:"compare"`
		FloatTolerance *float64 `json:"float_tolerance"`
		FileIOInput    *string  `json:"file_io_input"`
		FileIOOutput   *string  `json:"file_io_output"`
		FileIODesc     *string  `json:"file_io_desc"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	set := bson.M{}
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" || len([]rune(t)) > 64 {
			fail(c, http.StatusBadRequest, "标题需在 1-64 字符")
			return
		}
		set["title"] = t
	}
	if req.TimeLimitMS != nil {
		if *req.TimeLimitMS < 100 || *req.TimeLimitMS > 600000 {
			fail(c, http.StatusBadRequest, "时限需在 100-600000 毫秒")
			return
		}
		set["timeLimitMs"] = *req.TimeLimitMS
	}
	if req.MemoryLimitKB != nil {
		if *req.MemoryLimitKB < 16 || *req.MemoryLimitKB > 1048576 {
			fail(c, http.StatusBadRequest, "内存限制需在 16-1048576 MB")
			return
		}
		set["memoryLimitKb"] = *req.MemoryLimitKB
	}
	if req.Compare != nil {
		if !repo.ValidCompare(*req.Compare) {
			fail(c, http.StatusBadRequest, "比对模式需为 exact / tolerant")
			return
		}
		set["compare"] = *req.Compare
	}
	if req.FloatTolerance != nil {
		if *req.FloatTolerance <= 0 || *req.FloatTolerance > 1 {
			fail(c, http.StatusBadRequest, "浮点容差需在 (0, 1] 之间")
			return
		}
		set["floatTolerance"] = *req.FloatTolerance
	}
	// 文件 IO 文件名对与说明（v0.5b-3）：名字允许清空（回退默认 problem.in/problem.out），
	// 非法文件名字符（路径分隔/空白）拒绝；说明为 Markdown，仅启用场景渲染
	validIOName := func(s string) bool {
		return !strings.ContainsAny(s, "/\\ \t") && len(s) <= 64
	}
	if req.FileIOInput != nil {
		t := strings.TrimSpace(*req.FileIOInput)
		if t != "" && !validIOName(t) {
			fail(c, http.StatusBadRequest, "输入文件名不可含路径分隔符或空白")
			return
		}
		set["fileIoInput"] = t
	}
	if req.FileIOOutput != nil {
		t := strings.TrimSpace(*req.FileIOOutput)
		if t != "" && !validIOName(t) {
			fail(c, http.StatusBadRequest, "输出文件名不可含路径分隔符或空白")
			return
		}
		set["fileIoOutput"] = t
	}
	if req.FileIODesc != nil {
		if len(*req.FileIODesc) > 256<<10 {
			fail(c, http.StatusBadRequest, "文件 IO 说明过长（上限 256KB）")
			return
		}
		set["fileIoDesc"] = *req.FileIODesc
	}
	if len(set) == 0 {
		fail(c, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, set); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"updated": id})
}

// ---- helpers ----

func formValue(form *multipart.Form, key string) string {
	if v, ok := form.Value[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func parsePositiveFloat(s string, def float64) float64 {
	var f float64
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != 'e' && c != '-' && c != '+' {
			return def
		}
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &f); err != nil || f <= 0 {
		return def
	}
	return f
}

func quoteYaml(s string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"") + "\""
}

// safeExtractZip 把 zip 解压到 dst：拒绝穿越成分、条目数与解压总量超限、非法成对结构不在此校验
// （交给 Importer）。
func safeExtractZip(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return errors.New("不是合法的 zip 文件")
	}
	defer zr.Close()
	dstRoot := filepath.Clean(dst)
	total := int64(0)
	if len(zr.File) > maxZipEntries {
		return errors.New("压缩包条目过多")
	}
	for _, f := range zr.File {
		name := f.Name
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
			return errors.New("压缩包含非法路径")
		}
		if f.Mode().IsDir() {
			continue
		}
		if f.UncompressedSize64 > maxZipFile {
			return errors.New("压缩包内单文件超过 20MB")
		}
		total += int64(f.UncompressedSize64)
		if total > maxZipUncompressed {
			return errors.New("压缩包解压总量超过 200MB")
		}
		dest := filepath.Join(dstRoot, filepath.FromSlash(path.Clean("/"+name)))
		if !strings.HasPrefix(dest, dstRoot+string(os.PathSeparator)) {
			return errors.New("压缩包含非法路径")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}
