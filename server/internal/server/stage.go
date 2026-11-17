package server

// 测试数据 zip 暂存上传（v0.97.15）：选包即传、确认才生效。
// 建题/编辑页选入 zip 立即上传到容器临时目录暂存并即时校验（坏包当场报错，
// 不用等确认时才发现）；点「确认创建/确认修改」时表单带 stage=<id> 引用暂存
// 文件走原有导入链路，成功后暂存删除、旧数据按整包替换语义清除；未确认的
// 暂存由 TTL（24h）+ 启动清扫回收。无状态实现：id 即文件名
// soj-stage-<uid>-<ts>-<rand16>.zip，apply 端正则锚定解析属主与时间——无内存
// 表、重启不丢、无 IDOR（属主不匹配即拒）。
// 动机：大 zip 直传曾把建题/编辑请求拖成分钟级整页遮罩；剥离出提交路径后
// 上传有进度、页面全程可操作，确认只剩秒级落库。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"soj/internal/archive"
)

const (
	stageTTL       = 24 * time.Hour // 暂存有效期：选包到确认的正常间隔远小于此
	stagePerUser   = 3              // 每用户暂存上限（含未确认残留），超删最旧
	stageFilePrefix = "soj-stage-"
)

// stageFileRe 锚定暂存文件名（= stage id）：uid 与上传时间嵌入名内，apply 端免查表校验。
var stageFileRe = regexp.MustCompile(`^soj-stage-(\d{1,19})-(\d{1,19})-([0-9a-f]{16})\.zip$`)

// StageTestdata POST /api/problems/testdata/stage：选包即传的暂存入口。
// 收 multipart file（数据 zip）→ 流式落盘 → 与建题/替换同口径即时校验
//（safeExtractZip 三闸 + 摊平唯一根目录 + data/ 存在）→ 返回 {stage,name,size}。
func (h *Handlers) StageTestdata(c *gin.Context) {
	uid, _, _, logged := currentUser(c)
	if !logged {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRequest)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "上传失败：请求体过大（数据 zip 上限 64MB）或格式错误")
		return
	}
	files := form.File["file"]
	if len(files) != 1 {
		fail(c, http.StatusBadRequest, "请上传一个数据 zip（根下含 data/ 目录）")
		return
	}

	// 有界：同一用户暂存最多 stagePerUser 个（反复换包会留未确认残留），超删最旧
	evictStaleStage(uid)

	id := fmt.Sprintf("%s%d-%d-%s.zip", stageFilePrefix, uid, time.Now().Unix(), randHex(8))
	path := filepath.Join(os.TempDir(), id)
	fh, err := files[0].Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取上传文件失败："+err.Error())
		return
	}
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fh.Close()
		fail(c, http.StatusInternalServerError, "服务器暂存文件创建失败")
		return
	}
	size, err := io.Copy(dst, fh)
	fh.Close()
	dst.Close()
	if err != nil {
		os.Remove(path)
		fail(c, http.StatusInternalServerError, "保存上传文件失败："+err.Error())
		return
	}

	// 即时校验：与确认时同一条链（坏包在此刻报给用户，进度条旁直接可见）
	if msg := validateStagedZip(path); msg != "" {
		os.Remove(path)
		fail(c, http.StatusBadRequest, msg)
		return
	}
	ok(c, gin.H{"stage": id, "name": files[0].Filename, "size": size})
}

// validateStagedZip 与建题/替换一致的包合法性预检；返回空串 = 合法。
func validateStagedZip(zipPath string) string {
	tmpDir, err := os.MkdirTemp("", "soj-stage-check-")
	if err != nil {
		return "服务器临时目录创建失败"
	}
	defer os.RemoveAll(tmpDir)
	if err := safeExtractZip(zipPath, tmpDir); err != nil {
		return "数据包不合法：" + err.Error()
	}
	if err := archive.UnwrapSingleRootDir(tmpDir); err != nil {
		return "整理数据目录失败"
	}
	// 根平铺兼容（v0.97.16）：无 data/ 但根目录是数据对时自动收拢，坏结构在此刻报给用户
	if !ensureDataDir(tmpDir) {
		return "数据 zip 里未发现测试数据（数据对 *.in + *.out/.ans 平铺或包在 data/ 目录均可；带题面的题包请走「导入题目」）"
	}
	return ""
}

