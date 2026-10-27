package server

// 网页建题与元数据编辑（v0.15 统一题包格式，import-design.md）：
//   - POST /api/problems：multipart 建题——表单元数据 + 题面 + 可选数据 zip（只认 data/），
//     构造临时 SOJ 题目录（problem.md ← 表单题面）走 ImportDir 单管道，全题型共用；
//   - PATCH /api/problems/:id/meta：编辑标题/时限/内存/比对/来源等（problemEditable 校验）；
//   - POST /api/problems/:id/testdata：数据/配置整包替换（zip 只认 data/）；
//   - GET  /api/problems/:id/testdata/download：权威导出新格式题包。
// zip 安全面三道闸：请求体上限、条目数与解压总量上限、条目名穿越成分拒绝。

import (
	"archive/zip"
	"context"
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
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/yaml.v3"

	"soj/internal/repo"
)

const (
	maxCreateRequest   = 64 << 20  // 建题/替换请求体上限（数据 zip 走建题页的小包；大包走导入）
	maxZipEntries      = 2000      // zip 条目数上限
	maxZipUncompressed = 200 << 20 // zip 解压总量上限（防炸弹）
	maxZipFile         = 20 << 20  // zip 单文件上限
)

// CreateProblem 网页建题（全题型单管道）：表单字段构造临时 SOJ 题目录
// （problem.md ← 表单题面、problem.yaml ← 标题/题型），数据 zip 只认 data/
// （含 config.yaml 与附属文件），表单上传的 checker/driver/template/interactor
// 落 data/ 固定名；题面以表单为唯一来源（包内不收题面）。
func (h *Handlers) CreateProblem(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRequest)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败：请求体过大或格式错误（数据 zip 上限 64MB，更大的包请走「导入题目」）")
		return
	}

	title := strings.TrimSpace(formValue(form, "title"))
	if title == "" || len([]rune(title)) > 64 {
		fail(c, http.StatusBadRequest, "标题需在 1-64 字符")
		return
	}
	ptype := strings.TrimSpace(formValue(form, "type"))
	switch ptype {
	case "", "default", repo.TypeObjective, repo.TypeFunction, repo.TypeInteractive:
	default:
		fail(c, http.StatusBadRequest, "暂不支持题型 "+ptype+"（当前支持：传统题 default、客观题 objective、函数题 function、交互题 interactive）")
		return
	}
	statement := formValue(form, "statement")
	if len(statement) > 256<<10 {
		fail(c, http.StatusBadRequest, "题面过长（上限 256KB）")
		return
	}
	difficulty, derr := formDifficulty(form)
	if derr != nil {
		fail(c, http.StatusBadRequest, derr.Error())
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
	var tagIDs []int64
	if raw := formValue(form, "tag_ids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &tagIDs); err != nil || len(tagIDs) > 50 {
			fail(c, http.StatusBadRequest, "tag_ids 参数错误")
			return
		}
	}
	// 来源标签树节点（2026-10-25 双树）：来源四字段退役后的唯一来源入口
	var metaTagIDs []int64
	if raw := formValue(form, "meta_tag_ids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metaTagIDs); err != nil || len(metaTagIDs) > 50 {
			fail(c, http.StatusBadRequest, "meta_tag_ids 参数错误")
			return
		}
		if err := h.repo.ValidateMetaTagRefs(c.Request.Context(), metaTagIDs); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	// 客观题：整卷定义（含答案）由表单直传（Importer.ObjectiveQuestions 权威）
	var objQuestions []repo.ObjectiveQuestion
	if ptype == repo.TypeObjective {
		objQuestions, err = parseObjectiveQuestionsForm(formValue(form, "questions_json"))
		if err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	zips := form.File["zip"]
	if len(zips) > 1 {
		fail(c, http.StatusBadRequest, "请上传一个数据 zip（根下含 data/ 目录：数据对 + config.yaml + 附属文件）")
		return
	}
	if ptype == repo.TypeObjective && len(zips) > 0 {
		fail(c, http.StatusBadRequest, "客观题不需要测试数据包（请在题面里出题）")
		return
	}

	// ---- 构造临时 SOJ 题目录 ----
	tmpDir, err := os.MkdirTemp("", "soj-create-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "problem.md"), []byte(statement), 0o644); err != nil {
		fail(c, http.StatusInternalServerError, "写题面失败")
		return
	}
	yb, _ := yaml.Marshal(struct {
		Title string `yaml:"title"`
		Type  string `yaml:"type"`
	}{Title: title, Type: ptype})
	if err := os.WriteFile(filepath.Join(tmpDir, "problem.yaml"), yb, 0o644); err != nil {
		fail(c, http.StatusInternalServerError, "写题目文件失败")
		return
	}

	imp := &repo.Importer{
		Repo: h.repo, DataDir: h.cfg.DataDir, CreatorUID: uid,
		TagIDs: tagIDs, MetaTagIDs: metaTagIDs, Difficulty: difficulty,
		Compare: compare, FloatTolerance: tol,
		ObjectiveQuestions: objQuestions,
	}
	if ptype != repo.TypeObjective {
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
		imp.TimeLimitMS, imp.MemoryLimitKB = timeLimit, memLimit
		if name := strings.TrimSpace(formValue(form, "file_io_name")); name != "" {
			if strings.ContainsAny(name, "/\\ \t.") || len(name) > 64 {
				fail(c, http.StatusBadRequest, "文件名仅支持字母/数字/下划线/连字符（不含扩展名）")
				return
			}
			imp.FileIOName = name
		}
	}

	// 数据 zip：解压 → 摊平唯一根目录 → 只取 data/（其余忽略；题面/元数据以表单为准）
	if len(zips) == 1 {
		extractDir, err := os.MkdirTemp("", "soj-create-zip-")
		if err != nil {
			fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
			return
		}
		defer os.RemoveAll(extractDir)
		if err := extractUploadZip(zips[0], extractDir); err != nil {
			fail(c, http.StatusBadRequest, "数据包不合法："+err.Error())
			return
		}
		if err := unwrapSingleRootDir(extractDir); err != nil {
			fail(c, http.StatusInternalServerError, "整理数据目录失败")
			return
		}
		if fi, err := os.Stat(filepath.Join(extractDir, "data")); err != nil || !fi.IsDir() {
			fail(c, http.StatusBadRequest, "数据 zip 缺少 data/ 目录（zip 内放 data/：数据对 + config.yaml + 附属文件；带题面的题包请走「导入题目」）")
			return
		}
		if err := os.Rename(filepath.Join(extractDir, "data"), filepath.Join(tmpDir, "data")); err != nil {
			fail(c, http.StatusInternalServerError, "整理数据目录失败")
			return
		}
	}

	// 表单上传的题型文件 → data/ 固定名
	saveFormTo := func(field, dstName string, max int) error {
		files := form.File[field]
		if len(files) != 1 {
			return nil
		}
		f, err := files[0].Open()
		if err != nil {
			return err
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		if len(data) > max {
			return fmt.Errorf("%s 过大（上限 %dKB）", dstName, max>>10)
		}
		if err := os.MkdirAll(filepath.Join(tmpDir, "data"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tmpDir, "data", dstName), data, 0o644)
	}
	if err := saveFormTo("checker", "checker.cpp", 256<<10); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	imp.Checker = "checker.cpp" // 表单上传即启用（ImportDir 内文件不存在则忽略）
	if ptype == repo.TypeFunction {
		if err := saveFormTo("driver", "driver.cpp", 256<<10); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		if err := saveFormTo("template", "template.cpp", 256<<10); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	if ptype == repo.TypeInteractive {
		if err := saveFormTo("interactor", "interactor.cpp", 256<<10); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	pr, err := imp.ImportDir(c.Request.Context(), tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "建题失败："+err.Error())
		return
	}
	// 挑战时长与答案解析视频（v0.23）：随建题表单一并落库（复用编辑同款校验）
	setMeta := bson.M{}
	if v := strings.TrimSpace(formValue(form, "video_url")); v != "" {
		if len(v) > 1024 {
			fail(c, http.StatusBadRequest, "视频链接过长（上限 1024 字符）")
			return
		}
		setMeta["videoUrl"] = v
	}
	if raw := formValue(form, "challenge_time_sec"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 60 || n > 14400 {
			fail(c, http.StatusBadRequest, "挑战时长需在 60–14400 秒（1 分钟 – 4 小时），留空按难度默认")
			return
		}
		setMeta["challengeTimeSec"] = n
	}
	if len(setMeta) > 0 {
		if err := h.repo.UpdateProblemMeta(c.Request.Context(), pr.ID, setMeta); err != nil {
			fail(c, http.StatusInternalServerError, "建题元数据保存失败")
			return
		}
	}
	ok(c, gin.H{"id": pr.ID})
}

// parseObjectiveQuestionsForm 建题表单客观题整卷定义解析与校验。
func parseObjectiveQuestionsForm(raw string) ([]repo.ObjectiveQuestion, error) {
	if raw == "" {
		return nil, fmt.Errorf("缺少客观题内容（questions_json）")
	}
	var qs []repo.ObjectiveQuestion
	if err := json.Unmarshal([]byte(raw), &qs); err != nil {
		return nil, fmt.Errorf("客观题内容格式错误: %w", err)
	}
	for i := range qs {
		repo.NormalizeObjective(&qs[i])
	}
	repo.NormalizeScores(qs)
	if err := repo.ValidateObjectiveSet(qs); err != nil {
		return nil, err
	}
	return qs, nil
}

// extractUploadZip 上传 zip 安全解压到 dst（建题/替换共用小限额）。
func extractUploadZip(fh *multipart.FileHeader, dst string) error {
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

// UpdateChecker 上传/替换题目校验器（C++ 源文件，problemEditable 语义）。
// 替换后清空编译产物缓存，判题时懒编译生效。落卷固定名 data/checker.cpp。
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
	if err := h.store.WriteFile(id, "data/checker.cpp", data); err != nil {
		fail(c, http.StatusInternalServerError, "保存校验器失败")
		return
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{
		"checkerFile": "data/checker.cpp", "checkerSha": sha, "checkerFileId": "",
	}); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"checker": "data/checker.cpp", "sha": sha})
}

