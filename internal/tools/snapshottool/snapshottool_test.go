package snapshottool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotTool_Definition(t *testing.T) {
	tool := SnapshotTool()
	assert.Equal(t, "snapshot", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "Explicit")
	// Not Readable: create/rollback/delete mutate the working tree and branch
	// refs, so a batch of snapshot calls must run sequentially.
	assert.False(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

// TestSnapshotTool_SchemaUsesLowercaseType guards against emitting JSON-schema
// keys with a capital T ("Type" instead of "type"), which models reject as an
// unknown field and silently ignore, leaving the parameter undefined.
func TestSnapshotTool_SchemaUsesLowercaseType(t *testing.T) {
	tool := SnapshotTool()
	require.NotNil(t, tool.Definition.Params)
	for _, name := range []string{"action", "branch"} {
		prop, ok := tool.Definition.Params.Properties[name]
		require.Truef(t, ok, "schema missing property %q", name)
		b, err := json.Marshal(prop)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"type":"string"`,
			"property %q must declare a lowercase \"type\" key, got %s", name, string(b))
	}
}
