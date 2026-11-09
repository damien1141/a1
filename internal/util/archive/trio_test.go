package archive_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/util/archive"
)

// ---------- differential ----------

// patternBytes is deterministic pseudo-content with NULs, 0xff runs, and
// high-entropy bytes, so a byte-for-byte comparison cannot pass on luck or
// on zero-filled files.
func patternBytes(n int, seed byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i*131+int(seed)) ^ byte(i>>3)
	}
	return out
}

// diskTree enumerates what actually landed on disk through the filesystem
// alone (filepath.WalkDir + os.ReadFile), never through archive code.
func diskTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	require.NoError(t, err)
	return files
}

// tarGzContents re-reads a tar.gz with the stdlib reader and returns its
// regular-file entries: the ground truth the extractor must reproduce.
func tarGzContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		out[hdr.Name] = data
	}
	return out
}

// zipContents re-reads a zip with the stdlib reader and returns its
// non-directory entries: the ground truth the extractor must reproduce.
func zipContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer r.Close()
	out := map[string][]byte{}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		out[f.Name] = data
	}
	return out
}

func writeTarEntry(t *testing.T, tw *tar.Writer, name string, typeflag byte, data []byte) {
	t.Helper()
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: typeflag,
		Mode:     0o644,
		Size:     int64(len(data)),
	}))
	_, err := tw.Write(data)
	require.NoError(t, err)
}

// oracle: stdlib archive/tar — the fixture is written with archive/tar's
// writer, the expected tree is enumerated by re-reading the archive with a
// plain stdlib tar.Reader (regular files only), and the extracted result is
// enumerated independently through the filesystem. The two maps must agree
// byte-for-byte, including a PAX long-name entry and a symlink that must be
// skipped rather than materialized.
func TestExtractTarGz_TreeMatchesStdlibReader_Differential(t *testing.T) {
	destDir := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "fixture.tar.gz")

	longName := "deep/" + strings.Repeat("long-", 26) + "name.txt" // 143 chars → PAX header

	f, err := os.Create(archivePath)
	require.NoError(t, err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	writeTarEntry(t, tw, "top/", tar.TypeDir, nil)
	writeTarEntry(t, tw, "top/readme.txt", tar.TypeReg, []byte("extracted by a1\n"))
	writeTarEntry(t, tw, "top/data.bin", tar.TypeReg, patternBytes(4096, 7))
	writeTarEntry(t, tw, "implicit/parents/leaf.txt", tar.TypeReg, patternBytes(1000, 200))
	writeTarEntry(t, tw, "empty.txt", tar.TypeReg, nil)
	writeTarEntry(t, tw, longName, tar.TypeReg, patternBytes(2048, 99))
	// The extractor must silently skip symlinks; the oracle map counts only
	// regular files, so a broken extractor that materializes the link shows
	// up as an extra key.
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "top/link",
		Typeflag: tar.TypeSymlink,
		Linkname: "../../etc/passwd",
		Mode:     0o777,
	}))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, f.Close())

	require.NoError(t, archive.ExtractTarGz(archivePath, destDir))
	assert.Equal(t, tarGzContents(t, archivePath), diskTree(t, destDir),
		"extracted tree must match the archive's regular-file entries byte-for-byte")
}

// oracle: stdlib archive/zip — the fixture is written with archive/zip's
// writer, the expected tree is enumerated by re-reading the archive with
// zip.OpenReader (skipping directory entries), and the extracted result is
// enumerated independently through the filesystem. The two maps must agree
// byte-for-byte.
func TestExtractZip_TreeMatchesStdlibReader_Differential(t *testing.T) {
	destDir := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "fixture.zip")

	f, err := os.Create(archivePath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)

	// A trailing-slash entry is how zips record directories.
	_, err = zw.Create("pkg/")
	require.NoError(t, err)
	for name, data := range map[string][]byte{
		"pkg/main.go":            []byte("package pkg\n"),
		"pkg/assets/logo.bin":    patternBytes(8192, 42),
		"root.txt":               []byte("at the archive root\n"),
		"deep/one/two/three.txt": patternBytes(512, 3),
		"empty.dat":              nil,
	} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	require.NoError(t, archive.ExtractZip(archivePath, destDir))
	assert.Equal(t, zipContents(t, archivePath), diskTree(t, destDir),
		"extracted tree must match the archive's file entries byte-for-byte")
}
