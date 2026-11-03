package server

// 批量建号·中文花名册模式（v0.34，两段式）：上传识别（preview）→ 预览确认（可改
// id/用户名）→ 建号（commit）→ 前端导出账号 CSV。表头中英别名按名映射（序无关）；
// GBK/GB18030 自动识别（Excel 另存 CSV 常为 GBK）；用户名默认 = 姓名拼音小写连写 +
// id（张三 1001 → zhangsan1001，go-pinyin 默认读音，多音字预览可改）；初始密码每人
// 随机 8 位去混淆字符（明文只在预览/结果/导出三处出现，不落日志与审计）。
// 老英文模板（username/password 列填了优先）经同一张映射表自然可用。
// admin.user.manage 权限位（路由侧）；逐行 best-effort、失败行全量明细（沿 v0.24 口径）。

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/mozillazg/go-pinyin"
	"golang.org/x/text/encoding/simplifiedchinese"

	"soj/internal/repo"
)

const (
	maxUserImportBytes = 2 << 20 // 单文件 2MB（1000 行花名册远用不满）
	maxUserImportRows  = 1000    // 单次最多建 1000 个号
)

// userImportCols 列别名表：中文名/英文名 → 规范键（表头小写 trim 后查表）。
var userImportCols = map[string]string{
	// id / 学号（花名册语义：站内用户 ID，拼进默认用户名）
	"id": "id", "学号": "id", "编号": "id",
	// 姓名（落昵称，拼音生成默认用户名）
	"姓名": "name", "名字": "name", "nickname": "name", "昵称": "name",
	"性别": "gender", "gender": "gender",
	"年级": "grade", "grade": "grade",
	"学校": "school", "school": "school",
	"地区": "region", "region": "region",
	"角色": "role", "role": "role",
	// 老英文模板列：填了优先（用户名覆盖拼音生成、密码覆盖随机）
	"username": "username", "用户名": "username",
	"password": "password", "密码": "password",
}

// AdminUserImportTemplate GET /api/admin/users/import-template —— 中文花名册模板下载
//（带 BOM，Excel 直开；注释行说明列语义与生成规则）。
func (h *Handlers) AdminUserImportTemplate(c *gin.Context) {
	rows := [][]string{
		{"# SOJ 批量建号·中文花名册模板：每行一个账号，以 # 开头的行会被忽略"},
		{"# 必填：姓名；id 即站内用户 ID，可留空（自动顺延分配，预览表可见可改）；选填：性别、年级（如：初一/高一/7/12年级，自动归一为标准名）、学校、地区、角色（默认 default）"},
		{"# 用户名默认 = 姓名拼音小写 + id（张三 1001 → zhangsan1001），可在导入预览里逐行修改"},
		{"# 初始密码：密码列留空 = 每人随机生成（建号后导出表格发放）；填了用填的（8-64 位，勿用常见弱口令，全班统一密码请自行权衡风险）"},
		{"id", "姓名", "性别", "年级", "学校", "地区", "角色", "密码"},
		{"1001", "张三", "男", "初一", "市第一中学", "山东济南", "", ""},
		{"", "李四", "女", "高一", "市第一中学", "", "default", "Passw0rd!"},
	}
	serveCSV(c, csvSafeName("SOJ批量建号模板.csv"), rows)
}

// stripCSVBOM 去掉首字段可能的 UTF-8 BOM（表格软件另存常带）。
func stripCSVBOM(s string) string {
	return strings.TrimPrefix(s, "\xEF\xBB\xBF")
}

// decodeUserImportCSV 编码自动识别后解析：UTF-8（含 BOM）直接读，含非法 UTF-8
// 序列按 GB18030 解码再读（Excel 另存 CSV 默认 GBK，中文花名册刚需）。
func decodeUserImportCSV(raw []byte) ([][]string, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
	if !utf8.Valid(raw) {
		if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw); err == nil {
			raw = decoded
		}
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1 // 列数自校验（表头按名映射），不依赖每行等列
	return reader.ReadAll()
}

// pwAlphabet 随机密码字符集（8 位，去混淆：0/O/1/l/I 不入池）。
const pwAlphabet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"

// genImportPassword crypto/rand 随机 8 位初始密码。
func genImportPassword() string {
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(pwAlphabet))))
		if err != nil {
			b[i] = pwAlphabet[i%len(pwAlphabet)] // 系统熵源故障时退化为确定性序列（几乎不可能）
			continue
		}
		b[i] = pwAlphabet[n.Int64()]
	}
	return string(b)
}

