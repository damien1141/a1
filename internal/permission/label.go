package permission

import (
	"fmt"
	"slices"
)

// Trust represents the trust level of a data source or tool output.
type Trust int

// Trust values represent the trust level of a data source or tool output.
const (
	// Trusted means the source is TCB-controlled or pre-approved.
	Trusted Trust = iota
	// User means the source is direct user input.
	User
	// Untrusted means the source is external/untrusted data.
	Untrusted
	// Unknown means trust could not be determined.
	Unknown
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
		return "unknown"
	}
}

// JoinTrust returns the least upper bound of two trust levels.
func JoinTrust(a, b Trust) Trust {
	if a == Trusted || b == Trusted {
		return Trusted
	}
	if a == User || b == User {
		return User
	}
	if a == Untrusted || b == Untrusted {
		return Untrusted
	}
	return Unknown
}

// MeetTrust returns the greatest lower bound of two trust levels.
func MeetTrust(a, b Trust) Trust {
	if a == Unknown || b == Unknown {
		return Unknown
	}
	if a == Trusted && b == Trusted {
		return Trusted
	}
	if a == Untrusted || b == Untrusted {
		return Untrusted
	}
	return User
}

// Label is a minimal trust label attached to tool inputs/outputs.
type Label struct {
	Trust        Trust
	ReaderSet    []string
	EffectTokens []string
}

// Merge returns a new label representing the join of a and b.
func (a Label) Merge(b Label) Label {
	out := Label{
		Trust:        JoinTrust(a.Trust, b.Trust),
		ReaderSet:    sortedUnion(a.ReaderSet, b.ReaderSet),
		EffectTokens: sortedUnion(a.EffectTokens, b.EffectTokens),
	}
	if out.ReaderSet == nil {
		out.ReaderSet = []string{}
	}
	if out.EffectTokens == nil {
		out.EffectTokens = []string{}
	}
	return out
}

// Meet returns a new label representing the meet of a and b.
func (a Label) Meet(b Label) Label {
	out := Label{
		Trust:        MeetTrust(a.Trust, b.Trust),
		ReaderSet:    sortedIntersect(a.ReaderSet, b.ReaderSet),
		EffectTokens: sortedIntersect(a.EffectTokens, b.EffectTokens),
	}
	if out.ReaderSet == nil {
		out.ReaderSet = []string{}
	}
	if out.EffectTokens == nil {
		out.EffectTokens = []string{}
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
func DefaultTrustForToolArgs(tool string) Label {
	switch tool {
	case "read", "grep", "find", "ls":
		return Label{Trust: User, ReaderSet: []string{"user"}}
	default:
		return Label{Trust: Unknown, ReaderSet: []string{}}
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
		return Label{Trust: Unknown, ReaderSet: []string{}}
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
//   (Lest_a ∧ Lest_b, Usrc_a ∪ Usrc_b)
func (a PartialLabel) Meet(b PartialLabel) PartialLabel {
	return PartialLabel{
		Established: a.Established.Meet(b.Established),
		Unresolved:  sortedUnion(a.Unresolved, b.Unresolved),
	}
}

// Merge returns the join of a and b under partial labels:
//   (Lest_a ∨ Lest_b, Usrc_a ∪ Usrc_b)
func (a PartialLabel) Merge(b PartialLabel) PartialLabel {
	return PartialLabel{
		Established: a.Established.Merge(b.Established),
		Unresolved:  sortedUnion(a.Unresolved, b.Unresolved),
	}
}

// HasUnresolved reports whether this partial label contains unresolved sources.
func (p PartialLabel) HasUnresolved() bool {
	return len(p.Unresolved) > 0
}

// UnresolvedCount returns the number of unresolved sources.
func (p PartialLabel) UnresolvedCount() int {
	return len(p.Unresolved)
}

// AddUnresolved appends source identities to the unresolved set.
func (p PartialLabel) AddUnresolved(src ...string) PartialLabel {
	p.Unresolved = sortedUnion(p.Unresolved, src)
	return p
}

// Resolve replaces one unresolved source with a concrete label contribution,
// returning the new partial label. If src is not in the unresolved set, the
// label is unchanged. This models cast resolution: λ ≤ may_cast.
func (p PartialLabel) Resolve(src string, contribution Label) PartialLabel {
	found := false
	out := make([]string, 0, len(p.Unresolved))
	for _, u := range p.Unresolved {
		if u == src {
			found = true
			p.Established = p.Established.Meet(contribution)
		} else {
			out = append(out, u)
		}
	}
	if found {
		p.Unresolved = out
	}
	return p
}

// TrustOrder returns the total order index for trust levels.
// Lower index = more restrictive: Trusted < User < Untrusted < Unknown.
// This matches the meet semantics where Unknown is the top element and
// Trusted is the bottom.
func TrustOrder(t Trust) int {
	switch t {
	case Trusted:
		return 0
	case User:
		return 1
	case Untrusted:
		return 2
	case Unknown:
		return 3
	}
	return 3
}

// IsNarrower reports whether label b is strictly narrower than label a
// (i.e., b ≤ a and b ≠ a) under the trust chain ordering.
func IsNarrower(a, b Label) bool {
	if a.Trust != b.Trust {
		return TrustOrder(b.Trust) > TrustOrder(a.Trust)
	}
	// ReaderSet narrowing: b's readers must be a strict subset of a's.
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
	return len(bSet) < len(aSet) || len(b.EffectTokens) < len(a.EffectTokens)
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
	Source   string
	Ceiling  Label
	Resolve  func(src string) (Label, error)
}

// ResolveCast attempts to resolve an unresolved source via a registered cast.
// It returns ErrNoCast if no cast is registered for src, ErrCastFailed if the
// transformation fails, or ErrCastCeilingExceeded if the resolved label is
// wider than the declared may_cast ceiling. On success the resolved label is
// guaranteed to satisfy λ ≤ may_cast.
var (
	ErrNoCast            = fmt.Errorf("no cast registered for source")
	ErrCastFailed        = fmt.Errorf("cast resolution failed")
	ErrCastCeilingExceeded = fmt.Errorf("resolved label exceeds cast ceiling")
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
