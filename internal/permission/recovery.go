package permission

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// RecoveryState captures the APPA state relevant to recovering a blocked call.
// It corresponds to the paper's (σ, Γ) where σ = (L, E) and Γ is the residual
// gap set for the blocked call C.
type RecoveryState struct {
	// Label is the current trajectory label L.
	Label PartialLabel
	// Support is the committed-effect projection E(ℓ) as a set.
	Support []string
	// Gaps is the residual gap set Γ for the blocked call C.
	Gaps []string
}

// String returns a human-readable recovery state.
func (r RecoveryState) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("L=%s E=%v gaps=[", r.Label.Established, r.Support))
	for i, g := range r.Gaps {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(g)
	}
	sb.WriteString("]")
	return sb.String()
}

// TransitionKind identifies the type of recovery transition.
type TransitionKind string

const (
	// TransitionAdmit admits a tool and folds its contribution.
	TransitionAdmit TransitionKind = "admit"
	// TransitionTransform applies a cast or sanitizer.
	TransitionTransform TransitionKind = "transform"
	// TransitionBranch executes the narrowing call in a child branch.
	TransitionBranch TransitionKind = "branch"
	// TransitionRule consumes an authority ruling for a specific gap.
	TransitionRule TransitionKind = "rule"
	// TransitionAccept records narrowing acceptance.
	TransitionAccept TransitionKind = "accept"
)

// Transition represents one step in a recovery path.
type Transition struct {
	// Kind is the transition type.
	Kind TransitionKind
	// Tool is the tool involved (for admit/branch transitions).
	Tool string
	// Gap is the gap addressed (for rule/accept transitions).
	Gap string
	// Description is a human-readable explanation.
	Description string
}

// String returns a human-readable transition description.
func (t Transition) String() string {
	switch t.Kind {
	case TransitionAdmit:
		return fmt.Sprintf("admit %s", t.Tool)
	case TransitionTransform:
		return t.Description
	case TransitionBranch:
		return fmt.Sprintf("fork + %s", t.Description)
	case TransitionRule:
		return fmt.Sprintf("authority ruling: %s", t.Gap)
	case TransitionAccept:
		return "accept narrowing"
	default:
		return string(t.Kind)
	}
}

// RecoveryPath is an ordered list of transitions that clears all gaps.
type RecoveryPath struct {
	Transitions []Transition
}

// String returns a human-readable path description.
func (p RecoveryPath) String() string {
	var sb strings.Builder
	for i, t := range p.Transitions {
		if i > 0 {
			sb.WriteString(" → ")
		}
		sb.WriteString(t.String())
	}
	return sb.String()
}

// Empty reports whether the path has no transitions.
func (p RecoveryPath) Empty() bool {
	return len(p.Transitions) == 0
}

// Len returns the number of transitions.
func (p RecoveryPath) Len() int {
	return len(p.Transitions)
}

// RecoveryGraph is the finite recovery graph GC from the paper.
// It explores state transitions from a blocked call's initial state
// to find paths that clear all gaps.
type RecoveryGraph struct {
	// MaxDepth bounds the BFS search depth.
	MaxDepth int
	// contracts is the tool contract registry.
	contracts *ContractRegistry
	// authorities is the set of registered authorities.
	authorities map[string]Authority
	// casts is the set of registered cast resolvers.
	casts map[string]CastResolver
}

// NewRecoveryGraph creates an empty recovery graph.
func NewRecoveryGraph() *RecoveryGraph {
	return &RecoveryGraph{
		MaxDepth:    10,
		contracts:   DefaultContracts(),
		authorities: make(map[string]Authority),
		casts:       make(map[string]CastResolver),
	}
}

// SetContracts replaces the contract registry.
func (g *RecoveryGraph) SetContracts(reg *ContractRegistry) {
	if g != nil {
		g.contracts = reg
	}
}

// RegisterAuthority adds an authority by name.
func (g *RecoveryGraph) RegisterAuthority(name string, a Authority) {
	if g != nil && g.authorities != nil {
		g.authorities[name] = a
	}
}