// pinyinUsername 默认用户名：姓名拼音小写连写 + id。汉字段走 go-pinyin 默认读音
//（多音字由预览行内修改兜底）；非汉字仅保留字母数字小写直拼（英文名/数字），
// 空白与 ·/- 等分隔符丢弃。
func pinyinUsername(name string, id int64) string {
	args := pinyin.NewArgs()
	var sb strings.Builder
	var han strings.Builder
	flush := func() {
		if han.Len() == 0 {
			return
		}
		for _, part := range pinyin.LazyPinyin(han.String(), args) {
			sb.WriteString(strings.ToLower(part))
		}
		han.Reset()
	}
	for _, r := range name {
		if unicode.Is(unicode.Han, r) {
			han.WriteRune(r)
			continue
		}
		flush()
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	flush()
	return sb.String() + strconv.FormatInt(id, 10)
}

// userImportRow 一行识别结果（预览下发 + 确认回传同构；id/username 预览可改）。
type userImportRow struct {
	Row      int      `json:"row"` // CSV 行号（含表头 1-based，与 Excel 一致）
	ID       int64    `json:"id"`
	AutoID   bool     `json:"auto_id,omitempty"` // id 留空由预览预分配顺延号（前端「自动」标）
	Name     string   `json:"name"`
	Gender   string   `json:"gender"`
	Grade    string   `json:"grade"` // 标准显示名（别名输入解析即归一；空 = 未设置）
	School   string   `json:"school"`
	Region   string   `json:"region"`
	Role     string   `json:"role"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	Problems []string `json:"problems,omitempty"`
}

// userImportParse 上传文件 → 识别行集合（两段共用）：表头定位 + 逐行解析生成与
// 静态校验（格式/表内重复）；DB 占用查重由调用方批量补标。autoIDs 非空时，
// id 留空的行按队列逐个预分配（v0.61 按序补空号；空队列 = 无预分配来源，
// id 留空且未显式给用户名则报错）。
func userImportParse(records [][]string, autoIDs []int64) (rows []userImportRow, err error) {
	col := map[string]int{}
	headerSeen := false
	start := 0
	for i, rec := range records {
		if isUserImportSkipRow(rec) {
			continue
		}
		for j, cell := range rec {
			name := strings.ToLower(strings.TrimSpace(stripCSVBOM(cell)))
			if name == "" {
				continue
			}
			if key, ok := userImportCols[name]; ok {
				col[key] = j
			}
		}
		// 表头必填判定：花名册需「姓名」（id 列可整个不填，行内留空即预分配顺延号）；
		// 老英文模板有 username 列即可
		_, hasName := col["name"]
		_, hasUsername := col["username"]
		if !hasName && !hasUsername {
			return nil, fmt.Errorf("表头缺少必填列（花名册需「姓名」；老模板可用 username/password，详见下载的模板说明）")
		}
		headerSeen = true
		start = i + 1
		break
	}
	if !headerSeen {
		return nil, fmt.Errorf("表格为空：请先在第一行填写列名（或直接使用下载的模板）")
	}
	get := func(rec []string, key string) string {
		idx, ok := col[key]
		if !ok || idx >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[idx])
	}

	seenID := map[int64]bool{}
	seenUname := map[string]bool{}
	autoIdx := 0 // autoIDs 队列游标
	rowNo := start
	for _, rec := range records[start:] {
		rowNo++
		if isUserImportSkipRow(rec) {
			continue
		}
		row := userImportRow{
			Row:     rowNo,
			Name:    get(rec, "name"),
			Gender:  trimTo(get(rec, "gender"), 8),
			Grade:   strings.TrimSpace(get(rec, "grade")),
			School:  trimTo(get(rec, "school"), 64),
			Region:  trimTo(get(rec, "region"), 32),
			Role:    get(rec, "role"),
			Username: get(rec, "username"),
			Password: get(rec, "password"),
		}
		if row.Name == "" && row.Username == "" && row.Password == "" && row.Gender == "" &&
			row.Grade == "" && row.School == "" && row.Region == "" && row.Role == "" && get(rec, "id") == "" {
			continue // 全空行
		}

		// id：留空 = 预分配补空号（v0.61 按序回填已占用号之间的空缺，预览可见可改，
		// 建号即按此号）；填了校验正整数。无预分配来源（队列空）且未显式给用户名时才报必填错。
		if idStr := get(rec, "id"); idStr != "" {
			id, perr := strconv.ParseInt(idStr, 10, 64)
			if perr != nil || id < 1 || id > 1_000_000_000 {
				row.Problems = append(row.Problems, "id 需为正整数（上限 10 亿）")
			} else {
				row.ID = id
			}
		} else if autoIdx < len(autoIDs) {
			row.ID = autoIDs[autoIdx]
			row.AutoID = true
			autoIdx++
		} else if row.Username == "" {
			row.Problems = append(row.Problems, "id 必填（拼音用户名由它拼接）")
		}

		// 年级：可空；非空必须能识别（防脏档位），解析即归一为标准显示名
		//（填 7 / 七年级 → 预览与库内统一显示「初一」，三处入口同口径）
		if row.Grade != "" {
			if no := repo.ParseGrade(row.Grade); no > 0 {
				row.Grade = repo.GradeName(no)
			} else {
				row.Problems = append(row.Problems, "年级无法识别（支持：一年级…高三 / 初一…初三 / 已毕业 / 数字 1-12）")
			}
		}

		// 用户名：显式列优先，否则拼音 + id
		if row.Username == "" {
			if row.Name == "" {
				row.Problems = append(row.Problems, "姓名必填")
			} else if row.ID > 0 {
				row.Username = pinyinUsername(row.Name, row.ID)
			}
		}
		if row.Username != "" && !usernameRE.MatchString(row.Username) {
			row.Problems = append(row.Problems, "用户名需为 3-32 位字母/数字/下划线")
		}
		// 密码：显式列优先（合法性后判），否则预览阶段即生成（确认沿用同一批）
		if row.Password == "" {
			row.Password = genImportPassword()
		}
		if msg := passwordProblem(row.Password); msg != "" {
			row.Problems = append(row.Problems, msg)
		}

		// 表内重复
		if row.ID > 0 {
			if seenID[row.ID] {
				row.Problems = append(row.Problems, "表内 id 重复")
			}
			seenID[row.ID] = true
		}
		if row.Username != "" {
			lu := strings.ToLower(row.Username)
			if seenUname[lu] {
				row.Problems = append(row.Problems, "表内用户名重复")
			}
			seenUname[lu] = true
		}
		rows = append(rows, row)
	}
	if len(rows) > maxUserImportRows {
		return nil, fmt.Errorf("行数过多（上限 %d 个账号），请分批导入", maxUserImportRows)
	}
	return rows, nil
}

// markDBConflicts 批量查库补标占用冲突（ID/用户名小写归一判重）。
func (h *Handlers) markDBConflicts(c *gin.Context, rows []userImportRow) {
	ids := make([]int64, 0, len(rows))
	unames := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.ID > 0 {
			ids = append(ids, r.ID)
		}
		if r.Username != "" {
			unames = append(unames, r.Username)
		}
	}
	existingIDs, err := h.repo.ExistingUserIDs(c.Request.Context(), ids)
	if err != nil {
		return // 查重失败不拦预览，建号时唯一索引兜底
	}
	existingUnames, err := h.repo.ExistingUsernames(c.Request.Context(), unames)
	if err != nil {
		return
	}
	for i := range rows {
		if rows[i].ID > 0 && existingIDs[rows[i].ID] {
			rows[i].Problems = append(rows[i].Problems, "用户 ID 已被占用")
		}
		if rows[i].Username != "" && existingUnames[strings.ToLower(rows[i].Username)] {
			rows[i].Problems = append(rows[i].Problems, "用户名已存在")
		}
	}
}

// readImportUploadFile 取上传文件原文（2MB 上限）。
func readImportUploadFile(c *gin.Context) ([]byte, string, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUserImportBytes)
	form, err := c.MultipartForm()
	if err != nil {
		fail(c, http.StatusBadRequest, "上传失败：请求体过大或格式错误（单文件上限 2MB）")
		return nil, "", false
	}
	files := form.File["file"]
	if len(files) == 0 {
		fail(c, http.StatusBadRequest, "未选择文件")
		return nil, "", false
	}
	fh := files[0]
	src, err := fh.Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return nil, "", false
	}
	defer src.Close()
	raw, err := io.ReadAll(io.LimitReader(src, maxUserImportBytes))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return nil, "", false
	}
	return raw, fh.Filename, true
}

// AdminUserImportPreview POST /api/admin/users/import-preview —— 第一段：上传识别。
// 返回逐行识别结果（含生成的用户名/密码与问题清单），前端预览表展示、可改后确认。
// id 留空的行按「注册起点起的前 n 个空缺号」预分配（PeekUserSeqs 只读不落库；
// 真正建号按预览号指定创建，预览与建号之间的并发抢占由唯一索引兜底、行失败如实展示）。
func (h *Handlers) AdminUserImportPreview(c *gin.Context) {
	raw, _, okFile := readImportUploadFile(c)
	if !okFile {
		return
	}
	records, err := decodeUserImportCSV(raw)
	if err != nil {
		fail(c, http.StatusBadRequest, "CSV 解析失败："+err.Error())
		return
	}
	// 队列长度按记录行数上界给足（PeekUserSeqs 恒返回足量：洞不足尾部 max+1 顺延）
	autoIDs, perr := h.repo.PeekUserSeqs(c.Request.Context(), int64(len(records))+1)
	if perr != nil {
		autoIDs = nil // 预读失败降级：id 留空且未显式给用户名的行按必填报错
	}
	rows, err := userImportParse(records, autoIDs)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	h.markDBConflicts(c, rows)
	valid := 0
	for _, r := range rows {
		if len(r.Problems) == 0 {
			valid++
		}
	}
	ok(c, gin.H{"rows": rows, "total": len(rows), "valid": valid})
}

// AdminUserImportCommit POST /api/admin/users/import-commit —— 第二段：确认建号。
// 回传行全量重校验（不信任预览数据），逐行 best-effort；失败行全量明细。
func (h *Handlers) AdminUserImportCommit(c *gin.Context) {
	_, uname, _, _ := currentUser(c)
	var req struct {
		Rows []userImportRow `json:"rows"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if len(req.Rows) == 0 {
		fail(c, http.StatusBadRequest, "没有待导入的行")
		return
	}
	if len(req.Rows) > maxUserImportRows {
		fail(c, http.StatusBadRequest, fmt.Sprintf("行数过多（上限 %d），请分批导入", maxUserImportRows))
		return
	}

	// 角色集合一次拉取
	roles, err := h.repo.ListRoles(c.Request.Context())
	roleSet := map[string]bool{}
	if err == nil {
		for _, r := range roles {
			roleSet[r.ID] = true
		}
	}

	type failRow struct {
		Row      int    `json:"row"`
		Username string `json:"username"`
		Reason   string `json:"reason"`
	}
	seenID := map[int64]bool{}
	seenUname := map[string]bool{}
	failed := make([]failRow, 0, 8)
	success := 0
	for _, row := range req.Rows {
		row.Username = strings.TrimSpace(row.Username)
		row.Password = strings.TrimSpace(row.Password)
		row.Name = strings.TrimSpace(row.Name)
		reason := ""
		switch {
		case row.Username == "":
			reason = "用户名不能为空"
		case !usernameRE.MatchString(row.Username):
			reason = "用户名需为 3-32 位字母/数字/下划线"
		case seenUname[strings.ToLower(row.Username)]:
			reason = "表内用户名重复"
		case row.ID < 0 || row.ID > 1_000_000_000:
			reason = "用户 ID 需为正整数（上限 10 亿）"
		case row.ID > 0 && seenID[row.ID]:
			reason = "表内 id 重复"
		}
		if reason == "" {
			if msg := passwordProblem(row.Password); msg != "" {
				reason = msg
			} else if row.Grade != "" && repo.ParseGrade(row.Grade) == 0 {
				reason = "年级无法识别"
			} else if row.Role == "" {
				row.Role = repo.RoleDefault
			} else if !roleSet[row.Role] {
				reason = "角色不存在：" + row.Role
			}
		}
		if reason == "" {
			lu := strings.ToLower(row.Username)
			seenUname[lu] = true
			if row.ID > 0 {
				seenID[row.ID] = true
			}
			nickname := trimTo(row.Name, maxNicknameRunes)
			if nickname == "" {
				nickname = row.Username
			}
			_, err := h.repo.CreateUserWithProfile(c.Request.Context(), row.ID, row.Username,
				row.Password, nickname, row.Role, repo.UserProfile{
					Gender:  trimTo(row.Gender, 8),
					GradeNo: repo.ParseGrade(row.Grade),
					School:  trimTo(row.School, 64),
					Region:  trimTo(row.Region, 32),
				})
			if err != nil {
				switch {
				case isDupKey(err):
					if _, _, uerr := h.repo.GetUserByUsername(c.Request.Context(), row.Username); uerr == nil {
						reason = "用户名已存在"
					} else if row.ID > 0 {
						reason = "用户 ID 已被占用"
					} else {
						reason = "用户已存在"
					}
				default:
					reason = "创建失败（系统错误）"
				}
			} else {
				success++
				continue
			}
		}
		failed = append(failed, failRow{Row: row.Row, Username: row.Username, Reason: reason})
	}

	if success > 0 {
		_ = h.repo.AddSecurityLog(c.Request.Context(), repo.SecurityLog{
			Type: repo.SecUserImport, Username: uname,
			Detail: fmt.Sprintf("批量建号：成功 %d / 失败 %d", success, len(failed)),
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
