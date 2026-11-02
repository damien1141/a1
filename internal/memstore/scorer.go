package memstore

import (
	"math"
	"strings"
	"time"
)

// Params is the memory temperament (§11), parsed from SOUL.md at session
// start. Defaults are human-fitted (Anderson); calibrate per §11.
type Params struct {
	D         float64 // decay exponent (0.5). 0.35 nostalgic ↔ 0.8 crisp
	Tau       float64 // retrieval threshold (0.0). -1.5 eager ↔ +0.5 reticent
	Beta      float64 // encoding strength (3.0) → ~17d unrehearsed survival
	Lambda    float64 // semantic cue weight (2.5)
	WKwd      float64 // FTS keyword-hit bonus (0.5)
	Sigma     float64 // salience weight (1.0)
	Gamma     float64 // per-consolidation-pass cooling (0.7)
	Mu        float64 // encoding-context match weight (0.5)
	K         int     // per-turn injection budget (6)
	M         int     // candidate pool after RRF fusion (25)
	VecTop    int     // vector leg overfetch (25)
	FtsTop    int     // FTS leg overfetch (50)
	Diversity float64 // max intra-bundle cosine (0.85)
	TRef      float64 // time unit in hours (1.0) — keep 1.0
}

func DefaultParams() Params {
	return Params{D: 0.5, Tau: 0, Beta: 3, Lambda: 2.5, WKwd: 0.5,
		Sigma: 1, Gamma: 0.7, Mu: 0.5, K: 6, M: 25, VecTop: 25,
		FtsTop: 50, Diversity: 0.85, TRef: 1}
}

// Cues is the recall-time context (encoding specificity, §3.3).
type Cues struct {
	Project string  // git root or ""
	Hour    float64 // hour of day 0–24
	Valence float64 // session affect, -1..1 — drives §3.5 valence policy
}

// BaseActivation — eq. (1): B = β + ln( Σ_j (1 + Δt_j/t_ref)^(-d) )
// rehearsals must include the encoding time. Δt in hours.
// Worked examples (β=3, d=0.5, t_ref=1) — pinned by golden tests:
//   30d old, never retrieved:            B = -0.290  (below τ: forgotten)
//   30d old, retrieved once 2d ago:      B = +1.286  (alive)
//   encode + 5 weekly rehearsals:        B =  3.222  (effectively permanent)
// One fresh recall ≈ full reset: the Δt=0 term contributes 1.0 and dominates.
func BaseActivation(p Params, rehearsals []time.Time, now time.Time) float64 {
	sum := 0.0
	for _, t := range rehearsals {
		dt := now.Sub(t).Hours()
		if dt < 0 {
			dt = 0
		}
		sum += math.Pow(1+dt/p.TRef, -p.D)
	}
	if sum <= 0 {
		return math.Inf(-1)
	}
	return p.Beta + math.Log(sum)
}

// ContextMatch — eq. (4): C = 0.7·P + 0.3·H.
// Valence is deliberately absent (§3.3): mood-congruency is overridden at
// selection time instead.
func ContextMatch(recall Cues, enc EncodingCtx) float64 {
	P := 0.0
	switch {
	case enc.Project != "" && enc.Project == recall.Project:
		P = 1.0
	case enc.Project != "" && recall.Project != "":
		if top2(enc.Project) == top2(recall.Project) {
			P = 0.6
		}
	}
	dh := math.Abs(enc.Tod - recall.Hour)
	if dh > 12 {
		dh = 24 - dh // circular
	}
	H := (1 + math.Cos(dh*math.Pi/12)) / 2
	return 0.7*P + 0.3*H
}

func top2(p string) string {
	ps := strings.Split(strings.Trim(p, "/"), "/")
	if len(ps) >= 2 {
		return strings.Join(ps[len(ps)-2:], "/")
	}
	return p
}

// Activation — eq. (3). ftsHit is the k_i term.
func Activation(p Params, t *Trace, qvec []float32, recall Cues, ftsHit bool, now time.Time) float64 {
	B := BaseActivation(p, t.Rehearsals, now)
	a := B + p.Lambda*dot(qvec, t.Embed)
	if ftsHit {
		a += p.WKwd
	}
	a += p.Sigma * t.Salience * math.Pow(p.Gamma, float64(t.CoolPasses))
	a += p.Mu * ContextMatch(recall, t.Ctx)
	return a
}

// Probability — eq. (5). A > τ ⟺ P > 0.5: the floor IS the threshold.
// Inject P as the provenance confidence stamp (§10.1).
func Probability(a, tau float64) float64 {
	return 1 / (1 + math.Exp(tau-a))
}
