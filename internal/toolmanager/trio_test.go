package toolmanager

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/util/githubrelease"
)

// ---------- differential ----------

// refFindReleaseAsset re-derives the asset pick with a naive nested scan:
// candidate order is the priority, assets are scanned linearly per candidate.
// The implementation builds a name→asset map instead; the two must agree on
// every corpus where asset names are unique (GitHub release assets are).
func refFindReleaseAsset(assets []githubrelease.Asset, candidates []string) (githubrelease.Asset, bool) {
	for _, want := range candidates {
		for _, a := range assets {
			if a.Name == want {
				return a, true
			}
		}
	}
	return githubrelease.Asset{}, false
}

// oracle: naive nested-loop reference (above) over randomized asset lists and
// candidate orders, plus the empty edge cases.
func TestFindReleaseAssetVsNaiveScan_Differential(t *testing.T) {
	namePool := []string{
		"fd-v10.4.2-x86_64-unknown-linux-musl.tar.gz",
		"fd-v10.4.2-x86_64-unknown-linux-gnu.tar.gz",
		"ripgrep-15.2.0-aarch64-apple-darwin.tar.gz",
		"checksums.txt",
		"fd_10.4.2_amd64.deb",
		"README.md",
	}
	urlPool := []string{"https://example.com/a", "", "https://example.com/b", "https://example.com/c"}

	rng := rand.New(rand.NewSource(3))
	for i := range 150 {
		// Unique asset names, shuffled, with random (sometimes empty) URLs.
		perm := rng.Perm(len(namePool))
		assets := make([]githubrelease.Asset, 0, len(namePool))
		for _, idx := range perm {
			if rng.Intn(4) == 0 {
				continue // randomly drop some assets
			}
			assets = append(assets, githubrelease.Asset{
				Name:               namePool[idx],
				BrowserDownloadURL: urlPool[rng.Intn(len(urlPool))],
			})
		}
		// Candidate list: a shuffled subset of the pool plus unknown names.
		var candidates []string
		for _, idx := range rng.Perm(len(namePool)) {
			if rng.Intn(2) == 0 {
				candidates = append(candidates, namePool[idx])
			}
		}
		candidates = append(candidates, "does-not-exist.bin")

		wantAsset, wantOK := refFindReleaseAsset(assets, candidates)
		gotAsset, gotOK := findReleaseAsset(assets, candidates)
		assert.Equal(t, wantOK, gotOK, "case %d: assets=%v candidates=%v", i, assets, candidates)
		if wantOK {
			assert.Equal(t, wantAsset, gotAsset, "case %d: assets=%v candidates=%v", i, assets, candidates)
		}
	}

	// Edge cases.
	for _, tc := range []struct {
		assets     []githubrelease.Asset
		candidates []string
	}{
		{nil, nil},
		{nil, []string{"x"}},
		{[]githubrelease.Asset{{Name: "x"}}, nil},
		{[]githubrelease.Asset{{Name: "x"}}, []string{"x"}},
	} {
		wantAsset, wantOK := refFindReleaseAsset(tc.assets, tc.candidates)
		gotAsset, gotOK := findReleaseAsset(tc.assets, tc.candidates)
		assert.Equal(t, wantOK, gotOK, "assets=%v candidates=%v", tc.assets, tc.candidates)
		if wantOK {
			assert.Equal(t, wantAsset, gotAsset, "assets=%v candidates=%v", tc.assets, tc.candidates)
		}
	}
}

// refTagVersion strips a single leading v/V with a regexp — independent of
// githubrelease.TagVersion's byte slicing.
var refTagVersionRe = regexp.MustCompile(`^[vV]`)

func refTagVersion(tag string) string {
	return refTagVersionRe.ReplaceAllString(tag, "")
}

