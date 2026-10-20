// 外链图片转存：把题面 HTML 里 http(s) 外链 <img> 抓回本站存储，src 改写为 saveFn
// 返回的本站地址；单图失败保留外链（plan.md §2.3：失败保留外链并提示）。
// 站内相对路径（/uploads/...）与 data: 图不处理。
package probimport

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var imgSrcRe = regexp.MustCompile(`<img\b[^>]*\bsrc="(https?://[^"]+)"[^>]*>`)

// TransferImages 把 html 中的外链图经 fetch 拉取后交给 save（返回本站 URL），src 原位替换。
// saved 返回每张图的转存结果（成功/失败清单，供前端提示）。
func (f *Fetcher) TransferImages(ctx context.Context, html string, save func(body []byte, contentType string) (string, error)) (out string, saved []TransferResult) {
	out = imgSrcRe.ReplaceAllStringFunc(html, func(m string) string {
		src := imgSrcRe.FindStringSubmatch(m)[1]
		body, ct, err := f.GetImage(ctx, src)
		if err != nil {
			saved = append(saved, TransferResult{Src: src, Err: err.Error()})
			return m
		}
		siteURL, err := save(body, ct)
		if err != nil {
			saved = append(saved, TransferResult{Src: src, Err: err.Error()})
			return m
		}
		saved = append(saved, TransferResult{Src: src, To: siteURL})
		return strings.Replace(m, src, siteURL, 1)
	})
	return out, saved
}

type TransferResult struct {
	Src string `json:"src"`
	To  string `json:"to,omitempty"`
	Err string `json:"err,omitempty"`
}

// Summary 图片转存结果摘要（toast 用）。
func Summary(saved []TransferResult) string {
	var ok, fail int
	for _, s := range saved {
		if s.Err == "" {
			ok++
		} else {
			fail++
		}
	}
	switch {
	case ok == 0 && fail == 0:
		return ""
	case fail == 0:
		return fmt.Sprintf("已转存 %d 张图片", ok)
	case ok == 0:
		return fmt.Sprintf("%d 张图片转存失败，保留外链", fail)
	default:
		return fmt.Sprintf("转存 %d 张、失败 %d 张（保留外链）", ok, fail)
	}
}
