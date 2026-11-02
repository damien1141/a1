package update_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/util/update"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.0", false},
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.0 (abc)", "v0.1.1", true},
	}
	for _, tc := range cases {
		if got := update.VersionLess(tc.a, tc.b); got != tc.want {
			require.Equal(t, tc.want, got, "VersionLess(%q, %q)", tc.a, tc.b)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	require.True(t, update.IsDevBuild("dev"), "expected dev build: dev")
	require.True(t, update.IsDevBuild(""), "expected dev build: empty")
	require.True(t, update.IsDevBuild("v0.0.0"), "expected dev build: v0.0.0")
	require.False(t, update.IsDevBuild("v0.1.0"), "v0.1.0 should not be dev")
}
