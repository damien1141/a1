package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- helpers ----------

// refusingProxy stands up a local listener that accepts and immediately
// closes every connection. Pointing HTTPS_PROXY at it makes every outbound
// https request (api.github.com included) fail fast and hermetically — no
// packet ever leaves the loopback interface.
func refusingProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return "http://" + ln.Addr().String()
}

// offline hermetically cuts the package's only network dependency
// (api.github.com via the shared client's ProxyFromEnvironment).
func offline(t *testing.T) {
	t.Helper()
	t.Setenv("A1_SKIP_VERSION_CHECK", "")
	t.Setenv("A1_OFFLINE", "")
	t.Setenv("HTTPS_PROXY", refusingProxy(t))
	t.Setenv("HTTP_PROXY", refusingProxy(t))
}

// ---------- negative ----------

func TestInstallDevBuild_Negative(t *testing.T) {
	const wantErr = "dev build: `a1 update` is disabled. " +
		"Build a release tag or download from https://github.com/damien1141/a1/releases"
	for _, current := range []string{"", "dev", "0.0.0", "v0.0.0 (abc1234, 2026-05-12)"} {
		var out bytes.Buffer
		err := Install(t.Context(), InstallOptions{Current: current, Stdout: &out})
		require.Error(t, err, "current %q", current)
		assert.EqualError(t, err, wantErr, "current %q", current)
	}
}

func TestCheckOnlyDevBuild_Negative(t *testing.T) {
	// CheckOnly prints straight to os.Stdout; swap in a pipe to pin the text.
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	stdoutErr := CheckOnly(t.Context(), "v0.0.0")
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	require.NoError(t, stdoutErr, "a dev build is a graceful no-op, not an error")
	assert.Equal(t, "a1: dev build — `a1 update` is disabled\n", string(out))
}

func TestInstallNetworkFailure_Negative(t *testing.T) {
	offline(t)
	var out bytes.Buffer
	err := Install(t.Context(), InstallOptions{Current: "v0.1.0", Stdout: &out})
	require.Error(t, err, "an unreachable release API must fail the install")
	assert.Contains(t, err.Error(), "query latest release:")
}

// TestCheckCorruptCacheFallsThrough_Negative pins the cache contract on a
// hostile cache file: an unparseable update-check.json is ignored (never
// fatal), the network is consulted, and on failure Check degrades to a zero
// Info without touching the broken file.
func TestCheckCorruptCacheFallsThrough_Negative(t *testing.T) {
	offline(t)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, updateCheckFile)
	corrupt := []byte("{not json at all")
	require.NoError(t, os.WriteFile(cachePath, corrupt, 0o600))

	info := Check(t.Context(), CheckOptions{Current: "v0.1.0", CacheDir: dir})
	assert.Equal(t, "v0.1.0", info.Current, "the running version survives every failure")
	assert.False(t, info.Available, "a failed check never claims an update")
	assert.Empty(t, info.Latest)
	assert.Empty(t, info.URL)

	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.Equal(t, corrupt, data, "a failed network check must not rewrite the cache")
}

// TestCheckDevBuildWritesNothing_Negative: the dev guard fires before any
// IO, so no cache file appears.
func TestCheckDevBuildWritesNothing_Negative(t *testing.T) {
	dir := t.TempDir()
	info := Check(t.Context(), CheckOptions{Current: "dev", CacheDir: dir})
	assert.Equal(t, Info{Current: "dev"}, info)

	_, err := os.Stat(filepath.Join(dir, updateCheckFile))
	assert.True(t, os.IsNotExist(err), "skipped checks write no cache")
}

func TestLookupChecksumMisses_Negative(t *testing.T) {
	dir := t.TempDir()

	// Present, but the asset is not listed.
	path := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(path, []byte(
		"1111111111111111111111111111111111111111111111111111111111111111  other.tar.gz\n"+
			"onlyonefield\n"+
			"\n"), 0o644))
	_, err := lookupChecksum(path, "a1_2.0.0_linux_amd64.tar.gz")
	require.Error(t, err)
	assert.EqualError(t, err, "checksum for a1_2.0.0_linux_amd64.tar.gz not listed in checksums file")

	// Unreadable file.
	_, err = lookupChecksum(filepath.Join(dir, "missing.txt"), "a1.tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read checksums:")
}

