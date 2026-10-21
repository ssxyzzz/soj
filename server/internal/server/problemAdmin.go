package server

// 网页建题与元数据编辑（v0.5.2-1；题面 Markdown，v0.8 起单一事实源）：
//   - POST /api/problems：multipart 建题（元数据字段 + 题面 Markdown + 测试数据 zip），
//     zip 只需 cases/<N>/in|ans 结构（根下平铺 N.in/N.ans 或顶层单目录包裹亦可，
//     解压后归一/下钻），problem.yaml 由表单生成；
//   - PATCH /api/problems/:id/meta：编辑标题/时限/内存/比对模式（problemEditable 校验）。
// zip 安全面三道闸：请求体上限、条目数与解压总量上限、条目名穿越成分拒绝。

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"gopkg.in/yaml.v3"

	"soj/internal/repo"
)

// zipNameRe 匹配卷内测试点路径 cases/<N>/in|ans（下载平铺导出用）。
var zipNameRe = regexp.MustCompile(`^cases/(\d+)/(in|ans)$`)

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
	// 标签（v0.5.5）：级联选择直传节点 ID（JSON 数组字符串），存在性由 importer 校验
	var tagIDs []int64
	if raw := formValue(form, "tag_ids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &tagIDs); err != nil || len(tagIDs) > 50 {
			fail(c, http.StatusBadRequest, "tag_ids 参数错误")
			return
		}
	}
	zips := form.File["zip"]
	if len(zips) > 1 {
		fail(c, http.StatusBadRequest, "请上传一个测试数据 zip（含 cases/<N>/in|ans 结构）；也可以不上传直接创建（暂无数据的题目）")
		return
	}

	// 客观题（v0.9）：表单 question_json 定义题面作答，走同一导入管道入库
	if ptype := strings.TrimSpace(formValue(form, "type")); ptype == repo.TypeObjective {
		h.createObjectiveProblem(c, form, title, statement, tagIDs, uid, zips)
		return
	} else if ptype == repo.TypeFunction {
		h.createFunctionProblem(c, form, title, statement, tagIDs, uid, zips)
		return
	} else if ptype == repo.TypeInteractive {
		h.createInteractiveProblem(c, form, title, statement, tagIDs, uid, zips)
		return
	} else if ptype != "" && ptype != "default" {
		fail(c, http.StatusBadRequest, "暂不支持题型 "+ptype+"（当前支持：传统题 default、客观题 objective、函数题 function、交互题 interactive）")
		return
	}

	// 无数据建题（v0.6.1 验收反馈）：zip 可缺省——直接创建空壳题（cases 为空），
	// 题面/限制/标签照常入库；提交入口在本端点与判题侧都会拒绝无数据题，
	// 测试数据后续在编辑页整包上传补齐。
	if len(zips) == 0 {
		p := &repo.Problem{
			Title: title, Statement: statement, TimeLimitMS: timeLimit, MemoryLimitKB: memLimit,
			Compare: compare, FloatTolerance: tol,
			Cases: []repo.CaseMeta{}, CreatedBy: uid, CreatedAt: time.Now(),
		}
		if len(tagIDs) > 0 {
			if err := h.repo.ValidateTagRefs(c.Request.Context(), tagIDs); err != nil {
				fail(c, http.StatusBadRequest, err.Error())
				return
			}
			p.Tags = tagIDs
		}
		if err := h.repo.CreateProblemShell(c.Request.Context(), p); err != nil {
			fail(c, http.StatusInternalServerError, "建题失败："+err.Error())
			return
		}
		set := bson.M{}
		if len(form.File["checker"]) == 1 && p.CheckerFile == "" {
			if cf, err := form.File["checker"][0].Open(); err == nil {
				data, err := io.ReadAll(cf)
				cf.Close()
				name := form.File["checker"][0].Filename
				if err == nil && len(data) <= 256<<10 && strings.HasSuffix(strings.ToLower(name), ".cpp") {
					if err := h.store.WriteFile(p.ID, "checker.cpp", data); err == nil {
						set["checkerFile"] = "checker.cpp"
						set["checkerSha"] = fmt.Sprintf("%x", sha256.Sum256(data))
					}
				}
			}
		}
		if name := strings.TrimSpace(formValue(form, "file_io_name")); name != "" {
			if strings.ContainsAny(name, "/\\ \t.") || len(name) > 64 {
				fail(c, http.StatusBadRequest, "文件名仅支持字母/数字/下划线/连字符（不含扩展名）")
				return
			}
			set["fileIoInput"] = name + ".in"
			set["fileIoOutput"] = name + ".out"
		}
		if len(set) > 0 {
			if err := h.repo.UpdateProblemMeta(c.Request.Context(), p.ID, set); err != nil {
				fail(c, http.StatusInternalServerError, "建题成功但写入配置失败")
				return
			}
		}
		ok(c, gin.H{"id": p.ID})
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
	if err = unwrapSingleRootDir(tmpDir); err != nil {
		fail(c, http.StatusInternalServerError, "整理测试数据目录失败")
		return
	}

	// problem.yaml 由表单生成（zip 里若带了同名文件，以表单为准）；表单题面（Markdown）
	// 写进 statement.md 一起进导入管道——样例并入、数据卷归档与目录式导入同一条路
	yamlBytes, _ := yaml.Marshal(map[string]int{
		"timeLimitMs":   timeLimit,
		"memoryLimitKb": memLimit,
	})
	setup := map[string][]byte{
		"problem.yaml": append([]byte("title: "+quoteYaml(title)+"\n"), yamlBytes...),
	}
	if statement != "" {
		setup["statement.md"] = []byte(statement)
	}
	for name, data := range setup {
		if err := os.WriteFile(filepath.Join(tmpDir, name), data, 0o644); err != nil {
			fail(c, http.StatusInternalServerError, "写题目文件失败")
			return
		}
	}

	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid, TagIDs: tagIDs}).ImportDir(c.Request.Context(), tmpDir)
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
	// 文件 IO 基础文件名（v0.5.2-3，单字段）：填 problem 即 problem.in / problem.out；
	// zip 内 config.yaml 已声明 file_io_input/file_io_output 的以 config 为准
	if pr.FileIOInput == "" && pr.FileIOOutput == "" {
		if name := strings.TrimSpace(formValue(form, "file_io_name")); name != "" {
			if strings.ContainsAny(name, "/\\ \t.") || len(name) > 64 {
				fail(c, http.StatusBadRequest, "文件名仅支持字母/数字/下划线/连字符（不含扩展名）")
				return
			}
			set["fileIoInput"] = name + ".in"
			set["fileIoOutput"] = name + ".out"
		}
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

// UpdateFunctionFile 函数题 driver/template 即时替换（v0.9.1，编辑页不随保存提交）。
// file 路径参数取 driver|template；内容落卷固定名并更新元数据。
func (h *Handlers) UpdateFunctionFile(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	kind := c.Param("file") // driver | template
	if kind != "driver" && kind != "template" {
		fail(c, http.StatusBadRequest, "file 需为 driver 或 template")
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
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	files := form.File["file"]
	if len(files) != 1 {
		fail(c, http.StatusBadRequest, "请上传一个源文件")
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return
	}
	if len(data) > 256<<10 {
		fail(c, http.StatusBadRequest, "文件过大（上限 256KB）")
		return
	}
	name := kind + ".cpp"
	if err := h.provider().WriteFile(id, name, data); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	set := bson.M{}
	if kind == "driver" {
		set["driverFile"], set["driverSha"] = name, fmt.Sprintf("%x", sha256.Sum256(data))
	} else {
		set["templateFile"] = name
	}
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, set); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"file": name})
}