// UpdateFunctionFile 函数题 driver/template 即时替换（编辑页不随保存提交）。
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
	name := "data/" + kind + ".cpp"
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

// UpdateInteractor 交互器即时替换（判题懒编译缓存自动失效重编）。
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
	if err := h.provider().WriteFile(id, "data/interactor.cpp", data); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{
		"interactorFile":   "data/interactor.cpp",
		"interactorSha":    fmt.Sprintf("%x", sha256.Sum256(data)),
		"interactorFileId": "", // 编译缓存失效，判题时自动重编
	}); err != nil {
		fail(c, http.StatusInternalServerError, "保存失败")
		return
	}
	ok(c, gin.H{"file": "data/interactor.cpp"})
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
	_ = h.provider().Delete(id, "data/template.cpp")
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
	_ = h.store.Delete(id, "data/checker.cpp")
	if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{
		"checkerFile": "", "checkerSha": "", "checkerFileId": "",
	}); err != nil {
		fail(c, http.StatusInternalServerError, "移除失败")
		return
	}
	ok(c, gin.H{"removed": true})
}

// ReplaceProblemTestdata 数据/配置整包替换（编辑题目；zip 只认 data/）：
// 旧数据/附属全删重建；题面与元数据不动；data/config.yaml 可覆盖时限内存/子任务/文件 IO/校验器。
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
		fail(c, http.StatusBadRequest, "请求体过大或格式错误（数据 zip 上限 64MB，更大的包请走「导入题目」）")
		return
	}
	zips := form.File["zip"]
	if len(zips) != 1 {
		fail(c, http.StatusBadRequest, "请上传一个数据 zip（根下含 data/ 目录）")
		return
	}

	tmpZip, err := os.CreateTemp("", "soj-replace-*.zip")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时文件创建失败")
		return
	}
	defer os.Remove(tmpZip.Name())
	if err := extractUploadZip(zips[0], tmpZip.Name()); err != nil {
		fail(c, http.StatusBadRequest, "数据包不合法："+err.Error())
		return
	}
	tmpDir, err := os.MkdirTemp("", "soj-replace-dir-")
	if err != nil {
		fail(c, http.StatusInternalServerError, "服务器临时目录创建失败")
		return
	}
	defer os.RemoveAll(tmpDir)
	if err := safeExtractZip(tmpZip.Name(), tmpDir); err != nil {
		fail(c, http.StatusBadRequest, "数据包不合法："+err.Error())
		return
	}
	if err := unwrapSingleRootDir(tmpDir); err != nil {
		fail(c, http.StatusInternalServerError, "整理数据目录失败")
		return
	}
	pr, err := (&repo.Importer{Repo: h.repo, DataDir: h.cfg.DataDir}).ReplaceData(c.Request.Context(), id, tmpDir)
	if err != nil {
		fail(c, http.StatusBadRequest, "替换失败："+err.Error())
		return
	}
	ok(c, gin.H{"cases": len(pr.Cases)})
}