// Authorities returns the names of all registered authorities.
func (g *RecoveryGraph) Authorities() []string {
	if g == nil {
		return nil
	}
	out := make([]string, 0, len(g.authorities))
	for name := range g.authorities {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// String returns a human-readable summary of the recovery graph.
func (g *RecoveryGraph) String() string {
	if g == nil {
		return "recovery=<nil>"
	}
	return fmt.Sprintf("recovery: max_depth=%d authorities=%d", g.MaxDepth, len(g.authorities))
}

// RegisterCast adds a cast resolver by source name.
func (g *RecoveryGraph) RegisterCast(c CastResolver) {
	if g != nil && g.casts != nil {
		g.casts[c.Source] = c
	}
}

// Search finds recovery paths from initial state that clear all gaps.
// It returns up to maxPaths distinct paths. The search is bounded by
// MaxDepth and terminates with an empty result if no path exists.
func (g *RecoveryGraph) Search(
	ctx context.Context,
	initial RecoveryState,
	contract Contract,
	maxPaths int,
) []RecoveryPath {
	if g == nil {
		return nil
	}
	if maxPaths <= 0 {
		maxPaths = 1
	}

	// Fast path: no gaps means already admissible.
	if len(initial.Gaps) == 0 {
		return nil
	}

	// BFS over (state, transitions-so-far).
	type frame struct {
		state   RecoveryState
		path    []Transition
		visited map[string]bool
	}

	queue := []frame{{
		state:   initial,
		path:    nil,
		visited: map[string]bool{g.key(initial): true},
	}}

	var results []RecoveryPath

	for len(queue) > 0 && len(results) < maxPaths {
		f := queue[0]
		queue = queue[1:]

		if len(f.path) >= g.MaxDepth {
			continue
		}

		// Generate successor transitions.
		successors := g.successors(ctx, f.state, contract)

		for _, succ := range successors {
			newState := succ.NewState
			newPath := append(append([]Transition(nil), f.path...), succ.Transition)
			stateKey := g.key(newState)

			if f.visited[stateKey] {
				continue
			}

			newVisited := make(map[string]bool, len(f.visited)+1)
			newVisited[stateKey] = true
			for k, v := range f.visited {
				newVisited[k] = v
			}

			// Check if all gaps are cleared.
			if len(newState.Gaps) == 0 {
				results = append(results, RecoveryPath{Transitions: newPath})
				if len(results) >= maxPaths {
					break
				}
				continue
			}

			queue = append(queue, frame{
				state:   newState,
				path:    newPath,
				visited: newVisited,
			})
		}
	}

	return results
}

type successor struct {
	Transition Transition
	NewState   RecoveryState
}

func (g *RecoveryGraph) successors(ctx context.Context, state RecoveryState, contract Contract) []successor {
	var out []successor

	// TransitionAccept: clear narrowing gap.
	for i, gap := range state.Gaps {
		if gap == "narrowing" {
			newGaps := append([]string(nil), state.Gaps...)
			newGaps = append(newGaps[:i], newGaps[i+1:]...)
			out = append(out, successor{
				Transition: Transition{Kind: TransitionAccept},
				NewState: RecoveryState{
					Label:   state.Label,
					Support: append([]string(nil), state.Support...),
					Gaps:    newGaps,
				},
			})
			break
		}
	}

	// TransitionRule: consume authority ruling for a specific gap.
	for i, gap := range state.Gaps {
		if gap == "narrowing" {
			continue // narrowing requires accept, not rule
		}
		for name, auth := range g.authorities {
			req := Request{Tool: contract.Tool}
			dec, _ := auth.Authorize(ctx, fmt.Sprintf("rule-%s-%d", name, i), req)
			if dec == Allow {
				newGaps := append([]string(nil), state.Gaps...)
				newGaps = append(newGaps[:i], newGaps[i+1:]...)
				out = append(out, successor{
					Transition: Transition{
						Kind: TransitionRule,
						Gap:  gap,
						Description: fmt.Sprintf("authority %s approves %s", name, gap),
					},
					NewState: RecoveryState{
						Label:   state.Label,
						Support: append([]string(nil), state.Support...),
						Gaps:    newGaps,
					},
				})
				break
			}
		}
	}

	// TransitionAdmit: admit the tool and fold contribution.
	if contract.EffectTokens != nil || contract.Contribution.Trust != Unknown {
		newLabel := state.Label.Meet(PartialLabel{
			Established: contract.Contribution,
			Unresolved:  []string{},
		})
		newSupport := append([]string(nil), state.Support...)
		for _, token := range contract.EffectTokens {
			newSupport = append(newSupport, token)
		}
		// Recompute gaps after label change.
		newGaps := g.computeGaps(newLabel, newSupport, contract)
		out = append(out, successor{
			Transition: Transition{
				Kind: TransitionAdmit,
				Tool: contract.Tool,
				Description: fmt.Sprintf("admit %s → label=%s", contract.Tool, newLabel.Established),
			},
			NewState: RecoveryState{
				Label:   newLabel,
				Support: newSupport,
				Gaps:    newGaps,
			},
		})
	}

	// TransitionTransform: apply cast/sanitizer.
	for src, cast := range g.casts {
		if cast.Resolve == nil {
			continue
		}
		label, err := cast.ResolveCast(src)
		if err != nil {
			continue
		}
		newLabel := state.Label.Meet(PartialLabel{
			Established: label,
			Unresolved:  []string{},
		})
		newGaps := g.computeGaps(newLabel, state.Support, contract)
		out = append(out, successor{
			Transition: Transition{
				Kind: TransitionTransform,
				Description: fmt.Sprintf("cast %s → %s", src, label),
			},
			NewState: RecoveryState{
				Label:   newLabel,
				Support: append([]string(nil), state.Support...),
				Gaps:    newGaps,
			},
		})
	}

	// TransitionBranch: execute narrowing in child (parent label unchanged).
	if hasGapKind(state.Gaps, "narrowing") {
		newGaps := append([]string(nil), state.Gaps...)
		for i, g := range newGaps {
			if g == "narrowing" {
				newGaps = append(newGaps[:i], newGaps[i+1:]...)
				break
			}
		}
		out = append(out, successor{
			Transition: Transition{
				Kind: TransitionBranch,
				Description: "fork child branch: Lc := Lp, execute narrowing locally",
			},
			NewState: RecoveryState{
				Label:   state.Label,
				Support: append([]string(nil), state.Support...),
				Gaps:    newGaps,
			},
		})
	}

	return out
}

func (g *RecoveryGraph) key(state RecoveryState) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("L:%d:%d|", state.Label.Established.Trust, len(state.Label.Established.ReaderSet)))
	sb.WriteString(fmt.Sprintf("E:%d|", len(state.Support)))
	for _, gap := range state.Gaps {
		sb.WriteString(gap)
		sb.WriteString(",")
	}
	return sb.String()
}

