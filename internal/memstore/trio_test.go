package memstore

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- negative ----------

func TestOpenIn_Negative(t *testing.T) {
	_, err := OpenIn(nil, "persona", FakeEmbedder{DimCount: 8}, DefaultParams())
	require.Error(t, err)
	assert.EqualError(t, err, "memstore: OpenIn requires a db handle")

	// sql.Open is lazy, so an unopened handle is enough for the namespace check.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "v.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = OpenIn(db, "", FakeEmbedder{DimCount: 8}, DefaultParams())
	require.Error(t, err)
	assert.EqualError(t, err, "memstore: OpenIn requires a namespace")
}

func TestOpen_BadPaths_Negative(t *testing.T) {
	// Parent directory missing: sql.Open is lazy, so the schema exec reports it.
	_, err := Open(filepath.Join(t.TempDir(), "no", "such", "dir", "m.db"),
		FakeEmbedder{DimCount: 8}, DefaultParams())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to open database file")

	// A file that is not a SQLite database at all.
	bad := filepath.Join(t.TempDir(), "bad.db")
	require.NoError(t, os.WriteFile(bad, []byte("this is definitely not a sqlite database file"), 0o644))
	_, err = Open(bad, FakeEmbedder{DimCount: 8}, DefaultParams())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a database")
}

// Reads and writes on a closed store fail with the closed-handle error —
// no panic, no silent success. (Forget after Close is NOT covered: it posts
// to the closed writes channel and panics — see production concerns.)
func TestClosedStore_Negative(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"), FakeEmbedder{DimCount: 8}, DefaultParams())
	require.NoError(t, err)
	require.NoError(t, s.Close())

	const want = "sql: database is closed"
	_, err = s.Retrieve(t.Context(), "anything", ModeCompanion, Cues{Hour: 12}, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), want)

	_, _, err = s.Insert(t.Context(), NewTrace{Content: "x", Kind: "fact", Scope: "user"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), want)

	err = s.Revise(t.Context(), 1, "new", "why")
	require.Error(t, err)
	assert.Contains(t, err.Error(), want)

	err = s.Decay(time.Now().UTC().Format(time.RFC3339))
	require.Error(t, err)
	assert.Contains(t, err.Error(), want)

	// A nil store decays as a documented no-op.
	var nilStore *Store
	assert.NoError(t, nilStore.Decay("2020-01-01T00:00:00Z"))
}

// Params has no validation: K=0 silently disables the injection budget, and a
// negative VecTop reaches make() with a negative length and panics. Both are
// pinned as the actual behavior — see production concerns.
func TestRetrieve_UnguardedParams_Negative(t *testing.T) {
	seed := func(t *testing.T, s *Store) {
		t.Helper()
		for i, pair := range []string{
			"avocado beetroot", "cactus dahlia", "elderflower fig", "grapefruit hazelnut",
			"jackfruit kale", "lotus mango", "nectarine olive", "papaya quince",
		} {
			_, _, err := s.Insert(t.Context(), NewTrace{
				Content: pair, Kind: "fact", Scope: "user", Salience: 0.2 + 0.1*float64(i),
			})
			require.NoError(t, err)
		}
	}

	p := DefaultParams()
	p.K = 0 // budget of zero
	s, err := Open(filepath.Join(t.TempDir(), "k0.db"), FakeEmbedder{DimCount: 64}, p)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	seed(t, s)
	got, err := s.Retrieve(t.Context(), "avocado beetroot cactus dahlia elderflower fig",
		ModeCompanion, Cues{Hour: 12}, time.Now())
	require.NoError(t, err)
	assert.Len(t, got, 8, "K=0 must not cap the bundle: the budget is silently ignored")

	s2, err := Open(filepath.Join(t.TempDir(), "k6.db"), FakeEmbedder{DimCount: 64}, DefaultParams())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	seed(t, s2)
	got, err = s2.Retrieve(t.Context(), "avocado beetroot cactus dahlia elderflower fig",
		ModeCompanion, Cues{Hour: 12}, time.Now())
	require.NoError(t, err)
	assert.Len(t, got, 6, "the default budget caps the bundle at K=6")

	pv := DefaultParams()
	pv.VecTop = -1
	s3, err := Open(filepath.Join(t.TempDir(), "v.db"), FakeEmbedder{DimCount: 64}, pv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s3.Close() })
	assert.Panics(t, func() {
		_, _ = s3.Retrieve(t.Context(), "anything", ModeCompanion, Cues{Hour: 12}, time.Now())
	}, "a negative VecTop reaches make([]int64, -1) in the vector leg")
}

