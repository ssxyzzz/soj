// Package ai OpenAI 兼容 chat/completions 客户端（v0.33-③ AI 底座）：
// GLM / DeepSeek / 自定义 OpenAI 兼容服务统一走此客户端。同步调用（SSE 流式二期，
// 见 plan §3 暂缓），120s 超时；进程内并发闸（单副本边界口径——多实例部署需收敛共享）。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Provider 一档供应商配置（settings.aiConfig.providers 项）。
type Provider struct {
	Name    string `bson:"name" json:"name"`     // 档名（唯一，后台展示/切换用）
	BaseURL string `bson:"baseUrl" json:"base_url"` // OpenAI 兼容根地址（如 https://api.deepseek.com）
	APIKey  string `bson:"apiKey" json:"-"`      // 只写不回显（GET 打码、留空 = 不改）
	Model   string `bson:"model" json:"model"`   // 如 glm-4.6 / deepseek-chat
}

// Config AI 供应商配置（settings 集合 aiConfig 字段）。
type Config struct {
	Providers []Provider `bson:"providers" json:"providers"`
	Active    string     `bson:"active" json:"active"` // 当前启用的档名
}

// ActiveProvider 取启用档（未配置/空返回 nil）。
func (c *Config) ActiveProvider() *Provider {
	if c == nil {
		return nil
	}
	for i := range c.Providers {
		if c.Providers[i].Name == c.Active && c.Providers[i].BaseURL != "" && c.Providers[i].APIKey != "" {
			return &c.Providers[i]
		}
	}
	return nil
}

// Message 一条对话消息（system/user/assistant）。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response 完成结果（tokens 用量随回）。
type Response struct {
	Content      string
	PromptChars  int
	PromptTokens int
	TotalTokens  int
}

// ErrNoProvider 未配置可用的 AI 供应商（前端入口应隐藏；此错兜底）。
var ErrNoProvider = errors.New("AI 供应商未配置")

// Client 供应商客户端（无状态，按需构造；并发闸进程级共享）。
type Client struct {
	p    *Provider
	http *http.Client
}

var (
	gateMu sync.Mutex
	gateN  int // 在途请求数（并发闸）
	gateMax = 4
)

// NewClient 构造（p 为 ActiveProvider 结果，nil 返回 ErrNoProvider 语义由调用方先拦）。
func NewClient(p *Provider) *Client {
	return &Client{p: p, http: &http.Client{Timeout: 120 * time.Second}}
}

// Chat 同步对话补全。system 为提示词模板（服务端统一维护），user 为组装好的素材消息。
func (c *Client) Chat(ctx context.Context, system, user string) (*Response, error) {
	return c.chat(ctx, system, user, false)
}

// ChatSearch 带联网搜索的对话补全（v0.92 题库整理难度五源「网上答案」拍板路线）：
// 仅 bigmodel.cn 系端点随请求附 GLM web_search 工具（模型自行决定搜什么），
// 其余 OpenAI 兼容服务不支持该参数——静默降级为普通对话（四源判读照常），不报错。
func (c *Client) ChatSearch(ctx context.Context, system, user string) (*Response, error) {
	return c.chat(ctx, system, user, true)
}

func (c *Client) chat(ctx context.Context, system, user string, webSearch bool) (*Response, error) {
	// 进程内并发闸（AI 外呼同时最多 gateMax 路，防打爆上游与预算）
	gateMu.Lock()
	if gateN >= gateMax {
		gateMu.Unlock()
		return nil, errors.New("AI 请求繁忙，稍后再试")
	}
	gateN++
	gateMu.Unlock()
	defer func() {
		gateMu.Lock()
		gateN--
		gateMu.Unlock()
	}()

	body := map[string]any{
		"model": c.p.Model,
		"messages": []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"temperature": 0.3,
	}
	// GLM（智谱）glm-4.x 默认开深度思考——思考链先于正文产出，分级提示/找 bug 类
	// 结构化任务思考链动辄数千 token，60s 外呼预算内被撑爆（2026-10-31 实锤：ai-debug
	// 撞 context deadline exceeded 退费，服务器实测小请求 content 为空、全花在
	// reasoning）。官方端点自动附 thinking disabled；其余 OpenAI 兼容服务不发此参数
	//（防未知字段被拒收；自建反代 bigmodel 的部署如需关闭请代理层补写）。
	if strings.Contains(c.p.BaseURL, "bigmodel.cn") {
		body["thinking"] = map[string]string{"type": "disabled"}
		// web_search 工具同样仅 GLM 官方端点支持（v0.92）；搜索结果由模型消化，
		// 不回传 search_result 原文省 token
		if webSearch {
			body["tools"] = []map[string]any{
				{"type": "web_search", "web_search": map[string]any{"enable": true}},
			}
		}
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.p.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.p.APIKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("AI 服务返回 %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("AI 返回空回复")
	}
	return &Response{
		Content:      out.Choices[0].Message.Content,
		PromptChars:  len(user) + len(system),
		PromptTokens: out.Usage.PromptTokens,
		TotalTokens:  out.Usage.TotalTokens,
	}, nil
}
