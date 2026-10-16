package permission

import (
	"errors"
	"fmt"
	"slices"
)

// Trust represents the trust level of a data source.
type Trust int

// Trust values represent the trust level of a data source. The chain is
// totally ordered and used by the lattice's t dimension: a lower value is a
// less restrictive (more trusted) bound. The zero value is deliberately
// off-chain so an unset Label cannot silently claim "trusted".
const (
	// Unspecified is the zero value: no trust declared. It is not on the
	// chain, so a check consuming it must resolve the source or fail closed.
	Unspecified Trust = iota
	// Trusted means the source is TCB-controlled or pre-approved.
	Trusted
	// User means the source is direct user input.
	User
	// Untrusted means the source is external/untrusted data.
	Untrusted
)

func (t Trust) String() string {
	switch t {
	case Trusted:
		return "trusted"
	case User:
		return "user"
	case Untrusted:
		return "untrusted"
	default:
		return "unspecified"
	}
}

// JoinTrust returns the least upper bound of two trust levels: the *less*
// restrictive of the two, so a lower rank wins. An off-chain operand yields
// off-chain — joining "declared" with "undeclared" is not a declared value.
func JoinTrust(a, b Trust) Trust {
	if !IsKnownTrust(a) || !IsKnownTrust(b) {
		return Unspecified
	}
	if a == Trusted || b == Trusted {
		return Trusted
	}
	if a == User || b == User {
		return User
	}
	return Untrusted
}

// MeetTrust returns the greatest lower bound of two trust levels: the *more*
// restrictive of the two, so a higher rank wins. This is the lattice meet that
// folds a contribution into a trajectory label, so it can only ever lower
// trust. An off-chain operand yields off-chain: an undeclared source never
// launders its way into a declared rank.
func MeetTrust(a, b Trust) Trust {
	if !IsKnownTrust(a) || !IsKnownTrust(b) {
		return Unspecified
	}
	if a == Untrusted || b == Untrusted {
		return Untrusted
	}
	if a == User || b == User {
		return User
	}
	return Trusted
}

// TrustOrder returns the rank of t in the totally ordered trust chain.
// A lower rank is the less restrictive (more trusted) bound. Unspecified is
// not on the chain — unannotated data is tracked as an unresolved source on
// the PartialLabel, never as a trust rank (APPA §3: "there is no ordering
// trusted < unknown < suspicious").
func TrustOrder(t Trust) int {
	switch t {
	case Trusted:
		return 0
	case User:
		return 1
	case Untrusted:
		return 2
	default:
		return -1
	}
}

// IsKnownTrust reports whether t is a value on the trust chain, as opposed to
// the off-chain zero value.
func IsKnownTrust(t Trust) bool {
	return t == Trusted || t == User || t == Untrusted
}

// Label is a security label in the lattice S = P(U) × T: the reader set R and
// the trust rank t (APPA Appendix A). Labels are ordered by restrictiveness,
// (R′, t′) ≤ (R, t) iff R′ ⊆ R and t′ ≤ t, and combine by component-wise meet.
// Operational history deliberately does not live here — it belongs to the
// effect log E, which is shared tree-wide while labels stay trajectory-local.
type Label struct {
	Trust     Trust
	ReaderSet []string
}

// Merge returns a new label representing the join of a and b.
func (a Label) Merge(b Label) Label {
	out := Label{
		Trust:     JoinTrust(a.Trust, b.Trust),
		ReaderSet: sortedUnion(a.ReaderSet, b.ReaderSet),
	}
	if out.ReaderSet == nil {
		out.ReaderSet = []string{}
	}
	return out
}

// Meet returns a new label representing the meet of a and b.
func (a Label) Meet(b Label) Label {
	out := Label{
		Trust:     MeetTrust(a.Trust, b.Trust),
		ReaderSet: sortedIntersect(a.ReaderSet, b.ReaderSet),
	}
	if out.ReaderSet == nil {
		out.ReaderSet = []string{}
	}
	return out
}

func sortedUnion(a, b []string) []string {
	if len(a) == 0 {
		return slices.Clone(b)
	}
	if len(b) == 0 {
		return slices.Clone(a)
	}
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	slices.Sort(out)
	out = slices.Compact(out)
	return out
}

