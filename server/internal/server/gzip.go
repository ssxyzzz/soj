package server

import (
	"compress/gzip"
	"strings"

	"github.com/gin-gonic/gin"
)

// gzip 压缩中间件：本服务响应均为一次性写完的 JSON/静态文件（无 SSE/流式接口），
// 故在首次写入时按 Content-Type 与首块大小决定是否压缩，客户端不支持时原样透传。
const minGzipSize = 512

// gzipable 判断响应类型是否值得压缩：文本类收益明显，
// 图片/字体等已压缩二进制再压只会白费 CPU 甚至变大。
func gzipable(ct string) bool {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/javascript", "application/json", "application/xml", "image/svg+xml":
		return true
	}
	return false
}

func clientAcceptsGzip(acceptEncoding string) bool {
	for _, part := range strings.Split(acceptEncoding, ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]), "gzip") {
			return true
		}
	}
	return false
}

// gzipWriter 延迟到首次写入再决定是否压缩：那时 Content-Type 才确定。
type gzipWriter struct {
	gin.ResponseWriter
	gz      *gzip.Writer // nil 表示透传
	decided bool
}

func (w *gzipWriter) decide(n int) {
	w.decided = true
	h := w.Header()
	if n < minGzipSize || !gzipable(h.Get("Content-Type")) || h.Get("Content-Encoding") != "" {
		return
	}
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	h.Del("Content-Length") // 长度以压缩后为准，交由 chunked 传输
	w.gz = gzip.NewWriter(w.ResponseWriter)
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	if !w.decided {
		w.decide(len(b))
	}
	if w.gz != nil {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzipWriter) WriteString(s string) (int, error) {
	if !w.decided {
		w.decide(len(s))
	}
	if w.gz != nil {
		return w.gz.Write([]byte(s))
	}
	return w.ResponseWriter.WriteString(s)
}

// Flush 兼容分段推送的场景：压缩时先刷 gzip 缓冲再刷底层连接。
func (w *gzipWriter) Flush() {
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	w.ResponseWriter.Flush()
}

func (w *gzipWriter) close() {
	if w.gz != nil {
		_ = w.gz.Close() // 收尾并刷出压缩数据尾部
	}
}

// Gzip 对支持 gzip 的客户端启用响应压缩。
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Range 断点续传与压缩互斥，直接透传
		if !clientAcceptsGzip(c.Request.Header.Get("Accept-Encoding")) ||
			c.Request.Header.Get("Range") != "" {
			c.Next()
			return
		}
		rw := &gzipWriter{ResponseWriter: c.Writer}
		defer rw.close()
		c.Writer = rw
		c.Next()
	}
}