// refAssetNames re-derives the candidate asset filenames from a hand-written
// table of the public release naming scheme (owner repos publish
// <tool><prefix><version>-<arch><suffix>), not from the package's archMap.
func refAssetNames(tool, version, platform, arch string) []string {
	tools := map[string][2]string{
		// toolName, version prefix
		"fd": {"fd", "v"},
		"rg": {"ripgrep", ""},
	}
	suffixes := map[string][]string{
		PlatformDarwin: {"-apple-darwin.tar.gz"},
		PlatformLinux:  {"-unknown-linux-musl.tar.gz", "-unknown-linux-gnu.tar.gz"},
		PlatformWin32:  {"-pc-windows-msvc.zip"},
	}
	archNames := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}

	spec, known := tools[tool]
	sufs, knownPlatform := suffixes[platform]
	archName, knownArch := archNames[arch]
	if !known || !knownPlatform || !knownArch {
		return nil
	}
	full := spec[1] + version
	out := make([]string, 0, len(sufs))
	for _, suf := range sufs {
		out = append(out, spec[0]+"-"+full+"-"+archName+suf)
	}
	return out
}

// refSelectCompatibleAsset re-derives the whole selection with naive loops:
// walk releases in order, build candidates from the reference table, scan the
// release's assets linearly. The first hit with a download URL wins; a hit
// without a URL is an error; no hit at all is an error.
func refSelectCompatibleAsset(
	tool string,
	releases []githubrelease.Release,
	platform, arch string,
) (string, bool) {
	if len(refAssetNames(tool, "", platform, arch)) == 0 {
		return "", false // unsupported platform/arch
	}
	for _, rel := range releases {
		for _, cand := range refAssetNames(tool, refTagVersion(rel.TagName), platform, arch) {
			for _, a := range rel.Assets {
				if a.Name != cand {
					continue
				}
				if a.BrowserDownloadURL == "" {
					return "", false // found but unusable
				}
				return a.Name, true
			}
		}
	}
	return "", false
}

