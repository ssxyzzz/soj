package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRenderCSV(t *testing.T) {
	out := string(renderCSV([][]string{{"排名", "用户名"}, {"1", `张三,"a"`}}))
	if !strings.HasPrefix(out, "\xEF\xBB\xBF排名,用户名\n") {
		t.Fatalf("BOM 或表头不符合预期: %q", out)
	}
	if !strings.Contains(out, `"张三,""a"""`) {
		t.Fatalf("逗号/引号未按 RFC 4180 转义: %q", out)
	}
}

func TestServeCSVDisposition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	serveCSV(c, "期中考试_2026-10-18.csv", [][]string{{"a"}})
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename*=utf-8''") {
		t.Fatalf("中文文件名缺 RFC 5987 filename*: %q", cd)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestCSVSafeName(t *testing.T) {
	if got := csvSafeName(`A/B:C*测试`); got != "A_B_C_测试" {
		t.Fatalf("csvSafeName = %q", got)
	}
}
