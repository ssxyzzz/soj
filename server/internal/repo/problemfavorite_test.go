package repo

// 题目收藏（v0.86）repo 契约：toggle 往返幂等（唯一索引兜底并发重复）、
// FavoritePIDs 批查只回命中项。无本地 Mongo 自动 skip（同全 repo 测试口径）。

import (
	"testing"
)

func TestProblemFavoriteToggleAndBatch(t *testing.T) {
	r := openTestRepo(t)
	defer r.Close(t.Context())
	if err := r.EnsureProblemFavoriteIndexes(t.Context()); err != nil {
		t.Fatalf("建索引: %v", err)
	}

	fav, err := r.ToggleProblemFavorite(t.Context(), 9, 101)
	if err != nil || !fav {
		t.Fatalf("首点收藏 fav=%v err=%v，期望 true", fav, err)
	}
	if fav, err := r.ToggleProblemFavorite(t.Context(), 9, 101); err != nil || fav {
		t.Fatalf("再点取消 fav=%v err=%v，期望 false", fav, err)
	}
	// 再收藏后验证批查：命中 101，未收藏的 102 不出现
	if fav, err := r.ToggleProblemFavorite(t.Context(), 9, 101); err != nil || !fav {
		t.Fatalf("三点复收藏 fav=%v err=%v，期望 true", fav, err)
	}
	got, err := r.FavoritePIDs(t.Context(), 9, []int64{101, 102})
	if err != nil {
		t.Fatalf("批查: %v", err)
	}
	if !got[101] || got[102] || len(got) != 1 {
		t.Fatalf("批查结果 %v，期望仅 {101:true}", got)
	}
	// 其他用户隔离
	if got, _ := r.FavoritePIDs(t.Context(), 8, []int64{101}); len(got) != 0 {
		t.Fatalf("他人视角应无收藏，得到 %v", got)
	}
}
