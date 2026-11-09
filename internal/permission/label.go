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
//
// Audit (J5) extended this switch to mirror the DefaultContract trust
// classifications: read-only analysis tools trust their args; network fetch
// / browser / test / permission / bash keep Untrusted args because their
// input may carry content sourced from outside the workspace.
func DefaultTrustForToolArgs(tool string) Label {
        switch tool {
        case "read", "grep", "find", "ls", "search":
                // `search` folds the old grep/find/graph/rank tools via mode=grep|
                // find|graph|rank; all are read-only analysis. Treat as user-trusted
                // args the same way the old tools were.
                return Label{Trust: User, ReaderSet: []string{"user"}}
        case "code", "context", "docsync", "vector_search", "session_search", "secret", "ask_user", "contract", "budget":
                // Read-only analysis tools. Args are user-supplied (paths, queries,
                // prompts) and don't carry network-sourced content. `contract` args
                // are model-proposed check ids, validated by the harness; `budget`
                // args are model-proposed round counts, clamped by the harness.
                return Label{Trust: User, ReaderSet: []string{"user"}}
        default:
                return Label{Trust: Untrusted, ReaderSet: []string{}}
        }
}

// DefaultTrustForToolOutput returns the trust label for tool output.
//
// Audit (J5) extended this switch: read-only analysis tools' output is User-
// trusted (the agent is reporting workspace state back to itself); network
// fetch + browser + test + permission outputs are Untrusted because they
// reflect environment / network state the user did not author.
func DefaultTrustForToolOutput(tool string) Label {
        switch tool {
        case "bash", "test":
                // Test runs binaries (go test / pytest). Same shape as bash —
                // output is environment-derived, not user-authored.
                return Label{Trust: Untrusted, ReaderSet: []string{}}
        case "read", "grep", "find", "ls", "search":
                // `search` output is read-only analysis, same as the old grep/find
                // tools it folds.
                return Label{Trust: User, ReaderSet: []string{"user"}}
        case "code", "context", "docsync", "vector_search", "session_search", "secret", "ask_user", "contract", "budget":
                // Read-only analysis tools. Output reports workspace state back to
                // the agent — User-trusted, same shape as `read`/`search`. The
                // `contract` tool's output is harness-computed ledger status; the
                // `budget` tool's output is harness-computed budget state.
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
//      (Lest_a ∧ Lest_b, Usrc_a ∪ Usrc_b)
func (a PartialLabel) Meet(b PartialLabel) PartialLabel {
        return PartialLabel{
                Established: a.Established.Meet(b.Established),
                Unresolved:  sortedUnion(a.Unresolved, b.Unresolved),
        }
}

// Merge returns the join of a and b under partial labels:
//
//      (Lest_a ∨ Lest_b, Usrc_a ∪ Usrc_b)
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
// (i.e., b ≤ a and b ≠ a) under the restrictiveness order used by this
// codebase: ≤ means "more restrictive or equal". So "b is narrower than a"
// means "b is more restrictive than a" = "b ≤ a AND b ≠ a" = labelLE(a, b)
// && !labelsEqual(a, b).
func IsNarrower(a, b Label) bool {
        return labelLE(a, b) && !labelsEqual(a, b)
}

// IsNarrowerOrEqual reports whether label b is at most as restrictive as
// label a (b ≤ a in the codebase's restrictiveness order, where ≤ means
// "more restrictive or equal"). Used for the cast ceiling check: the cast's
// resolved label must be at least as restrictive as its declared may_cast
// ceiling, so a label equal to the ceiling is allowed. The paper's "bounded
// by declared target ceilings (may_cast)" means ceiling ≤ label, i.e.,
// IsNarrowerOrEqual(label, ceiling).
func IsNarrowerOrEqual(a, b Label) bool {
        return labelLE(a, b)
}

// labelLE reports whether b ≤ a in the restrictiveness order used by this
// codebase: b ≤ a iff b is *more restrictive or equal* to a. The codebase
// convention is the opposite of the standard math lattice order — meet
// here means "more restrictive", so ≤ means "more restrictive or equal".
// Component-wise for on-chain operands: b ≤ a iff b.ReaderSet ⊆ a.ReaderSet
// AND b.Trust ≥ a.Trust (higher TrustOrder = more restrictive). Off-chain
// (Unspecified) is treated as the MOST restrictive end of the order, so:
//   - off-chain b ≤ any a (b is at the bottom of the order)
//   - on-chain b ≤ off-chain a is false (b is above a in the order)
//   - both off-chain: compare by reader set only
func labelLE(a, b Label) bool {
        ta, tb := TrustOrder(a.Trust), TrustOrder(b.Trust)
        aOff := ta < 0
        bOff := tb < 0
        if bOff && !aOff {
                // b is off-chain (most restrictive), a is on-chain. b is below a
                // in the order, so b ≤ a is true.
                return true
        }
        if aOff && !bOff {
                // a is off-chain (most restrictive), b is on-chain. b is above a
                // in the order, so b ≤ a is false.
                return false
        }
        if aOff && bOff {
                // Both off-chain: trust dimension is the same (both off-chain),
                // so b ≤ a reduces to b.R ⊆ a.R.
                return readerSetSubset(b.ReaderSet, a.ReaderSet)
        }
        // Both on-chain. b ≤ a requires b more-or-equally restrictive in BOTH
        // dimensions: b.ReaderSet ⊆ a.ReaderSet AND b.Trust ≥ a.Trust (in
        // TrustOrder, higher = more restrictive).
        if tb < ta {
                // b's trust is less restrictive than a's (lower TrustOrder) → b is
                // wider in trust → b is NOT ≤ a.
                return false
        }
        // tb >= ta: b is more-or-equally restrictive in trust. Check reader set.
        return readerSetSubset(b.ReaderSet, a.ReaderSet)
}

// labelsEqual reports whether a and b have the same trust and reader set.
// Reader sets are compared as sets, not slices — Meet sorts its output
// via sortedIntersect, so a label and its meet-equal counterpart may have
// differently-ordered (but set-equal) reader sets. An order-sensitive
// comparison would falsely report them as unequal and let IsNarrower
// return true for identical labels.
func labelsEqual(a, b Label) bool {
        if a.Trust != b.Trust {
                return false
        }
        if len(a.ReaderSet) != len(b.ReaderSet) {
                return false
        }
        set := make(map[string]struct{}, len(a.ReaderSet))
        for _, r := range a.ReaderSet {
                set[r] = struct{}{}
        }
        for _, r := range b.ReaderSet {
                if _, ok := set[r]; !ok {
                        return false
                }
        }
        return true
}

// readerSetSubset reports whether sub is a subset of sup. Both must be
// sorted (Caller responsibility — labels produced by Merge/Meet sort
// their reader sets). Empty sub is a subset of anything.
func readerSetSubset(sub, sup []string) bool {
        if len(sub) == 0 {
                return true
        }
        if len(sup) == 0 {
                return false
        }
        set := make(map[string]struct{}, len(sup))
        for _, r := range sup {
                set[r] = struct{}{}
        }
        for _, r := range sub {
                if _, ok := set[r]; !ok {
                        return false
                }
        }
        return true
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
// guaranteed to satisfy λ ≤ may_cast (equal is allowed).
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
        // Enforce may_cast ceiling: the cast's resolved label must be AT LEAST
        // as restrictive as the declared ceiling (label ≤ ceiling in the
        // codebase's restrictiveness order, where ≤ means "more-or-equally
        // restrictive"). A cast that returns a wider label (e.g. resolving an
        // unannotated source to Trusted when the ceiling is Untrusted) is
        // laundering — it makes untrusted data look more trusted than the
        // cast is authorized to claim. IsNarrowerOrEqual(ceiling, label)
        // returns true iff label ≤ ceiling (label is narrower-or-equal to
        // ceiling), so the cast fails when the resolved label is wider than
        // the ceiling. Equality is allowed — a cast that resolves to exactly
        // its declared ceiling is permitted.
        if !IsNarrowerOrEqual(c.Ceiling, label) {
                return Label{}, fmt.Errorf("%w: resolved=%v ceiling=%v", ErrCastCeilingExceeded, label, c.Ceiling)
        }
        return label, nil
}
