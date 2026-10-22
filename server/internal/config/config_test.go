package config

import (
	"reflect"
	"testing"
)

// parseTrustedProxies（v0.13）：none 关闭解析、* 放开全部、常规逗号分隔去空白。
func TestParseTrustedProxies(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"none", nil},
		{"NONE", nil},
		{"*", []string{"0.0.0.0/0", "::/0"}},
		{"127.0.0.1", []string{"127.0.0.1"}},
		{"127.0.0.1,::1", []string{"127.0.0.1", "::1"}},
		{" 10.0.0.0/8 , 172.16.0.0/12 ,", []string{"10.0.0.0/8", "172.16.0.0/12"}},
	}
	for _, tc := range cases {
		if got := parseTrustedProxies(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseTrustedProxies(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
