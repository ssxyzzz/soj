package repo

import (
	"fmt"
	"testing"
	"time"
)

// v0.27 lbCache 防膨胀（纯 map 逻辑，不依赖 Mongo）：超上限丢最旧、覆盖已有键不触发淘汰。
func TestLbCachePutEviction(t *testing.T) {
	saved := lbCache
	lbCache = map[string]lbCacheEntry{}
	defer func() { lbCache = saved }()

	now := time.Now()
	for i := 0; i < lbCacheCap; i++ {
		lbCache[fmt.Sprintf("f%d", i)] = lbCacheEntry{at: now.Add(time.Duration(i) * time.Second)}
	}
	lbCachePut("new", lbCacheEntry{at: now})
	if len(lbCache) > lbCacheCap {
		t.Fatalf("超限后应保持上限内: got %d", len(lbCache))
	}
	if _, ok := lbCache["f0"]; ok {
		t.Fatal("应淘汰最旧的 f0")
	}
	if _, ok := lbCache["new"]; !ok {
		t.Fatal("新键应保留")
	}

	// 覆盖已有键不改变条目数
	before := len(lbCache)
	lbCachePut("new", lbCacheEntry{at: now})
	if len(lbCache) != before {
		t.Fatalf("覆盖已有键不应改变条目数: got %d want %d", len(lbCache), before)
	}
}
