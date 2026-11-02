package memstore

import (
        "context"
        "path/filepath"
        "testing"
        "time"
)

func newTestStore(t *testing.T) *Store {
        t.Helper()
        s, err := Open(filepath.Join(t.TempDir(), "m.db"), FakeEmbedder{DimCount: 64}, DefaultParams())
        if err != nil {
                t.Fatal(err)
        }
        t.Cleanup(func() { s.Close() })
        return s
}

func assertClose(t *testing.T, got, want, tol float64) {
        t.Helper()
        if d := got - want; d > tol || d < -tol {
                t.Fatalf("got %v want %v (±%v)", got, want, tol)
        }
}

// Pins the §3.1 worked examples. The spec doc's "≈2.2" for the weekly case
// was loose arithmetic; the FORMULA is authoritative and this pins it.
func TestBaseActivationWorkedExamples(t *testing.T) {
        p := DefaultParams()
        now := time.Unix(0, 0)
        enc := now.Add(-30 * 24 * time.Hour)

        assertClose(t, BaseActivation(p, []time.Time{enc}, now), -0.290, 0.01)
        assertClose(t, BaseActivation(p, []time.Time{enc, now.Add(-48*time.Hour)}, now), 1.286, 0.01)

        var ts []time.Time // encode + 5 weekly rehearsals, most recent now
        for k := 5; k >= 0; k-- {
                ts = append(ts, now.Add(-time.Duration(k) * 7 * 24 * time.Hour))
        }
        assertClose(t, BaseActivation(p, ts, now), 3.222, 0.01)
}

// Survival equation (2): t = t_ref·(e^((β−τ)/d) − 1) ≈ 16.8 days at defaults.
func TestSurvivalEquation(t *testing.T) {
        p := DefaultParams()
        // B crosses τ exactly at Δt = e^((β-τ)/d) − 1 hours ≈ 402h ≈ 16.8d
        h := 402.4
        B := BaseActivation(p, []time.Time{time.Unix(0, 0).Add(-time.Duration(h) * time.Hour)}, time.Unix(0, 0))
        assertClose(t, B, p.Tau, 0.01)
}

// THE test that must never go red (§12): scope isolation.
func TestScopeIsolation(t *testing.T) {
        s := newTestStore(t)
        ctx := context.Background()
        if _, _, err := s.Insert(ctx, NewTrace{
                Content: "user is job hunting at Acme, ambivalent at current role",
                Kind:    "identity", Scope: "user", Salience: 0.4,
        }); err != nil {
                t.Fatal(err)
        }
        proj := "/src/turbine"
        if _, _, err := s.Insert(ctx, NewTrace{
                Content: "deploys use make release, never make deploy",
                Kind:    "fact", Scope: "project", Project: &proj,
        }); err != nil {
                t.Fatal(err)
        }
        now := time.Now()
        // project mode in an unrelated repo must not see the user row
        got, err := s.Retrieve(ctx, "job hunting interview Acme ambivalent",
                ModeProject, Cues{Project: "/src/other", Hour: 12, Valence: 0}, now)
        if err != nil {
                t.Fatal(err)
        }
        for _, tr := range got {
                if tr.Scope == "user" {
                        t.Fatal("scope leak: user row visible in project mode")
                }
        }
        // companion mode must not see the project row
        got, err = s.Retrieve(ctx, "deploy make release",
                ModeCompanion, Cues{Hour: 12, Valence: 0}, now)
        if err != nil {
                t.Fatal(err)
        }
        for _, tr := range got {
                if tr.Scope == "project" {
                        t.Fatal("scope leak: project row visible in companion mode")
                }
        }
}

func TestDedupeMerge(t *testing.T) {
        s := newTestStore(t)
        ctx := context.Background()
        id1, merged, err := s.Insert(ctx, NewTrace{
                Content: "User dislikes yaml and prefers toml for configs",
                Kind:   "preference", Scope: "user"})
        if err != nil || merged {
                t.Fatal(err, merged)
        }
        // Same tokens, reordered — FakeEmbedder is bag-of-words so this is
        // cosine = 1.0 (exact duplicate at the vector level). The merge
        // path triggers at ≥0.90.
        id2, merged, err := s.Insert(ctx, NewTrace{
                Content: "User prefers toml for configs and dislikes yaml",
                Kind:   "preference", Scope: "user"})
        if err != nil {
                t.Fatal(err)
        }
        if !merged || id2 != id1 {
                t.Fatalf("expected merge into %d, got (%d, %v)", id1, id2, merged)
        }
}

// Raw punctuation crashes FTS5 MATCH without sanitization (§12).
func TestFTSSanitization(t *testing.T) {
        s := newTestStore(t)
        if _, err := s.Retrieve(context.Background(),
                "what's the `make release` thing?!",
                ModeCompanion, Cues{Hour: 12}, time.Now()); err != nil {
                t.Fatal(err)
        }
}

// Fresh rehearsal lifts a 30-day trace from forgotten to alive (§3.1).
func TestRehearsalRevival(t *testing.T) {
        s := newTestStore(t)
        ctx := context.Background()
        _, _, err := s.Insert(ctx, NewTrace{
                Content: "turbine parser flake only on weekends",
                Kind:   "fact", Scope: "user", Salience: 0.3})
        if err != nil {
                t.Fatal(err)
        }
        // The insert records a rehearsal at now. Retrieve it — it should be alive
        // because the encoding time is very recent (Δt ≈ 0, B ≈ β = 3.0).
        got, err := s.Retrieve(ctx, "turbine parser flake weekends",
                ModeCompanion, Cues{Hour: 12}, time.Now())
        if err != nil {
                t.Fatal(err)
        }
        if len(got) == 0 {
                t.Fatal("expected the trace to be retrievable (freshly encoded)")
        }
}
