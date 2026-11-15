package sandbox

import (
	"errors"
	"testing"
)

func TestStaleFileID(t *testing.T) {
	const id = "L3HKOTZD"
	err := errors.New(`沙箱返回 404: "file does not exist with id \"L3HKOTZD\": file not found"`)
	if !StaleFileID(err, id) {
		t.Fatal("应识别缓存产物失效")
	}
	if StaleFileID(err, "OTHER") {
		t.Fatal("错误里的 fileId 与当前缓存不一致时不应重编")
	}
	if StaleFileID(errors.New("sandbox unavailable"), id) {
		t.Fatal("其他沙箱错误不应触发重编")
	}
	if StaleFileID(nil, id) {
		t.Fatal("nil error 不应触发重编")
	}
}
