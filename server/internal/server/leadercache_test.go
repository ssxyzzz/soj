package server

import (
	"fmt"
	"testing"
	"time"
)

// v0.27 榜单缓存容量治理：超上限先清过期、仍超丢最旧、覆盖已有键不触发淘汰。
func TestLeaderCachePutEviction(t *testing.T) {
	now := time.Now()

	m := map[string]leaderEntry{}
	m["a"] = leaderEntry{at: now}
	leaderCachePut(m, "a", leaderEntry{at: now})
	if len(m) != 1 {
		t.Fatalf("覆盖已有键不应改变条目数: got %d", len(m))
	}

	// 超上限且全过期：过期条目应被整轮清掉
	for i := 0; i < leaderCacheCap+5; i++ {
		m[fmt.Sprintf("old%d", i)] = leaderEntry{at: now.Add(-2 * leaderCacheTTL)}
	}
	leaderCachePut(m, "b", leaderEntry{at: now})
	if _, ok := m["b"]; !ok {
		t.Fatal("新键应写入")
	}
	if len(m) > leaderCacheCap {
		t.Fatalf("条目数应不超过上限: got %d", len(m))
	}
	for i := 0; i < leaderCacheCap+5; i++ {
		if _, ok := m[fmt.Sprintf("old%d", i)]; ok {
			t.Fatal("过期条目应被清理")
		}
	}

	// 超上限且全新鲜：丢最旧给新键腾位
	m2 := map[string]leaderEntry{}
	for i := 0; i < leaderCacheCap; i++ {
		m2[fmt.Sprintf("f%d", i)] = leaderEntry{at: now.Add(time.Duration(i) * time.Second)}
	}
	leaderCachePut(m2, "new", leaderEntry{at: now})
	if len(m2) > leaderCacheCap {
		t.Fatalf("全新鲜超限后应保持上限内: got %d", len(m2))
	}
	if _, ok := m2["f0"]; ok {
		t.Fatal("应淘汰最旧的 f0")
	}
	if _, ok := m2["new"]; !ok {
		t.Fatal("新键应保留")
	}
}
