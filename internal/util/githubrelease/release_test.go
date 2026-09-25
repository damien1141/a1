package githubrelease

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTagVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag  string
		want string
	}{
		{tag: "v1.2.3", want: "1.2.3"},
		{tag: "V1.2.3", want: "1.2.3"},
		{tag: "1.2.3", want: "1.2.3"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, TagVersion(tt.tag), "TagVersion(%q)", tt.tag)
	}
}

func TestDownloadBaseURL(t *testing.T) {
	t.Parallel()
	in := "https://github.com/damien1141/a1/releases/tag/v0.1.0"
	want := "https://github.com/damien1141/a1/releases/download/v0.1.0"
	require.Equal(t, want, DownloadBaseURL(in))
}

func TestDecodeReleaseIncludesAssets(t *testing.T) {
	t.Parallel()
	response := `{
		"tag_name": "15.2.0",
		"html_url": "https://github.com/BurntSushi/ripgrep/releases/tag/15.2.0",
		"assets": [{
			"name": "ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz",
			"browser_download_url": "https://github.com/BurntSushi/ripgrep/releases/download/15.2.0/ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz"
		}]
	}`

	release, err := decodeRelease(strings.NewReader(response))
	require.NoError(t, err)
	require.Equal(t, "15.2.0", release.TagName, "TagName")
	require.Len(t, release.Assets, 1, "len(Assets)")
	asset := release.Assets[0]
	require.Equal(t, "ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz", asset.Name, "asset Name")
	require.NotEmpty(t, asset.BrowserDownloadURL, "asset BrowserDownloadURL is empty")
}

func TestStableReleasesFiltersDraftsAndPrereleases(t *testing.T) {
	t.Parallel()
	response := `[
		{"tag_name": "v3.0.0-rc.1", "prerelease": true},
		{"tag_name": "v2.0.0", "draft": true},
		{"tag_name": "v1.0.0", "assets": [{"name": "tool.tar.gz"}]}
	]`

	releases, err := decodeReleases(strings.NewReader(response))
	require.NoError(t, err)
	stable := stableReleases(releases)
	require.Len(t, stable, 1)
	require.Equal(t, "v1.0.0", stable[0].TagName)
}

func TestFetchRecentRejectsInvalidLimit(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{0, maxReleasesPerPage + 1} {
		if _, err := FetchRecent(t.Context(), "owner/repo", limit); err == nil {
			require.Failf(t, "unexpected success", "FetchRecent(limit=%d) unexpectedly succeeded", limit)
		}
	}
}
