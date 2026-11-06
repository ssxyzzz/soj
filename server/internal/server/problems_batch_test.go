package server

// batchSummaries（选题器按题号导入 v0.73）纯函数单测。

import (
	"testing"

	"soj/internal/repo"
)

func TestBatchSummaries(t *testing.T) {
	sums := map[int64]*repo.ProblemSummary{
		1001: {ID: 1001, Title: "A"},
		1002: {ID: 1002, Title: "B", Hidden: true},
		1003: {ID: 1003, Title: "C", Duplicate: true},
	}
	items, invalid := batchSummaries([]int64{1001, 9999, 1002, 1003}, sums)
	if len(items) != 1 || items[0]["id"] != int64(1001) || items[0]["title"] != "A" {
		t.Fatalf("可见题应进 items: %v", items)
	}
	if len(invalid) != 3 || invalid[0] != 9999 || invalid[1] != 1002 || invalid[2] != 1003 {
		t.Fatalf("缺失/隐藏/重复应保序进 invalid: %v", invalid)
	}
	if items2, invalid2 := batchSummaries(nil, sums); len(items2) != 0 || len(invalid2) != 0 {
		t.Fatalf("空入参应得空切片: %v, %v", items2, invalid2)
	}
}
