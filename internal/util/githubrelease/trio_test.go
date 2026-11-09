package githubrelease

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- negative ----------

func TestFetchTagEmptyTag_Negative(t *testing.T) {
	_, err := FetchTag(t.Context(), "owner/repo", "   ")
	require.Error(t, err)
	assert.EqualError(t, err, "empty release tag for owner/repo")
}

// TestFetchReleaseHTTPStatus_Negative drives the unexported fetchRelease
// against a local httptest origin so every status class can be pinned.
func TestFetchReleaseHTTPStatus_Negative(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{
			name:    "404 maps to the caller's not-found message",
			status:  http.StatusNotFound,
			wantErr: "no published release for owner/repo (publish one with ./scripts/bump.sh vX.Y.Z && git push --follow-tags)",
		},
		{
			name:    "500 surfaces the raw status",
			status:  http.StatusInternalServerError,
			wantErr: "github api owner/repo: status 500",
		},
		{
			name:    "rate limit surfaces the raw status, body is dropped",
			status:  http.StatusForbidden,
			body:    `{"message":"rate limit exceeded"}`,
			wantErr: "github api owner/repo: status 403",
		},
		{
			name:    "redirect-ish 302 is not ok",
			status:  http.StatusFound,
			wantErr: "github api owner/repo: status 302",
		},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			if tc.body != "" {
				_, _ = w.Write([]byte(tc.body))
			}
		}))
		rel, err := fetchRelease(t.Context(), "owner/repo", srv.URL+"/releases/latest", tc.wantErr)
		srv.Close()
		require.Error(t, err, "%s", tc.name)
		assert.EqualError(t, err, tc.wantErr, "%s", tc.name)
		assert.Equal(t, Release{}, rel, "%s: no partial release on failure", tc.name)
	}
}

func TestFetchReleaseMalformedBody_Negative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<<definitely not json>>"))
	}))
	defer srv.Close()

	_, err := fetchRelease(t.Context(), "owner/repo", srv.URL, "unused")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `decode release response from "owner/repo"`)
	assert.Contains(t, err.Error(), "invalid character", "the JSON decoder's reason is kept")

	// A literal null body decodes to the zero Release with no error — the
	// hostile-payload shape callers must be ready for.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("null"))
	}))
	defer srv2.Close()
	rel, err := fetchRelease(t.Context(), "owner/repo", srv2.URL, "unused")
	require.NoError(t, err)
	assert.Equal(t, Release{}, rel, "null decodes to the zero value, not an error")
}

func TestDownloadFileFailures_Negative(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.bin")

	// 404 / 500 carry the URL and the status.
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		err := DownloadFile(t.Context(), srv.URL+"/a1_2.0.0_linux_amd64.tar.gz", dest)
		srv.Close()
		require.Error(t, err, "HTTP %d", status)
		assert.Contains(t, err.Error(), "HTTP ", "status 404/500 is reported")
	}

	// Truncated body: Content-Length promises more than the handler writes,
	// so the copy fails instead of saving a short file silently.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1024")
		_, _ = w.Write([]byte("short"))
	}))
	err := DownloadFile(t.Context(), srv.URL+"/a1.tar.gz", dest)
	srv.Close()
	require.Error(t, err, "a truncated download must fail, not half-land")
	assert.Contains(t, err.Error(), "write downloaded content")
	assert.Contains(t, err.Error(), "unexpected EOF")

	// A URL the request builder rejects outright.
	err = DownloadFile(t.Context(), "http://bad host name/x", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create download request for")

	// A canceled context aborts before anything is written.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceledDest := filepath.Join(t.TempDir(), "canceled.bin")
	err = DownloadFile(ctx, "http://127.0.0.1:1/x", canceledDest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download ")

	_, statErr := os.Stat(canceledDest)
	assert.True(t, os.IsNotExist(statErr), "a request that never left leaves no file")

	// The truncated download, by contrast, leaves its partial bytes on
	// disk — DownloadFile does not clean up after a failed copy. Pin the
	// actual behavior (the updater's checksum step is what catches it).
	partial, err := os.ReadFile(dest)
	require.NoError(t, err, "partial artifact from the truncated download")
	assert.Equal(t, "short", string(partial), "only the bytes that arrived are on disk")
}

// ---------- differential ----------

// releaseFixturePayloads are hostile-but-valid GitHub API response shapes:
// missing optional keys, explicit false flags, null asset lists, unknown
// extra fields, and unicode tags.
var releaseFixturePayloads = []string{
	`[
                {"tag_name": "v3.0.0-rc.1", "prerelease": true, "assets": []},
                {"tag_name": "v2.0.0", "draft": true},
                {"tag_name": "v1.0.0", "assets": [
                        {"name": "a1_1.0.0_linux_amd64.tar.gz",
                         "browser_download_url": "https://example.com/download/a1_1.0.0_linux_amd64.tar.gz",
                         "unknown_field": {"nested": true}}
                ]},
                {"tag_name": "v0.9.0", "html_url": "https://example.com/v0.9.0", "assets": null},
                {"tag_name": "v0.8.0", "draft": false, "prerelease": false, "extra": []}
        ]`,
	`[]`,
	`[{"tag_name": "ünïcode-täg", "prerelease": null, "draft": null}]`,
	`[{"tag_name": "dupe"}, {"tag_name": "dupe"}]`,
}

// oracle: naive encoding/json map-decode reference — the same payloads
// decoded as []map[string]any with the stable filter (not draft, not
// prerelease) and tag extraction written out longhand, independent of the
// typed Release struct decode under test.
func TestDecodeReleasesStableFilter_Differential(t *testing.T) {
	for _, payload := range releaseFixturePayloads {
		releases, err := decodeReleases(strings.NewReader(payload))
		require.NoError(t, err)

		var raw []map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &raw))

		var wantTags []string
		for _, m := range raw {
			if truthy(m["draft"]) || truthy(m["prerelease"]) {
				continue
			}
			tag, _ := m["tag_name"].(string)
			wantTags = append(wantTags, tag)
		}

		got := stableReleases(releases)
		gotTags := make([]string, 0, len(got))
		for _, r := range got {
			gotTags = append(gotTags, r.TagName)
		}
		assert.Equal(t, normalizeTags(wantTags), normalizeTags(gotTags), "payload %s", payload)
	}
}

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}

