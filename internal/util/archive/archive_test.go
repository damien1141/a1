package archive_test

import (
        "archive/tar"
        "archive/zip"
        "compress/gzip"
        "os"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/util/archive"
)

// TestExtractTarGz_PathTraversal_Rejects is the regression test for #15:
// the old extractArchive shelled out to system `tar` with no
// path-traversal guard. A malicious archive with entries like
// "../../etc/passwd" could write outside destDir. The in-Go extractor
// must reject every traversal attempt.
//
// Note: absolute paths like "/tmp/escape.txt" are NOT a traversal under
// the in-Go guard because filepath.Join(destDir, "/tmp/escape.txt")
// normalizes to "destDir/tmp/escape.txt" — safe. Only "../" sequences
// that escape destDir after Join+Clean are caught.
func TestExtractTarGz_PathTraversal_Rejects(t *testing.T) {
        cases := []struct {
                name    string
                entries []struct {
                        name     string
                        typeflag byte
                        content  string
                }
        }{
                {
                        name: "relative parent traversal",
                        entries: []struct {
                                name     string
                                typeflag byte
                                content  string
                        }{
                                {"../../escape.txt", tar.TypeReg, "pwned"},
                        },
                },
                {
                        name: "nested parent traversal",
                        entries: []struct {
                                name     string
                                typeflag byte
                                content  string
                        }{
                                {"sub/../../../escape.txt", tar.TypeReg, "pwned"},
                        },
                },
        }

        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        destDir := t.TempDir()
                        archivePath := filepath.Join(t.TempDir(), "evil.tar.gz")

                        // Build the malicious archive in-process.
                        f, err := os.Create(archivePath)
                        require.NoError(t, err)
                        gz := gzip.NewWriter(f)
                        tw := tar.NewWriter(gz)
                        for _, e := range tc.entries {
                                err := tw.WriteHeader(&tar.Header{
                                        Name:     e.name,
                                        Typeflag: e.typeflag,
                                        Mode:     0o644,
                                        Size:     int64(len(e.content)),
                                })
                                require.NoError(t, err)
                                _, _ = tw.Write([]byte(e.content))
                        }
                        require.NoError(t, tw.Close())
                        require.NoError(t, gz.Close())
                        require.NoError(t, f.Close())

                        err = archive.ExtractTarGz(archivePath, destDir)
                        require.Error(t, err, "path-traversal entry must be rejected")
                        assert.Contains(t, err.Error(), "path traversal",
                                "error must name the traversal: %s", err)
                })
        }
}

// TestExtractTarGz_SkipsSymlinks verifies that symlink entries are silently
// skipped (not extracted). The old `tar -xzf` would extract symlinks,
// which could point outside destDir and let a later entry overwrite a
// system file.
func TestExtractTarGz_SkipsSymlinks(t *testing.T) {
        destDir := t.TempDir()
        archivePath := filepath.Join(t.TempDir(), "symlink.tar.gz")

        f, err := os.Create(archivePath)
        require.NoError(t, err)
        gz := gzip.NewWriter(f)
        tw := tar.NewWriter(gz)

        // A symlink pointing outside destDir.
        require.NoError(t, tw.WriteHeader(&tar.Header{
                Name:     "link",
                Typeflag: tar.TypeSymlink,
                Linkname: "../../etc/passwd",
                Mode:     0o777,
                Size:     0,
        }))
        // A regular file that would be fine on its own.
        require.NoError(t, tw.WriteHeader(&tar.Header{
                Name:     "safe.txt",
                Typeflag: tar.TypeReg,
                Mode:     0o644,
                Size:     int64(len("safe")),
        }))
        _, _ = tw.Write([]byte("safe"))
        require.NoError(t, tw.Close())
        require.NoError(t, gz.Close())
        require.NoError(t, f.Close())

        err = archive.ExtractTarGz(archivePath, destDir)
        require.NoError(t, err, "extraction succeeds; symlink is silently skipped")

        // safe.txt exists; link does not.
        _, err = os.Stat(filepath.Join(destDir, "safe.txt"))
        assert.NoError(t, err, "regular file is extracted")
        _, err = os.Lstat(filepath.Join(destDir, "link"))
        assert.True(t, os.IsNotExist(err), "symlink is not extracted")
}

// TestExtractTarGz_NormalExtraction verifies that a well-formed archive
// extracts correctly — the guard rejects bad archives but must not break
// good ones.
func TestExtractTarGz_NormalExtraction(t *testing.T) {
        destDir := t.TempDir()
        archivePath := filepath.Join(t.TempDir(), "good.tar.gz")

        f, err := os.Create(archivePath)
        require.NoError(t, err)
        gz := gzip.NewWriter(f)
        tw := tar.NewWriter(gz)

        require.NoError(t, tw.WriteHeader(&tar.Header{
                Name:     "dir",
                Typeflag: tar.TypeDir,
                Mode:     0o755,
                Size:     0,
        }))
        require.NoError(t, tw.WriteHeader(&tar.Header{
                Name:     "dir/file.txt",
                Typeflag: tar.TypeReg,
                Mode:     0o644,
                Size:     4,
        }))
        _, _ = tw.Write([]byte("test"))
        require.NoError(t, tw.Close())
        require.NoError(t, gz.Close())
        require.NoError(t, f.Close())

        err = archive.ExtractTarGz(archivePath, destDir)
        require.NoError(t, err)
        got, err := os.ReadFile(filepath.Join(destDir, "dir", "file.txt"))
        require.NoError(t, err)
        assert.Equal(t, "test", string(got))
}

// TestExtractZip_PathTraversal_Rejects is the zip equivalent of the
// tar.gz traversal test.
func TestExtractZip_PathTraversal_Rejects(t *testing.T) {
        cases := []string{
                "../../escape.txt",
                "sub/../../../escape.txt",
        }

        for _, name := range cases {
                t.Run(name, func(t *testing.T) {
                        destDir := t.TempDir()
                        archivePath := filepath.Join(t.TempDir(), "evil.zip")

                        zf, err := os.Create(archivePath)
                        require.NoError(t, err)
                        zw := zip.NewWriter(zf)
                        _, err = zw.Create(name)
                        require.NoError(t, err)
                        require.NoError(t, zw.Close())
                        require.NoError(t, zf.Close())

                        err = archive.ExtractZip(archivePath, destDir)
                        require.Error(t, err)
                        assert.Contains(t, err.Error(), "path traversal")
                })
        }
}
