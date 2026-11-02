// Package eval is the Mirror (Bet 07): the offline scorer and replay
// runner that turn a1's own session trees into a labeled dataset.
//
// Every persisted session is a free eval sample nobody reads: the JSONL
// tree records what the model did (tool runs, token spend, compactions)
// and the evidence receipts record what the harness verified. This
// package joins the two files a1 already writes and scores them — no LLM,
// no mutation, pure read path.
//
// Scoring semantics (mirroring the engine's live gates):
//
//   - A verification round is one Observe batch — every receipt from a
//     single command shares the At timestamp the executor stamped, so
//     equal-timestamp receipt runs group into rounds offline.
//   - Rounds-to-green counts verification rounds until the replayed
//     ledger satisfies the session's recovered contract
//     (Ledger.Residual is empty). Never green reports the total plus a
//     not-green flag.
//   - Rollback rate is harness-cause receipts (RevertTo's "harness:<cause>"
//     lines) over verification rounds.
//   - Stall rate is "[evidence] stopping this turn" stop notes over
//     governed turns (turns at or after the session's first contract
//     declaration or contract/evidence note — the offline approximation of
//     the engine's sticky governed latch).
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/evidence"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/skillstats"
	"github.com/damien1141/a1/internal/telemetry"
)

// UnknownModel keys token spend for sessions whose tree predates per-model
// attribution: the session JSONL records usage per assistant entry but not
// which model produced it, so offline scoring can only bucket by a caller
// hint (the replay runner knows the model; plain scoring does not).
const UnknownModel = "unknown"

// EvalResult is one scored session.
type EvalResult struct {
	SessionID string `json:"session_id"`
	File      string `json:"file,omitempty"`
	// Prompt is the session's first real user message (the task), for
	// reports; truncated by the caller when rendering.
	Prompt string `json:"prompt,omitempty"`

	RoundsToGreen int  `json:"rounds_to_green"`
	Green         bool `json:"green"`

	VerificationRounds int     `json:"verification_rounds"`
	Rollbacks          int     `json:"rollbacks"`
	RollbackRate       float64 `json:"rollback_rate"`

	StallStops    int     `json:"stall_stops"`
	GovernedTurns int     `json:"governed_turns"`
	StallRate     float64 `json:"stall_rate"`

	TokensByModel map[string]int64 `json:"tokens_by_model"`
	TotalTokens   int64            `json:"total_tokens"`

	Compactions int `json:"compactions"`
	Turns       int `json:"turns"`

	// Contract is the recovered task contract (declared check ids).
	Contract []string `json:"contract,omitempty"`

	// SkillsPicked are the skills whose SKILL.md the session read (the
	// routing signal the executor records live; re-derived offline for
	// outcome attribution).
	SkillsPicked []string `json:"skills_picked,omitempty"`

	turnRows         []telemetry.TurnRow
	governedFromTurn int // 1-based turn index the governed latch fired in; 0 = never
}

// TurnRows returns the per-turn token rows for telemetry materialization.
func (r EvalResult) TurnRows() []telemetry.TurnRow {
	return r.turnRows
}

// ScoreFile scores one session file (vault-first resolution, like resume).
// modelHint buckets token spend; empty falls back to UnknownModel.
func ScoreFile(path, modelHint string) (EvalResult, error) {
	m, err := session.OpenSession(path)
	if err != nil {
		return EvalResult{}, fmt.Errorf("eval: open %s: %w", path, err)
	}
	return ScoreSession(m, modelHint)
}

// ScoreSession scores an opened session manager. Pure read path: nothing is
// persisted here — Materialize is the separate write step the eval command
// performs against the vault.
func ScoreSession(m *session.Manager, modelHint string) (EvalResult, error) {
	if m == nil {
		return EvalResult{}, fmt.Errorf("eval: session is required")
	}
	if modelHint == "" {
		modelHint = UnknownModel
	}

	res := EvalResult{
		SessionID:     m.ID(),
		File:          m.File(),
		TokensByModel: map[string]int64{},
	}

	// The scored walk is the ACTIVE branch (leaf → root), not every
	// abandoned fork: side explorations the session rewound from are not
	// the work that produced the final state.
	branch := m.GetBranch(m.LeafID())
	// Oldest first.
	for i := len(branch) - 1; i >= 0; i-- {
		walkEntry(branch[i], modelHint, &res)
	}

	records, err := loadReceipts(m.File())
	if err != nil {
		return res, err
	}
	r2g, verif, green, err := replayGreen(records, res.Contract)
	if err != nil {
		return res, err
	}
	res.RoundsToGreen = r2g
	res.VerificationRounds = verif
	res.Green = green && len(res.Contract) > 0

	res.Rollbacks = countHarnessReceipts(records)
	res.RollbackRate = ratio(res.Rollbacks, res.VerificationRounds)

	if res.governedFromTurn > 0 {
		res.GovernedTurns = max(res.Turns-res.governedFromTurn+1, 1)
	}
	if res.GovernedTurns > 0 {
		res.StallRate = ratio(res.StallStops, res.GovernedTurns)
	} else if res.StallStops > 0 {
		// A stop note without a recoverable governing event still means the
		// gate was live; count the denominator as at least one turn.
		res.StallRate = ratio(res.StallStops, 1)
	}
	return res, nil
}

