package update

import (
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"

        "github.com/stretchr/testify/require"
)

func TestStagingDirPrefersBinaryParent(t *testing.T) {
        dir := t.TempDir()
        bin := filepath.Join(dir, "a1")
        require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

        tmp, err := stagingDir(bin)
        require.NoError(t, err)
        t.Cleanup(func() { _ = os.RemoveAll(tmp) })

        rel, err := filepath.Rel(dir, tmp)
        require.NoError(t, err)
        require.False(t, strings.HasPrefix(rel, ".."), "staging dir %q should be under %q", tmp, dir)
}

// TestReleaseAssetNameStampedA1 pins the GoReleaser asset prefix: the
// pre-rename phi_ prefix made `a1 update` 404 on every download (the
// install scripts were fixed earlier; this is the in-binary offender).
func TestReleaseAssetNameStampedA1(t *testing.T) {
        name, format, err := releaseAssetName("v2.0.1")
        require.NoError(t, err)
        require.True(t, strings.HasPrefix(name, "a1_2.0.1_"), "asset %q must carry the a1_ prefix", name)
        require.Equal(t, runtime.GOOS+"_"+runtime.GOARCH+"."+format,
                strings.TrimPrefix(name, "a1_2.0.1_"))
}

func TestRelocateFile(t *testing.T) {
        dir := t.TempDir()
        src := filepath.Join(dir, "src.bin")
        dst := filepath.Join(dir, "dst.bin")
        require.NoError(t, os.WriteFile(src, []byte("new-payload"), 0o755))
        require.NoError(t, os.WriteFile(dst, []byte("old"), 0o755))

        require.NoError(t, relocateFile(src, dst))
        got, err := os.ReadFile(dst)
        require.NoError(t, err)
        require.Equal(t, "new-payload", string(got))
}

func TestCopyFileOverwritesViaTemp(t *testing.T) {
        // copyFile writes to dst+".new" then renames — the path used when
        // os.Rename(src, dst) cannot cross volumes (Windows D: vs C:\Temp).
        dir := t.TempDir()
        srcDir := filepath.Join(dir, "srcvol")
        dstDir := filepath.Join(dir, "dstvol")
        require.NoError(t, os.MkdirAll(srcDir, 0o755))
        require.NoError(t, os.MkdirAll(dstDir, 0o755))
        src := filepath.Join(srcDir, "a1.exe")
        dst := filepath.Join(dstDir, "a1.exe")
        require.NoError(t, os.WriteFile(src, []byte("new"), 0o755))
        require.NoError(t, os.WriteFile(dst, []byte("old"), 0o755))

        require.NoError(t, copyFile(src, dst))
        got, err := os.ReadFile(dst)
        require.NoError(t, err)
        require.Equal(t, "new", string(got))
}

func TestCopyFileIntoMissingDestination(t *testing.T) {
        // Windows replaceBinary renames the live exe to .old first, so dst is gone.
        dir := t.TempDir()
        src := filepath.Join(dir, "new.exe")
        dst := filepath.Join(dir, "a1.exe")
        require.NoError(t, os.WriteFile(src, []byte("new"), 0o755))

        require.NoError(t, copyFile(src, dst))
        got, err := os.ReadFile(dst)
        require.NoError(t, err)
        require.Equal(t, "new", string(got))
}

func TestReplaceBinary(t *testing.T) {
        dir := t.TempDir()
        cur := filepath.Join(dir, "a1")
        if runtime.GOOS == "windows" {
                cur += ".exe"
        }
        newBin := filepath.Join(dir, "extracted-a1")
        if runtime.GOOS == "windows" {
                newBin += ".exe"
        }
        require.NoError(t, os.WriteFile(cur, []byte("old-bin"), 0o755))
        require.NoError(t, os.WriteFile(newBin, []byte("new-bin"), 0o755))

        require.NoError(t, replaceBinary(cur, newBin))
        got, err := os.ReadFile(cur)
        require.NoError(t, err)
        require.Equal(t, "new-bin", string(got))

        if runtime.GOOS == "windows" {
                bak := cur + ".old"
                bakData, err := os.ReadFile(bak)
                require.NoError(t, err)
                require.Equal(t, "old-bin", string(bakData))
        }
}

func TestCopyFile(t *testing.T) {
        dir := t.TempDir()
        src := filepath.Join(dir, "a")
        dst := filepath.Join(dir, "subdir", "b")
        require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
        require.NoError(t, os.WriteFile(src, []byte("payload"), 0o600))

        require.NoError(t, copyFile(src, dst))
        got, err := os.ReadFile(dst)
        require.NoError(t, err)
        require.Equal(t, "payload", string(got))
        // Source remains; temp cleanup is the caller's job.
        _, err = os.Stat(src)
        require.NoError(t, err)
}
