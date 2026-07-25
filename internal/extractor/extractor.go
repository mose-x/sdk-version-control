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

// Extractor extraction interface
type Extractor interface {
	Extract(archivePath, destDir string) error
}

// NewExtractor selects extraction strategy based on file extension
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
		return nil, fmt.Errorf("unsupported archive format: %s", filename)
	}
}

// StripTopDir moves contents up one level if destDir has only one subdirectory and no files
// Used to handle extra top-level directories in archives like JDK/Node.js
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
			return fmt.Errorf("failed to move %s: %w", e.Name(), err)
		}
	}
	return os.Remove(topDir)
}

// ZipExtractor .zip extractor
type ZipExtractor struct{}

func (e *ZipExtractor) Extract(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(destDir) {
			return fmt.Errorf("invalid file path: %s", f.Name)
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

// TarGzExtractor .tar.gz / .tgz extractor
type TarGzExtractor struct{}

func (e *TarGzExtractor) Extract(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	return extractTar(tar.NewReader(gzr), destDir)
}

// TarXzExtractor .tar.xz extractor
type TarXzExtractor struct{}

func (e *TarXzExtractor) Extract(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create xz reader: %w", err)
	}

	return extractTar(tar.NewReader(xzr), destDir)
}

// SevenZipExtractor .7z extractor
type SevenZipExtractor struct{}

func (e *SevenZipExtractor) Extract(archivePath, destDir string) error {
	r, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open 7z file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(destDir) {
			return fmt.Errorf("invalid file path: %s", f.Name)
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

// extractTar generic tar extractor
func extractTar(tr *tar.Reader, destDir string) error {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		path := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(destDir) {
			return fmt.Errorf("invalid file path: %s", header.Name)
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
				return fmt.Errorf("invalid symlink: %s -> %s", header.Name, header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			os.Remove(path)
			if err := os.Symlink(header.Linkname, path); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}
	return nil
}
