package repo

// UnbindTags（标签树保存被删拦截后的「一键/逐题解绑」）回归测试：
// 只摘目标标签、题目其余标签保留；未引用目标标签的题目不计入改动数。
// 复用 openTestRepo（本地无 Mongo 自动 skip）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUnbindTags(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	// 唯一前缀避免与库内其它测试题冲突（_id 用 99 万段测试专用号段）
	mk := func(id int64, title string, tags []int64) {
		t.Helper()
		if _, err := r.problems.InsertOne(ctx, bson.M{
			"_id": id, "title": title, "tags": tags, "createdAt": time.Now(),
		}); err != nil {
			t.Fatalf("insert problem %d: %v", id, err)
		}
	}
	mk(990101, "解绑测试·含两个待删标签", []int64{9101, 9102, 77001})
	mk(990102, "解绑测试·只含一个待删标签", []int64{9102})
	mk(990103, "解绑测试·不涉及待删标签", []int64{77001})

	n, err := r.UnbindTags(ctx, []int64{990101, 990102, 990103}, []int64{9101, 9102})
	if err != nil {
		t.Fatalf("UnbindTags: %v", err)
	}
	// 990101/990102 都含待删标签 → 改动 2 题；990103 不含 → 不动
	if n != 2 {
		t.Fatalf("改动题数 = %d，应為 2", n)
	}
	for id, want := range map[int64][]int64{990101: {77001}, 990102: {}, 990103: {77001}} {
		var doc struct {
			Tags []int64 `bson:"tags"`
		}
		if err := r.problems.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("reload %d: %v", id, err)
		}
		if len(doc.Tags) != len(want) {
			t.Fatalf("题 %d 标签 = %v，应为 %v", id, doc.Tags, want)
		}
		got := map[int64]bool{}
		for _, tg := range doc.Tags {
			got[tg] = true
		}
		for _, tg := range want {
			if !got[tg] {
				t.Fatalf("题 %d 标签 = %v，缺 %d", id, doc.Tags, tg)
			}
		}
	}

	// 空入参安全：不动任何题
	if n, err := r.UnbindTags(ctx, nil, []int64{9101}); err != nil || n != 0 {
		t.Fatalf("空 problem_ids 应返回 0,nil，得到 %d,%v", n, err)
	}
}
