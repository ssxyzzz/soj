package server

// profileJSON 本人视角下发契约（v0.97.24 起含登录用户名只读展示）。

import (
	"testing"

	"github.com/gin-gonic/gin"

	"soj/internal/repo"
)

func TestProfileJSONUsername(t *testing.T) {
	u := &repo.User{ID: 7, Username: "zhangsan7", Nickname: "张三"}
	info := profileJSON(u, *repo.DefaultProfileEditConfig())
	if info["username"] != "zhangsan7" {
		t.Fatalf("本人视角应下发登录用户名，got %v", info["username"])
	}
	if info["nickname"] != "张三" {
		t.Fatalf("昵称下发回归，got %v", info["nickname"])
	}
	ed := info["editable"].(gin.H)
	if _, hasUsername := ed["username"]; hasUsername {
		t.Fatal("用户名恒只读展示，不进 editable 自改标志体系")
	}
}