// UpdateProblemMeta 编辑题目元数据（标题/时限/内存/比对模式/来源；题面走既有 statement 接口）。
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
		Title            *string  `json:"title"`
		TimeLimitMS      *int     `json:"time_limit_ms"`
		MemoryLimitKB    *int     `json:"memory_limit_kb"`
		Difficulty       *int     `json:"difficulty"`   // 难度 1–10（v0.11）
		MetaTagIDs       *[]int64 `json:"meta_tag_ids"` // 来源标签树节点（2026-10-25 双树，替换来源四字段）
		Compare          *string  `json:"compare"`
		FloatTolerance   *float64 `json:"float_tolerance"`
		FileIOName       *string  `json:"file_io_name"`
		TagIDs           *[]int64 `json:"tag_ids"`            // 指针语义：缺省不动，空数组清空
		QuestionsJSON    *string  `json:"questions_json"`     // 客观题整卷定义编辑（仅客观题可带）
		VideoURL         *string  `json:"video_url"`          // 答案解析视频链接（v0.23；空串清除）
		ChallengeTimeSec *int     `json:"challenge_time_sec"` // 挑战时长秒（v0.23；0 = 按难度默认）
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
		if req.Difficulty != nil {
			if !repo.ValidDifficulty(*req.Difficulty) {
				fail(c, http.StatusBadRequest, "难度需在 1-10 之间")
				return
			}
			if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{"difficulty": *req.Difficulty}); err != nil {
				fail(c, http.StatusInternalServerError, "保存失败")
				return
			}
		}
		if req.MetaTagIDs != nil {
			if err := h.repo.ValidateMetaTagRefs(c.Request.Context(), *req.MetaTagIDs); err != nil {
				fail(c, http.StatusBadRequest, err.Error())
				return
			}
			if err := h.repo.UpdateProblemMeta(c.Request.Context(), id, bson.M{"meta_tags": *req.MetaTagIDs}); err != nil {
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
	if req.Difficulty != nil {
		if !repo.ValidDifficulty(*req.Difficulty) {
			fail(c, http.StatusBadRequest, "难度需在 1-10 之间")
			return
		}
		set["difficulty"] = *req.Difficulty
	}
	if req.MetaTagIDs != nil {
		if err := h.repo.ValidateMetaTagRefs(c.Request.Context(), *req.MetaTagIDs); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		set["meta_tags"] = *req.MetaTagIDs
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
	// 文件 IO 基础文件名：填 problem 即 problem.in / problem.out；允许清空
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
	// 答案解析视频链接（v0.23）：B 站/直链 mp4，空串清除
	if req.VideoURL != nil {
		v := strings.TrimSpace(*req.VideoURL)
		if len(v) > 1024 {
			fail(c, http.StatusBadRequest, "视频链接过长（上限 1024 字符）")
			return
		}
		set["videoUrl"] = v
	}
	// 挑战时长（v0.23）：0 = 按难度×5min 默认；显式配置 60–14400 秒
	if req.ChallengeTimeSec != nil {
		n := *req.ChallengeTimeSec
		if n != 0 && (n < 60 || n > 14400) {
			fail(c, http.StatusBadRequest, "挑战时长需在 60–14400 秒（1 分钟 – 4 小时），留空按难度默认")
			return
		}
		set["challengeTimeSec"] = n
	}
	// 标签：节点 ID 数组，全部存在于树中才接受
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

// formDifficulty 建题表单难度（v0.11）：缺省 D3，越界报错。
func formDifficulty(form *multipart.Form) (int, error) {
	d := atoiDefault(formValue(form, "difficulty"), 3)
	if !repo.ValidDifficulty(d) {
		return 0, fmt.Errorf("难度需在 1-10 之间")
	}
	return d, nil
}

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

// safeExtractZip 把 zip 解压到 dst：拒绝穿越成分、条目数与解压总量超限。
func safeExtractZip(zipPath, dst string) error {
	return safeExtractZipLimits(zipPath, dst, maxZipEntries, maxZipUncompressed, maxZipFile, "20MB", "200MB", nil)
}

// zipEntryName 条目名归一：旧式 zip（Windows 资源管理器「发送到压缩文件夹」与
// 多数中文压缩工具）不设 UTF-8 标志位，文件名是系统 ANSI 字节（中文系统即 GBK），
// Go 原样透传成非法 UTF-8——目录名整体乱码。标志位缺失时按 GB18030 回退解码，
// 解码失败（其他码系产物）原样返回、不比现状差。
func zipEntryName(f *zip.File) string {
	if !f.NonUTF8 {
		return f.Name
	}
	if dec, err := simplifiedchinese.GB18030.NewDecoder().Bytes([]byte(f.Name)); err == nil {
		return string(dec)
	}
	return f.Name
}

// safeExtractZipLimits 参数化限额版（批量导入：整包 GB 级、条目随限额放宽）。
// progress（可 nil）：每写完一个条目回调（已解压字节, 总字节）——批量导入用它做解压实时进度。
func safeExtractZipLimits(zipPath, dst string, maxEntries int, maxTotal, maxSingle int64, singleMBHint, totalMBHint string, progress func(done, total int64)) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return errors.New("不是合法的 zip 文件")
	}
	defer zr.Close()
	dstRoot := filepath.Clean(dst)
	if len(zr.File) > maxEntries {
		return errors.New("压缩包条目过多")
	}
	// 预检遍历：路径合法性/单文件/总量限额 + 汇总总字节（进度分母），
	// 与写盘遍历分离——回调的 total 在写第一个文件前就已知
	total := int64(0)
	for _, f := range zr.File {
		name := zipEntryName(f)
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
			return errors.New("压缩包含非法路径")
		}
		if f.Mode().IsDir() {
			continue
		}
		if f.UncompressedSize64 > uint64(maxSingle) {
			return fmt.Errorf("压缩包内单文件超过 %s", singleMBHint)
		}
		total += int64(f.UncompressedSize64)
		if total > maxTotal {
			return fmt.Errorf("压缩包解压总量超过 %s", totalMBHint)
		}
	}
	done := int64(0)
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		dest := filepath.Join(dstRoot, filepath.FromSlash(path.Clean("/"+zipEntryName(f))))
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
		done += int64(f.UncompressedSize64)
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}

