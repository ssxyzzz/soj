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
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// File 描述 stdio（stdin/stdout/stderr）三者的去向。
// go-judge 语义：含 content 的条目被物化为绑定到该 fd 的文件；
// 仅 name+max 的条目是内存收集管道（输出捕获必须用这种）。
// 因此 stdin 用 Stdin()（内容可为空但字段必须存在），stdout/stderr 用 Pipe()。
//
// 跨 cmd 的命名管道（交互题）：生产端 Pipe(name, max)、消费端 PipeReader(name)。
// 实测（go-judge v1.13.3）两端都必须显式带 max 键——消费端 max:0 也必须出现，
// 否则 400 "file type is not valid"；故 Max 用指针而非 int64+omitempty。
type File struct {
	Content *string `json:"content,omitempty"`
	Name    string  `json:"name,omitempty"`
	Max     *int64  `json:"max,omitempty"`
}

// Stdin 以给定内容作为程序的标准输入（内容为空也必须显式传 "content": ""，
// 空对象会被沙箱以 400 拒绝）。
func Stdin(content string) File { return File{Content: &content} }

// Pipe 声明一个内存收集管道（用于捕获 stdout/stderr），同时可作为命名管道的生产端。
func Pipe(name string, max int64) File { return File{Name: name, Max: &max} }

// Null 不指定的 fd（JSON 序列化为 null）：交互题里由 pipeMapping 接管 stdin/stdout 时用。
func Null() File { return File{} }

// MarshalJSON 零值 File 输出 null——go-judge 对映射接管的 fd 要求未指定而非空对象。
func (f File) MarshalJSON() ([]byte, error) {
	if f.Content == nil && f.Name == "" && f.Max == nil {
		return []byte("null"), nil
	}
	type alias File
	return json.Marshal(alias(f))
}

// PipeReader 命名管道的消费端（另一个 cmd 以此名字 Pipe 生产）。
func PipeReader(name string) File {
	zero := int64(0)
	return File{Name: name, Max: &zero}
}

// CopyFile 描述 copyIn 的文件来源：内容或已缓存的 fileId 二选一。
// Content 与 File.Content 同理用指针：string+omitempty 会把「空内容的合法拷入」
//（如文件 IO 模式下 0 字节的测试点输入）整个丢成 {}，被沙箱以 400 拒绝
//（2026-10-27 比赛文件 IO 判题 System Error 的根因）。经 CopyContent 构造。
type CopyFile struct {
	Content *string `json:"content,omitempty"`
	FileID  string  `json:"fileId,omitempty"`
}

// CopyContent 以给定内容拷入文件（内容为空也必须显式带 "content": ""）。
func CopyContent(content string) CopyFile { return CopyFile{Content: &content} }

type Cmd struct {
	Args          []string            `json:"args"`
	Env           []string            `json:"env,omitempty"`
	Files         []File              `json:"files"`                 // [0]=stdin [1]=stdout [2]=stderr
	CPULimit      int64               `json:"cpuLimit,omitempty"`    // 纳秒
	ClockLimit    int64               `json:"clockLimit,omitempty"`  // 墙钟时间，防 sleep 绕过 cpuLimit
	MemoryLimit   int64               `json:"memoryLimit,omitempty"` // 字节
	ProcLimit     int64               `json:"procLimit,omitempty"`
	CopyIn        map[string]CopyFile `json:"copyIn,omitempty"`
	CopyOut       []string            `json:"copyOut,omitempty"`
	CopyOutCached []string            `json:"copyOutCached,omitempty"` // 编译产物以 fileId 形式缓存
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

// File 取出 CopyOut 收集的具名文件内容（文件 IO 模式的考生输出）。
func (r *Result) File(name string) string { return DecodeContent(r.Files[name]) }

// HasFile 该具名文件是否被实际收集到（文件 IO 模式判断考生是否生成了输出文件）。
func (r *Result) HasFile(name string) bool {
	_, ok := r.Files[name]
	return ok
}

// FileID 取 copyOutCached 缓存文件的 fileId（大输出走沙箱文件形态时使用）。
func (r *Result) FileID(name string) (string, bool) {
	id, ok := r.FileIDs[name]
	return id, ok
}

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

// Available 探测沙箱是否可达（任何 HTTP 响应都视为可用）。
func (c *Client) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/version", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// PipeIndex 指向某条 cmd 的某个 fd（0=stdin 1=stdout 2=stderr）。
type PipeIndex struct {
	Index int `json:"index"`
	Fd    int `json:"fd"`
}

// PipeMap 进程间直连管道：In 为写入端（生产），Out 为读取端（消费）。
// 交互题用它把选手 stdout 接交互器 stdin、交互器 stdout 接选手 stdin；
// 此时对应 Files 槽位传 nil（由映射接管）。
type PipeMap struct {
	In  PipeIndex `json:"in"`
	Out PipeIndex `json:"out"`
}

// Run 单/多 cmd 执行（无进程间管道）。
func (c *Client) Run(ctx context.Context, cmds []Cmd) ([]Result, error) {
	return c.RunPiped(ctx, cmds, nil)
}

// RunPiped 带 pipeMapping 的执行（多 cmd 互相通信，交互题判题用）。
func (c *Client) RunPiped(ctx context.Context, cmds []Cmd, pipes []PipeMap) ([]Result, error) {
	payload := map[string]any{"cmd": cmds}
	if len(pipes) > 0 {
		payload["pipeMapping"] = pipes
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if os.Getenv("SOJ_DEBUG") != "" {
		slog.Info("沙箱请求（SOJ_DEBUG）", "component", "sandbox", "body", truncate(string(body), 4096))
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

// FetchFile 取回 copyOutCached 缓存的文件内容（原始字节直传，不经 /run 响应的 JSON 信封——
// MB 级考生输出若内联在响应里会撑爆响应读取上限）。max 为内容字节上限，超出报错
// （正常不会触发：收集管道的 Max 已先行封顶）。
func (c *Client) FetchFile(ctx context.Context, fileID string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/file/"+url.PathEscape(fileID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("拉取沙箱文件返回 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("沙箱文件超过 %d 字节上限", max)
	}
	return data, nil
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
