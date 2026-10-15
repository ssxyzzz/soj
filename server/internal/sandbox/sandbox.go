// Package sandbox 是 go-judge (criyle/go-judge) REST API 的最小客户端。
// 接口契约见官方文档: https://docs.goj.ac
package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// File 对应 go-judge 的文件描述：stdin/stdout/stderr 与 copyIn 均用它表达。
type File struct {
	Src     string `json:"src,omitempty"`
	Content string `json:"content,omitempty"`
	Name    string `json:"name,omitempty"`
	FileID  string `json:"fileId,omitempty"`
	Max     int64  `json:"max,omitempty"`
}

type Cmd struct {
	Args          []string        `json:"args"`
	Env           []string        `json:"env,omitempty"`
	Files         []File          `json:"files"`                 // [0]=stdin [1]=stdout [2]=stderr
	CPULimit      int64           `json:"cpuLimit,omitempty"`    // 纳秒
	ClockLimit    int64           `json:"clockLimit,omitempty"`  // 墙钟时间，防 sleep 绕过 cpuLimit
	MemoryLimit   int64           `json:"memoryLimit,omitempty"` // 字节
	ProcLimit     int64           `json:"procLimit,omitempty"`
	CopyIn        map[string]File `json:"copyIn,omitempty"`
	CopyOut       []string        `json:"copyOut,omitempty"`
	CopyOutCached []string        `json:"copyOutCached,omitempty"` // 编译产物以 fileId 形式缓存
}

// Result 中 Time 为纳秒、Memory 为字节（go-judge 约定）；
// Status 取值: Accepted / Time Limit Exceeded / Memory Limit Exceeded /
// Output Limit Exceeded / Non Zero Exit Status / Signalled / File Error /
// Dangerous Syscall / Internal Error（其中 Accepted 表示退出码为 0 且未超限）。
type Result struct {
	Status     string            `json:"status"`
	ExitStatus int               `json:"exitStatus"`
	Time       int64             `json:"time"`
	Memory     int64             `json:"memory"`
	Files      map[string]string `json:"files"`
	FileIDs    map[string]string `json:"fileIds"`
}

// Stdout / Stderr 取出管道收集的输出内容。
func (r *Result) Stdout() string { return DecodeContent(r.Files["stdout"]) }

func (r *Result) Stderr() string { return DecodeContent(r.Files["stderr"]) }

type Client struct {
	baseURL string
	http    *http.Client
}

func New(addr string) *Client {
	return &Client{
		baseURL: strings.TrimRight(addr, "/"),
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Run(ctx context.Context, cmds []Cmd) ([]Result, error) {
	body, err := json.Marshal(map[string]any{"cmd": cmds})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/run", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("沙箱返回 %d: %s", resp.StatusCode, truncate(string(data), 512))
	}

	// 正常返回结果数组；兼容 {"results": []} 包装格式
	var arr []Result
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	var obj struct {
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal(data, &obj); err == nil && len(obj.Results) > 0 {
		return obj.Results, nil
	}
	return nil, fmt.Errorf("无法解析沙箱响应: %s", truncate(string(data), 512))
}

// DeleteFile 释放 copyOutCached 缓存的编译产物。
func (c *Client) DeleteFile(ctx context.Context, fileID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/file/"+url.PathEscape(fileID), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("删除沙箱文件返回 %d", resp.StatusCode)
	}
	return nil
}

// DecodeContent 解码 go-judge files map 中的内容（base64 编码）。
// 做一次回编校验，避免把恰好长得像 base64 的明文错误解码。
func DecodeContent(s string) string {
	if s == "" {
		return s
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && base64.StdEncoding.EncodeToString(b) == s {
		return string(b)
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