// UpdateInteractor 交互器即时替换（v0.9.2，判题懒编译缓存自动失效重编）。
func (h *Handlers) UpdateInteractor(c *gin.Context) {
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
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	files := form.File["file"]
	if len(files) != 1 {
		fail(c, http.StatusBadRequest, "请上传一个源文件")
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return
	}
	if len(data) > 256<<10 {
		fail(c, http.StatusBadRequest, "交互器过大（上限 256KB）")
		return
	}
	if err := h.provider().WriteFile(id, "interactor.cpp", data); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{
		"interactorFile":   "interactor.cpp",
		"interactorSha":    fmt.Sprintf("%x", sha256.Sum256(data)),
		"interactorFileId": "", // 编译缓存失效，判题时自动重编
	}); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"file": "interactor.cpp"})
}

// RemoveFunctionFile 移除函数题 template（driver 不允许移除——判题依赖）。
func (h *Handlers) RemoveFunctionFile(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	if c.Param("file") != "template" {
		fail(c, http.StatusBadRequest, "仅 template 可移除（driver 为判题依赖）")
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
	_ = h.provider().Delete(id, "template.cpp")
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{"templateFile": ""}); err != nil {
		fail(c, http.StatusInternalServerError, "移除失败")
		return
	}
	ok(c, gin.H{"removed": true})
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

// ReplaceProblemTestdata 整包替换题目测试数据（编辑题目 v0.5.2-4）：
// 旧测试点文件全部删除，按新 zip 重建 cases；元数据与题面正文保留，
// 样例章节按新数据重建；新包 config.yaml 可覆盖样例/子任务/文件 IO/校验器。
func (h *Handlers) ReplaceProblemTestdata(c *gin.Context) {
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRequest)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "请求体过大或格式错误（测试数据上限 64MB）")
		return
	}
	zips := form.File["zip"]
	if len(zips) != 1 {
		fail(c, http.StatusBadRequest, "请上传一个测试数据 zip（含 cases/<N>/in|ans 结构）")
		return
	}

	tmpZip, err := os.CreateTemp("", "soj-replace-*.zip")
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

	tmpDir, err := os.MkdirTemp("", "soj-replace-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	if err = safeExtractZip(tmpZip.Name(), tmpDir); err != nil {
		fail(c, http.StatusBadRequest, "测试数据包不合法："+err.Error())
		return
	}
	if err = unwrapSingleRootDir(tmpDir); err != nil {
		fail(c, http.StatusInternalServerError, "整理测试数据目录失败")
		return
	}
	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir}).ReplaceCases(c.Request.Context(), id, tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "替换失败："+err.Error())
		return
	}
	ok(c, gin.H{"cases": len(pr.Cases)})
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
		FileIOName     *string  `json:"file_io_name"`
		TagIDs         *[]int64 `json:"tag_ids"`        // 指针语义：缺省不动，空数组清空（v0.5.5）
		QuestionsJSON  *string  `json:"questions_json"` // 客观题整卷定义编辑（v0.9.3；仅客观题可带）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	// 客观题（v0.9）：题干/选项/答案/解析随 question 节整体替换；时空限制等传统题字段忽略
	if p.IsObjective() {
		if req.QuestionsJSON != nil {
			var qs []repo.ObjectiveQuestion
			if err := json.Unmarshal([]byte(*req.QuestionsJSON), &qs); err != nil {
				fail(c, http.StatusBadRequest, "客观题内容格式错误: "+err.Error())
				return
			}
			for i := range qs {
				repo.NormalizeObjective(&qs[i])
			}
			repo.NormalizeScores(qs)
			if err := repo.ValidateObjectiveSet(qs); err != nil {
				fail(c, http.StatusBadRequest, err.Error())
				return
			}
			setQ := bson.M{"questions": qs}
			if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, setQ); err != nil {
				fail(c, http.StatusInternalServerError, "保存失败")
				return
			}
		}
		if req.Title != nil {
			t := strings.TrimSpace(*req.Title)
			if t == "" || len([]rune(t)) > 64 {
				fail(c, http.StatusBadRequest, "标题需在 1-64 字符")
				return
			}
			setT := bson.M{"title": t}
			if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, setT); err != nil {
				fail(c, http.StatusInternalServerError, "保存失败")
				return
			}
		}
		if req.TagIDs != nil {
			if err := h.repo.ValidateTagRefs(c.Request.Context(), *req.TagIDs); err != nil {
				fail(c, http.StatusBadRequest, err.Error())
				return
			}
			setTags := bson.M{"tags": *req.TagIDs}
			if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, setTags); err != nil {
				fail(c, http.StatusInternalServerError, "保存失败")
				return
			}
		}
		ok(c, gin.H{"updated": id})
		return
	}
	if req.QuestionsJSON != nil {
		fail(c, http.StatusBadRequest, "questions_json 仅适用于客观题")
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
	// 文件 IO 基础文件名（v0.5.2-3，单字段）：填 problem 即 problem.in / problem.out；
	// 允许清空（回退默认 problem），非法文件名字符（路径分隔/空白/点开头的扩展名混淆）拒绝
	if req.FileIOName != nil {
		t := strings.TrimSpace(*req.FileIOName)
		if t != "" {
			if strings.ContainsAny(t, "/\\ \t.") || len(t) > 64 {
				fail(c, http.StatusBadRequest, "文件名仅支持字母/数字/下划线/连字符（不含扩展名）")
				return
			}
			set["fileIoInput"] = t + ".in"
			set["fileIoOutput"] = t + ".out"
		} else {
			set["fileIoInput"] = ""
			set["fileIoOutput"] = ""
		}
	}
	// 标签（v0.5.5）：节点 ID 数组，全部存在于树中才接受
	if req.TagIDs != nil {
		if err := h.repo.ValidateTagRefs(c.Request.Context(), *req.TagIDs); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		ids := *req.TagIDs
		if ids == nil {
			ids = []int64{}
		}
		set["tags"] = ids
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

// unwrapSingleRootDir 摊平「整个包套在顶层目录里」的解压结果（下载的数据包再压缩常见）：
// 根下剔除打包杂物后只剩一个子目录、且该子目录不叫 cases 时，把其内容整体上移一层，
// 反复进行直到根下不再满足该形态。cases 是根布局保留名（cases/<N>/in|ans），
// 根下仅有 cases 时说明已是标准结构，下钻反而会拆散它。
func unwrapSingleRootDir(dir string) error {
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		live := make([]os.DirEntry, 0, len(entries))
		for _, e := range entries {
			if !isPackagingJunk(e.Name()) {
				live = append(live, e)
			}
		}
		if len(live) != 1 || !live[0].IsDir() || live[0].Name() == "cases" {
			return nil
		}
		inner := filepath.Join(dir, live[0].Name())
		sub, err := os.ReadDir(inner)
		if err != nil {
			return err
		}
		for _, e := range sub {
			if err := os.Rename(filepath.Join(inner, e.Name()), filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
		if err := os.Remove(inner); err != nil {
			return err
		}
	}
}

// isPackagingJunk 压缩工具附带的系统杂物：macOS 的 __MACOSX/、.DS_Store 与 ._ 资源分叉文件。
func isPackagingJunk(name string) bool {
	return name == "__MACOSX" || name == ".DS_Store" || strings.HasPrefix(name, "._")
}

// DownloadProblemTestdata 下载题目测试数据包（zip，v0.8.3）：
// problem.data.download 权限位 + 系统设置「每日可下载题数」限额（0 = 不限制；
// 同一题当天重复下载不重复计数）。内容 = 数据卷整目录镜像（cases / problem.yaml /
// config.yaml / statement.md / 校验器 / extra 存档），重新上传可完整重建。
func (h *Handlers) DownloadProblemTestdata(c *gin.Context) {
	id, okID := parseID(c, "id")
	if !okID {
		return
	}
	uid, _, role, _ := currentUser(c)
	p, err := h.repo.GetProblem(c.Request.Context(), id)
	if repo.IsNotFound(err) {
		fail(c, http.StatusNotFound, "题目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, "读取题目失败")
		return
	}
	// root 恒不限量；其余权限位持有者按系统设置限额（0 = 不限制）
	if role != repo.RoleRoot {
		setting, err := h.repo.GetSystemSettings(c.Request.Context())
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取设置失败")
			return
		}
		if limit := setting.DownloadDailyLimit(); limit > 0 {
			allowed, err := h.repo.TestDataDownloadAllowed(c.Request.Context(), uid, id, limit)
			if err != nil {
				fail(c, http.StatusInternalServerError, "限额校验失败")
				return
			}
			if !allowed {
				fail(c, http.StatusForbidden, fmt.Sprintf("已达每日下载数据上限（%d 题/天），请明日再试", limit))
				return
			}
		}
	}

	// 权威导出（v0.9）：problem.yaml / statement.md 按 db 当前值渲染（网页编辑后卷内
	// 副本会过期），卷内同名文件让位不打包；客观题无 cases 也允许导出（题面即全部）
	skip := map[string]bool{"problem.yaml": true, "config.yaml": true, "statement.md": true}
	var rels []string
	if err := h.provider().Walk(id, func(rel string) error {
		if !skip[rel] {
			rels = append(rels, rel)
		}
		return nil
	}); err != nil {
		fail(c, http.StatusInternalServerError, "读取测试数据失败")
		return
	}
	// 测试点平铺导出（v0.9 验收反馈）：cases/<N>/in|ans → 根下 <N>.in / <N>.ans，
	// 下载包与常见题包形态一致（重新上传按平铺归一无感接收）
	flat := func(rel string) string {
		m := zipNameRe.FindStringSubmatch(rel)
		if m == nil {
			return rel
		}
		return m[1] + "." + m[2]
	}
	if len(rels) == 0 && !p.IsObjective() {
		fail(c, http.StatusNotFound, "该题还没有测试数据")
		return
	}

	c.Header("Content-Type", "application/zip")
	// 文件名 = 题目 ID + 标题（problem - 16 - Atbash 密码.zip）；中文走 RFC 5987
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": fmt.Sprintf("problem - %d - %s.zip", id, downloadSafeName(p.Title)),
	}))
	zw := zip.NewWriter(c.Writer)
	imp := &repo.Importer{Repo: h.repo}
	if yb := imp.RenderProblemYAML(c.Request.Context(), p); len(yb) > 0 {
		if w, err := zw.Create("problem.yaml"); err == nil {
			_, _ = w.Write(yb)
		}
	}
	if p.Statement != "" {
		if w, err := zw.Create("statement.md"); err == nil {
			_, _ = w.Write([]byte(p.Statement))
		}
	}
	for _, rel := range rels {
		f, err := h.provider().Open(id, rel)
		if err != nil {
			return // 响应已开始，只能截断（本地卷文件在打包间隙被删的极端场景）
		}
		w, err := zw.Create(flat(rel))
		if err == nil {
			_, err = io.Copy(w, f)
		}
		f.Close()
		if err != nil {
			return
		}
	}
	_ = zw.Close()
	// 打包已交付后才记账（root 不记账）；记账失败不影响本次下载
	if role != repo.RoleRoot {
		if err := h.repo.RecordTestDataDownload(c.Request.Context(), uid, id); err != nil {
			log.Printf("[data-download] 记录下载失败 uid=%d pid=%d: %v", uid, id, err)
		}
	}
}

