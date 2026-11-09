package agent

import (
        "encoding/json"
        "path/filepath"
        "sort"
        "strconv"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm"
)

// The verify-on-stop gate (stolen from hermes-agent's verification_stop.py
// and turn_stop_gates.py): the passive evidence ledger records what ran, the
// zero-rank stop judges the finish against what the model DECLARED — and
// this gate covers the undeclared case. A model that edits code and tries
// to finish without a single check passing since the edits is proposing an
// unverified "done". The harness turns that answer into an interim row plus
// a synthetic nudge and re-opens the turn, capped at
// defaultVerifyNudgeBudget nudges; after the cap the model's answer stands
// (the gate is a brake, not a wall — the residual state stays visible in
// the ledger either way).

// defaultVerifyNudgeBudget is how many times the gate may re-open a turn
// for verification before the model's answer stands as given. Hermes caps
// at 2; a third nudge against a model that has twice declined to verify
// buys nothing the user has not already seen.
const defaultVerifyNudgeBudget = 2

// maxChangedPathsInNudge bounds the path list in the nudge text; beyond it
// the note summarizes ("... and N more") instead of scrolling the model's
// context with a generated-files wall.
const maxChangedPathsInNudge = 8

// MutationFact is one successful workspace-mutating tool call: the path a
// file-shaped tool reported (write/edit/git and friends carry a "path"
// argument), stamped when the call completed. Bash-style mutators without a
// path argument are deliberately invisible here — a shell command's file
// effects cannot be attributed without semantic analysis, and counting
// every bash run (grep included) as an edit would fire the gate on
// read-only turns (hermes tracks file-edit tools only, for the same
// reason).
type MutationFact struct {
        Tool string
        Path string
        At   time.Time
}

// recordMutation folds one successful non-read-only call into the fact
// list. Caller guarantees the call succeeded; a failed or rejected write
// mutated nothing.
func (e *Executor) recordMutation(call llm.ToolCall, args json.RawMessage) {
        if e == nil {
                return
        }
        var in struct {
                Path string `json:"path"`
        }
        if err := json.Unmarshal(args, &in); err != nil {
                return
        }
        path := strings.TrimSpace(in.Path)
        if path == "" {
                return
        }
        e.mutationMu.Lock()
        e.mutations = append(e.mutations, MutationFact{Tool: call.Function.Name, Path: path, At: time.Now()})
        e.mutationMu.Unlock()
}

// DrainMutations returns and clears the mutation facts collected during the
// last executor.Run. The engine folds them into the turn's verify state
// after each batch, and discards stale ones at Loop entry — the same
// contract as DrainEvidenceVerdicts.
func (e *Executor) DrainMutations() []MutationFact {
        if e == nil {
                return nil
        }
        e.mutationMu.Lock()
        defer e.mutationMu.Unlock()
        facts := e.mutations
        e.mutations = nil
        return facts
}

// turnVerifyState is the per-turn record of workspace code mutations: which
// paths were touched and when the last one landed. The "when" is what
// freshness is judged against — a check that passed before the last edit
// describes the code as it was, not the code as it stands. Reset at every
// Loop entry, like the turn contract.
type turnVerifyState struct {
        // paths holds the last fact per mutated path (dedup; the nudge lists
        // paths, not edits).
        paths map[string]MutationFact
        // lastAt is the completion time of the most recent mutation.
        lastAt time.Time
}

func (t *turnVerifyState) reset() {
        if t == nil {
                return
        }
        t.paths = nil
        t.lastAt = time.Time{}
}

// fold merges drained executor facts into the turn state.
func (t *turnVerifyState) fold(facts []MutationFact) {
        if t == nil || len(facts) == 0 {
                return
        }
        if t.paths == nil {
                t.paths = make(map[string]MutationFact)
        }
        for _, f := range facts {
                t.paths[f.Path] = f
                if f.At.After(t.lastAt) {
                        t.lastAt = f.At
                }
        }
}

// codePaths returns the sorted, deduplicated mutated paths that are code.
// Prose-only turns are exempt from the gate: a SKILL.md or LICENSE edit has
// no runtime behavior to verify and must never demand a verification round
// (hermes-agent's _NON_CODE_VERIFY_* sets).
func (t *turnVerifyState) codePaths() []string {
        if t == nil {
                return nil
        }
        var out []string
        for p := range t.paths {
                if !isNonCodePath(p) {
                        out = append(out, p)
                }
        }
        sort.Strings(out)
        return out
}

var nonCodeVerifyExtensions = map[string]bool{
        ".md": true, ".markdown": true, ".mdx": true, ".rst": true, ".txt": true,
        ".text": true, ".adoc": true, ".asciidoc": true, ".org": true, ".log": true,
        ".csv": true, ".tsv": true,
}

