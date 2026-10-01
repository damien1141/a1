package permission

import "slices"

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
