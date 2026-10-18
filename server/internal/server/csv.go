package server

// 成绩导出 CSV（v0.5c 老师教学闭环）：比赛/考试榜单与作业进度的成绩单下载。
// 文件统一 UTF-8 BOM 头（老师侧 Excel 直接打开中文不乱码），文件名走
// RFC 5987 filename*（中文文件名在 Content-Disposition 里正确编码）。

import (
	"bytes"
	"encoding/csv"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// renderCSV 按 RFC 4180 构建带 BOM 的 CSV 字节流（单元格转义由 encoding/csv 处理）。
func renderCSV(rows [][]string) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	for _, row := range rows {
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes()
}

// serveCSV 以附件形式写出 CSV；文件名含中文时由 mime.FormatMediaType 生成
// `filename*=utf-8''…`（纯 ASCII 名自动退回普通 filename=）。
func serveCSV(c *gin.Context, filename string, rows [][]string) {
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", renderCSV(rows))
}

// csvSafeName 文件名安全化：标题中的文件系统保留字符替换为下划线
//（mime 层只管编码合法，浏览器落盘时这些字符在 Windows 仍是非法文件名）。
func csvSafeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
}
