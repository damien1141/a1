package bw

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: stdlib strings.NewReplacer equivalence — the escape table as a
// single replacement pass must match the hand-rolled rune loop for every
// input, randomized and edge-case.
func TestJSStringVsReplacerReference_Differential(t *testing.T) {
	ref := strings.NewReplacer(
		"'", `\'`,
		"\\", `\\`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)

	fixed := []string{
		"",
		"plain",
		"it's",
		"back\\slash",
		"line\nbreak",
		"carriage\rreturn",
		"tab\tsep",
		"all ' \\ \n \r \t",
		"double \" quote stays raw",
		"unicode 日本語 stays raw",
		"'\\'",
		"\n\n\n",
	}
	for _, s := range fixed {
		assert.Equal(t, "'"+ref.Replace(s)+"'", JSString(s), "input=%q", s)
	}

	alphabet := []string{"a", "Z", "7", "'", "\\", "\n", "\r", "\t", "\"", " ", "日", "😀", "é"}
	rng := rand.New(rand.NewSource(17))
	for i := range 300 {
		var b strings.Builder
		for n := rng.Intn(16); n > 0; n-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := b.String()
		assert.Equal(t, "'"+ref.Replace(s)+"'", JSString(s), "case %d: input=%q", i, s)
	}
}

// oracle: hand-written golden literals — every combination of challenge
// field presence (2^4 = 16) renders to exactly this text, URL beats Page for
// the location, and only the FIRST challenge of a list is reported.
func TestHandoffReasonGoldenMatrix_Differential(t *testing.T) {
	const (
		page     = "login screen"
		provider = "cloudflare"
		url      = "https://shop.example.com/checkout"
		advice   = "solve before retrying"
	)

	cases := []struct {
		page, provider, url, advice string
		want                        string
	}{
		{"", "", "", "", "bot challenge"},
		{page, "", "", "", "bot challenge at login screen"},
		{"", provider, "", "", "bot challenge (cloudflare)"},
		{"", "", url, "", "bot challenge at https://shop.example.com/checkout"},
		{"", "", "", advice, "bot challenge: solve before retrying"},
		{page, provider, "", "", "bot challenge (cloudflare) at login screen"},
		{page, "", "", advice, "bot challenge at login screen: solve before retrying"},
		{page, "", url, "", "bot challenge at https://shop.example.com/checkout"},
		{page, "", url, advice, "bot challenge at https://shop.example.com/checkout: solve before retrying"},
		{"", provider, url, "", "bot challenge (cloudflare) at https://shop.example.com/checkout"},
		{"", provider, "", advice, "bot challenge (cloudflare): solve before retrying"},
		{"", provider, url, advice, "bot challenge (cloudflare) at https://shop.example.com/checkout: solve before retrying"},
		{page, provider, "", advice, "bot challenge (cloudflare) at login screen: solve before retrying"},
		{page, provider, url, "", "bot challenge (cloudflare) at https://shop.example.com/checkout"},
		{page, "", url, advice, "bot challenge at https://shop.example.com/checkout: solve before retrying"},
		{page, provider, url, advice, "bot challenge (cloudflare) at https://shop.example.com/checkout: solve before retrying"},
	}
	for i, tc := range cases {
		env := Envelope{Challenges: []Challenge{{
			Page:     tc.page,
			Provider: tc.provider,
			URL:      tc.url,
			Advice:   tc.advice,
		}}}
		assert.Equal(t, tc.want, env.HandoffReason(), "matrix case %d", i)
	}

	// Only the first challenge is the handoff reason.
	first := Envelope{Challenges: []Challenge{
		{Provider: "cloudflare", URL: "https://a.example/"},
		{Provider: "datadome", URL: "https://b.example/"},
	}}
	assert.Equal(t, "bot challenge (cloudflare) at https://a.example/", first.HandoffReason())
}

// oracle: hand-written golden argv literals — the exact betterwright command
// line for lane/profile/headed combinations, including lane defaulting (""
// and blanks → a1) and profile normalization ("" and "default" → no flag).
func TestRunLaneArgvGolden_Differential(t *testing.T) {
	orig := Resolve
	Resolve = func() (string, error) { return "/usr/bin/fake-bw", nil }
	t.Cleanup(func() { Resolve = orig })
	ResetSpawnTracking()
	t.Cleanup(ResetSpawnTracking)

	var got []string
	fakeDaemon(t, func(_ string, args []string) ([]byte, error) {
		got = args
		return []byte(`{"ok":true,"result":true}`), nil
	})

	scenarios := []struct {
		name string
		run  func() error
		want []string
	}{
		{
			name: "background lane, named profile, headed",
			run:  func() error { _, err := RunLane(t.Context(), BackgroundSession, "work", true, "return 1"); return err },
			want: []string{"run", "--session", "a1-bg", "--profile", "work", "--headed", "-c", "return 1"},
		},
		{
			name: "blank lane and profile default",
			run:  func() error { _, err := RunLane(t.Context(), "", "", false, "return 2"); return err },
			want: []string{"run", "--session", "a1", "-c", "return 2"},
		},
		{
			name: "whitespace lane and literal default profile normalize",
			run:  func() error { _, err := RunLane(t.Context(), "  ", "default", false, "return 3"); return err },
			want: []string{"run", "--session", "a1", "-c", "return 3"},
		},
		{
			name: "interactive Run keeps the a1 lane",
			run:  func() error { _, err := Run(t.Context(), "work", false, "return 4"); return err },
			want: []string{"run", "--session", "a1", "--profile", "work", "-c", "return 4"},
		},
		{
			name: "close one session of a named profile",
			run:  func() error { return Close(t.Context(), false, "work") },
			want: []string{"close", "--session", "a1", "--profile", "work"},
		},
		{
			name: "close all",
			run:  func() error { return Close(t.Context(), true, "") },
			want: []string{"close", "--all"},
		},
	}
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.run())
			assert.Equal(t, tc.want, got)
		})
	}
}
