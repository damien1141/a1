package permission

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Gate evaluates permission requests. Check is the prospective Gate 1
// (pre-dispatch); Admit is the retrospective Gate 2 (post-execution
// admission). Neither may dispatch or execute anything itself.
type Gate interface {
	Check(ctx context.Context, req Request) (Decision, string)
	Admit(ctx context.Context, req Request) (Decision, string)
}

// AdmissionGate is an optional extension of Gate for post-execution admission checks.
type AdmissionGate interface {
	// Admit evaluates whether a completed tool execution should be admitted
	// based on output labels and effect-log requirements.
	Admit(ctx context.Context, req Request) (Decision, string)
}

// TrajectoryState tracks the APPA trajectory label L=(R,t), effect log E(ℓ),
// acceptance records, and call-scoped authority rulings.
type TrajectoryState struct {
	// Label is the current trajectory label L, modeled as a partial label
	// to support gradual security annotation with unresolved sources.
	Label PartialLabel

	// Effects is the append-only effect log E(ℓ), shared tree-wide: a child
	// branch commits into the same log the parent reads.
	Effects *EffectLog

	// AcceptedNarrowing accumulates the calls whose narrowing the user has
	// accepted. The paper records acceptance indivisibly with dispatch, so a
	// bare bool that resets on the next Admit loses the log entry that makes
	// an inadequate dispatch auditable.
	AcceptedNarrowing map[string]bool

	// Rulings is the append-only call-scoped authority ruling log.
	Rulings *RulingLog
}

// NewTrajectoryState creates an initial trajectory at the agent's working
// level: the User trust rank over the full reader universe. Seeding higher
// would make every read/write a "descent" needing acceptance, and seeding at
// the empty reader set (the lattice's most restrictive label) would narrow
// every trajectory immediately — neither is what the Prop. 2 descent bound
// measures.
func NewTrajectoryState() *TrajectoryState {
	return &TrajectoryState{
		Label: PartialLabel{
			Established: Label{Trust: User, ReaderSet: TopReaderSet},
			Unresolved:  []string{},
		},
		Effects:           NewEffectLog(),
		AcceptedNarrowing: make(map[string]bool),
		Rulings:           NewRulingLog(),
	}
}

// TopReaderSet is the universe of reader identities a fresh trajectory starts
// with. Every fold can only intersect it downward; a concrete deployment
// replaces it with its own audience universe.
var TopReaderSet = []string{"user", "agent", "tool"}

// HasAcceptedNarrowing reports whether the narrowing for callHash was accepted.
func (t *TrajectoryState) HasAcceptedNarrowing(callHash string) bool {
	if t == nil || t.AcceptedNarrowing == nil {
		return false
	}
	return t.AcceptedNarrowing[callHash]
}

// RecordNarrowingAcceptance logs that the narrowing for callHash was accepted.
func (t *TrajectoryState) RecordNarrowingAcceptance(callHash string) {
	if t == nil {
		return
	}
	if t.AcceptedNarrowing == nil {
		t.AcceptedNarrowing = make(map[string]bool)
	}
	t.AcceptedNarrowing[callHash] = true
}

// String returns a human-readable summary of the trajectory state.
func (t *TrajectoryState) String() string {
	if t == nil {
		return "trajectory=<nil>"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("L=%s", t.Label.Established))
	sb.WriteString(fmt.Sprintf(" effects=%d", t.Effects.Len()))
	sb.WriteString(fmt.Sprintf(" accepted_narrowing=%d", len(t.AcceptedNarrowing)))
	sb.WriteString(fmt.Sprintf(" rulings=%d", t.Rulings.Len()))
	return sb.String()
}

