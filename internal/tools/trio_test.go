package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// refLookup is the naive registry reference: a linear scan over the tool
// slice. The registry is a map; the two must agree on every probe.
func refLookup(ts []Tool, name string) (Tool, bool) {
	for _, t := range ts {
		if t.Definition.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// oracle: naive linear-scan reference (above) — for every registered tool
// name plus hostile probes (empty, missing, wrong case, padded), the map
// registry must return exactly what the scan returns, and misses must be
// clean misses.
func TestNewRegistryLookupVsNaiveScan_Differential(t *testing.T) {
	defs := DefaultTools()
	reg := NewRegistry(defs)

	probes := make([]string, 0, len(defs)+8)
	for _, t := range defs {
		probes = append(probes, t.Definition.Name)
	}
	probes = append(probes,
		"", "nope", "Bash", " bash", "bash ", "WRITE", "searchx", "ask-user",
	)

	for _, name := range probes {
		want, wantOK := refLookup(defs, name)
		got, gotOK := reg[name]

		assert.Equal(t, wantOK, gotOK, "probe %q: registry hit must match the scan", name)
		if !wantOK {
			continue
		}
		require.NotNil(t, got.Run, "probe %q: registered tool must carry a Run handler", name)
		assert.Equal(t, want.Definition.Name, got.Definition.Name, "probe %q", name)
		assert.Equal(t, want.Definition.Description, got.Definition.Description, "probe %q", name)
		assert.Equal(t, want.Definition.Params, got.Definition.Params, "probe %q", name)
	}

	// Read-only set indexes the same way.
	ro := ReadonlyTools()
	roReg := NewRegistry(ro)
	for _, name := range probes {
		want, wantOK := refLookup(ro, name)
		got, gotOK := roReg[name]
		assert.Equal(t, wantOK, gotOK, "readonly probe %q", name)
		if wantOK {
			assert.Equal(t, want.Definition.Name, got.Definition.Name, "readonly probe %q", name)
		}
	}
}

// oracle: naive set-difference reference — the read-only set must be exactly
// DefaultTools with the write-capable registrations ("write", "script")
// filtered out, in the same order.
func TestReadonlyToolsVsDefaultMinusWritable_Differential(t *testing.T) {
	writable := map[string]bool{"write": true, "script": true}

	want := make([]string, 0, len(DefaultTools()))
	for _, t := range DefaultTools() {
		if !writable[t.Definition.Name] {
			want = append(want, t.Definition.Name)
		}
	}

	got := make([]string, 0, len(ReadonlyTools()))
	for _, t := range ReadonlyTools() {
		got = append(got, t.Definition.Name)
	}

	assert.Equal(t, want, got, "ReadonlyTools must be DefaultTools minus write/script, order preserved")
}

// oracle: encoding/json equivalence — every DefaultTools definition must
// marshal to JSON that decodes back to the same name/description and a
// JSON-object parameters field, and Definitions() must preserve order.
func TestDefinitionsJSONMarshalVsStdlibDecode_Differential(t *testing.T) {
	defs := DefaultTools()

	for _, tool := range defs {
		name := tool.Definition.Name
		raw, err := json.Marshal(tool.Definition)
		require.NoError(t, err, "marshal definition of %q", name)

		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m), "marshal output of %q must be valid JSON", name)
		assert.Equal(t, name, m["name"], "json name field of %q", name)
		assert.Equal(t, tool.Definition.Description, m["description"], "json description field of %q", name)
		assert.NotContains(t, m, "Readable", "Readable must stay unserialized for %q", name)

		switch tool.Definition.Params {
		case nil:
			assert.Nil(t, m["parameters"], "nil Params must serialize to null for %q", name)
		default:
			params, ok := m["parameters"].(map[string]any)
			require.True(t, ok, "parameters of %q must be a JSON object, got %T", name, m["parameters"])
			assert.Equal(t, tool.Definition.Params.Type, params["type"], "parameters.type of %q", name)
			assert.NotEmpty(t, params["type"], "parameters.type of %q must be set", name)
		}
	}

	// Definitions() must be the same tools in the same order.
	names := make([]string, 0, len(defs))
	for _, t := range defs {
		names = append(names, t.Definition.Name)
	}
	extracted := Definitions(defs)
	require.Len(t, extracted, len(defs))
	for i, d := range extracted {
		assert.Equal(t, names[i], d.Name, "Definitions() order at %d", i)
		assert.Equal(t, defs[i].Definition, d, "Definitions() entry %d must equal the tool's definition", i)
	}
}