// createFunctionProblem 函数题建题（v0.9.1）：driver（必须）/template（可选）来自表单文件，
// 与 zip（可选，含 cases）一起进 ImportDir 管道；无 zip 时空壳入库、数据编辑页补齐。
func (h *Handlers) createFunctionProblem(c *gin.Context, form *multipart.Form, title, statement string, tagIDs []int64, uid int64, zips []*multipart.FileHeader) {
	tmpDir, err := os.MkdirTemp("", "soj-create-func-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	yb, _ := yaml.Marshal(map[string]any{"title": title, "type": repo.TypeFunction})
	if err := os.WriteFile(filepath.Join(tmpDir, "problem.yaml"), yb, 0o644); err != nil {
		fail(c, http.StatusInternalServerError, "写题目文件失败")
		return
	}
	if statement != "" {
		if err := os.WriteFile(filepath.Join(tmpDir, "statement.md"), []byte(statement), 0o644); err != nil {
			fail(c, http.StatusInternalServerError, "写题目文件失败")
			return
		}
	}
	saveFormFile := func(field, dstName string) (bool, error) {
		files := form.File[field]
		if len(files) != 1 {
			return false, nil
		}
		f, err := files[0].Open()
		if err != nil {
			return false, err
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			return false, err
		}
		if len(data) > 256<<10 {
			return false, fmt.Errorf("%s 过大（上限 256KB）", dstName)
		}
		return true, os.WriteFile(filepath.Join(tmpDir, dstName), data, 0o644)
	}
	if _, err := saveFormFile("driver", "driver.cpp"); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := saveFormFile("template", "template.cpp"); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(form.File["driver"]) != 1 && len(zips) == 0 {
		fail(c, http.StatusBadRequest, "函数题需要 driver.cpp（含 main、调用学生实现的函数）：表单上传或放进 zip 均可")
		return
	}
	// zip（可选）：解压后并入同一导入目录（题包内 driver/template/cases 均可）
	if len(zips) > 0 {
		if err := extractZipInto(zips[0], tmpDir); err != nil {
			fail(c, http.StatusBadRequest, "测试数据包不合法："+err.Error())
			return
		}
		if err := unwrapSingleRootDir(tmpDir); err != nil {
			fail(c, http.StatusInternalServerError, "整理测试数据目录失败")
			return
		}
	}
	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid, TagIDs: tagIDs}).ImportDir(c.Request.Context(), tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败："+err.Error())
		return
	}
	ok(c, gin.H{"id": pr.ID, "source": "web"})
}