func (g *RecoveryGraph) computeGaps(label PartialLabel, support []string, contract Contract) []string {
	// Compute residual gaps for a contract given current label and support.
	// This is a simplified version that checks preconditions.
	var gaps []string
	prospective := label.Meet(PartialLabel{
		Established: contract.Contribution,
		Unresolved:  []string{},
	})
	log := NewEffectLog()
	for _, token := range support {
		log.Commit("", token)
	}
	for _, pre := range contract.Requires {
		ok, _ := pre.Check(prospective.Established, log)
		if !ok {
			gaps = append(gaps, string(pre.Kind))
		}
	}
	if RequiresNarrowing(label.Established, contract.Contribution) {
		gaps = append(gaps, "narrowing")
	}
	return gaps
}

func hasGapKind(gaps []string, kind string) bool {
	for _, g := range gaps {
		if g == kind || strings.HasPrefix(g, kind+":") {
			return true
		}
	}
	return false
}

// RecoverySearchResult bundles the found paths with explanatory text.
type RecoverySearchResult struct {
	// Paths is the list of recovery paths found.
	Paths []RecoveryPath
	// Message is a human-readable summary.
	Message string
}

// String returns a formatted summary of the recovery result.
func (r RecoverySearchResult) String() string {
	if len(r.Paths) == 0 {
		return "no recovery path available"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d recovery path(s) available:\n", len(r.Paths)))
	for i, path := range r.Paths {
		sb.WriteString(fmt.Sprintf("  %d. ", i+1))
		for j, t := range path.Transitions {
			if j > 0 {
				sb.WriteString(" → ")
			}
			sb.WriteString(t.String())
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
