package server

import (
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// 等级榜排序口径（v0.41 服务端重排 + 同级按经验细化）：等级 desc → 经验 desc → uid asc；
// 无 AC 历史的用户（无 level/xp 字段）按 0 沉底；入参（共享缓存条目）不被改写。
func TestLevelBoardSorted(t *testing.T) {
	in := []gin.H{
		// 故意乱序：uid 顺序 ≠ 期望名次顺序
		{"rank": 1, "user_id": int64(7), "level": 3, "xp": int64(1300), "solved": 9},
		{"rank": 2, "user_id": int64(2), "level": 5, "xp": int64(3100), "solved": 8},
		{"rank": 3, "user_id": int64(9), "level": 3, "xp": int64(1300), "solved": 5}, // 同级同经验 → uid 小者前
		{"rank": 4, "user_id": int64(4), "level": 3, "xp": int64(2600), "solved": 2}, // 同级经验高者前（解题数反而少——锁定非解题数口径）
		{"rank": 5, "user_id": int64(1), "solved": 11}, // 无 AC 历史（无 level/xp）：视为 0 沉底
	}
	out := levelBoardSorted(in)
	got := make([]int64, len(out))
	for i, it := range out {
		got[i] = it["user_id"].(int64)
		if it["rank"] != i+1 {
			t.Fatalf("名次未随重排更新：pos %d rank=%v", i, it["rank"])
		}
	}
	want := []int64{2, 4, 7, 9, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("排序结果 = %v, want %v", got, want)
	}
	// 深拷贝校验：共享缓存（入参切片）顺序与名次不被污染
	if in[0]["user_id"] != int64(7) || in[0]["rank"] != 1 {
		t.Fatalf("入参被改写：in[0] = %v", in[0])
	}
}