// unwrapSingleRootDir 摊平「整个包套在顶层目录里」的解压结果：根下剔除打包杂物后
// 只剩一个子目录、且该子目录不叫 data/cases 时，把其内容整体上移一层，反复进行。
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
		if len(live) != 1 || !live[0].IsDir() || live[0].Name() == "data" || live[0].Name() == "cases" {
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

// DownloadProblemTestdata 下载题包（权威导出新 SOJ 格式，import-design.md §3）：
// problem.md（题面）+ problem.yaml（元数据；客观题另带 answer.yaml 答案 + answer.md 解析）
// + data/config.yaml（按 db 当前值渲染）+ data/<卷内原名文件>。
// problem.data.download 权限位 + 每日限额（root 恒不限）。
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

	var rels []string
	if err := h.provider().Walk(id, func(rel string) error {
		if strings.HasPrefix(rel, "data/") {
			rels = append(rels, rel)
		}
		return nil
	}); err != nil {
		fail(c, http.StatusInternalServerError, "读取测试数据失败")
		return
	}
	if len(rels) == 0 && !p.IsObjective() {
		fail(c, http.StatusNotFound, "该题还没有测试数据")
		return
	}

	c.Header("Content-Type", "application/zip")
	// 文件名 = 题目 ID + 标题；中文走 RFC 5987
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": fmt.Sprintf("%d_%s.zip", id, downloadSafeName(p.Title)),
	}))
	zw := zip.NewWriter(c.Writer)
	if err := h.exportProblemInto(c.Request.Context(), zw, p, "", rels); err != nil {
		return // 响应已开始，只能截断
	}
	_ = zw.Close()
	// 打包已交付后才记账（root 不记账）
	if role != repo.RoleRoot {
		if err := h.repo.RecordTestDataDownload(c.Request.Context(), uid, id); err != nil {
			log.Printf("[data-download] 记录下载失败 uid=%d pid=%d: %v", uid, id, err)
		}
	}
}

