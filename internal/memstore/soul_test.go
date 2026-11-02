package memstore

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSoulParams pins the SOUL block parse (Bet 06): a fenced block
// with §11 keys overrides the defaults; absent keys and absent blocks
// keep them — every pre-existing persona is byte-identical.
func TestParseSoulParams(t *testing.T) {
	fence := "```"

	// No block → defaults.
	p, ok := ParseSoulParams("You are a detective. Solve crimes.")
	assert.False(t, ok)
	assert.Equal(t, DefaultParams(), p)

	// Nostalgic persona: slow forgetting, low threshold.
	prompt := "You are Sherlock.\n\n" + fence + "soul\nd: 0.35\ntau: -1.5\nsigma: 1.4\nk: 10\n" + fence + "\n\nDeduce."
	p, ok = ParseSoulParams(prompt)
	require.True(t, ok)
	assert.Equal(t, 0.35, p.D)
	assert.Equal(t, -1.5, p.Tau)
	assert.Equal(t, 1.4, p.Sigma)
	assert.Equal(t, 10, p.K)
	// Untouched keys keep defaults.
	assert.Equal(t, DefaultParams().Beta, p.Beta)
	assert.Equal(t, DefaultParams().Gamma, p.Gamma)
	assert.Equal(t, DefaultParams().Diversity, p.Diversity)

	// Full key coverage.
	full := fence + "soul\nd: 0.8\ntau: 0.5\nbeta: 4\nlambda: 2\nw_kwd: 0.6\nsigma: 0.9\ngamma: 0.5\nmu: 0.7\nk: 4\nm: 20\nvec_top: 30\nfts_top: 40\ndiversity: 0.9\ntref: 1.5\n" + fence
	p, ok = ParseSoulParams(full)
	require.True(t, ok)
	assert.Equal(t, 0.8, p.D)
	assert.Equal(t, 0.5, p.Tau)
	assert.Equal(t, 4.0, p.Beta)
	assert.Equal(t, 2.0, p.Lambda)
	assert.Equal(t, 0.6, p.WKwd)
	assert.Equal(t, 0.9, p.Sigma)
	assert.Equal(t, 0.5, p.Gamma)
	assert.Equal(t, 0.7, p.Mu)
	assert.Equal(t, 4, p.K)
	assert.Equal(t, 20, p.M)
	assert.Equal(t, 30, p.VecTop)
	assert.Equal(t, 40, p.FtsTop)
	assert.Equal(t, 0.9, p.Diversity)
	assert.Equal(t, 1.5, p.TRef)

	// Malformed values and unknown keys keep defaults / are ignored.
	p, ok = ParseSoulParams(fence + "soul\nd: not-a-number\ntau: -2\nfuture_key: 99\n" + fence)
	require.True(t, ok)
	assert.Equal(t, DefaultParams().D, p.D)
	assert.Equal(t, -2.0, p.Tau)

	// Unterminated block → no soul.
	_, ok = ParseSoulParams(fence + "soul\nd: 0.35\n")
	assert.False(t, ok)

	// "```soulsong" is not a soul block.
	_, ok = ParseSoulParams(fence + "soulsong\nd: 0.35\n" + fence)
	assert.False(t, ok)

	// Comments and blanks are skipped.
	p, ok = ParseSoulParams(fence + "soul\n# temperament\nd: 0.35\n\nk: 8\n" + fence)
	require.True(t, ok)
	assert.Equal(t, 0.35, p.D)
	assert.Equal(t, 8, p.K)
}

// TestDecayIncrementsCoolPasses pins the background decay pass (Bet 06):
// unrehearsed memories gain a cool_pass, rehearsed ones do not, and the
// salience-cooling term finally moves.
func TestDecayIncrementsCoolPasses(t *testing.T) {
	s, err := Open(t.TempDir()+"/mem.sqlite", FakeEmbedder{DimCount: 8}, DefaultParams())
	require.NoError(t, err)
	defer s.Close()

	ctx := t.Context()
	idOld, _, err := s.Insert(
		ctx,
		NewTrace{Content: "old unrehearsed fact", Kind: "fact", Scope: "user", Salience: 0.5},
	)
	require.NoError(t, err)
	idHot, _, err := s.Insert(
		ctx,
		NewTrace{Content: "recently rehearsed fact", Kind: "fact", Scope: "user", Salience: 0.5},
	)
	require.NoError(t, err)
	// Age the first memory — and its insert-time rehearsal — past the
	// decay cutoff (Insert stamps "now" for both).
	require.NoError(t, s.doWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET created_at = ? WHERE id = ?`, s.table("memories")),
			time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339), idOld); err != nil {
			return err
		}
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET at = ? WHERE memory_id = ?`, s.table("rehearsals")),
			time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339), idOld)
		return err
	}))
	// A rehearsal "now" keeps idHot warm.
	s.recordRehearsals([]int64{idHot})

	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	require.NoError(t, s.Decay(cutoff))

	traces, err := s.Retrieve(ctx, "fact", ModeCompanion, Cues{Project: ""}, time.Now())
	require.NoError(t, err)
	byContent := map[string]ScoredTrace{}
	for _, tr := range traces {
		byContent[tr.Content] = tr
	}
	require.Contains(t, byContent, "old unrehearsed fact")
	require.Contains(t, byContent, "recently rehearsed fact")
	assert.Equal(t, 1, byContent["old unrehearsed fact"].CoolPasses, "unrehearsed memory cools")
	assert.Equal(t, 0, byContent["recently rehearsed fact"].CoolPasses, "rehearsed memory stays warm")
}