// ---------- differential: brute-force retrieval reference ----------

// refEmbed recomputes the FakeEmbedder's FNV-1a token-hash vector from
// scratch — the query embedding is never taken from the code under test.
func refEmbed(text string) []float32 {
	const d = 64
	v := make([]float32, d)
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		h := uint32(2166136261)
		for _, c := range tok {
			h = (h ^ uint32(c)) * 16777619
		}
		v[h%d]++
		v[(h>>8)%d] += 0.5
	}
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
	return v
}

func refDecode(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func refDot(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var s float64
	for i := 0; i < n; i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// refTokens is a hand-rolled [a-z0-9_]{2,} tokenizer for the FTS leg.
func refTokens(s string) []string {
	var out []string
	var cur []rune
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			cur = append(cur, r)
			continue
		}
		if len(cur) >= 2 {
			out = append(out, string(cur))
		}
		cur = nil
	}
	if len(cur) >= 2 {
		out = append(out, string(cur))
	}
	return out
}

func refTop2(p string) string {
	ps := strings.Split(strings.Trim(p, "/"), "/")
	if len(ps) >= 2 {
		return strings.Join(ps[len(ps)-2:], "/")
	}
	return p
}

func refCtxMatch(recall Cues, encProj string, encTod float64) float64 {
	p := 0.0
	switch {
	case encProj != "" && encProj == recall.Project:
		p = 1.0
	case encProj != "" && recall.Project != "":
		if refTop2(encProj) == refTop2(recall.Project) {
			p = 0.6
		}
	}
	dh := math.Abs(encTod - recall.Hour)
	if dh > 12 {
		dh = 24 - dh
	}
	h := (1 + math.Cos(dh*math.Pi/12)) / 2
	return 0.7*p + 0.3*h
}

type refRow struct {
	id                   int64
	content, kind, scope string
	project, status      string
	salience             float64
	cool                 int
	valence              float64
	hasValence           bool
	embed                []float32
	rehearsals           []time.Time
	encProj              string
	encTod               float64
}

// loadRefRows snapshots the seeded rows with plain SQL — the store's own
// loading path is never used to build the reference.
func loadRefRows(t *testing.T, s *Store) map[int64]refRow {
	t.Helper()
	rows, err := s.db.Query(`SELECT id, content, kind, scope, IFNULL(project_path,''),
                salience, cool_passes, IFNULL(valence,0), valence IS NOT NULL, embedding,
                encoding_ctx, status FROM memories`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[int64]refRow{}
	for rows.Next() {
		var r refRow
		var emb []byte
		var ctxJSON string
		require.NoError(t, rows.Scan(&r.id, &r.content, &r.kind, &r.scope, &r.project,
			&r.salience, &r.cool, &r.valence, &r.hasValence, &emb, &ctxJSON, &r.status))
		r.embed = refDecode(emb)
		var ctx struct {
			Project string  `json:"project"`
			Tod     float64 `json:"tod"`
		}
		require.NoError(t, json.Unmarshal([]byte(ctxJSON), &ctx))
		r.encProj, r.encTod = ctx.Project, ctx.Tod
		out[r.id] = r
	}
	require.NoError(t, rows.Err())

	rrows, err := s.db.Query(`SELECT memory_id, at FROM rehearsals`)
	require.NoError(t, err)
	defer rrows.Close()
	for rrows.Next() {
		var id int64
		var at string
		require.NoError(t, rrows.Scan(&id, &at))
		ts, err := time.Parse(time.RFC3339, at)
		require.NoError(t, err)
		r := out[id]
		r.rehearsals = append(r.rehearsals, ts)
		out[id] = r
	}
	require.NoError(t, rrows.Err())
	return out
}

func refVisible(r refRow, mode Mode, gitRoot string) bool {
	if r.status != "active" {
		return false
	}
	special := map[string]bool{"identity": true, "beat": true, "affect": true, "bit": true}
	if mode == ModeProject {
		if special[r.kind] {
			return false
		}
		return r.scope == "global" || (r.scope == "project" && r.project == gitRoot)
	}
	return r.scope == "user" || special[r.kind]
}

type refScored struct {
	row     refRow
	a, p, B float64
	cos     float64
	kwd     bool
}

// refSelect replays the §3 selection rules over brute-force scores.
func refSelect(rows map[int64]refRow, mode Mode, gitRoot, query string, cues Cues,
	now time.Time, p Params,
) []refScored {
	q := refEmbed(query)
	qtoks := refTokens(query)
	var cands []refScored
	for _, r := range rows {
		if !refVisible(r, mode, gitRoot) {
			continue
		}
		sum := 0.0
		for _, ts := range r.rehearsals {
			dt := now.Sub(ts).Hours()
			if dt < 0 {
				dt = 0
			}
			sum += math.Pow(1+dt/p.TRef, -p.D)
		}
		B := math.Inf(-1)
		if sum > 0 {
			B = p.Beta + math.Log(sum)
		}
		cos := refDot(q, r.embed)
		kwd := false
		ctoks := map[string]bool{}
		for _, tk := range refTokens(r.content) {
			ctoks[tk] = true
		}
		for _, qt := range qtoks {
			if ctoks[qt] {
				kwd = true
				break
			}
		}
		a := B + p.Lambda*cos
		if kwd {
			a += p.WKwd
		}
		a += p.Sigma * r.salience * math.Pow(p.Gamma, float64(r.cool))
		a += p.Mu * refCtxMatch(cues, r.encProj, r.encTod)
		cands = append(cands, refScored{row: r, a: a, p: 1 / (1 + math.Exp(p.Tau-a)), B: B, cos: cos, kwd: kwd})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].a != cands[j].a {
			return cands[i].a > cands[j].a
		}
		return cands[i].row.id < cands[j].row.id
	})

	var out []refScored
	for _, c := range cands {
		if c.a <= p.Tau {
			break
		}
		dup := false
		for _, o := range out {
			if refDot(c.row.embed, o.row.embed) >= p.Diversity {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, c)
		if len(out) == p.K {
			break
		}
	}
	// Mood-congruency override (§3.5): a sad session wants ≥ ⌈K/3⌉ positive traces.
	if cues.Valence < -0.3 {
		need := (p.K + 2) / 3
		pos := func(c refScored) bool { return c.row.hasValence && c.row.valence > 0.2 }
		have := 0
		for _, o := range out {
			if pos(o) {
				have++
			}
		}
		inOut := func(id int64) bool {
			for _, o := range out {
				if o.row.id == id {
					return true
				}
			}
			return false
		}
		for have < need {
			var cand *refScored
			for i := range cands {
				if pos(cands[i]) && !inOut(cands[i].row.id) {
					cand = &cands[i]
					break
				}
			}
			if cand == nil {
				break
			}
			// Mirror the policy: replace the LAST non-positive slot — the
			// bundle's weakest member — so the top scorer stays at rank 0.
			replaced := false
			for i := len(out) - 1; i >= 0; i-- {
				if !pos(out[i]) {
					out[i] = *cand
					replaced = true
					break
				}
			}
			if !replaced {
				out = append(out, *cand)
			}
			have++
		}
	}
	return out
}

