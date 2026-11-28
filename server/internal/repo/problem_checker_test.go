package repo

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ListCheckerProblems：只列 checkerFile 非空的题（字段缺失 / 空串 / null 都不算），
// 题号升序，与落库顺序无关。
func TestListCheckerProblems(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()
	seedRankProblem(t, r, 12, bson.M{"title": "空串", "checkerFile": ""})
	seedRankProblem(t, r, 10, bson.M{"title": "甲", "checkerFile": "data/checker.cpp"})
	seedRankProblem(t, r, 11, bson.M{"title": "没这个字段"})
	seedRankProblem(t, r, 13, bson.M{"title": "null", "checkerFile": nil})
	seedRankProblem(t, r, 9, bson.M{"title": "乙", "checkerFile": "data/checker.cc"})

	got, truncated, err := r.ListCheckerProblems(ctx)
	if err != nil {
		t.Fatalf("ListCheckerProblems: %v", err)
	}
	want := []CheckerMeta{
		{ID: 9, Title: "乙", File: "data/checker.cc"},
		{ID: 10, Title: "甲", File: "data/checker.cpp"},
	}
	if len(got) != len(want) || truncated {
		t.Fatalf("got %v (truncated %v), want %v", got, truncated, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项 = %+v, want %+v", i, got[i], want[i])
		}
	}
}
