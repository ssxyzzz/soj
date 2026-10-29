// Package archive 题包 zip 解压与摊平（v0.32 巨型文件治理，自 server/problemAdmin.go
// 拆出的纯函数，行为零变化）：三道闸防 zip 炸弹与路径穿越——条目数上限、解压总量/
// 单文件上限、条目名穿越成分拒绝；GB18030 条目名回退解码；「整包套顶层目录」摊平。
// 限额是调用方策略（建题小限额 / 批量导入 GB 级），经参数传入。
package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// SafeExtractZipLimits 把 zip 解压到 dst：拒绝穿越成分、条目数与解压总量超限。
// progress（可 nil）：每写完一个条目回调（已解压字节, 总字节）——批量导入用它做解压实时进度。
func SafeExtractZipLimits(zipPath, dst string, maxEntries int, maxTotal, maxSingle int64, singleMBHint, totalMBHint string, progress func(done, total int64)) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return errors.New("不是合法的 zip 文件")
	}
	defer zr.Close()
	dstRoot := filepath.Clean(dst)
	if len(zr.File) > maxEntries {
		return errors.New("压缩包条目过多")
	}
	// 预检遍历：路径合法性/单文件/总量限额 + 汇总总字节（进度分母），
	// 与写盘遍历分离——回调的 total 在写第一个文件前就已知
	total := int64(0)
	for _, f := range zr.File {
		name := zipEntryName(f)
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
			return errors.New("压缩包含非法路径")
		}
		if f.Mode().IsDir() {
			continue
		}
		if f.UncompressedSize64 > uint64(maxSingle) {
			return fmt.Errorf("压缩包内单文件超过 %s", singleMBHint)
		}
		total += int64(f.UncompressedSize64)
		if total > maxTotal {
			return fmt.Errorf("压缩包解压总量超过 %s", totalMBHint)
		}
	}
	done := int64(0)
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		dest := filepath.Join(dstRoot, filepath.FromSlash(path.Clean("/"+zipEntryName(f))))
		if !strings.HasPrefix(dest, dstRoot+string(os.PathSeparator)) {
			return errors.New("压缩包含非法路径")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
		done += int64(f.UncompressedSize64)
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}

// zipEntryName 条目名归一：旧式 zip（Windows 资源管理器「发送到压缩文件夹」与
// 多数中文压缩工具）不设 UTF-8 标志位，文件名是系统 ANSI 字节（中文系统即 GBK），
// Go 原样透传成非法 UTF-8——目录名整体乱码。标志位缺失时按 GB18030 回退解码，
// 解码失败（其他码系产物）原样返回、不比现状差。
func zipEntryName(f *zip.File) string {
	if !f.NonUTF8 {
		return f.Name
	}
	if dec, err := simplifiedchinese.GB18030.NewDecoder().Bytes([]byte(f.Name)); err == nil {
		return string(dec)
	}
	return f.Name
}

// UnwrapSingleRootDir 摊平「整个包套在顶层目录里」的解压结果：根下剔除打包杂物后
// 只剩一个子目录、且该子目录不叫 data/cases 时，把其内容整体上移一层，反复进行。
func UnwrapSingleRootDir(dir string) error {
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		live := make([]os.DirEntry, 0, len(entries))
		for _, e := range entries {
			if !IsPackagingJunk(e.Name()) {
				live = append(live, e)
			}
		}
		if len(live) != 1 || !live[0].IsDir() || live[0].Name() == "data" || live[0].Name() == "cases" {
			return nil
		}
		inner := filepath.Join(dir, live[0].Name())
		sub, err := os.ReadDir(inner)
		if err != nil {
			return err
		}
		for _, e := range sub {
			if err := os.Rename(filepath.Join(inner, e.Name()), filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
		if err := os.Remove(inner); err != nil {
			return err
		}
	}
}

// IsPackagingJunk 压缩工具附带的系统杂物：macOS 的 __MACOSX/、.DS_Store 与 ._ 资源分叉文件。
func IsPackagingJunk(name string) bool {
	return name == "__MACOSX" || name == ".DS_Store" || strings.HasPrefix(name, "._")
}