// walkEntry folds one tree entry into the running result.
func walkEntry(entry session.MessageEntry, model string, res *EvalResult) {
	switch e := entry.(type) {
	case session.CompactionEntry:
		res.Compactions++
	case session.SessionMessageEntry:
		msg := e.Message
		switch msg.Role {
		case llm.RoleUser:
			walkUserEntry(msg, res)
		case llm.RoleAssistant:
			walkAssistantEntry(e, model, res)
		}
	}
}

func walkUserEntry(msg llm.Message, res *EvalResult) {
	if strings.HasPrefix(msg.Content, "System note:") {
		// Harness-injected notes are not turns; they are outcomes.
		if strings.Contains(msg.Content, "[evidence] stopping this turn") {
			res.StallStops++
		}
		if strings.Contains(msg.Content, "[contract]") {
			markGoverned(res)
		}
		return
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	res.Turns++
	if res.Prompt == "" {
		res.Prompt = msg.Content
	}
}

func walkAssistantEntry(e session.SessionMessageEntry, model string, res *EvalResult) {
	msg := e.Message
	if u := msg.Usage; u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
		total := int64(u.TotalTokens)
		if total == 0 {
			// Disjoint buckets: prompt (uncached) + cached + completion.
			total = int64(u.PromptTokens + u.CachedTokens() + u.CompletionTokens)
		}
		res.TokensByModel[model] += total
		res.TotalTokens += total
		res.turnRows = append(res.turnRows, telemetry.TurnRow{
			Model:            model,
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			CachedTokens:     u.CachedTokens(),
			TotalTokens:      u.TotalTokens,
			At:               e.Timestamp.UTC().Format(time.RFC3339Nano),
		})
	}
	for _, call := range msg.ToolCalls {
		switch call.Function.Name {
		case "contract":
			declareContract(call.Function.Arguments, res)
			markGoverned(res)
		case "read":
			// Skill picks re-derived offline: same signal the executor
			// records live (a successful SKILL.md read).
			var in struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(call.Function.Arguments), &in) == nil && in.Path != "" {
				if skill, ok := skillstats.PickFromReadPath(in.Path); ok && !containsString(res.SkillsPicked, skill) {
					res.SkillsPicked = append(res.SkillsPicked, skill)
				}
			}
		}
	}
}

// markGoverned approximates the engine's sticky governed latch: the turn
// containing the session's first contract declaration (or first
// contract/evidence note) is where evidence surfaces went live, and every
// turn from there on is governed. The latch never clears once set.
func markGoverned(res *EvalResult) {
	if res.governedFromTurn == 0 {
		res.governedFromTurn = max(res.Turns, 1)
	}
}

// declareContract folds a contract tool call's check ids into the session
// contract. Mirrors the engine's ratchet: only valid ids, no duplicates.
func declareContract(args string, res *EvalResult) {
	var in struct {
		Checks []string `json:"checks"`
	}
	if json.Unmarshal([]byte(args), &in) != nil {
		return
	}
	for _, raw := range in.Checks {
		id := strings.TrimSpace(raw)
		if evidence.ValidCheckID(id) && !containsString(res.Contract, id) {
			res.Contract = append(res.Contract, id)
		}
	}
	sort.Strings(res.Contract)
}

// loadReceipts loads the evidence receipts for a session's scope, mirroring
// the engine's defaultEvidenceLedger: vault rows when the vault is live,
// otherwise the evidence.jsonl file next to the session. A missing store is
// an empty ledger, not an error (first run).
func loadReceipts(sessionFile string) ([]evidence.Record, error) {
	scope := filepath.Dir(sessionFile)
	var payloads []string
	if v := session.DefaultVault(); v != nil {
		rows, err := v.LoadEvidenceReceipts(scope)
		if err != nil {
			return nil, fmt.Errorf("eval: load receipts (vault): %w", err)
		}
		payloads = rows
	} else {
		data, err := os.ReadFile(filepath.Join(scope, "evidence.jsonl"))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("eval: read receipts: %w", err)
			}
		} else {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.TrimSpace(line) != "" {
					payloads = append(payloads, line)
				}
			}
		}
	}

	records := make([]evidence.Record, 0, len(payloads))
	for _, p := range payloads {
		var rec evidence.Record
		if err := json.Unmarshal([]byte(p), &rec); err != nil {
			continue // corrupt lines are skipped, same as Ledger.Load
		}
		if rec.Check == "" || (rec.Status != evidence.Pass && rec.Status != evidence.Fail) {
			continue
		}
		records = append(records, rec)
	}
	// Chronological order is the replay order; stores are append-ordered but
	// a stable sort keeps any already-sorted set untouched.
	sort.SliceStable(records, func(i, j int) bool { return records[i].At.Before(records[j].At) })
	return records, nil
}

// receiptGroup is one verification round (or one harness reversion batch):
// every receipt a single Observe/RevertTo call stamped with one At value.
type receiptGroup struct {
	records  []evidence.Record
	harness  bool
	endIndex int // slice offset one past the group's last record
}

