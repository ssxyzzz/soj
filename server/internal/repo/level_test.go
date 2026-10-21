package repo

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// countsOf 构造各难度 AC 题数。
func countsOf(pairs ...int) [10]int64 {
	var c [10]int64
	for i := 0; i < len(pairs); i += 2 {
		c[pairs[i]-1] = int64(pairs[i+1])
	}
	return c
}

func TestDecayFactor(t *testing.T) {
	cfg := DefaultLevelConfig()
	cases := []struct {
		level, diff int
		want        float64
	}{
		{5, 5, 1}, {5, 6, 1}, {5, 4, cfg.Decay1}, {5, 3, cfg.Decay2}, {5, 2, cfg.Decay3}, {5, 1, 0},
	}
	for _, tc := range cases {
		if got := DecayFactor(cfg, tc.level, tc.diff); got != tc.want {
			t.Fatalf("DecayFactor(L%d,D%d) = %v, want %v", tc.level, tc.diff, got, tc.want)
		}
	}
}

func TestLevelProgress(t *testing.T) {
	cfg := DefaultLevelConfig()
	t.Run("空历史 Lv.1", func(t *testing.T) {
		lv, xp, _, _, hasNext := LevelProgress(cfg, countsOf(), 0)
		if lv != 1 || xp != 0 || !hasNext {
			t.Fatalf("got lv=%d xp=%d hasNext=%v", lv, xp, hasNext)
		}
	})
	// 200 道 D5（XP=1000）：L3 达标（≥600）、L4 差 200 → Lv.3
	t.Run("200 道 D5 到 Lv.3", func(t *testing.T) {
		lv, xp, cur, next, _ := LevelProgress(cfg, countsOf(5, 200), 0)
		if lv != 3 || xp != 1000 || cur != 600 || next != 1200 {
			t.Fatalf("got lv=%d xp=%d cur=%d next=%d", lv, xp, cur, next)
		}
	})
	// D5 高位刷怪衰减：400 道 D1 在 Lv.2 只值 200 → 恰好 Lv.2（打太简单的题涨得慢）
	t.Run("400 道 D1 恰到 Lv.2", func(t *testing.T) {
		lv, _, _, _, _ := LevelProgress(cfg, countsOf(1, 400), 0)
		if lv != 2 {
			t.Fatalf("got lv=%d", lv)
		}
	})
	// 混合：200 D5 + 600 D6 → L6 达标（500+3600=4100 ≥ 3000）、L7 不足 → Lv.6
	t.Run("混合难度 Lv.6", func(t *testing.T) {
		lv, _, _, _, _ := LevelProgress(cfg, countsOf(5, 200, 6, 600), 0)
		if lv != 6 {
			t.Fatalf("got lv=%d", lv)
		}
	})
	// 经验调整：+600 让 200 道 D5（XP 1000）从 Lv.3 升到 Lv.4（1200）；-5000 大幅减也不低于 Lv.1
	t.Run("经验调整升降级", func(t *testing.T) {
		if lv, _, _, _, _ := LevelProgress(cfg, countsOf(5, 200), 600); lv != 4 {
			t.Fatalf("+600: got lv=%d, want 4", lv)
		}
		if lv, xp, _, _, _ := LevelProgress(cfg, countsOf(5, 200), -5000); lv != 1 || xp != 0 {
			t.Fatalf("-5000: got lv=%d xp=%d, want lv=1 xp=0", lv, xp)
		}
	})
	// 封顶：海量 D10 到 Lv.10 且无下一级
	t.Run("Lv.10 封顶", func(t *testing.T) {
		lv, _, _, _, hasNext := LevelProgress(cfg, countsOf(10, 2000), 0)
		if lv != 10 || hasNext {
			t.Fatalf("got lv=%d hasNext=%v", lv, hasNext)
		}
	})
}

func TestLevelConfigNormalize(t *testing.T) {
	cfg := DefaultLevelConfig()
	cfg.Names[3] = ""
	cfg.Thresholds = [9]int64{100, 50, 300, 400, 500, 600, 700, 800, 900}
	if cfg.Normalize() {
		t.Fatal("空名字/阈值非递增应返回 false")
	}
	if cfg.Names[3] != DefaultLevelNames[3] {
		t.Fatal("空名字应回默认")
	}
	if cfg.Thresholds != DefaultLevelThresholds {
		t.Fatal("非递增阈值应整体回默认")
	}
	cfg.Decay2 = 1.5
	cfg.Normalize()
	if cfg.Decay2 != 1 {
		t.Fatal("系数应夹取到 [0,1]")
	}
}

func TestProblemYAMLDifficulty(t *testing.T) {
	var y problemYAML
	if err := yaml.Unmarshal([]byte("title: t\ndifficulty: 7\n"), &y); err != nil {
		t.Fatal(err)
	}
	if y.Difficulty != 7 {
		t.Fatalf("difficulty = %d, want 7", y.Difficulty)
	}
	if !ValidDifficulty(y.Difficulty) || ValidDifficulty(0) || ValidDifficulty(11) {
		t.Fatal("ValidDifficulty 边界不对")
	}
}
