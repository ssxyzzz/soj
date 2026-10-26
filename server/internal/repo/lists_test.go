package repo

// 题单（v0.18.3）单测：章节树形状校验（深度/混挂/标题长度）与排名聚合口径。
// 依赖本地 MongoDB，不可达时跳过（openTestRepo 同款）。

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestValidateListShape(t *testing.T) {
	ok := []ListEntry{
		{Title: "第一章", Problems: []int64{1, 2}},
		{Title: "第二章", Desc: "递归", Children: []ListEntry{
			{Title: "2.1", Problems: []int64{3}},
			{Title: "2.2", Problems: []int64{3, 4}}, // 同题跨节重复挂载：形状合法
		}},
	}
	if err := ValidateListShape(ok); err != nil {
		t.Fatalf("合法树被拒: %v", err)
	}

	cases := []struct {
		name string
		in   []ListEntry
	}{
		{"章标题空", []ListEntry{{Title: "  "}}},
		{"章标题超长", []ListEntry{{Title: string(make([]rune, 33))}}},
		{"章描述超长", []ListEntry{{Title: "章", Desc: string(make([]rune, 129))}}},
		{"单章混挂", []ListEntry{{Title: "章", Problems: []int64{1}, Children: []ListEntry{{Title: "节"}}}}},
		{"三层嵌套", []ListEntry{{Title: "章", Children: []ListEntry{{Title: "节", Children: []ListEntry{{Title: "子节"}}}}}}},
		{"节标题空", []ListEntry{{Title: "章", Children: []ListEntry{{Title: ""}}}}},
	}
	for _, c := range cases {
		if err := ValidateListShape(c.in); err == nil {
			t.Fatalf("%s: 应拒绝", c.name)
		}
	}
}

func TestFlattenPIDs(t *testing.T) {
	got := FlattenPIDs([]ListEntry{
		{Title: "A", Problems: []int64{1, 2}},
		{Title: "B", Children: []ListEntry{
			{Title: "B1", Problems: []int64{2, 3}},
		}},
	})
	want := []int64{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("拍平结果 %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("拍平结果 %v, want %v（应保首次出现序）", got, want)
		}
	}
}

func TestListRankRows(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)

	// pids = {1,2}：uid7 AC 1、2 → solved 2；uid8 AC 1 → solved 1；
	// uid9 AC 3（不在集合）→ 不入榜；uid10 WA → 不入榜；uid11 未发布 AC → 不计
	insert(t, r, bson.M{"_id": int64(61), "uid": int64(7), "pid": int64(1), "status": StatusAccepted, "score": 100, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(62), "uid": int64(7), "pid": int64(2), "status": StatusAccepted, "score": 100, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(63), "uid": int64(7), "pid": int64(1), "status": StatusAccepted, "score": 100, "published": true, "createdAt": base}) // 同题重复 AC：去重
	insert(t, r, bson.M{"_id": int64(64), "uid": int64(8), "pid": int64(1), "status": StatusAccepted, "score": 100, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(65), "uid": int64(9), "pid": int64(3), "status": StatusAccepted, "score": 100, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(66), "uid": int64(10), "pid": int64(1), "status": StatusWrongAnswer, "score": 30, "published": true, "createdAt": base})
	insert(t, r, bson.M{"_id": int64(67), "uid": int64(11), "pid": int64(1), "status": StatusAccepted, "score": 100, "published": false, "createdAt": base})

	rows, err := r.ListRankRows(ctx, []int64{1, 2})
	if err != nil {
		t.Fatalf("ListRankRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应恰 2 行: %+v", rows)
	}
	if rows[0].UID != 7 || rows[0].Solved != 2 || rows[1].UID != 8 || rows[1].Solved != 1 {
		t.Fatalf("排序/计数错误: %+v", rows)
	}

	if empty, err := r.ListRankRows(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("空集合应返回空: %v %v", empty, err)
	}
}