// oracle: naive reference implementation (above) with its own naming table —
// randomized release lists (v-prefixed and bare tags, decoy assets, missing
// URLs) plus fixed fallback scenarios must pick the same asset (or fail the
// same way) as the map-based implementation.
func TestSelectCompatibleAssetVsNaiveReference_Differential(t *testing.T) {
	// Fixed scenarios: priority within a release, fallback to older releases,
	// decoys only, and asset present but URL-less.
	fixed := []struct {
		name     string
		tool     string
		releases []githubrelease.Release
		platform string
		arch     string
	}{
		{
			name: "musl preferred over gnu in same release",
			tool: "rg",
			releases: []githubrelease.Release{{TagName: "15.2.0", Assets: []githubrelease.Asset{
				{Name: "ripgrep-15.2.0-x86_64-unknown-linux-gnu.tar.gz", BrowserDownloadURL: "u-gnu"},
				{Name: "ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz", BrowserDownloadURL: "u-musl"},
			}}},
			platform: PlatformLinux,
			arch:     ArchAMD64,
		},
		{
			name: "fallback to older release",
			tool: "fd",
			releases: []githubrelease.Release{
				{TagName: "v10.4.2", Assets: []githubrelease.Asset{{Name: "fd-v10.4.2-aarch64-apple-darwin.tar.gz", BrowserDownloadURL: "u-arm"}}},
				{TagName: "v10.3.0", Assets: []githubrelease.Asset{{Name: "fd-v10.3.0-x86_64-apple-darwin.tar.gz", BrowserDownloadURL: "u-old"}}},
			},
			platform: PlatformDarwin,
			arch:     ArchAMD64,
		},
		{
			name: "decoys only",
			tool: "fd",
			releases: []githubrelease.Release{
				{TagName: "v10.4.2", Assets: []githubrelease.Asset{{Name: "checksums.txt", BrowserDownloadURL: "u"}}},
			},
			platform: PlatformDarwin,
			arch:     ArchAMD64,
		},
		{
			name: "asset without URL",
			tool: "rg",
			releases: []githubrelease.Release{
				{TagName: "15.2.0", Assets: []githubrelease.Asset{{Name: "ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz"}}},
			},
			platform: PlatformLinux,
			arch:     ArchAMD64,
		},
		{
			name:     "unsupported arch",
			tool:     "rg",
			releases: []githubrelease.Release{{TagName: "15.2.0", Assets: []githubrelease.Asset{{Name: "ripgrep-15.2.0-x86.tar.gz", BrowserDownloadURL: "u"}}}},
			platform: PlatformLinux,
			arch:     ArchX86,
		},
		{
			name:     "windows zip",
			tool:     "fd",
			releases: []githubrelease.Release{{TagName: "v10.4.2", Assets: []githubrelease.Asset{{Name: "fd-v10.4.2-aarch64-pc-windows-msvc.zip", BrowserDownloadURL: "u"}}}},
			platform: PlatformWin32,
			arch:     ArchARM64,
		},
	}

	runCase := func(t *testing.T, tool string, releases []githubrelease.Release, platform, arch string) {
		t.Helper()
		wantName, wantOK := refSelectCompatibleAsset(tool, releases, platform, arch)
		asset, err := selectCompatibleAsset(Tools[tool], releases, platform, arch)
		if !wantOK {
			require.Error(t, err, "reference expects failure for %s %s/%s", tool, platform, arch)
			return
		}
		require.NoError(t, err, "reference expects asset %q for %s %s/%s", wantName, tool, platform, arch)
		assert.Equal(t, wantName, asset.Name, "selected asset for %s %s/%s", tool, platform, arch)
	}

	for _, tc := range fixed {
		t.Run(tc.name, func(t *testing.T) {
			runCase(t, tc.tool, tc.releases, tc.platform, tc.arch)
		})
	}

	// Randomized release lists: tags with and without the v prefix, correct
	// assets mixed with decoys, sometimes-empty URLs, shuffled asset order.
	rng := rand.New(rand.NewSource(5))
	decoys := []string{"checksums.txt", "fd_10.4.0_amd64.deb", "source.tar.gz", "ripgrep-15.2.0-arm-unknown-linux-gnueabihf.tar.gz"}
	platforms := []string{PlatformDarwin, PlatformLinux, PlatformWin32}
	archs := []string{ArchAMD64, ArchARM64, ArchX86}

	for i := range 200 {
		tool := []string{"fd", "rg"}[rng.Intn(2)]
		platform := platforms[rng.Intn(len(platforms))]
		arch := archs[rng.Intn(len(archs))]

		var releases []githubrelease.Release
		for n := rng.Intn(4); n > 0; n-- {
			version := fmt.Sprintf("%d.%d.%d", rng.Intn(20), rng.Intn(10), rng.Intn(10))
			tag := version
			if rng.Intn(2) == 0 {
				tag = "v" + version
			}
			var assets []githubrelease.Asset
			correct := refAssetNames(tool, version, platform, arch)
			for _, name := range correct {
				if rng.Intn(3) == 0 {
					continue // sometimes the correct asset is absent
				}
				assets = append(assets, githubrelease.Asset{
					Name:               name,
					BrowserDownloadURL: fmt.Sprintf("https://example.com/%s/%s", tag, name),
				})
			}
			for _, d := range decoys {
				if rng.Intn(3) == 0 {
					assets = append(assets, githubrelease.Asset{Name: d, BrowserDownloadURL: "https://example.com/" + d})
				}
			}
			if len(assets) > 0 && rng.Intn(5) == 0 {
				// Empty URL on a random asset (possibly the correct one).
				assets[rng.Intn(len(assets))].BrowserDownloadURL = ""
			}
			rng.Shuffle(len(assets), func(a, b int) { assets[a], assets[b] = assets[b], assets[a] })
			releases = append(releases, githubrelease.Release{TagName: tag, Assets: assets})
		}

		wantName, wantOK := refSelectCompatibleAsset(tool, releases, platform, arch)
		asset, err := selectCompatibleAsset(Tools[tool], releases, platform, arch)
		if !wantOK {
			assert.Error(t, err, "case %d: %s %s/%s releases=%+v", i, tool, platform, arch, releases)
			continue
		}
		if !assert.NoError(t, err, "case %d: %s %s/%s releases=%+v", i, tool, platform, arch, releases) {
			continue
		}
		assert.Equal(t, wantName, asset.Name, "case %d: %s %s/%s", i, tool, platform, arch)
	}
}