// createInteractiveProblem 交互题建题（v0.9.2）：interactor 必须有（表单或 zip），
// 测试数据可后补（ans 文件可缺省——判定权在交互器）。
func (h *Handlers) createInteractiveProblem(c *gin.Context, form *multipart.Form, title, statement string, tagIDs []int64, uid int64, zips []*multipart.FileHeader) {
	tmpDir, err := os.MkdirTemp("", "soj-create-int-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	yb, _ := yaml.Marshal(map[string]any{"title": title, "type": repo.TypeInteractive})
	if err := os.WriteFile(filepath.Join(tmpDir, "problem.yaml"), yb, 0o644); err != nil {
		fail(c, http.StatusInternalServerError, "写题目文件失败")
		return
	}
	if statement != "" {
		if err := os.WriteFile(filepath.Join(tmpDir, "statement.md"), []byte(statement), 0o644); err != nil {
			fail(c, http.StatusInternalServerError, "写题目文件失败")
			return
		}
	}
	if files := form.File["interactor"]; len(files) == 1 {
		if f, ferr := files[0].Open(); ferr == nil {
			data, _ := io.ReadAll(f)
			f.Close()
			if len(data) > 256<<10 {
				fail(c, http.StatusBadRequest, "交互器过大（上限 256KB）")
				return
			}
			_ = os.WriteFile(filepath.Join(tmpDir, "interactor.cpp"), data, 0o644)
		}
	}
	if len(zips) > 0 {
		if err := extractZipInto(zips[0], tmpDir); err != nil {
			fail(c, http.StatusBadRequest, "测试数据包不合法："+err.Error())
			return
		}
		if err := unwrapSingleRootDir(tmpDir); err != nil {
			fail(c, http.StatusInternalServerError, "整理测试数据目录失败")
			return
		}
	}
	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid, TagIDs: tagIDs}).ImportDir(c.Request.Context(), tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败："+err.Error())
		return
	}
	ok(c, gin.H{"id": pr.ID, "source": "web"})
}