func TestExtractArchiveUnknownFormat_Negative(t *testing.T) {
	err := extractArchive(t.Context(), "whatever.rar", "rar", t.TempDir())
	require.Error(t, err)
	assert.EqualError(t, err, "unknown archive format: rar")
}

func TestReplaceBinaryMissingCurrent_Negative(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "new")
	require.NoError(t, os.WriteFile(newBin, []byte("x"), 0o755))

	err := replaceBinary(filepath.Join(dir, "does-not-exist"), newBin)
	require.Error(t, err, "replacing a vanished binary must fail, not guess")
	assert.Contains(t, err.Error(), "stat current binary:")
}

// TestVersionParsingGarbage_Negative pins how the parser degrades on
// invalid and boundary version strings: non-numeric components fold to
// zero, only the first three components are compared, and a leading minus
// parses as a negative number.
func TestVersionParsingGarbage_Negative(t *testing.T) {
	assert.Equal(t, []int{0, 0, 0}, splitVersion("vX.Y.Z"), "non-numeric parts fold to zero")
	assert.Equal(t, []int{1, 2, 3, 4}, splitVersion("1.2.3.4"), "all components parse")

	assert.True(t, VersionLess("vX.Y.Z", "v0.0.1"), "all-zero garbage sorts below any real patch")
	assert.False(t, VersionLess("vX.Y.Z", "v0.0.0"))
	assert.False(t, VersionLess("1.2.3.4", "1.2.3.5"), "differences past component three are invisible")
	assert.False(t, VersionLess("v1.2", "v1.2.0"), "missing components pad with zero")
	assert.True(t, VersionLess("v-1.0.0", "v0.0.0"), "a negative component parses and compares")

	assert.Equal(t, "v0.2.0", versionOnly("v0.2.0 (abc1234, 2026-05-12)"))
	assert.Equal(t, "v0.2.0", versionOnly("v0.2.0(abc)"))
	assert.Equal(t, "(leading", versionOnly("(leading"), "a marker at index zero is not a separator")
}

// TestChecksumMismatchDetected_Negative builds the two halves of the
// updater's verify step by hand and pins that a tampered archive is
// detectable: the digest recomputed from the file must differ from the one
// listed in the checksums file, in exactly the way Install compares them.
func TestChecksumMismatchDetected_Negative(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a1_2.0.0_linux_amd64.tar.gz")
	genuine := bytes.Repeat([]byte("genuine archive bytes, "), 4096)
	tampered := append(append([]byte{}, genuine[:len(genuine)/2]...), []byte("EVIL PAYLOAD")...)
	require.NoError(t, os.WriteFile(archive, tampered, 0o644))

	// The release's checksums file lists the digest of the *untampered* bytes.
	want := sha256.Sum256(genuine)
	sums := filepath.Join(dir, "checksums_2.0.0.txt")
	require.NoError(t, os.WriteFile(sums, []byte(
		hex.EncodeToString(want[:])+"  a1_2.0.0_linux_amd64.tar.gz\n"), 0o644))

	listed, err := lookupChecksum(sums, "a1_2.0.0_linux_amd64.tar.gz")
	require.NoError(t, err)
	got, err := sha256File(archive)
	require.NoError(t, err)

	assert.NotEqual(t, listed, got, "a flipped archive must not match its listed checksum")
	assert.False(t, strings.EqualFold(got, listed), "the exact comparison Install uses")
}

// ---------- differential ----------

// naiveVersionLess is the reference: strip the v/V prefix, cut build
// metadata, split on dots, parse every component, pad the shorter side with
// zeros, compare numerically. Written longhand against the implementation's
// positional three-slot loop.
func naiveVersionLess(a, b string) bool {
	strip := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
		if i := strings.IndexAny(v, " (-"); i > 0 {
			v = v[:i]
		}
		parts := strings.Split(v, ".")
		out := make([]int, 0, len(parts))
		for _, p := range parts {
			n, _ := strconv.Atoi(p)
			out = append(out, n)
		}
		return out
	}
	as, bs := strip(a), strip(b)
	n := max(len(as), len(bs))
	for i := range n {
		av, bv := 0, 0
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}

