package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// user 压测虚拟用户：uid/uname 与 repo 种子一致，pass 来自环境变量，token 为登录后的 Bearer。
type user struct {
	uid   int64
	uname string
	pass  string
	token string
}

// apiResp 统一响应包装：code=0 且 HTTP 200 才算业务成功。
type apiResp struct {
	Status  int
	Code    int
	Message string
	Data    json.RawMessage
}

// callJSON 发 JSON 请求并计时（毫秒）；body 为 nil 时不带请求体（GET/DELETE）。
func callJSON(hc *http.Client, method, base, path, token string, body any) (apiResp, float64, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return apiResp{}, 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		return apiResp{}, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	start := time.Now()
	resp, err := hc.Do(req)
	ms := float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return apiResp{}, ms, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var wrap struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(raw, &wrap) // 非 JSON 响应按失败处理（Code=0 之外）
	return apiResp{Status: resp.StatusCode, Code: wrap.Code, Message: wrap.Message, Data: wrap.Data}, ms, nil
}

// parseID 从 {"id":n} 形态的 data 里取 id。
func parseID(d json.RawMessage) (int64, bool) {
	var s struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(d, &s) == nil && s.ID > 0 {
		return s.ID, true
	}
	return 0, false
}

// metrics 按端点聚合延迟样本（毫秒）与失败计数。
type metrics struct {
	mu       sync.Mutex
	samples  map[string][]float64
	errs     map[string]int
	statuses map[string]map[int]int
}

func newMetrics() *metrics {
	return &metrics{samples: map[string][]float64{}, errs: map[string]int{}, statuses: map[string]map[int]int{}}
}

// observe 记一次调用：网络错误 / 非 200 / code≠0 均计为失败。
func (m *metrics) observe(ep string, r apiResp, ms float64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples[ep] = append(m.samples[ep], ms)
	if err != nil || r.Status != http.StatusOK || (r.Status == http.StatusOK && r.Code != 0) {
		m.errs[ep]++
	}
	if m.statuses[ep] == nil {
		m.statuses[ep] = map[int]int{}
	}
	m.statuses[ep][r.Status]++
}

func (m *metrics) endpoints() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	eps := make([]string, 0, len(m.samples))
	for ep := range m.samples {
		eps = append(eps, ep)
	}
	sort.Strings(eps)
	return eps
}

func (m *metrics) snapshot(ep string) ([]float64, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := append([]float64(nil), m.samples[ep]...)
	return cp, m.errs[ep]
}

// percentile 已排序样本分位数（p∈(0,1]，线性取整下标）。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
