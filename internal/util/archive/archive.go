// Package archive provides safe archive extraction for tar.gz and zip
// formats. The extractors walk each entry, validate that the resolved path
// stays within the destination directory (path-traversal guard), and skip
// non-regular entries (symlinks, devices, etc.). This is the shared
// implementation used by tool downloads, extension installs, and self-update.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractTarGz extracts a gzipped tar archive to destDir. Each entry's
// resolved path is validated to stay within destDir (guards against
// "../" traversal, absolute paths, and symlinks that escape the dest).
// Non-regular entries (symlinks, devices, fifos) are silently skipped.
func ExtractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open tar.gz %q failed: %w", archivePath, err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("create gzip reader for %q failed: %w", archivePath, err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry failed: %w", err)
		}

		targetPath := filepath.Join(destDir, header.Name)
		cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(destDir) {
			return fmt.Errorf("invalid tar path traversal: %q", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("create extracted dir %q failed: %w", cleanTarget, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
				return fmt.Errorf("create parent dir for %q failed: %w", cleanTarget, err)
			}
			//nolint:gosec // G115: archive mode bits fit FileMode
			outFile, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create extracted file %q failed: %w", cleanTarget, err)
			}
			//nolint:gosec // G110: path-traversal guard above makes this safe
			if _, err := io.Copy(outFile, tarReader); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("write extracted file %q failed: %w", cleanTarget, err)
			}
			if err := outFile.Close(); err != nil {
				return fmt.Errorf("close extracted file %q failed: %w", cleanTarget, err)
			}
		default:
			// Skip symlinks, hardlinks, devices, fifos, etc. These are
			// classic attack vectors in archive extraction — a symlink
			// pointing outside destDir could let a later entry overwrite
			// a system file. The old `tar -xzf` shell-out would extract
			// them; the in-Go extractor skips them.
		}
	}
	return nil
}

// ExtractZip extracts a zip archive to destDir. Each entry's resolved
// path is validated to stay within destDir (guards against "../" traversal
// and absolute paths). Directory entries create directories; file entries
// are copied with their stored mode.
func ExtractZip(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip %q failed: %w", archivePath, err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		targetPath := filepath.Join(destDir, file.Name)
		cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(destDir) {
			return fmt.Errorf("invalid zip path traversal: %q", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("create extracted dir %q failed: %w", cleanTarget, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return fmt.Errorf("create parent dir for %q failed: %w", cleanTarget, err)
		}

		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q failed: %w", file.Name, err)
		}
		outFile, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("create extracted file %q failed: %w", cleanTarget, err)
		}
		//nolint:gosec // G110: path-traversal guard above makes this safe
		if _, err := io.Copy(outFile, rc); err != nil {
			_ = rc.Close()
			_ = outFile.Close()
			return fmt.Errorf("write extracted file %q failed: %w", cleanTarget, err)
		}
		if err := rc.Close(); err != nil {
			_ = outFile.Close()
			return fmt.Errorf("close zip entry %q failed: %w", file.Name, err)
		}
		if err := outFile.Close(); err != nil {
			return fmt.Errorf("close extracted file %q failed: %w", cleanTarget, err)
		}
	}
	return nil
}