var nonCodeVerifyFilenames = map[string]bool{
        "license": true, "licence": true, "notice": true, "authors": true,
        "contributors": true, "changelog": true, "codeowners": true,
}

// isNonCodePath reports whether a mutated path is documentation or data
// with no verifiable runtime behavior. Extension-less prose filenames
// (LICENSE, NOTICE) are checked case-insensitively.
func isNonCodePath(p string) bool {
        ext := strings.ToLower(filepath.Ext(p))
        if nonCodeVerifyExtensions[ext] {
                return true
        }
        if ext == "" {
                return nonCodeVerifyFilenames[strings.ToLower(filepath.Base(p))]
        }
        return false
}

// verifyStopCheck evaluates the undeclared finish at the moment the model
// stops calling tools. Empty note = the finish stands. A non-empty note
// re-opens the turn: the engine appends it as a user-role system note (the
// model's answer already streamed and persisted — it stays visible as the
// interim answer, hermes' pending_verification_response made mechanical).
//
// The gate fires only when ALL of these hold:
//
//   - the turn's contract is empty — a declared finish line is the
//     zero-rank stop's to judge, and it already did;
//   - the session is governed — the same chat-mode ergonomics latch as the
//     evidence surfaces: an ungoverned session records nothing, and a
//     casual "fix this typo" chat turn must not suddenly demand a test
//     run;
//   - the turn mutated at least one code path (prose exempt);
//   - verification is stale: no check has passed since the last mutation,
//     or some check is failing right now.
//
// After defaultVerifyNudgeBudget nudges the answer stands: the gate never
// hard-stops an unwilling model, it just refuses to let an unverified
// "done" pass silently.
func (engine *Engine) verifyStopCheck(nudges *int) string {
        if engine == nil || engine.evidence == nil {
                return ""
        }
        // A declared contract owns the finish line.
        if len(engine.turnContract.snapshot()) > 0 {
                return ""
        }
        // Chat-mode ergonomics: ungoverned sessions get no verification
        // nudges (same latch as evidence recording).
        if !engine.IsGoverned() {
                return ""
        }
        paths := engine.turnVerify.codePaths()
        if len(paths) == 0 {
                return ""
        }
        failing := failingRecords(engine.evidence.State())
        fresh := engine.evidence.LastPassAt().After(engine.turnVerify.lastAt)
        if fresh && len(failing) == 0 {
                return ""
        }
        if nudges == nil || *nudges >= defaultVerifyNudgeBudget {
                // Budget spent: the model's answer stands as given.
                return ""
        }
        *nudges++
        return verifyNudgeNote(paths, failing, *nudges, defaultVerifyNudgeBudget)
}

// failingRecords filters ledger state down to the currently-failing checks.
func failingRecords(state []evidence.Record) []evidence.Record {
        var out []evidence.Record
        for _, r := range state {
                if r.Status == evidence.Fail {
                        out = append(out, r)
                }
        }
        return out
}

// verifyNudgeNote renders the re-open note in the system-note voice the
// other evidence gates use.
func verifyNudgeNote(paths []string, failing []evidence.Record, used, budget int) string {
        var b strings.Builder
        b.WriteString("System note: [verify] the turn cannot end yet — code was edited this turn and no check has passed since the edits (verification nudge ")
        b.WriteString(strconv.Itoa(used))
        b.WriteString(" of ")
        b.WriteString(strconv.Itoa(budget))
        b.WriteString("):")
        b.WriteString(formatChangedPaths(paths))
        if len(failing) > 0 {
                b.WriteString("\nCurrently failing checks:")
                b.WriteString(formatChecks(failing))
                b.WriteString("\nFix these failures, then re-run the checks.")
        } else {
                b.WriteString("\nRun the relevant checks now (go build, go vet, go test ./..., or the project's suite), read any failure, repair the code, and summarize what passed.")
        }
        b.WriteString("\nIf verification is not possible, explain the concrete blocker instead of claiming the work is verified.")
        return b.String()
}

// formatChangedPaths lists up to maxChangedPathsInNudge paths, then
// summarizes the rest.
func formatChangedPaths(paths []string) string {
        var b strings.Builder
        b.WriteString("\nChanged paths:")
        shown := paths
        if len(shown) > maxChangedPathsInNudge {
                shown = shown[:maxChangedPathsInNudge]
        }
        for _, p := range shown {
                b.WriteString("\n- " + p)
        }
        if len(paths) > maxChangedPathsInNudge {
                b.WriteString("\n- ... and " + strconv.Itoa(len(paths)-maxChangedPathsInNudge) + " more")
        }
        return b.String()
}
