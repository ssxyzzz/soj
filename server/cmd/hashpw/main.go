// hashpw 是一个种子数据小工具：生成 bcrypt 密码哈希。
// 用法: go run ./cmd/hashpw <密码>
package main

import (
	"fmt"
	"os"

	"soj/internal/auth"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "用法: hashpw <密码>")
		os.Exit(1)
	}
	hash, err := auth.HashPassword(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成失败:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