// oracle: naive semantic-parse reference (split on dots, numeric compare,
// zero-pad the shorter side) — the version matrix is restricted to
// well-formed dotted numerics, the shapes a1 actually publishes.
func TestVersionLessMatrix_Differential(t *testing.T) {
	versions := []string{
		"v0.1.0", "0.2.0", "v0.2.0", "v1.2.3", "v1.2.4", "v10.0.0", "v9.0.0",
		"v0.10.0", "v0.9.9", "v1.2", "v1.2.0", "v1.2.1", "v1", "v1.0.0", "v1.0.1",
		"v0.0.1", "v0.1.0", "V2.0.0", "v1.99.99", "v01.02.03", "v1.2.4",
		"v0.2.0 (abc1234, 2026-05-12)", "v0.2.1", "v0.2.0-rc.1", "v0.2.0",
		"999999.0.0", "1000000.0.0", "v2.0.0", "V10.0.0",
	}
	for _, a := range versions {
		for _, b := range versions {
			want := naiveVersionLess(a, b)
			assert.Equal(t, want, VersionLess(a, b), "VersionLess(%q, %q)", a, b)
		}
	}
}

// oracle: stdlib sha256.Sum256 over the same hand-built bytes — a one-shot
// digest of the whole payload, independent of the streaming os.Open +
// io.Copy path sha256File uses. The payload deliberately crosses the
// 64 KiB hash-block and copy-buffer boundaries.
func TestSha256FileStreaming_Differential(t *testing.T) {
	payload := make([]byte, 0, 300*1024)
	for i := range 300 * 1024 {
		payload = append(payload, byte(i%251))
	}
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	require.NoError(t, os.WriteFile(path, payload, 0o644))

	want := sha256.Sum256(payload)
	got, err := sha256File(path)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(want[:]), got, "streaming digest matches the one-shot oracle")

	// The empty file, the other boundary of the copy loop.
	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	wantEmpty := sha256.Sum256(nil)
	gotEmpty, err := sha256File(empty)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(wantEmpty[:]), gotEmpty)
}

// oracle: hand-built expected values — the checksums file is written by
// hand in goreleaser's "<hex>  <asset>" shape, its digests computed with the
// stdlib one-shot Sum256, and lookupChecksum must pick the right row out of
// a multi-asset list, including an uppercase-hex row (Install compares with
// EqualFold).
func TestLookupChecksumMultiAsset_Differential(t *testing.T) {
	dir := t.TempDir()
	linuxAsset := "a1_2.0.0_linux_amd64.tar.gz"
	darwinAsset := "a1_2.0.0_darwin_arm64.tar.gz"

	linuxBytes := []byte("linux archive payload")
	darwinBytes := []byte("darwin archive payload")
	linuxSum := sha256.Sum256(linuxBytes)
	darwinSum := sha256.Sum256(darwinBytes)

	sums := filepath.Join(dir, "checksums_2.0.0.txt")
	require.NoError(t, os.WriteFile(sums, []byte(
		"# generated by goreleaser\n"+
			strings.ToUpper(hex.EncodeToString(darwinSum[:]))+"  "+darwinAsset+"\n"+
			hex.EncodeToString(linuxSum[:])+"  "+linuxAsset+"\n"+
			"deadbeef  a1_2.0.0_windows_amd64.zip\n"), 0o644))

	linuxPath := filepath.Join(dir, linuxAsset)
	require.NoError(t, os.WriteFile(linuxPath, linuxBytes, 0o644))

	listed, err := lookupChecksum(sums, linuxAsset)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(linuxSum[:]), listed, "the right row for the right asset")

	got, err := sha256File(linuxPath)
	require.NoError(t, err)
	assert.Equal(t, listed, got, "the pipeline verifies a genuine download end to end")

	// Uppercase hex in the file must still verify: Install's EqualFold.
	upperListed, err := lookupChecksum(sums, darwinAsset)
	require.NoError(t, err)
	assert.Equal(t, strings.ToUpper(hex.EncodeToString(darwinSum[:])), upperListed)
	assert.True(t, strings.EqualFold(hex.EncodeToString(darwinSum[:]), upperListed),
		"the case-insensitive comparison Install performs")
}