func sortedIntersect(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return []string{}
	}
	tmp := make([]string, len(a))
	copy(tmp, a)
	slices.Sort(tmp)
	set := make(map[string]struct{}, len(b))
	for _, v := range b {
		set[v] = struct{}{}
	}
	var out []string
	for _, v := range tmp {
		if _, ok := set[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

// DefaultTrustForToolArgs returns the trust label for direct tool arguments.
// An unannotated tool contributes no established trust: the zero Trust is
// off-chain (see TrustOrder), so callers must treat it as an unresolved source
// rather than as a rank.
func DefaultTrustForToolArgs(tool string) Label {
	switch tool {
	case "read", "grep", "find", "ls":
		return Label{Trust: User, ReaderSet: []string{"user"}}
	default:
		return Label{Trust: Untrusted, ReaderSet: []string{}}
	}
}

// DefaultTrustForToolOutput returns the trust label for tool output.
func DefaultTrustForToolOutput(tool string) Label {
	switch tool {
	case "bash":
		return Label{Trust: Untrusted, ReaderSet: []string{}}
	case "read", "grep", "find", "ls":
		return Label{Trust: User, ReaderSet: []string{"user"}}
	default:
		return Label{Trust: Untrusted, ReaderSet: []string{}}
	}
}

// PartialLabel models gradual security annotation: an established lattice bound
// plus a set of unresolved source identities whose labels have not yet been
// determined. Lazy combination lets unannotated data travel without premature
// blocking; checks that consume unresolved dimensions trigger cast resolution.
type PartialLabel struct {
	Established Label    // Lest: current concrete bound
	Unresolved  []string // Usrc: unresolved source identifiers
}

// NewPartialLabel creates a PartialLabel from an established bound.
func NewPartialLabel(est Label, unresolved []string) PartialLabel {
	if unresolved == nil {
		unresolved = []string{}
	}
	return PartialLabel{Established: est, Unresolved: unresolved}
}

// Meet returns the meet of a and b under partial labels:
//
//	(Lest_a ∧ Lest_b, Usrc_a ∪ Usrc_b)
func (a PartialLabel) Meet(b PartialLabel) PartialLabel {
	return PartialLabel{
		Established: a.Established.Meet(b.Established),
		Unresolved:  sortedUnion(a.Unresolved, b.Unresolved),
	}
}

// Merge returns the join of a and b under partial labels:
//
//	(Lest_a ∨ Lest_b, Usrc_a ∪ Usrc_b)
func (a PartialLabel) Merge(b PartialLabel) PartialLabel {
	return PartialLabel{
		Established: a.Established.Merge(b.Established),
		Unresolved:  sortedUnion(a.Unresolved, b.Unresolved),
	}
}

// HasUnresolved reports whether this partial label contains unresolved sources.
func (a PartialLabel) HasUnresolved() bool {
	return len(a.Unresolved) > 0
}

// UnresolvedCount returns the number of unresolved sources.
func (a PartialLabel) UnresolvedCount() int {
	return len(a.Unresolved)
}

// AddUnresolved appends source identities to the unresolved set.
func (a PartialLabel) AddUnresolved(src ...string) PartialLabel {
	a.Unresolved = sortedUnion(a.Unresolved, src)
	return a
}

// Resolve replaces one unresolved source with a concrete label contribution,
// returning the new partial label. If src is not in the unresolved set, the
// label is unchanged. This models cast resolution: λ ≤ may_cast.
func (a PartialLabel) Resolve(src string, contribution Label) PartialLabel {
	found := false
	out := make([]string, 0, len(a.Unresolved))
	for _, u := range a.Unresolved {
		if u == src {
			found = true
			a.Established = a.Established.Meet(contribution)
		} else {
			out = append(out, u)
		}
	}
	if found {
		a.Unresolved = out
	}
	return a
}

// IsNarrower reports whether label b is strictly narrower than label a
// (i.e., b ≤ a and b ≠ a) under the restrictiveness order
// (R′, t′) ≤ (R, t) iff R′ ⊆ R and t′ ≤ t.
func IsNarrower(a, b Label) bool {
	ta, tb := TrustOrder(a.Trust), TrustOrder(b.Trust)
	if tb != ta {
		// Falling off the chain (a declared rank becoming undeclared) is a
		// restriction, never a widening.
		if tb < 0 {
			return true
		}
		if ta < 0 {
			return false
		}
		return tb > ta
	}
	if ta < 0 {
		// Both off-chain: narrowing can only come from the reader set.
		return len(b.ReaderSet) < len(a.ReaderSet)
	}
	// ReaderSet shrinking: b's readers must be a strict subset of a's.
	aSet := make(map[string]struct{}, len(a.ReaderSet))
	for _, r := range a.ReaderSet {
		aSet[r] = struct{}{}
	}
	bSet := make(map[string]struct{}, len(b.ReaderSet))
	for _, r := range b.ReaderSet {
		bSet[r] = struct{}{}
	}
	for r := range bSet {
		if _, ok := aSet[r]; !ok {
			return false
		}
	}
	return len(bSet) < len(aSet)
}

// RequiresNarrowing reports whether folding contribution into current would
// narrow the trajectory label (i.e., requires explicit acceptance).
func RequiresNarrowing(current, contribution Label) bool {
	next := current.Meet(contribution)
	return IsNarrower(current, next)
}

// CastResolvers is a registry of TCB transformations that can resolve
// unresolved sources into concrete labels. Each entry bounds the maximum
// restrictiveness the cast may return (may_cast ceiling).
type CastResolver struct {
	Source  string
	Ceiling Label
	Resolve func(src string) (Label, error)
}

// ResolveCast attempts to resolve an unresolved source via a registered cast.
// It returns ErrNoCast if no cast is registered for src, ErrCastFailed if the
// transformation fails, or ErrCastCeilingExceeded if the resolved label is
// wider than the declared may_cast ceiling. On success the resolved label is
// guaranteed to satisfy λ ≤ may_cast.
var (
	ErrNoCast              = errors.New("no cast registered for source")
	ErrCastFailed          = errors.New("cast resolution failed")
	ErrCastCeilingExceeded = errors.New("resolved label exceeds cast ceiling")
)

func (c CastResolver) ResolveCast(src string) (Label, error) {
	if c.Source != src {
		return Label{}, ErrNoCast
	}
	if c.Resolve == nil {
		return Label{}, ErrCastFailed
	}
	label, err := c.Resolve(src)
	if err != nil {
		return Label{}, fmt.Errorf("%w: %v", ErrCastFailed, err)
	}
	// Enforce may_cast ceiling: resolved label must be at least as restrictive.
	if !IsNarrower(c.Ceiling, label) {
		return Label{}, fmt.Errorf("%w: resolved=%v ceiling=%v", ErrCastCeilingExceeded, label, c.Ceiling)
	}
	return label, nil
}
