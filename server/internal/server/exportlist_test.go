package server

// parsePositiveIDList（多题打包导出 v0.18.1）纯函数单测。

import "testing"

func TestParsePositiveIDList(t *testing.T) {
	got, err := parsePositiveIDList("3, 1,3 ,2", 200)
	if err != nil || len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("去重保序: %v, %v", got, err)
	}
	if _, err := parsePositiveIDList("", 200); err == nil {
		t.Fatal("空串应报错")
	}
	if _, err := parsePositiveIDList("0,1", 200); err == nil {
		t.Fatal("0 应报错")
	}
	if _, err := parsePositiveIDList("abc", 200); err == nil {
		t.Fatal("非数字应报错")
	}
	if _, err := parsePositiveIDList("-5", 200); err == nil {
		t.Fatal("负数应报错")
	}
	ids := ""
	for i := 0; i < 201; i++ {
		if i > 0 {
			ids += ","
		}
		ids += string(rune('0'+i%10)) + string(rune('0'+i/10%10)) // 粗糙但唯一的两位数
	}
	if _, err := parsePositiveIDList(ids, 200); err == nil {
		t.Fatal("超过上限 200 应报错")
	}
}