// normalizeTags folds empty-but-non-nil and nil slices together so the
// []-payload compares cleanly.
func normalizeTags(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// oracle: naive encoding/json map-decode reference — every field of the
// typed Release decode (tag, url, flags, asset names and URLs) is re-derived
// from a generic map[string]any with longhand type switches.
func TestDecodeReleaseFields_Differential(t *testing.T) {
	payloads := []string{
		`{"tag_name": "v2.1.0", "html_url": "https://example.com/tag/v2.1.0",
                  "draft": true, "prerelease": false,
                  "assets": [
                    {"name": "a1_2.1.0_linux_amd64.tar.gz", "browser_download_url": "https://example.com/dl/1"},
                    {"name": "a1_2.1.0_darwin_arm64.tar.gz", "browser_download_url": "https://example.com/dl/2"},
                    {"name": "no-url-asset.bin"}
                  ]}`,
		`{"tag_name": "v2.0.0"}`,
		`{"tag_name": "v1.9.0", "assets": [], "html_url": null, "draft": null, "prerelease": null}`,
		`{"unknown_top": 1, "tag_name": "v1.8.0", "assets": [{"unknown_asset_field": "x", "name": "only-name"}]}`,
	}
	for _, payload := range payloads {
		release, err := decodeRelease(strings.NewReader(payload))
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &raw))

		assert.Equal(t, stringField(raw, "tag_name"), release.TagName, "payload %s", payload)
		assert.Equal(t, stringField(raw, "html_url"), release.HTMLURL, "payload %s", payload)
		assert.Equal(t, truthy(raw["draft"]), release.Draft, "payload %s", payload)
		assert.Equal(t, truthy(raw["prerelease"]), release.Prerelease, "payload %s", payload)

		var wantAssets []Asset
		if list, ok := raw["assets"].([]any); ok {
			for _, a := range list {
				m, _ := a.(map[string]any)
				wantAssets = append(wantAssets, Asset{
					Name:               stringField(m, "name"),
					BrowserDownloadURL: stringField(m, "browser_download_url"),
				})
			}
		}
		if wantAssets == nil {
			wantAssets = []Asset{}
		}
		if release.Assets == nil {
			release.Assets = []Asset{}
		}
		assert.Equal(t, wantAssets, release.Assets, "payload %s", payload)
	}
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// oracle: hand-written golden literals — expected outputs computed by reading
// the documented rule (strip one leading v/V; rewrite /releases/tag/ to
// /releases/download/ once), never by calling the functions under test.
func TestTagAndBaseURLGoldens_Differential(t *testing.T) {
	tagCases := []struct{ in, want string }{
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v", ""}, // a lone prefix strips to nothing
		{"V", ""},
		{"version", "ersion"}, // only the first byte is considered
		{"vv1", "v1"},
		{"", ""},
	}
	for _, tc := range tagCases {
		assert.Equal(t, tc.want, TagVersion(tc.in), "TagVersion(%q)", tc.in)
	}

	urlCases := []struct{ in, want string }{
		{"https://github.com/o/r/releases/tag/v0.1.0", "https://github.com/o/r/releases/download/v0.1.0"},
		{"https://github.com/o/r/releases/tag/v0.1.0/", "https://github.com/o/r/releases/download/v0.1.0"}, // trailing slash trimmed first
		{"https://github.com/o/r/releases", "https://github.com/o/r/releases"},                             // no tag path: unchanged
		{"", ""},
	}
	for _, tc := range urlCases {
		assert.Equal(t, tc.want, DownloadBaseURL(tc.in), "DownloadBaseURL(%q)", tc.in)
	}
}
