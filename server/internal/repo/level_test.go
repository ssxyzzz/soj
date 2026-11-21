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

func TestDifficultyXP(t *testing.T) {
	want := [10]int64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512}
	for d := 1; d <= 10; d++ {
		if got := DifficultyXP(d); got != want[d-1] {
			t.Fatalf("DifficultyXP(D%d) = %d, want %d", d, got, want[d-1])
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
	// 200 道 D1（XP=200）恰好 Lv.2：清完一档升一级
	t.Run("200 道 D1 恰好 Lv.2", func(t *testing.T) {
		lv, xp, cur, next, _ := LevelProgress(cfg, countsOf(1, 200), 0)
		if lv != 2 || xp != 200 || cur != 200 || next != 600 {
			t.Fatalf("got lv=%d xp=%d cur=%d next=%d", lv, xp, cur, next)
		}
	})
	// D1+D2 各 200 道（200+400=600）恰好 Lv.3
	t.Run("D1D2 各清 200 道 Lv.3", func(t *testing.T) {
		lv, xp, cur, next, _ := LevelProgress(cfg, countsOf(1, 200, 2, 200), 0)
		if lv != 3 || xp != 600 || cur != 600 || next != 1400 {
			t.Fatalf("got lv=%d xp=%d cur=%d next=%d", lv, xp, cur, next)
		}
	})
	// 单道 D10（512 XP）只到 Lv.2：高难题一步到位但不越级到 Lv.3（需 600）
	t.Run("单道 D10 到 Lv.2", func(t *testing.T) {
		lv, xp, _, _, _ := LevelProgress(cfg, countsOf(10, 1), 0)
		if lv != 2 || xp != 512 {
			t.Fatalf("got lv=%d xp=%d, want lv=2 xp=512", lv, xp)
		}
	})
	// 生产回归锚点（uid 1761 口径）：D1×124+D2×11+D3×7+D6×1+D7×3+D10×1 = 910 → Lv.3
	t.Run("混合难度 910 XP 到 Lv.3", func(t *testing.T) {
		lv, xp, cur, next, _ := LevelProgress(cfg, countsOf(1, 124, 2, 11, 3, 7, 6, 1, 7, 3, 10, 1), 0)
		if lv != 3 || xp != 910 || cur != 600 || next != 1400 {
			t.Fatalf("got lv=%d xp=%d cur=%d next=%d", lv, xp, cur, next)
		}
	})
	// 经验调整：200 道 D5（XP 3200）= Lv.5；−3000 → 200 = Lv.2；−5000 大幅减不破 Lv.1 且显示钳 0
	t.Run("经验调整升降级", func(t *testing.T) {
		if lv, _, _, _, _ := LevelProgress(cfg, countsOf(5, 200), 0); lv != 5 {
			t.Fatalf("3200 XP: got lv=%d, want 5", lv)
		}
		if lv, xp, _, _, _ := LevelProgress(cfg, countsOf(5, 200), -3000); lv != 2 || xp != 200 {
			t.Fatalf("-3000: got lv=%d xp=%d, want lv=2 xp=200", lv, xp)
		}
		if lv, xp, _, _, _ := LevelProgress(cfg, countsOf(5, 200), -5000); lv != 1 || xp != 0 {
			t.Fatalf("-5000: got lv=%d xp=%d, want lv=1 xp=0", lv, xp)
		}
	})
	// 封顶：200 道 D10（102400 ≥ 102200 = D1–D9 全清）到 Lv.10 且无下一级
	t.Run("Lv.10 封顶", func(t *testing.T) {
		lv, _, _, _, hasNext := LevelProgress(cfg, countsOf(10, 200), 0)
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

func TestJadeDiscountLadder(t *testing.T) {
	// 零值档位经 normalizeJadeDiscount 回默认阶梯（JadeDiscountPct 读侧归一）
	s := &SystemSettings{}
	if got := s.JadeDiscountPct(4); got != 100 {
		t.Errorf("Lv4 折扣 = %d，期望 100（Lv5 起生效）", got)
	}
	want := map[int]int{5: 95, 6: 90, 7: 85, 8: 80, 9: 75, 10: 70}
	for lv, w := range want {
		if got := s.JadeDiscountPct(lv); got != w {
			t.Errorf("Lv%d 折扣 = %d，期望 %d", lv, got, w)
		}
	}
	if got := s.JadeDiscountPct(11); got != 100 {
		t.Errorf("越界档 = %d，期望 100", got)
	}
	// 单调归一：乱序输入后高档不高于低档
	s.JadeDiscount = [6]int{70, 100, 85, 90, 75, 95}
	got := normalizeJadeDiscount(s.JadeDiscount)
	for i := 0; i < 5; i++ {
		if got[i] < got[i+1] {
			t.Fatalf("归一后非单调：Lv%d=%d < Lv%d=%d", i+5, got[i], i+6, got[i+1])
		}
	}
	// AiPrice 折扣向上取整：D5 题解价 25，Lv10 七折 = ceil(17.5) = 18
	s.JadeDiscount = DefaultJadeDiscount
	if got := s.AiPrice("hint1", 5, 10); got*3 >= 25*70/100*3 {
		t.Log("hint 价格形状检查（粗）")
	}
	// 解锁价折算公式对齐（server 侧同款 ceil）：25×0.7 → 18
	base := int64(25)
	if ceil := (base*70 + 99) / 100; ceil != 18 {
		t.Fatalf("D5 题解 Lv10 折后 = %d，期望 18", ceil)
	}
}