// exportProblemInto 把单题的权威导出写进 zip：problem.yaml / problem.md /
// 客观题 answer.yaml+answer.md / data/config.yaml（按 db 当前值渲染）+ data/ 原名文件。
// dir 为空 = 单题下载根目录；多题导出传 "<id>_<安全标题>/" 子目录。rels = 该题
// data/ 文件清单（调用方先 Walk）。写失败返回错误（响应已开始时由调用方截断）。
func (h *Handlers) exportProblemInto(ctx context.Context, zw *zip.Writer, p *repo.Problem, dir string, rels []string) error {
	write := func(name string, b []byte) error {
		w, err := zw.Create(dir + name)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	imp := &repo.Importer{Repo: h.repo}
	if yb := imp.RenderProblemYAML(ctx, p); len(yb) > 0 {
		if err := write("problem.yaml", yb); err != nil {
			return err
		}
	}
	if p.Statement != "" {
		if err := write("problem.md", []byte(p.Statement)); err != nil {
			return err
		}
	}
	if p.IsObjective() {
		if yb := repo.RenderAnswerYAML(p.Questions); len(yb) > 0 {
			if err := write("answer.yaml", yb); err != nil {
				return err
			}
		}
		if md := repo.RenderAnswerMarkdown(p.Questions); len(md) > 0 {
			if err := write("answer.md", md); err != nil {
				return err
			}
		}
	} else if yb := imp.RenderConfigYAML(p); len(yb) > 0 {
		if err := write("data/config.yaml", yb); err != nil {
			return err
		}
	}
	for _, rel := range rels {
		f, err := h.provider().Open(p.ID, rel)
		if err != nil {
			return err
		}
		w, err := zw.Create(dir + rel)
		if err == nil {
			_, err = io.Copy(w, f)
		}
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// parsePositiveIDList 解析逗号分隔的正整数 ID（去重保序、上限 cap），多题导出用。
func parsePositiveIDList(raw string, cap int) ([]int64, error) {
	seen := map[int64]bool{}
	out := make([]int64, 0, 8)
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("ids 需为逗号分隔的正整数题号")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ids 不能为空")
	}
	if len(out) > cap {
		return nil, fmt.Errorf("单次最多导出 %d 题", cap)
	}
	return out, nil
}

// ExportProblems 多题打包导出（v0.18.1）：GET /api/problems/export?ids=1,2,3。
// 根下每题一个目录（<id>_<安全标题>/），内容与单题「下载数据」同一权威渲染内核。
// problem.data.download 权限位；每日限额合并预检（选中数 > 当日剩余即拒），
// 打包交付后按题记账（同题同天去重、root 不限不记账）。题目已删/读失败跳过不中断整批，
// 无数据的非客观题照常出壳包（多选批量不做单题 404）。
func (h *Handlers) ExportProblems(c *gin.Context) {
	ids, err := parsePositiveIDList(c.Query("ids"), 200)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	uid, _, role, _ := currentUser(c)
	if role != repo.RoleRoot {
		setting, err := h.repo.GetSystemSettings(c.Request.Context())
		if err != nil {
			fail(c, http.StatusInternalServerError, "读取设置失败")
			return
		}
		if limit := setting.DownloadDailyLimit(); limit > 0 {
			done, err := h.repo.TestDataDownloadedToday(c.Request.Context(), uid, ids)
			if err != nil {
				fail(c, http.StatusInternalServerError, "限额校验失败")
				return
			}
			if remaining := int64(limit) - done; int64(len(ids)) > remaining {
				fail(c, http.StatusForbidden, fmt.Sprintf(
					"今日数据下载限额剩 %d 题（上限 %d/天），已选 %d 题，请减少勾选或明日再试",
					remaining, limit, len(ids)))
				return
			}
		}
	}

	type problemExport struct {
		p    *repo.Problem
		rels []string
	}
	entries := make([]problemExport, 0, len(ids))
	for _, id := range ids {
		p, err := h.repo.GetProblem(c.Request.Context(), id)
		if err != nil {
			continue // 已删/读失败：跳过，不中断整批
		}
		var rels []string
		if err := h.provider().Walk(id, func(rel string) error {
			if strings.HasPrefix(rel, "data/") {
				rels = append(rels, rel)
			}
			return nil
		}); err != nil {
			continue
		}
		entries = append(entries, problemExport{p: p, rels: rels})
	}
	if len(entries) == 0 {
		fail(c, http.StatusNotFound, "选中的题目均不存在或已删除")
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": fmt.Sprintf("soj-packages-%d-%s.zip", len(entries), time.Now().Format("20060102")),
	}))
	zw := zip.NewWriter(c.Writer)
	exported := make([]int64, 0, len(entries))
	for _, e := range entries {
		dir := fmt.Sprintf("%d_%s/", e.p.ID, downloadSafeName(e.p.Title))
		if err := h.exportProblemInto(c.Request.Context(), zw, e.p, dir, e.rels); err != nil {
			break // 响应已开始，只能截断
		}
		exported = append(exported, e.p.ID)
	}
	_ = zw.Close()
	if role != repo.RoleRoot {
		for _, id := range exported {
			if err := h.repo.RecordTestDataDownload(c.Request.Context(), uid, id); err != nil {
				log.Printf("[data-download] 多题导出记账失败 uid=%d pid=%d: %v", uid, id, err)
			}
		}
	}
}

func downloadSafeName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		return r
	}, name)
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	return name
}