// refFindBinary re-derives the binary search with os.ReadDir recursion: all
// matches are collected depth-first (sorted entries per directory, the same
// order filepath.WalkDir visits), and the first one wins. The implementation
// short-circuits with filepath.SkipDir after its first hit; the two must
// agree on which file that is.
func refFindBinary(root, name string) (string, error) {
	var matches []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if e.Name() == name {
				matches = append(matches, p)
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0], nil
}

// oracle: naive os.ReadDir recursion reference (above) over a fixture tree
// where the target base name appears exactly once (the real contract: one
// binary per extracted archive) among look-alikes at several depths, plus the
// missing-root / missing-file edges.
func TestFindBinaryRecursivelyVsReadDirReference_Differential(t *testing.T) {
	root := t.TempDir()
	tree := map[string]string{
		"zz.txt":      "root file",
		"m/other.txt": "filler",
		"a/notrg.txt": "lookalike name",
		"a/deep/rg":   "the only match, deep in a",
		"a/deep/xrg":  "suffix lookalike",
		"a/rgo.txt":   "prefix lookalike",
		"b/c/d/other": "deep filler",
		"m/sub/rgo":   "near-miss under m",
		"rgo":         "near-miss at root",
	}
	for rel, content := range tree {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755), "fixture dir for %s", rel)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644), "fixture file %s", rel)
	}

	// WalkDir visits root entries in lexical order; the single "rg" must be
	// found regardless of depth.
	got, err := findBinaryRecursively(root, "rg")
	require.NoError(t, err)
	want, refErr := refFindBinary(root, "rg")
	require.NoError(t, refErr)
	assert.Equal(t, want, got, "the only match on the walk must be the reference's match")
	assert.Equal(t, filepath.Join(root, "a", "deep", "rg"), got)

	// No match anywhere: empty result, no error.
	got, err = findBinaryRecursively(root, "absent.bin")
	require.NoError(t, err)
	want, refErr = refFindBinary(root, "absent.bin")
	require.NoError(t, refErr)
	assert.Equal(t, want, got)
	assert.Empty(t, got)

	// Missing root: both fail.
	_, err = findBinaryRecursively(filepath.Join(root, "nope"), "rg")
	assert.Error(t, err)
	_, refErr = refFindBinary(filepath.Join(root, "nope"), "rg")
	assert.Error(t, refErr)

	// Randomized single-match trees: N nested dirs, exactly one of them (or
	// none) holds the target, so the match position is not hand-picked.
	rng := rand.New(rand.NewSource(9))
	for i := range 40 {
		rnd := t.TempDir()
		var dirs []string
		for d := rng.Intn(6); d > 0; d-- {
			dir := filepath.Join(rnd, fmt.Sprintf("d%d", rng.Intn(4)), fmt.Sprintf("e%d", rng.Intn(4)))
			require.NoError(t, os.MkdirAll(dir, 0o755))
			dirs = append(dirs, dir)
			if rng.Intn(2) == 0 {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "targetx"), []byte("x"), 0o644))
			}
		}
		if len(dirs) > 0 {
			hit := dirs[rng.Intn(len(dirs))]
			require.NoError(t, os.WriteFile(filepath.Join(hit, "target"), []byte("x"), 0o644))
		}
		sort.Strings(dirs)
		got, err := findBinaryRecursively(rnd, "target")
		require.NoError(t, err, "case %d", i)
		want, err := refFindBinary(rnd, "target")
		require.NoError(t, err, "case %d", i)
		assert.Equal(t, want, got, "case %d", i)
	}
}
