package permission

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Gate evaluates permission requests. It has no side effects; Ask is handled by the caller.
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
// and call-scoped authority rulings.
type TrajectoryState struct {
	// Label is the current trajectory label L, modeled as a partial label
	// to support gradual security annotation with unresolved sources.
	Label PartialLabel

	// Effects is the append-only effect log E(ℓ).
	Effects *EffectLog

	// AcceptedNarrowing records whether the current narrowing has been accepted.
	// A call that would narrow L requires explicit acceptance before dispatch.
	AcceptedNarrowing bool

	// Rulings is the append-only call-scoped authority ruling log.
	Rulings *RulingLog
}

// NewTrajectoryState creates an initial trajectory with the top label.
func NewTrajectoryState() *TrajectoryState {
	return &TrajectoryState{
		Label: PartialLabel{
			Established: Label{Trust: Trusted, ReaderSet: []string{}},
			Unresolved:  []string{},
		},
		Effects: NewEffectLog(),
		Rulings: NewRulingLog(),
	}
}

// String returns a human-readable summary of the trajectory state.
func (t *TrajectoryState) String() string {
	if t == nil {
		return "trajectory=<nil>"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("L=%s", t.Label.Established))
	sb.WriteString(fmt.Sprintf(" effects=%d", t.Effects.Len()))
	sb.WriteString(fmt.Sprintf(" accepted_narrowing=%v", t.AcceptedNarrowing))
	sb.WriteString(fmt.Sprintf(" rulings=%d", t.Rulings.Len()))
	return sb.String()
}

// StaticGate evaluates against a fixed Policy and workspace root, maintaining
// APPA trajectory state across tool calls.
type StaticGate struct {
	Policy    Policy
	Workspace string

	bashAllow     []*regexp.Regexp
	bashDeny      []*regexp.Regexp
	contracts     *ContractRegistry
	trajectory    *TrajectoryState
	acceptNarrow  func(ctx context.Context, req Request, reason string) (bool, error)
	recovery      *RecoveryGraph
	resolver      Resolver
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
		return fmt.Errorf("gate is nil")
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

// consultAuthority applies Step 4 call-scoped authority when the policy requires it.
// On a terminal Allow/Deny decision, the ruling is logged to the trajectory.
func (g *StaticGate) ConsultAuthority(
	ctx context.Context,
	dec Decision,
	reason string,
	req Request,
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
	callHash := CallHash(req)
	dec, reason = authority.Authorize(ctx, callHash, req)
	if dec == Allow || dec == Deny {
		if g.trajectory != nil && g.trajectory.Rulings != nil {
			g.trajectory.Rulings.Append(Ruling{
				CallHash:      callHash,
				AuthorityName: fmt.Sprintf("%T", authority),
				GapsCovered:   []string{"consult"},
				Decision:      dec,
				Reason:        reason,
				Timestamp:     time.Now(),
			})
		}
		return dec, reason
	}
	return Ask, reason
}

// Check evaluates req prospectively (Gate 1): computes cτ(L), evaluates release
// requirements and history predicates, and determines whether the call may dispatch.
func (g *StaticGate) Check(ctx context.Context, req Request) (Decision, string) {
	if g.Policy.DangerouslyAllowAll || g.Policy.AllowAllSession {
		return Allow, ""
	}
	_ = ctx
	dec, reason := g.evaluate(req)
	dec, reason = g.foldMode(dec, reason, req)
	if dec != Ask {
		return dec, reason
	}

	// Prospective APPA check: evaluate release requirements and history predicates.
	contract := g.contracts.Lookup(req.Tool)
	prospective := g.trajectory.Label.Meet(PartialLabel{
		Established: contract.Contribution,
		Unresolved:  []string{},
	})

	// Check preconditions from contract.
	var gaps []string
	for _, pre := range contract.Requires {
		if ok, why := pre.Check(prospective.Established, g.trajectory.Effects); !ok {
			gaps = append(gaps, fmt.Sprintf("precondition:%s", why))
		}
	}

	// Check for unaccepted narrowing.
	if RequiresNarrowing(g.trajectory.Label.Established, contract.Contribution) {
		if !g.trajectory.AcceptedNarrowing {
			if g.acceptNarrow != nil {
				approved, err := g.acceptNarrow(ctx, req, fmt.Sprintf("narrowing %s → %s requires acceptance",
					g.trajectory.Label.Established, contract.Contribution))
				if err != nil {
					return Ask, fmt.Sprintf("acceptance error: %v", err)
				}
				if approved {
					g.trajectory.AcceptedNarrowing = true
					return Ask, reason
				}
			}
			gaps = append(gaps, "narrowing")
		}
	}

	if len(gaps) > 0 {
		reason = fmt.Sprintf("%s; recovery: %s", reason, g.describeRecovery(ctx, req, gaps))
		return Ask, reason
	}

	return dec, reason
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
	paths := g.recovery.Search(ctx, state, contract, 3)
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

// Admit evaluates whether a completed execution should be admitted based on
// realized output labels and effect-log requirements. On admission the trajectory
// label is folded and effect tokens are committed.
func (g *StaticGate) Admit(_ context.Context, req Request) (Decision, string) {
	if g.Policy.DangerouslyAllowAll || g.Policy.AllowAllSession {
		return Allow, ""
	}

	out := req.OutputLabel
	contract := g.contracts.Lookup(req.Tool)

	// Unannotated / unknown output without effects: admit lazily.
	if out.Trust == Unknown && len(out.EffectTokens) == 0 && len(contract.EffectTokens) == 0 {
		// Fold the contribution (admit the tool's declared label).
		g.trajectory.Label = g.trajectory.Label.Meet(PartialLabel{
			Established: contract.Contribution,
			Unresolved:  []string{},
		})
		for _, token := range contract.EffectTokens {
			g.trajectory.Effects.Commit(req.Tool, token)
		}
		g.trajectory.AcceptedNarrowing = false
		return Allow, ""
	}

	// If policy requires no untrusted output, enforce it.
	if g.Policy.NoUntrustedOutput && out.Trust == Untrusted {
		return Deny, "untrusted output denied by policy"
	}

	// Enforce required effects.
	if len(g.Policy.RequiredEffects) > 0 {
		if err := g.trajectory.Effects.CheckRequiredEffects(g.Policy.RequiredEffects); err != nil {
			return Ask, fmt.Sprintf("admission requires effect-log verification: %v", err)
		}
	}

	// Admission passes: fold contribution into trajectory label and commit effects.
	g.trajectory.Label = g.trajectory.Label.Meet(PartialLabel{
		Established: contract.Contribution,
		Unresolved:  []string{},
	})
	for _, token := range contract.EffectTokens {
		g.trajectory.Effects.Commit(req.Tool, token)
	}
	g.trajectory.AcceptedNarrowing = false
	return Allow, ""
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
	for _, token := range contract.EffectTokens {
		g.trajectory.Effects.Commit(req.Tool, token)
	}
	g.trajectory.AcceptedNarrowing = false
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
