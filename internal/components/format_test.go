package components_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func TestFormatTokens(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		999:     "999",
		1200:    "1.2k",
		15000:   "15k",
		1500000: "1.5M",
	}
	for n, want := range cases {
		require.Equal(t, want, components.FormatTokens(n))
	}
	require.Equal(t, "0", components.FormatTokens(-5), "negative counts clamp to zero")
}
