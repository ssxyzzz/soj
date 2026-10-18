package repo

import "testing"

func TestSeedPID(t *testing.T) {
	cases := []struct {
		name string
		want int64
	}{
		{"1_A_Plus_B", 1},
		{"6_hidden", 6},
		{"42_", 42},
		{"12_only_prefix_12", 12},
		{"no_prefix", 0},
		{"abc_def", 0},
		{"0_zero_is_invalid", 0},
		{"-3_negative", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := seedPID(c.name); got != c.want {
			t.Errorf("seedPID(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}
