package repo

// 系统设置持久化回归（v0.33-④ 特权分层实锤回归）：UpdateSystemSettings 的 $set
// 清单曾漏登 aiStaffDailyLimit——保存接口改内存结构体后落库不含该键，读侧永远回
// 默认值，「改了不生效」。本测锁住保存→重读全字段回显，防同类漏项。

import (
	"context"
	"testing"
)

func TestUpdateSystemSettingsRoundTrip(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	s, err := r.GetSystemSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 取一批有默认值兜底的代表性字段写非默认值（覆盖各批新增：讨论区/玉经济/AI 计费/
	// AI 名字/特权层上限/资料自改开关/取号起点）
	v := func(i int) *int { return &i }
	s.DiscussTopicDailyLimit = v(9)
	s.CurrencyName = "钻石"
	s.JadeBountyMax = v(666)
	s.AiDailyLimit = v(6)
	s.AiStaffDailyLimit = v(7) // 本回归的主角
	s.AiTutorName = "阿灵"
	s.UserSeqStart = v(300)
	s.ProfileEdit = &ProfileEditConfig{Nickname: true, Gender: false, Grade: false, School: true, Region: true}
	if err := r.UpdateSystemSettings(ctx, s); err != nil {
		t.Fatal(err)
	}

	got, err := r.GetSystemSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"DiscussTopicDailyLimit", got.DiscussTopicDailyLimitVal(), 9},
		{"CurrencyName", got.CurrencyNameVal(), "钻石"},
		{"JadeBountyMax", got.JadeBountyMaxVal(), 666},
		{"AiDailyLimit", got.AiDailyLimitVal(), 6},
		{"AiStaffDailyLimit", got.AiStaffDailyLimitVal(), 7}, // 漏登时读到默认 20
		{"AiTutorName", got.AiTutorNameVal(), "阿灵"},
		{"UserSeqStart", got.UserSeqStartVal(), int64(300)},
		{"ProfileEdit.Gender", got.ProfileEditCfg().Gender, false},
		{"ProfileEdit.Nickname", got.ProfileEditCfg().Nickname, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s 保存后重读 = %v，期望 %v（$set 清单疑似漏登该字段）", c.name, c.got, c.want)
		}
	}
}
