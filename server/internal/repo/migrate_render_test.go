package repo

// 渲染缓存迁移集成测试：历史文档（无 html 字段）补齐、空 md 跳过、
// lute 版本变更全量重刷。本地无 Mongo 自动 skip（openTestRepo 同款）。

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"soj/internal/mdrender"
)

func TestMigrateRenderedHtml(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 模拟历史文档：只有 md、无 html 字段
	mk := func(id int64, stmt string) {
		if err := r.UpsertProblem(ctx, &Problem{ID: id, Title: "t", Statement: stmt,
			TimeLimitMS: 1000, MemoryLimitKB: 262144}); err != nil {
			t.Fatalf("seed #%d: %v", id, err)
		}
		// 抹掉 html 模拟存量（UpsertProblem 已双写）
		if _, err := r.problems.UpdateOne(ctx,
			bson.M{"_id": id}, bson.M{"$unset": bson.M{"statementHtml": ""}}); err != nil {
			t.Fatalf("unset #%d: %v", id, err)
		}
	}
	mk(1, "题面 **A**")
	mk(2, "题面 `code`")
	mk(3, "") // 空 md：跳过

	n, err := r.MigrateRenderedHtml(ctx, false)
	if err != nil || n != 2 {
		t.Fatalf("迁移: n=%d err=%v（期望 2）", n, err)
	}
	// 验证补齐
	p1, _ := r.GetProblem(ctx, 1)
	if p1.StatementHtml == "" || p1.StatementHtml != mdrender.Render("题面 **A**") {
		t.Fatalf("#1 html 未补齐: %q", p1.StatementHtml)
	}
	p3, _ := r.GetProblem(ctx, 3)
	if p3.StatementHtml != "" {
		t.Fatalf("#3 空 md 不应写 html: %q", p3.StatementHtml)
	}

	// 幂等：再跑一遍应零新增
	n, err = r.MigrateRenderedHtml(ctx, false)
	if err != nil || n != 0 {
		t.Fatalf("二次迁移: n=%d err=%v（期望 0）", n, err)
	}

	// 全量重刷模式：人为改坏 #1 的 html，refetch 模式应重算
	if _, err := r.problems.UpdateOne(ctx, bson.M{"_id": int64(1)},
		bson.M{"$set": bson.M{"statementHtml": "stale"}}); err != nil {
		t.Fatal(err)
	}
	n, err = r.MigrateRenderedHtml(ctx, true)
	if err != nil || n < 1 {
		t.Fatalf("全量重刷: n=%d err=%v", n, err)
	}
	p1, _ = r.GetProblem(ctx, 1)
	if p1.StatementHtml != mdrender.Render("题面 **A**") {
		t.Fatalf("#1 重刷失败: %q", p1.StatementHtml)
	}
}
