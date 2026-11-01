package repo

// AI 调用审计游标分页（v0.51）单测：三页遍历顺序与 total、末页 next 收口、脏游标 400。
// 复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestListAiCallsCursor(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	// 45 行审计（2 页整 + 5 行），_id 走 seq、at 逐行错开保证 (at,_id) 全序
	for i := 0; i < 45; i++ {
		c := &AiCall{UID: 71, PID: 72, Kind: "hint1", At: time.Now().Add(time.Duration(i) * time.Second)}
		r.RecordAiCall(ctx, c)
		if c.ID == 0 {
			t.Fatalf("RecordAiCall 未取得 _id")
		}
	}
	// 三页游标遍历：每页 20 条，at 降序 = 后插的在前
	seen := 0
	cursor := ""
	pages := 0
	for {
		list, next, total, err := r.ListAiCalls(ctx, 0, cursor, 20)
		if err != nil {
			t.Fatalf("ListAiCalls(page=%d): %v", pages, err)
		}
		if total != 45 {
			t.Fatalf("total 应恒为全量 45，得 %d", total)
		}
		for i := 1; i < len(list); i++ {
			if !list[i-1].At.After(list[i].At) {
				t.Fatalf("页内 at 应降序: %v → %v", list[i-1].At, list[i].At)
			}
		}
		seen += len(list)
		pages++
		if next == "" {
			break
		}
		cursor = next
		if pages > 5 {
			t.Fatalf("游标未收口，翻页失控")
		}
	}
	if seen != 45 || pages != 3 {
		t.Fatalf("三页应取尽 45 行，得 %d 行 / %d 页", seen, pages)
	}
	// uid 过滤 total 隔离
	_, _, total, err := r.ListAiCalls(ctx, 999, "", 20)
	if err != nil || total != 0 {
		t.Fatalf("他人过滤应 0 行，得 %d / %v", total, err)
	}
	// 脏游标：必须报 ErrInvalidCursor（调用方 400），不得静默回首页
	if _, _, _, err := r.ListAiCalls(ctx, 0, "not-a-cursor", 20); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("脏游标应 ErrInvalidCursor，得 %v", err)
	}
}
