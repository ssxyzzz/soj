package server

// 资产可见性判定纯测（v0.31）：与 GetProblem 的 404 条款同口径——隐藏题对非特权一律
// 拒、未发布赛事隐藏期拒（running 放行）、特权全放行。

import (
	"testing"
)

func TestAssetViewDenied(t *testing.T) {
	running, scheduled := contestLock{Phase: "running"}, contestLock{Phase: "scheduled"}
	cases := []struct {
		name       string
		hidden     bool
		privileged bool
		lk         *contestLock
		want       bool
	}{
		{"公开题无锁", false, false, nil, false},
		{"公开题赛时", false, false, &running, false},
		{"公开题隐藏期", false, false, &scheduled, true},
		{"隐藏题学生", true, false, nil, true},
		{"隐藏题赛时学生", true, false, &running, true},
		{"隐藏题特权", true, true, nil, false},
		{"隐藏期特权", false, true, &scheduled, false},
	}
	for _, c := range cases {
		if got := assetViewDenied(c.hidden, c.privileged, c.lk); got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