// extractZipInto 把上传 zip 安全解压进 dst（建题临时目录）。
func extractZipInto(fh *multipart.FileHeader, dst string) error {
	tmpZip, err := os.CreateTemp("", "soj-zip-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpZip.Name())
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = io.Copy(tmpZip, f); err != nil {
		tmpZip.Close()
		return err
	}
	tmpZip.Close()
	return safeExtractZip(tmpZip.Name(), dst)
}

// createObjectiveProblem 客观题建题（v0.9）：question_json 解析校验后生成 v2 problem.yaml，
// 走 ImportDir 客观题路径入库（无 zip；填空占位数校验在导入管道内完成）。
func (h *Handlers) createObjectiveProblem(c *gin.Context, form *multipart.Form, title, statement string, tagIDs []int64, uid int64, zips []*multipart.FileHeader) {
	if len(zips) > 0 {
		fail(c, http.StatusBadRequest, "客观题不需要测试数据包（请在题面里出题）")
		return
	}
	raw := formValue(form, "questions_json")
	if raw == "" {
		fail(c, http.StatusBadRequest, "缺少客观题内容（questions_json）")
		return
	}
	var qs []repo.ObjectiveQuestion
	if err := json.Unmarshal([]byte(raw), &qs); err != nil {
		fail(c, http.StatusBadRequest, "客观题内容格式错误: "+err.Error())
		return
	}
	for i := range qs {
		repo.NormalizeObjective(&qs[i])
	}
	repo.NormalizeScores(qs)
	if err := repo.ValidateObjectiveSet(qs); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	yb, err := repo.BuildObjectiveYAML(title, qs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "生成题目配置失败")
		return
	}
	tmpDir, err := os.MkdirTemp("", "soj-create-obj-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	files := map[string][]byte{"problem.yaml": yb}
	if statement != "" {
		files["statement.md"] = []byte(statement)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), data, 0o644); err != nil {
			fail(c, http.StatusInternalServerError, "写题目文件失败")
			return
		}
	}
	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid, TagIDs: tagIDs}).ImportDir(c.Request.Context(), tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败："+err.Error())
		return
	}
	ok(c, gin.H{"id": pr.ID, "source": "web"})
}

// downloadSafeName 下载文件名清理：替换 Windows/Linux 文件名非法字符为空格、压连续空白。
func downloadSafeName(name string) string {
	repl := strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", " ", "?", " ", "\"", " ", "<", " ", ">", " ", "|", " ",
	)
	out := strings.Join(strings.Fields(repl.Replace(name)), " ")
	if out == "" {
		out = "untitled"
	}
	return out
}
