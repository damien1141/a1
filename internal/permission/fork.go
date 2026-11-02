package permission

// ForkChildGate returns a gate for a child branch spawned from parent.
//
// The child inherits the parent's current label (L0c := Lp) and shares its
// effect log E, so the branch cannot start from an unconstrained top label —
// which would be an illicit laundering path for confidential data the parent
// already ingested. The parent's label is never mutated by the child; only an
// explicit merge at the parent does that (APPA §6, Theorem 8).
//
// The child's policy is the parent's policy with the mode replaced, so the
// role's readonly/headless folding still applies. Pending acceptances do not
// cross the boundary: a child must obtain its own acceptance for any narrowing
// it performs inside the branch.
func ForkChildGate(parent *StaticGate, mode Mode) *StaticGate {
	child := &StaticGate{
		Policy:     parent.Policy,
		Workspace:  parent.Workspace,
		bashAllow:  parent.bashAllow,
		bashDeny:   parent.bashDeny,
		contracts:  parent.contracts,
		recovery:   parent.recovery,
		resolver:   parent.resolver,
		trajectory: forkTrajectory(parent.trajectory),
	}
	child.Policy.Mode = mode
	// A child branch never inherits the parent's session-wide overrides:
	// the parent may be in allow-all for convenience, but a spawned branch
	// must still answer to the algebra.
	child.Policy.DangerouslyAllowAll = false
	child.Policy.AllowAllSession = false
	if mode == ModeReadonly || mode == ModeHeadlessStrict {
		child.Policy.BashDefault = Allow
	}
	return child
}

// forkTrajectory copies the parent's trajectory state into a child-local one.
// The label and effect log are shared by reference (E is tree-wide), while the
// acceptances and rulings start empty because they are scope-bound.
func forkTrajectory(parent *TrajectoryState) *TrajectoryState {
	if parent == nil {
		return NewTrajectoryState()
	}
	return &TrajectoryState{
		Label:             parent.Label,
		Effects:           parent.Effects,
		AcceptedNarrowing: make(map[string]bool),
		Rulings:           NewRulingLog(),
	}
}

// StaticGateOf unwraps g to the underlying StaticGate, or nil when there is
// none (AllowAll, a nil gate, an unknown wrapper).
func StaticGateOf(g Gate) *StaticGate {
	for g != nil {
		switch x := g.(type) {
		case *StaticGate:
			return x
		case *SessionAllowGate:
			g = x.Inner
		case *ConfinementGate:
			g = x.Inner
		default:
			return nil
		}
	}
	return nil
}