// resolveUploadZip 建题/替换共用的 zip 来源裁决（v0.97.15）：表单带 stage=<id>
//（暂存式上传）→ 校验属主与 TTL 后返回暂存路径（staged=true，apply 成功后由
// 调用方删除）；否则回落 multipart zip part → 流式落临时副本返回（staged=false，
// 调用方 defer 删除）。两者都没有 → path=""（建题可选、替换必填由调用方判）。
// 同时提供 stage 与 zip part 视为客户端 bug 直接拒绝。
func resolveUploadZip(form *multipart.Form, uid int64) (path string, staged bool, err error) {
	stageID := formValue(form, "stage")
	zips := form.File["zip"]
	if stageID != "" && len(zips) > 0 {
		return "", false, fmt.Errorf("stage 与 zip 不能同时提供")
	}
	if len(zips) > 1 {
		return "", false, fmt.Errorf("请上传一个数据 zip（根下含 data/ 目录）")
	}
	if stageID != "" {
		p, err := stagedZipPath(uid, stageID)
		return p, true, err
	}
	if len(zips) == 1 {
		tmp, err := os.CreateTemp("", "soj-upload-*.zip")
		if err != nil {
			return "", false, fmt.Errorf("服务器临时文件创建失败")
		}
		fh, err := zips[0].Open()
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", false, fmt.Errorf("读取上传文件失败")
		}
		_, cerr := io.Copy(tmp, fh)
		fh.Close()
		tmp.Close()
		if cerr != nil {
			os.Remove(tmp.Name())
			return "", false, fmt.Errorf("保存上传文件失败")
		}
		return tmp.Name(), false, nil
	}
	return "", false, nil
}

// stagedZipPath 校验 stage id（格式锚定 + 属主 + TTL + 文件存在）并返回绝对路径。
func stagedZipPath(uid int64, id string) (string, error) {
	m := stageFileRe.FindStringSubmatch(id)
	if m == nil {
		return "", fmt.Errorf("暂存数据标识不合法，请重新上传")
	}
	owner, _ := strconv.ParseInt(m[1], 10, 64)
	if owner != uid {
		return "", fmt.Errorf("无权使用该暂存数据")
	}
	ts, _ := strconv.ParseInt(m[2], 10, 64)
	if d := time.Since(time.Unix(ts, 0)); d < 0 || d > stageTTL {
		return "", fmt.Errorf("暂存数据已过期（上传后 24 小时内有效），请重新上传")
	}
	path := filepath.Join(os.TempDir(), id)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("暂存数据不存在或已被清理，请重新上传")
	}
	return path, nil
}

// evictStaleStage 同一用户暂存超上限时删最旧（文件名内嵌同 uid 的 ts，字典序即时间序）。
func evictStaleStage(uid int64) {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), fmt.Sprintf("%s%d-*.zip", stageFilePrefix, uid)))
	if len(matches) < stagePerUser {
		return
	}
	// 新包即将落盘：先清出空位（保留最新 stagePerUser-1 个）
	for _, old := range matches[:len(matches)-(stagePerUser-1)] {
		os.Remove(old)
	}
}

// CleanupStaleStage 清扫容器临时目录下过期的暂存 zip（main.go 启动时与
// CleanupStaleImportTemp 同址调用；阈值须 ≥ stageTTL，避免误杀进行中的暂存）。
func CleanupStaleStage(olderThan time.Duration) int {
	removed := 0
	cutoff := time.Now().Add(-olderThan)
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), stageFilePrefix+"*.zip"))
	if err != nil {
		return 0
	}
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(m) == nil {
			removed++
		}
	}
	return removed
}

// randHex n 字节加密随机数的十六进制（文件名随机段；emailauth 同源写法）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见：退化为时间熵，文件名仍满足唯一性（O_EXCL 兜底冲突）
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