func seedTrace(t *testing.T, s *Store, in NewTrace) int64 {
	t.Helper()
	id, _, err := s.Insert(t.Context(), in)
	require.NoError(t, err)
	return id
}

// oracle: brute-force reference scan in this test — plain SQL over the seeded
// rows plus naive arithmetic (a hand-written FNV-1a embedder, cosine, and the
// §3 activation and selection rules recomputed here), compared with Retrieve's
// bundle, order, and per-trace scores in both scope modes.
func TestRetrieve_Ranking_Differential(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "rank.db"), FakeEmbedder{DimCount: 64}, DefaultParams())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	turbine := "/src/turbine"
	positive, negative := 0.6, -0.4
	id1 := seedTrace(t, s, NewTrace{Content: "alpha bravo charlie delta", Kind: "fact", Scope: "user", Salience: 0.9})
	id2 := seedTrace(t, s, NewTrace{Content: "echo foxtrot golf", Kind: "preference", Scope: "user", Salience: 0.1})
	id3 := seedTrace(t, s, NewTrace{Content: "the user is a night owl who ships on fridays",
		Kind: "identity", Scope: "user", Salience: 0.3, Valence: &positive, Ctx: EncodingCtx{Tod: 23}})
	id4 := seedTrace(t, s, NewTrace{Content: "hotel india juliet", Kind: "fact", Scope: "user",
		Salience: 0.5, Valence: &negative})
	id5 := seedTrace(t, s, NewTrace{Content: "deploys use make release never make deploy",
		Kind: "fact", Scope: "project", Project: &turbine, Salience: 0.4,
		Ctx: EncodingCtx{Project: turbine, Tod: 10}})
	id6 := seedTrace(t, s, NewTrace{Content: "kilo lima mike november", Kind: "beat", Scope: "project", Salience: 0.2})
	id7 := seedTrace(t, s, NewTrace{Content: "oscar papa quebec", Kind: "fact", Scope: "global", Salience: 0.6})
	id8 := seedTrace(t, s, NewTrace{Content: "romeo sierra tango", Kind: "fact", Scope: "project",
		Project: &turbine, Salience: 0.7, Ctx: EncodingCtx{Project: "/home/dev/src/turbine", Tod: 8}})
	id9 := seedTrace(t, s, NewTrace{Content: "uniform victor whiskey", Kind: "fact", Scope: "user", Salience: 0.8})
	id10 := seedTrace(t, s, NewTrace{Content: "xray yankee zulu baking bread at altitude",
		Kind: "affect", Scope: "user", Salience: 0.25})
	id11 := seedTrace(t, s, NewTrace{Content: "the kettle sings at dawn", Kind: "bit", Scope: "user", Salience: 0.15})

	// Hostile rows straight via SQL: a cooled trace and an inactive one.
	_, err = s.db.Exec(`UPDATE memories SET cool_passes = 2 WHERE id = ?`, id4)
	require.NoError(t, err)
	_, err = s.db.Exec(`UPDATE memories SET status = 'inactive' WHERE id = ?`, id9)
	require.NoError(t, err)

	rows := loadRefRows(t, s)
	require.Len(t, rows, 11)
	now := time.Now()

	t.Run("companion mode", func(t *testing.T) {
		cues := Cues{Hour: 9, Valence: 0}
		want := refSelect(rows, ModeCompanion, "", "alpha charlie golf night owl", cues, now, DefaultParams())
		require.NotEmpty(t, want)
		require.Len(t, want, 6, "seven visible traces must be capped at K=6")

		got, err := s.Retrieve(t.Context(), "alpha charlie golf night owl", ModeCompanion, cues, now)
		require.NoError(t, err)
		require.Len(t, got, len(want))
		for i, w := range want {
			g := got[i]
			assert.Equal(t, w.row.id, g.ID, "rank %d", i)
			assert.InDelta(t, w.B, g.B, 1e-9, "rank %d base activation", i)
			assert.InDelta(t, w.cos, g.Cos, 1e-9, "rank %d cosine", i)
			assert.InDelta(t, w.a, g.A, 1e-9, "rank %d activation", i)
			assert.InDelta(t, w.p, g.P, 1e-9, "rank %d probability", i)
			assert.Equal(t, w.kwd, g.Kwd, "rank %d keyword hit", i)
		}
		// The invisible rows never leak into the companion bundle.
		for _, g := range got {
			assert.NotContains(t, []int64{id5, id7, id8, id9}, g.ID, "scope leak")
		}
	})

	t.Run("project mode", func(t *testing.T) {
		cues := Cues{Project: "/src/turbine", Hour: 9, Valence: 0}
		want := refSelect(rows, ModeProject, "/src/turbine", "make release deploys romeo", cues, now, DefaultParams())
		require.Len(t, want, 3, "project-scope rows only: %v", want)

		got, err := s.Retrieve(t.Context(), "make release deploys romeo", ModeProject, cues, now)
		require.NoError(t, err)
		require.Len(t, got, len(want))
		for i, w := range want {
			g := got[i]
			assert.Equal(t, w.row.id, g.ID, "rank %d", i)
			assert.InDelta(t, w.a, g.A, 1e-9, "rank %d activation", i)
			assert.InDelta(t, w.p, g.P, 1e-9, "rank %d probability", i)
			assert.Equal(t, w.kwd, g.Kwd, "rank %d keyword hit", i)
		}
		for _, g := range got {
			assert.NotContains(t, []int64{id1, id2, id3, id4, id6, id9, id10, id11}, g.ID, "scope leak")
		}
	})
}

