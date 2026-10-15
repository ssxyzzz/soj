// Package testdata 实现评测数据的存储层。
//
// 设计约定（见 DEVELOPMENT.md「评测数据按需抓取」）：
// 数据库只存元数据（文件清单/大小/sha256），测试数据本体放文件存储，
// 判题时逐测试点按需读取。读取层抽象为 Provider 接口，
// v0.2 实现 LocalFS；日后换 S3/MinIO 只需新增实现，评测流程不动。
package testdata

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Provider 评测数据存储接口。relPath 使用正斜杠相对路径，如 "cases/1/in"。
type Provider interface {
	Open(problemID int64, relPath string) (io.ReadCloser, error)
	WriteFile(problemID int64, relPath string, data []byte) error
}

// LocalFS 本地文件系统实现，目录布局: <Root>/<problemID>/<relPath>。
type LocalFS struct {
	Root string
}

func NewLocalFS(root string) *LocalFS { return &LocalFS{Root: root} }

func (p *LocalFS) path(problemID int64, relPath string) (string, error) {
	if path.Clean(relPath) != relPath || strings.HasPrefix(relPath, "/") || containsDotDot(relPath) {
		return "", os.ErrInvalid
	}
	return filepath.Join(p.Root, strconv.FormatInt(problemID, 10), filepath.FromSlash(relPath)), nil
}

func containsDotDot(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func (p *LocalFS) Open(problemID int64, relPath string) (io.ReadCloser, error) {
	full, err := p.path(problemID, relPath)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

func (p *LocalFS) WriteFile(problemID int64, relPath string, data []byte) error {
	full, err := p.path(problemID, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

// ReadAll 读取一个小文件的全部内容（测试点输入/答案通常在 MB 级以内）。
func ReadAll(p Provider, problemID int64, relPath string) ([]byte, error) {
	f, err := p.Open(problemID, relPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