// groupReceipts splits receipts into rounds by At. All records from one
// Observe call share a timestamp (the executor stamps one time.Now() per
// call), so an At change is a round boundary. Zero timestamps (hand-written
// fixtures) get synthetic increasing times so ordering survives.
func groupReceipts(records []evidence.Record) []receiptGroup {
	var groups []receiptGroup
	synth := time.Time{}
	for i := 0; i < len(records); {
		at := records[i].At
		if at.IsZero() {
			synth = synth.Add(time.Nanosecond)
			at = synth
		}
		j := i
		harnessSeen := false
		for j < len(records) {
			cur := records[j].At
			fix := cur
			if fix.IsZero() {
				fix = at // zero timestamps stay inside the current group
			}
			if !fix.Equal(at) {
				break
			}
			if strings.HasPrefix(records[j].Command, "harness:") {
				harnessSeen = true
			}
			j++
		}
		groups = append(groups, receiptGroup{
			records:  records[i:j],
			harness:  harnessSeen,
			endIndex: j,
		})
		i = j
	}
	return groups
}

// replayGreen replays the receipts into fresh evidence ledgers and reports
// when the contract first went green. Rounds are verification batches (the
// ledger's verdict Any() is true by construction: every receipt came from a
// classified check run); harness reversion batches carry ledger state but
// do not count as verification rounds. A session that never went green
// reports the total round count plus green=false.
func replayGreen(records []evidence.Record, contract []string) (roundsToGreen, verificationRounds int, green bool, err error) {
	if len(contract) == 0 {
		return 0, 0, false, nil
	}
	groups := groupReceipts(records)
	for _, g := range groups {
		if g.harness {
			continue
		}
		verificationRounds++
		if green {
			continue // already satisfied; keep counting rounds only
		}
		residual, err := residualAfter(records[:g.endIndex], contract)
		if err != nil {
			return 0, verificationRounds, false, err
		}
		if len(residual) == 0 {
			green = true
			roundsToGreen = verificationRounds
		}
	}
	if !green {
		roundsToGreen = verificationRounds // never green: report the total
	}
	return roundsToGreen, verificationRounds, green, nil
}

// residualAfter replays a receipt prefix into a fresh ledger (via a temp
// file — the ledger's only public replay surface is NewLedger(path) +
// Load()) and judges the contract against it. O(prefix) per round; the
// scorer is offline, so clarity beats cleverness here.
func residualAfter(prefix []evidence.Record, contract []string) ([]evidence.CheckStatus, error) {
	if len(prefix) == 0 {
		// No evidence at all: every check is no-evidence, the residual set.
		l := evidence.NewLedger("")
		return l.Residual(contract), nil
	}
	tmp, err := os.CreateTemp("", "a1-eval-replay-*.jsonl")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := writeReceipts(path, prefix); err != nil {
		return nil, err
	}
	l := evidence.NewLedger(path)
	if err := l.Load(); err != nil {
		return nil, err
	}
	return l.Residual(contract), nil
}

func writeReceipts(path string, records []evidence.Record) error {
	var b strings.Builder
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func countHarnessReceipts(records []evidence.Record) int {
	n := 0
	for _, rec := range records {
		if strings.HasPrefix(rec.Command, "harness:") {
			n++
		}
	}
	return n
}

// ScoreDir scores every session in a project session dir, newest first.
func ScoreDir(dir, modelHint string) ([]EvalResult, error) {
	metas, err := session.ListSessions(dir)
	if err != nil {
		return nil, fmt.Errorf("eval: list %s: %w", dir, err)
	}
	var out []EvalResult
	for _, meta := range metas {
		res, err := ScoreFile(meta.File, modelHint)
		if err != nil {
			// One unreadable session must not sink the report; the file
			// layer already skips malformed sessions in listings.
			continue
		}
		out = append(out, res)
	}
	return out, nil
}

// Totals aggregates a set of scored sessions for the report footer.
type Totals struct {
	Sessions           int     `json:"sessions"`
	Green              int     `json:"green"`
	RoundsToGreen      int     `json:"rounds_to_green"` // sum over green sessions
	VerificationRounds int     `json:"verification_rounds"`
	Rollbacks          int     `json:"rollbacks"`
	StallStops         int     `json:"stall_stops"`
	TotalTokens        int64   `json:"total_tokens"`
	Compactions        int     `json:"compactions"`
	MeanRoundsToGreen  float64 `json:"mean_rounds_to_green"`
}

// Summarize aggregates results.
func Summarize(results []EvalResult) Totals {
	var t Totals
	for _, r := range results {
		t.Sessions++
		if r.Green {
			t.Green++
			t.RoundsToGreen += r.RoundsToGreen
		}
		t.VerificationRounds += r.VerificationRounds
		t.Rollbacks += r.Rollbacks
		t.StallStops += r.StallStops
		t.TotalTokens += r.TotalTokens
		t.Compactions += r.Compactions
	}
	if t.Green > 0 {
		t.MeanRoundsToGreen = float64(t.RoundsToGreen) / float64(t.Green)
	}
	return t
}

func ratio(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