// oracle: the same brute-force reference as the ranking test, with the
// mood-congruency override armed — a sad session with K=2 must pull at least
// one positive-valence trace into the bundle.
func TestRetrieve_MoodCongruency_Differential(t *testing.T) {
	p := DefaultParams()
	p.K = 2
	s, err := Open(filepath.Join(t.TempDir(), "mood.db"), FakeEmbedder{DimCount: 64}, p)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	veryPos := 0.9
	idA := seedTrace(t, s, NewTrace{Content: "alpha alpha bravo bravo", Kind: "fact", Scope: "user", Salience: 0.9})
	idB := seedTrace(t, s, NewTrace{Content: "charlie charlie delta delta", Kind: "fact", Scope: "user", Salience: 0.8})
	idC := seedTrace(t, s, NewTrace{Content: "echo echo foxtrot foxtrot", Kind: "fact", Scope: "user",
		Salience: 0.5, Valence: &veryPos})

	rows := loadRefRows(t, s)
	now := time.Now()
	cues := Cues{Hour: 9, Valence: -0.5}
	query := "alpha bravo charlie delta"

	want := refSelect(rows, ModeCompanion, "", query, cues, now, p)
	require.Len(t, want, 2)

	got, err := s.Retrieve(t.Context(), query, ModeCompanion, cues, now)
	require.NoError(t, err)
	require.Len(t, got, 2)
	for i, w := range want {
		assert.Equal(t, w.row.id, got[i].ID, "rank %d", i)
		assert.InDelta(t, w.a, got[i].A, 1e-9, "rank %d activation", i)
	}
	// The policy's intent, checked independently of the reference: a sad
	// session's bundle is not allowed to be all-negative.
	hasPositive := false
	for _, g := range got {
		if g.Valence.Valid && g.Valence.Float64 > 0.2 {
			hasPositive = true
		}
	}
	assert.True(t, hasPositive, "the override must inject a positive trace, got %d and %d", got[0].ID, got[1].ID)
	// The override costs the weakest slot: the raw top scorer stays at
	// rank 0, the injected positive renders at the tail (rank 1), and the
	// activation gap is the invariant that proves injection rather than a
	// scoring win — the positive's raw A trails the retained scorer's. The
	// displaced one is the second scorer.
	require.Equal(t, idA, got[0].ID, "the raw top scorer is retained at rank 0")
	require.Equal(t, idC, got[1].ID, "the injected positive renders at the tail")
	assert.Greater(t, got[0].A, got[1].A, "the positive entered via injection, not scoring")
	for _, g := range got {
		assert.NotEqual(t, idB, g.ID, "the displaced second scorer is replaced, not kept alongside")
	}
}

