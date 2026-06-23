package extractor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/ulikunitz/xz"
)

// Extractor 解压接口
type Extractor interface {
	Extract(archivePath, destDir string) error
}

// NewExtractor 根据文件扩展名选择解压策略
func NewExtractor(filename string) (Extractor, error) {
	switch {
	case strings.HasSuffix(filename, ".zip"):
		return &ZipExtractor{}, nil
	case strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz"):
		return &TarGzExtractor{}, nil
	case strings.HasSuffix(filename, ".tar.xz"):
		return &TarXzExtractor{}, nil
	case strings.HasSuffix(filename, ".7z"):
		return &SevenZipExtractor{}, nil
	default:
		return nil, fmt.Errorf("不支持的压缩格式: %s", filename)
	}
}

// StripTopDir 如果 destDir 下只有一个子目录且没有文件，则将该子目录的内容上移一层
// 用于处理 JDK/Node.js 等压缩包中多余的顶层目录
func StripTopDir(destDir string) error {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil
	}
	topDir := filepath.Join(destDir, entries[0].Name())
	subEntries, err := os.ReadDir(topDir)
	if err != nil {
		return err
	}
	for _, e := range subEntries {
		src := filepath.Join(topDir, e.Name())
		dst := filepath.Join(destDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("移动 %s 失败: %w", e.Name(), err)
		}
	}
	return os.Remove(topDir)
}

// ZipExtractor .zip 解压
type ZipExtractor struct{}

func (e *ZipExtractor) Extract(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开zip文件失败: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(destDir) {
			return fmt.Errorf("非法文件路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// TarGzExtractor .tar.gz / .tgz 解压
type TarGzExtractor struct{}

func (e *TarGzExtractor) Extract(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("创建gzip reader失败: %w", err)
	}
	defer gzr.Close()

	return extractTar(tar.NewReader(gzr), destDir)
}

// TarXzExtractor .tar.xz 解压
type TarXzExtractor struct{}

func (e *TarXzExtractor) Extract(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("创建xz reader失败: %w", err)
	}

	return extractTar(tar.NewReader(xzr), destDir)
}

// SevenZipExtractor .7z 解压
type SevenZipExtractor struct{}

func (e *SevenZipExtractor) Extract(archivePath, destDir string) error {
	r, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开7z文件失败: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(destDir) {
			return fmt.Errorf("非法文件路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// extractTar 通用 tar 解压
func extractTar(tr *tar.Reader, destDir string) error {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取tar条目失败: %w", err)
		}

		path := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(destDir) {
			return fmt.Errorf("非法文件路径: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, err = io.Copy(outFile, tr)
			outFile.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			linkTarget := header.Linkname
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(path), linkTarget)
			}
			if !strings.HasPrefix(filepath.Clean(linkTarget), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(linkTarget) != filepath.Clean(destDir) {
				return fmt.Errorf("非法符号链接: %s -> %s", header.Name, header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			os.Remove(path)
			if err := os.Symlink(header.Linkname, path); err != nil {
				return fmt.Errorf("创建符号链接失败: %w", err)
			}
		}
	}
	return nil
}
