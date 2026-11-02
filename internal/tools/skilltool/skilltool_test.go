package skilltool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/skillstats"

	"github.com/damien1141/a1/internal/vault"
)

func writeSkill(t *testing.T, root, name, triggers string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "---\nname: " + name + "\ndescription: " + name + " skill\nmetadata:\n  triggers: " + triggers + "\n---\n\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
}

func runListSkills(t *testing.T, skillPath string) string {
	t.Helper()
	tool := ListSkillsTool(skillPath)
	res, err := tool.Run(t.Context(), json.RawMessage("{}"))
	require.NoError(t, err)
	return res.Content
}

func TestOrderCatalogWithoutVaultKeepsAuthoredOrder(t *testing.T) {
	session.SetDefaultVault(nil)
	root := t.TempDir()
	writeSkill(t, root, "alpha", "a1")
	writeSkill(t, root, "beta", "b1")
	writeSkill(t, root, "gamma", "c1")

	catalog := runListSkills(t, root)
	assert.Less(t, indexOf(catalog, "### alpha"), indexOf(catalog, "### beta"))
	assert.Less(t, indexOf(catalog, "### beta"), indexOf(catalog, "### gamma"))
	assert.NotContains(t, catalog, "recently unhelpful")
}

func TestOrderCatalogDemotesStrugglingSkill(t *testing.T) {
	root := t.TempDir()
	v, err := vault.Open(root)
	require.NoError(t, err)
	t.Cleanup(func() { v.Close() })
	session.SetDefaultVault(v)
	t.Cleanup(func() { session.SetDefaultVault(nil) })
	db := v.DB()

	writeSkill(t, root, "alpha", "a1")
	writeSkill(t, root, "beta", "b1")
	writeSkill(t, root, "gamma", "c1")

	// beta: 6 picks, 6 losses → (0+1)/(6+2) = 0.125 < 0.35 → demoted.
	for range 6 {
		require.NoError(t, skillstats.RecordPick(db, "beta"))
		require.NoError(t, skillstats.RecordOutcome(db, "beta", false))
	}
	// alpha: picked often but winning — stays at the top, no annotation.
	for range 6 {
		require.NoError(t, skillstats.RecordPick(db, "alpha"))
		require.NoError(t, skillstats.RecordOutcome(db, "alpha", true))
	}
	// gamma: struggling but only 2 picks — not enough evidence to demote.
	for range 2 {
		require.NoError(t, skillstats.RecordPick(db, "gamma"))
		require.NoError(t, skillstats.RecordOutcome(db, "gamma", false))
	}

	catalog := runListSkills(t, root)
	alphaAt := indexOf(catalog, "### alpha")
	betaAt := indexOf(catalog, "### beta (recently unhelpful)")
	gammaAt := indexOf(catalog, "### gamma")
	require.True(t, alphaAt >= 0, "alpha present: %s", catalog)
	require.True(t, betaAt >= 0, "beta demoted and annotated: %s", catalog)
	require.True(t, gammaAt >= 0, "gamma present: %s", catalog)

	assert.Less(t, alphaAt, betaAt, "healthy skill renders before the demoted one")
	assert.Less(t, gammaAt, betaAt, "low-evidence skill keeps its authored slot")
	assert.NotContains(t, catalog, "### alpha (recently unhelpful)", "winners are never annotated")
}

func TestOrderCatalogEmptyStatsKeepsOrder(t *testing.T) {
	root := t.TempDir()
	v, err := vault.Open(root)
	require.NoError(t, err)
	t.Cleanup(func() { v.Close() })
	session.SetDefaultVault(v)
	t.Cleanup(func() { session.SetDefaultVault(nil) })

	writeSkill(t, root, "alpha", "a1")
	catalog := runListSkills(t, root)
	assert.Contains(t, catalog, "### alpha")
	assert.NotContains(t, catalog, "recently unhelpful")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