// oracle: plain-SQL reference — the decay rule (cool every active memory
// created before the cutoff with no rehearsal after it) recomputed from the
// raw timestamps, including the strict-inequality boundaries, then compared
// with the cool_passes Decay leaves behind after two passes.
func TestDecay_Rule_Differential(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "decay.db"), FakeEmbedder{DimCount: 8}, DefaultParams())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	ids := make([]int64, 7)
	for i := range ids {
		ids[i] = seedTrace(t, s, NewTrace{
			Content: fmt.Sprintf("decay probe number %d", i), Kind: "fact", Scope: "user", Salience: 0.3,
		})
	}

	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	cutStr := cutoff.Format(time.RFC3339)
	oldStr := now.Add(-72 * time.Hour).Format(time.RFC3339)
	midStr := now.Add(-48 * time.Hour).Format(time.RFC3339)
	newStr := now.Add(-1 * time.Hour).Format(time.RFC3339)

	setBoth := func(id int64, created, rehearsal string) {
		_, err := s.db.Exec(`UPDATE memories SET created_at = ? WHERE id = ?`, created, id)
		require.NoError(t, err)
		_, err = s.db.Exec(`UPDATE rehearsals SET at = ? WHERE memory_id = ?`, rehearsal, id)
		require.NoError(t, err)
	}
	//                created   rehearsal   → cools?
	setBoth(ids[0], oldStr, midStr) // yes: old, unrehearsed since cutoff
	setBoth(ids[1], oldStr, newStr) // no: rehearsed after cutoff
	setBoth(ids[2], newStr, newStr) // no: created after cutoff
	setBoth(ids[3], oldStr, cutStr) // yes: rehearsal AT the cutoff does not protect (strict >)
	setBoth(ids[4], cutStr, oldStr) // no: created AT the cutoff is not old enough (strict <)
	setBoth(ids[5], oldStr, midStr) // yes
	// ids[6]: old with no rehearsal row at all.
	_, err = s.db.Exec(`DELETE FROM rehearsals WHERE memory_id = ?`, ids[6])
	require.NoError(t, err)
	_, err = s.db.Exec(`UPDATE memories SET created_at = ? WHERE id = ?`, oldStr, ids[6])
	require.NoError(t, err)

	wantCool := map[int64]int{}
	for i, id := range ids {
		switch i {
		case 0, 3, 5, 6:
			wantCool[id] = 2 // two decay passes
		default:
			wantCool[id] = 0
		}
	}

	require.NoError(t, s.Decay(cutStr))
	require.NoError(t, s.Decay(cutStr))

	got := map[int64]int{}
	drows, err := s.db.Query(`SELECT id, cool_passes FROM memories`)
	require.NoError(t, err)
	defer drows.Close()
	for drows.Next() {
		var id int64
		var cool int
		require.NoError(t, drows.Scan(&id, &cool))
		got[id] = cool
	}
	require.NoError(t, drows.Err())
	assert.Equal(t, wantCool, got)
}
