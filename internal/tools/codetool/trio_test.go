package codetool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveField mimics encoding/json's case-insensitive struct-field fallback:
// an exact key wins, otherwise the first key that equal-folds matches. This
// keeps the map-based reference faithful to what the production struct
// decode accepts.
func naiveField(m map[string]any, key string) (any, bool) {
	v, ok := m[key]
	if ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// naiveCodeDetail re-derives DetailFromArgs through a map-based
// encoding/json decode — a different unmarshal path than the production
// struct decode — including the mode normalization, the path-over-text
// precedence, and the byte-wise 40-byte text truncation.
func naiveCodeDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	strField := func(key string) string {
		v, _ := naiveField(m, key)
		s, _ := v.(string)
		return s
	}
	mode := strings.ToLower(strings.TrimSpace(strField("mode")))
	if p := strings.TrimSpace(strField("path")); p != "" {
		return mode + " " + p
	}
	if txt := strings.TrimSpace(strField("text")); txt != "" {
		if len(txt) > 40 {
			txt = txt[:40] + "..."
		}
		return mode + " " + txt
	}
	return mode
}

// oracle: naive reference implementation in this test — a map-based
// encoding/json decode plus independently written normalization and
// truncation, compared over normal, edge, and hostile inputs including
// non-JSON payloads, which the detail helper must survive.
func TestDetailFromArgs_Differential(t *testing.T) {
	exactly40 := strings.Repeat("e", 40)
	over40 := strings.Repeat("o", 41)
	multibyte := strings.Repeat("日", 15) // 45 bytes, cut lands mid-rune

	cases := []struct {
		name  string
		input string
	}{
		{"mode and path", `{"mode":"api","path":"./internal/tools"}`},
		{"mode only", `{"mode":"deadcode"}`},
		{"mode is lowercased", `{"mode":"  API  ","path":"./x"}`},
		{"path is trimmed", `{"mode":"api","path":"  ./internal/tools  "}`},
		{"path beats text", `{"mode":"error","path":"./x","text":"undefined: foo"}`},
		{"mode and short text", `{"mode":"error","text":"undefined: foo"}`},
		{"text is trimmed before truncating", `{"mode":"stack","text":"   goroutine leak   "}`},
		{"text exactly 40 bytes", `{"mode":"error","text":"` + exactly40 + `"}`},
		{"text 41 bytes truncates", `{"mode":"error","text":"` + over40 + `"}`},
		{"multibyte text cuts on the byte", `{"mode":"error","text":"` + multibyte + `"}`},
		{"blank text falls back to mode", `{"mode":"error","text":"   "}`},
		{"blank path falls back to text", `{"mode":"errtrans","path":"","text":"ENOENT"}`},
		{"missing mode with path", `{"path":"./orphan"}`},
		{"empty object", `{}`},
		{"not json", `not json at all`},
		{"json array", `["error"]`},
		{"wrong field types", `{"mode":7,"path":true,"text":[]}`},
		{"null fields", `{"mode":null,"path":null,"text":null}`},
		{"uppercase keys still decode", `{"MODE":"error","PATH":"./x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveCodeDetail(json.RawMessage(tc.input)), CodeTool().DetailFromArgs(json.RawMessage(tc.input)))
		})
	}

	// The bytes that back the byte-wise cut stay in the detail line: a 45-byte
	// multibyte paste truncates to 40 bytes + "..." exactly, not 40 runes.
	t.Run("truncation is byte-wise", func(t *testing.T) {
		require.Greater(t, len(multibyte), 40)
		raw, err := json.Marshal(map[string]any{"mode": "error", "text": multibyte})
		require.NoError(t, err)
		got := CodeTool().DetailFromArgs(raw)
		assert.Len(t, got, len("error ")+40+len("..."))
		assert.True(t, strings.HasSuffix(got, "..."))
	})
}
