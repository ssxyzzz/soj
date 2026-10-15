// Package webui 内嵌前端构建产物（vite build 输出到 dist/）。
// 本地开发时先 `cp -r web/dist server/internal/webui/dist`（Makefile 已封装）；
// Docker 构建时由多阶段构建自动完成。dist 不存在时保留 .gitkeep 以便编译。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回 dist 目录的文件系统视图。
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