// StaticGate evaluates against a fixed Policy and workspace root, maintaining
// APPA trajectory state across tool calls.
type StaticGate struct {
	Policy    Policy
	Workspace string

	bashAllow    []*regexp.Regexp
	bashDeny     []*regexp.Regexp
	contracts    *ContractRegistry
	trajectory   *TrajectoryState
	acceptNarrow func(ctx context.Context, req Request, reason string) (bool, error)
	recovery     *RecoveryGraph
	resolver     Resolver
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// NewGate compiles policy regexes and returns a Gate.
// Empty workspace uses WorkspaceRoot().
func NewGate(policy Policy, workspace string) (*StaticGate, error) {
	if workspace == "" {
		workspace = WorkspaceRoot()
	}
	g := &StaticGate{
		Policy:     policy,
		Workspace:  workspace,
		contracts:  DefaultContracts(),
		trajectory: NewTrajectoryState(),
		recovery:   NewRecoveryGraph(),
	}
	if policy.DangerouslyAllowAll || policy.AllowAllSession {
		g.Policy.BashDefault = Allow
		g.Policy.WorkspaceOnlyWrites = false
		g.Policy.WorkspaceOnlyReads = false
		g.Policy.SensitivePathDeny = nil
		g.bashAllow = nil
		g.bashDeny = nil
		return g, nil
	}
	var err error
	g.bashAllow, err = compilePatterns(policy.BashAllow)
	if err != nil {
		return nil, fmt.Errorf("bash allow: %w", err)
	}
	g.bashDeny, err = compilePatterns(policy.BashDeny)
	if err != nil {
		return nil, fmt.Errorf("bash deny: %w", err)
	}
	return g, nil
}

// NewGateAt builds a gate whose workspace root is resolved from startDir
// instead of the process working directory. Every caller that knows the
// session cwd must use this, so a session opened in a subdirectory gates
// paths against that subdirectory rather than the repository root.
func NewGateAt(policy Policy, startDir string) (*StaticGate, error) {
	return NewGate(policy, WorkspaceRootAt(startDir))
}

// BashAllow returns the current allowlist patterns (raw strings).
func (g *StaticGate) BashAllow() []string {
	if g == nil {
		return nil
	}
	out := make([]string, 0, len(g.bashAllow))
	for _, re := range g.bashAllow {
		out = append(out, re.String())
	}
	return out
}

// SetBashAllow replaces the allowlist with the provided regex patterns.
// Patterns are compiled immediately; invalid patterns are rejected.
func (g *StaticGate) SetBashAllow(patterns []string) error {
	if g == nil {
		return errors.New("gate is nil")
	}
	re, err := compilePatterns(patterns)
	if err != nil {
		return err
	}
	g.bashAllow = re
	g.Policy.BashAllow = patterns
	return nil
}

// AppendBashAllow compiles pattern and appends it to the live allowlist.
// Returns the compiled regex on success, or an error if the pattern is invalid.
func (g *StaticGate) AppendBashAllow(pattern string) (*regexp.Regexp, error) {
	if g == nil {
		return nil, fmt.Errorf("gate is nil")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	g.bashAllow = append(g.bashAllow, re)
	g.Policy.BashAllow = append(g.Policy.BashAllow, pattern)
	return re, nil
}

// SetAcceptNarrowing installs the callback invoked when a call would narrow
// the trajectory label and requires explicit user acceptance.
func (g *StaticGate) SetAcceptNarrowing(fn func(ctx context.Context, req Request, reason string) (bool, error)) {
	if g != nil {
		g.acceptNarrow = fn
	}
}

// SetContracts replaces the contract registry.
func (g *StaticGate) SetContracts(reg *ContractRegistry) {
	if g != nil {
		g.contracts = reg
	}
}

// Trajectory returns the current trajectory state for inspection.
func (g *StaticGate) Trajectory() *TrajectoryState {
	if g == nil {
		return nil
	}
	return g.trajectory
}

// SetTrajectory replaces the current trajectory state (used to preserve APPA
// state across mode switches or gate rebuilds).
func (g *StaticGate) SetTrajectory(state *TrajectoryState) {
	if g != nil {
		g.trajectory = state
	}
}

// SetRecoveryGraph replaces the recovery graph used for post-failure search.
func (g *StaticGate) SetRecoveryGraph(r *RecoveryGraph) {
	if g != nil {
		g.recovery = r
	}
}

// RecoveryGraph returns the current recovery graph.
func (g *StaticGate) RecoveryGraph() *RecoveryGraph {
	if g == nil {
		return nil
	}
	return g.recovery
}

// SetResolver sets the authority resolver backend used when Policy.Authority
// is not configured. The resolver is consulted after the authority check.
func (g *StaticGate) SetResolver(r Resolver) {
	if g != nil {
		g.resolver = r
	}
}

// Resolver returns the current authority resolver backend.
func (g *StaticGate) Resolver() Resolver {
	if g == nil {
		return nil
	}
	return g.resolver
}

// ConsultAuthority applies Step 4 call-scoped authority when the policy requires it.
// On a terminal Allow/Deny decision, the ruling is logged to the trajectory with
// the concrete gaps it covers, per Theorem 6 ("dedicated covering rulings over
// its exact rendered form").
func (g *StaticGate) ConsultAuthority(
	ctx context.Context,
	dec Decision,
	reason string,
	req Request,
	gaps []string,
) (Decision, string) {
	if dec != Ask {
		return dec, reason
	}
	if !g.Policy.RequiresAuthority {
		return dec, reason
	}
	authority, _ := g.Policy.Authority.(Authority)
	if authority == nil {
		return dec, reason
	}
	if len(gaps) == 0 {
		// Nothing to cover: an authority ruling without a target gap would be
		// an unbounded override, which the paper's mandate model forbids.
		return dec, reason
	}
	callHash := CallHash(req)
	dec, reason = authority.Authorize(ctx, callHash, req)
	if dec == Allow || dec == Deny {
		if g.trajectory != nil && g.trajectory.Rulings != nil {
			g.trajectory.Rulings.Append(Ruling{
				CallHash:      callHash,
				AuthorityName: fmt.Sprintf("%T", authority),
				GapsCovered:   append([]string(nil), gaps...),
				Decision:      dec,
				Reason:        reason,
				Timestamp:     time.Now(),
			})
		}
		return dec, reason
	}
	return Ask, reason
}

// Prospective is the Gate 1 result for one call: the label the trajectory
// would have after folding the tool's declared contribution, plus the gaps
// that keep it from dispatching.
type Prospective struct {
	// Label is cτ(L) = L ∧ dτ.
	Label PartialLabel
	// Gaps lists the unsatisfied requirements (trust floor, recipient cover,
	// history predicates) evaluated against Prospective.Label.
	Gaps []string
	// Narrowing is true when cτ(L) < L, which requires explicit acceptance
	// or a child branch.
	Narrowing bool
	// Contract is the tool contract that produced this evaluation.
	Contract Contract
}

// EvaluateProspective computes cτ(L) = L ∧ dτ and the gaps of the contract,
// without mutating any state. APPA §4 evaluates release-side requirements and
// history predicates prospectively, and Proposition 4 shows reactive checking
// against L alone is insufficient to block composite read-and-release tools.
func (g *StaticGate) EvaluateProspective(req Request) Prospective {
	contract := g.contracts.Lookup(req.Tool)
	prospective := g.trajectory.Label.Meet(PartialLabel{
		Established: contract.Contribution,
		Unresolved:  []string{},
	})

	var gaps []string
	for _, pre := range contract.Requires {
		if ok, why := pre.Check(prospective.Established, g.trajectory.Effects); !ok {
			gaps = append(gaps, "precondition:"+why)
		}
	}

	return Prospective{
		Label:     prospective,
		Gaps:      gaps,
		Narrowing: RequiresNarrowing(g.trajectory.Label.Established, contract.Contribution),
		Contract:  contract,
	}
}

// Check evaluates req prospectively (Gate 1). It computes cτ(L), evaluates
// release requirements and history predicates against it, then hard gates.
// Gate 1 never mutates L or E.
func (g *StaticGate) Check(ctx context.Context, req Request) (Decision, string) {
	if g.Policy.DangerouslyAllowAll || g.Policy.AllowAllSession {
		return Allow, ""
	}

	// Hard gates first: they are the only source of Deny and are independent
	// of the trajectory label (APPA §4, "hard gates against the exact
	// rendered call").
	dec, reason := g.evaluate(req)
	if dec == Deny {
		return dec, reason
	}

	// Prospective evaluation is unconditional: a call that passes the
	// conventional checks must still be blocked when folding its declared
	// contribution would violate a release-side requirement.
	prospective := g.EvaluateProspective(req)

	gaps := append([]string(nil), prospective.Gaps...)
	if prospective.Narrowing && !g.trajectory.HasAcceptedNarrowing(CallHash(req)) {
		if g.acceptNarrow != nil {
			approved, err := g.acceptNarrow(ctx, req, fmt.Sprintf("narrowing %s → %s requires acceptance",
				g.trajectory.Label.Established, prospective.Contract.Contribution))
			if err != nil {
				return Ask, fmt.Sprintf("acceptance error: %v", err)
			}
			if approved {
				// Record the acceptance keyed to the exact call so the
				// dispatch log can match it (Theorem 6(iii)). The label
				// itself is only mutated at admission.
				g.trajectory.RecordNarrowingAcceptance(CallHash(req))
			}
		}
		if !g.trajectory.HasAcceptedNarrowing(CallHash(req)) {
			gaps = append(gaps, "narrowing")
		}
	}

	dec, reason = g.foldMode(dec, reason, req)
	if dec == Deny {
		return dec, reason
	}
	if dec != Ask {
		// Allowed by the conventional checks. A policy-authorized narrowing
		// does not need a prompt — the allowlist *is* the operator's standing
		// authorization — but a prospective requirement gap still blocks.
		if len(prospective.Gaps) > 0 {
			return Ask, fmt.Sprintf(
				"%s; recovery: %s",
				describeGaps(prospective.Gaps),
				g.describeRecovery(ctx, req, prospective.Gaps),
			)
		}
		return Allow, reason
	}

	if len(gaps) > 0 {
		return Ask, fmt.Sprintf("%s; recovery: %s", describeGaps(gaps), g.describeRecovery(ctx, req, gaps))
	}
	return Ask, reason
}

// describeGaps renders the blocking gaps for the operator-facing reason.
func describeGaps(gaps []string) string {
	if len(gaps) == 0 {
		return ""
	}
	return "blocks: " + strings.Join(gaps, ", ")
}

func (g *StaticGate) describeRecovery(ctx context.Context, req Request, gaps []string) string {
	if g.recovery == nil {
		return "no recovery graph configured"
	}
	state := RecoveryState{
		Label:   g.trajectory.Label,
		Support: g.trajectory.Effects.Support(),
		Gaps:    gaps,
	}
	contract := g.contracts.Lookup(req.Tool)
	paths := g.recovery.Search(ctx, state, contract, g.recovery.MaxPaths())
	if len(paths) == 0 {
		return "no recovery paths available"
	}
	var sb strings.Builder
	sb.WriteString("available recovery paths:")
	for i, p := range paths {
		sb.WriteString(fmt.Sprintf(" [%d] %s", i+1, p.String()))
	}
	return sb.String()
}

// Admit evaluates a completed execution (Gate 2): it verifies the realized
// output against its declared label, then folds the contribution and commits
// the declared effect tokens. Gate 2 is the only place L and E mutate.
//
// req.Succeeded carries the reported outcome. APPA §3: "failed or
// indeterminate outcomes contribute none", so effect tokens are committed only
// on success. An unannotated output (no reader set, no trust) is recorded as an
// unresolved source rather than silently adopting the declared contribution.
func (g *StaticGate) Admit(_ context.Context, req Request) (Decision, string) {
	if g.Policy.DangerouslyAllowAll || g.Policy.AllowAllSession {
		return Allow, ""
	}

	contract := g.contracts.Lookup(req.Tool)

	// Realized-return verification (Fig. 1 Gate 2): the value about to enter
	// context must satisfy the policy's output constraints before it folds.
	if g.Policy.NoUntrustedOutput && req.OutputLabel.Trust == Untrusted {
		return Deny, "untrusted output denied by policy"
	}
	// An output whose trust is off the chain is not automatically unadmittable:
	// it is an unannotated result, which flows as an unresolved source until a
	// consuming check resolves it. Only a policy that explicitly forbids
	// unverified labels blocks it here.
	if g.Policy.RequireLabeledOutput && !IsKnownTrust(req.OutputLabel.Trust) {
		return Ask, "realized output is unlabeled; admission requires verification or a cast"
	}

	if len(g.Policy.RequiredEffects) > 0 {
		if err := g.trajectory.Effects.CheckRequiredEffects(g.Policy.RequiredEffects); err != nil {
			return Ask, fmt.Sprintf("admission requires effect-log verification: %v", err)
		}
	}

	// Fold the contribution into the trajectory label and commit Kτ — only
	// when the tool reported success.
	unannotated := !IsKnownTrust(contract.Contribution.Trust) && len(contract.Contribution.ReaderSet) == 0
	if unannotated {
		// The tool declares no output label: the realized output enters the
		// partial label as an unresolved source instead of assuming a default.
		g.trajectory.Label = g.trajectory.Label.AddUnresolved(outputSourceOf(req.Tool))
	} else {
		g.trajectory.Label = g.trajectory.Label.Meet(PartialLabel{
			Established: contract.Contribution,
			Unresolved:  []string{},
		})
	}
	if req.Succeeded {
		for _, token := range contract.EffectTokens {
			g.trajectory.Effects.Commit(req.Tool, token)
		}
	}
	return Allow, ""
}

// ApproveAdmit folds the tool's contribution into the trajectory label and
// commits effect tokens after an admission Ask is approved by the user.
func (g *StaticGate) ApproveAdmit(req Request) {
	if g == nil {
		return
	}
	contract := g.contracts.Lookup(req.Tool)
	g.trajectory.Label = g.trajectory.Label.Meet(PartialLabel{
		Established: contract.Contribution,
		Unresolved:  []string{},
	})
	if req.Succeeded {
		for _, token := range contract.EffectTokens {
			g.trajectory.Effects.Commit(req.Tool, token)
		}
	}
}

// outputSourceOf names the unresolved source contributed by an unannotated
// tool's realized output.
func outputSourceOf(tool string) string {
	return "out:" + strings.TrimSpace(tool)
}

// evaluate runs the conventional mode-independent permission checks.
func (g *StaticGate) evaluate(req Request) (Decision, string) {
	switch req.Action {
	case ActionBash:
		return g.checkBash(req)
	case ActionWrite, ActionEdit:
		return g.checkWrite(req)
	case ActionRead, ActionGrep, ActionFind, ActionLs:
		return g.checkRead(req)
	case ActionAgent:
		return Allow, ""
	default:
		return Ask, fmt.Sprintf("unknown action %q requires approval", req.Action)
	}
}

// checkBash evaluates bash commands against allow/deny lists.
func (g *StaticGate) checkBash(req Request) (Decision, string) {
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		return Deny, "empty bash command denied"
	}
	for _, re := range g.bashDeny {
		if re.MatchString(cmd) {
			return Deny, "bash denied by policy: matches " + re.String()
		}
	}
	// Allowlist only applies to a single simple command. Prefix matches like
	// ^ls\b must not green-light "ls && rm -rf …".
	if bashEligibleForAllowlist(cmd) {
		for _, re := range g.bashAllow {
			if re.MatchString(cmd) {
				return Allow, ""
			}
		}
	}
	def := g.Policy.BashDefault
	if def == Allow {
		return Allow, ""
	}
	if def == Deny {
		return Deny, "bash denied by default policy"
	}
	return Ask, "bash requires approval: " + truncate(cmd, 120)
}

// checkWrite evaluates write/edit requests.
func (g *StaticGate) checkWrite(req Request) (Decision, string) {
	if len(req.Paths) == 0 {
		return Deny, "write/edit without path denied"
	}
	for _, p := range req.Paths {
		if IsSensitivePath(p, g.Policy.SensitivePathDeny) {
			return Deny, "write to sensitive path denied: " + p
		}
		if g.Policy.WorkspaceOnlyWrites && !InWorkspace(p, g.Workspace) {
			return Ask, "write outside workspace: " + p
		}
	}
	return Allow, ""
}

// checkRead evaluates read/grep/find/ls requests.
func (g *StaticGate) checkRead(req Request) (Decision, string) {
	if len(req.Paths) == 0 {
		// grep/find with default "." is normalized by extract; empty = allow cwd
		return Allow, ""
	}
	for _, p := range req.Paths {
		if IsSensitivePath(p, g.Policy.SensitivePathDeny) {
			return Deny, "read of sensitive path denied: " + p
		}
		if g.Policy.WorkspaceOnlyReads && !InWorkspace(p, g.Workspace) {
			return Ask, "read outside workspace: " + p
		}
	}
	return Allow, ""
}

// foldMode applies mode-specific folding to a raw decision.
func (g *StaticGate) foldMode(dec Decision, reason string, req Request) (Decision, string) {
	mode := g.Policy.Mode
	if mode == "" {
		mode = ModeInteractive
	}

	switch mode {
	case ModeInteractive:
		return dec, reason

	case ModeReadonly:
		if isMutating(req.Action) {
			// Allow bash only if it already matched allowlist (dec==Allow for bash).
			if req.Action == ActionBash && dec == Allow {
				return Allow, reason
			}
			if dec == Allow || dec == Ask {
				return Deny, readonlyReason(req, reason)
			}
		}
		if dec == Ask {
			return Deny, askFoldReason(reason, mode)
		}
		return dec, reason

	case ModeYolo:
		// Only Ask folds. An explicit Deny — sudo, rm -rf, curl|sh — stays
		// denied, so "stop asking me" never becomes "disable the safety net".
		// This is the whole reason yolo is a mode rather than another
		// allow-all flag: the flags short-circuit before the gate runs, which
		// would take the deny list down with them.
		if dec == Ask {
			return Allow, ""
		}
		return dec, reason

	case ModeAutopilot, ModeHeadlessStrict:
		if dec == Ask {
			return Deny, askFoldReason(reason, mode)
		}
		return dec, reason

	default:
		return dec, reason
	}
}

func isMutating(a Action) bool {
	switch a {
	case ActionWrite, ActionEdit, ActionBash:
		return true
	default:
		return false
	}
}

func readonlyReason(req Request, fallback string) string {
	if fallback != "" && !strings.Contains(fallback, "requires approval") {
		return fallback
	}
	return fmt.Sprintf("readonly mode denies %s", req.Action)
}

func askFoldReason(reason string, mode Mode) string {
	if reason == "" {
		return fmt.Sprintf("%s mode denies operations that would require approval", mode)
	}
	return fmt.Sprintf("%s mode: %s", mode, reason)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// AllowAll is a Gate that always allows (tests / nil-policy fallback).
type AllowAll struct{}

// Check always returns Allow.
func (AllowAll) Check(context.Context, Request) (Decision, string) {
	return Allow, ""
}

// Admit always returns Allow.
func (AllowAll) Admit(context.Context, Request) (Decision, string) {
	return Allow, ""
}
