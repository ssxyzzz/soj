package server

// 批量建号（v0.24 小批）：CSV 表格上传 + 模板下载，admin.user.manage 权限位（路由侧）。
// 列固定 id,username,password,nickname,role（id/昵称/角色可留空，表头序无关、按名映射）；
// # 注释行与空行忽略；逐行独立处理（best-effort），失败行全部列出不中断——班级场景
// 一次导入几十上百个号，单行错误不该让人重传整份表格。ID 占用冲突由唯一索引兜底，
// 手动 ID 建号走 CreateUserWithID（计数器 $max 避让，后续自增不撞）。

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

const (
	maxUserImportBytes = 2 << 20 // 单文件 2MB（1000 行 CSV 远用不满）
	maxUserImportRows  = 1000    // 单次最多建 1000 个号
)

// AdminUserImportTemplate GET /api/admin/users/import-template —— 建号模板下载（带 BOM，Excel 直开）。
func (h *Handlers) AdminUserImportTemplate(c *gin.Context) {
	rows := [][]string{
		{"# SOJ 批量建号模板：每行一个账号，以 # 开头的行会被忽略"},
		{"# 列说明：id 可留空（自动分配，从 100 起，也可手动指定）；username 需 3-32 位字母/数字/下划线；password 至少 8 位；nickname 留空默认同用户名；role 留空默认 default"},
		{"id", "username", "password", "nickname", "role"},
		{"1001", "zhangsan", "Passw0rd!", "张三", "default"},
		{"", "lisi", "Passw0rd!", "李四", "default"},
	}
	serveCSV(c, csvSafeName("SOJ批量建号模板.csv"), rows)
}

// userImportResultRow 单行结果（失败明细下发前端表格）。
type userImportResultRow struct {
	Row      int    `json:"row"`
	Username string `json:"username"`
	Reason   string `json:"reason"`
}

// stripCSVBOM 去掉首字段可能的 UTF-8 BOM（表格软件另存常带）。
func stripCSVBOM(s string) string {
	return strings.TrimPrefix(s, "\xEF\xBB\xBF")
}

// AdminImportUsers POST /api/admin/users/import —— CSV 批量建号。
func (h *Handlers) AdminImportUsers(c *gin.Context) {
	_, uname, _, _ := currentUser(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUserImportBytes)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "上传失败：请求体过大或格式错误（单文件上限 2MB）")
		return
	}
	files := form.File["file"]
	if len(files) == 0 {
		fail(c, http.StatusBadRequest, "未选择文件")
		return
	}
	fh := files[0]
	src, err := fh.Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return
	}
	defer src.Close()

	reader := csv.NewReader(io.LimitReader(src, maxUserImportBytes))
	reader.FieldsPerRecord = -1 // 列数自校验（表头按名映射），不依赖每行等列
	records, err := reader.ReadAll()
	if err != nil {
		fail(c, http.StatusBadRequest, "CSV 解析失败："+err.Error())
		return
	}
	if len(records) > maxUserImportRows+16 { // 16 行余量容纳注释与表头
		fail(c, http.StatusBadRequest, fmt.Sprintf("行数过多（上限 %d 个账号），请分批导入", maxUserImportRows))
		return
	}

	// 定位表头并按列名映射（首条非空非注释行）
	col := map[string]int{}
	headerSeen := false
	start := 0
	for i, rec := range records {
		if isUserImportSkipRow(rec) {
			continue
		}
		for j, cell := range rec {
			name := strings.ToLower(strings.TrimSpace(stripCSVBOM(cell)))
			if name != "" {
				col[name] = j
			}
		}
		for _, want := range []string{"username", "password"} {
			if _, ok := col[want]; !ok {
				fail(c, http.StatusBadRequest, "表头缺少列 "+want+"（请使用官方模板的列名：id,username,password,nickname,role）")
				return
			}
		}
		headerSeen = true
		start = i + 1
		break
	}
	if !headerSeen {
		fail(c, http.StatusBadRequest, "表格为空：请先在第一行填写列名（或直接使用下载的模板）")
		return
	}
	get := func(rec []string, name string) string {
		idx, ok := col[name]
		if !ok || idx >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[idx])
	}

	// 角色集合一次拉取（表内逐行查库太浪费）
	roles, err := h.repo.ListRoles(c.Request.Context())
	roleSet := map[string]bool{}
	if err == nil {
		for _, r := range roles {
			roleSet[r.ID] = true
		}
	}

	seenUsernames := map[string]bool{}
	failed := make([]userImportResultRow, 0, 8)
	success := 0
	rowNo := start
	for _, rec := range records[start:] {
		rowNo++
		if isUserImportSkipRow(rec) {
			continue
		}
		username := get(rec, "username")
		password := get(rec, "password")
		idStr := get(rec, "id")
		nickname := get(rec, "nickname")
		role := get(rec, "role")
		// CSV 数据行号（含表头计数的 1-based，跟 Excel 行号一致，方便定位）
		failRow := func(reason string) {
			failed = append(failed, userImportResultRow{Row: rowNo, Username: username, Reason: reason})
		}

		if username == "" && password == "" && idStr == "" && nickname == "" && role == "" {
			continue
		}
		lu := strings.ToLower(username)
		if !usernameRE.MatchString(username) {
			failRow("用户名需为 3-32 位字母/数字/下划线")
			continue
		}
		if seenUsernames[lu] {
			failRow("表内用户名重复")
			continue
		}
		if msg := passwordProblem(password); msg != "" {
			failRow(msg)
			continue
		}
		if role == "" {
			role = repo.RoleDefault
		}
		if !roleSet[role] {
			failRow("角色不存在：" + role)
			continue
		}
		if nickname == "" {
			nickname = username
		}
		seenUsernames[lu] = true

		if idStr != "" {
			id, perr := strconv.ParseInt(idStr, 10, 64)
			if perr != nil || id < 1 || id > 1_000_000_000 {
				failRow("用户 ID 需为正整数（上限 10 亿）")
				continue
			}
			_, err = h.repo.CreateUserWithID(c.Request.Context(), id, username, password, nickname, role)
		} else {
			_, err = h.repo.CreateUserWithPassword(c.Request.Context(), username, password, nickname, role)
		}
		if err != nil {
			if isDupKey(err) {
				if _, _, uerr := h.repo.GetUserByUsername(c.Request.Context(), username); uerr == nil {
					failRow("用户名已存在")
				} else if idStr != "" {
					failRow("用户 ID 已被占用")
				} else {
					failRow("用户已存在")
				}
				continue
			}
			failRow("创建失败（系统错误）")
			continue
		}
		success++
	}

	if success > 0 {
		_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
			Type: repo.SecUserImport, Username: uname,
			Detail: fmt.Sprintf("批量建号：成功 %d / 失败 %d（文件 %s）", success, len(failed), fh.Filename),
		})
	}
	ok(c, gin.H{
		"total":   success + len(failed),
		"success": success,
		"failed":  failed,
	})
}

// isUserImportSkipRow 空行与 # 注释行（模板顶部的说明行）。
func isUserImportSkipRow(rec []string) bool {
	for _, cell := range rec {
		t := strings.TrimSpace(stripCSVBOM(cell))
		if t == "" {
			continue
		}
		return strings.HasPrefix(t, "#")
	}
	return true // 全空行
}
