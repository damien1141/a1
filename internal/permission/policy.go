package permission

// Mode controls how Ask decisions are folded.
type Mode string

// Mode values control how Ask decisions are folded.
const (
	ModeInteractive    Mode = "interactive"
	ModeReadonly       Mode = "readonly"
	ModeAutopilot      Mode = "autopilot"
	ModeHeadlessStrict Mode = "headless-strict"
	// ModeYolo disables prompting: every Ask becomes Allow. It is deliberately
	// a mode rather than a config flag, so the deny list still fires and the
	// APPA trajectory still records what happened. The two allow-everything
	// flags (DangerouslyAllowAll, AllowAllSession) short-circuit before the
	// gate consults anything, which means they also disable the deny rules.
	ModeYolo Mode = "yolo"
)

// AllModes lists every mode in the order a shift-tab cycle should reach them.
// Yolo is last so sweeping the cycle reaches it deliberately instead of
// passing through it on the way somewhere else.
//
// Every surface that offers a mode choice reads this list, because the four
// copies that used to exist drifted: adding a mode meant finding four places,
// and missing one left a mode the web switcher could set but the terminal
// could not.
var AllModes = []Mode{
	ModeInteractive,
	ModeReadonly,
	ModeAutopilot,
	ModeHeadlessStrict,
	ModeYolo,
}

// Decision is the gate outcome before optional Ask folding.
type Decision int

// Decision values are the gate outcomes before optional Ask folding.
const (
	Allow Decision = iota
	Deny
	Ask
)

func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	default:
		return "unknown"
	}
}

// ModeOf returns the permission Mode configured on g, if known.
// SessionAllowGate unwraps to its Inner. Unknown gate types return "".
func ModeOf(g Gate) Mode {
	for g != nil {
		switch x := g.(type) {
		case *StaticGate:
			if x.Policy.Mode != "" {
				return x.Policy.Mode
			}
			return ModeInteractive
		case *SessionAllowGate:
			g = x.Inner
		case AllowAll:
			return ""
		default:
			return ""
		}
	}
	return ""
}

// Action names the kind of tool operation being gated.
type Action string

// Action values name the kinds of tool operations being gated.
const (
	ActionBash  Action = "bash"
	ActionRead  Action = "read"
	ActionWrite Action = "write"
	ActionEdit  Action = "edit"
	ActionGrep  Action = "grep"
	ActionFind  Action = "find"
	ActionLs    Action = "ls"
	ActionAgent Action = "agent"
)

// Request describes a tool invocation for permission evaluation.
type Request struct {
	Action      Action
	Tool        string
	Paths       []string // absolute, cleaned
	Command     string
	InputLabels []Label

	// OutputLabel is populated after tool execution for admission checks.
	OutputLabel Label

	// Succeeded reports whether the tool execution completed successfully.
	// Effect tokens are committed only on reported success (APPA §3: "failed
	// or indeterminate outcomes contribute none").
	Succeeded bool
}

// Policy is the configurable permission ruleset.
type Policy struct {
	Mode                Mode
	WorkspaceOnlyWrites bool
	AskTimeoutSec       int
	BashDefault         Decision // typically Ask
	BashAllow           []string // regex
	BashDeny            []string // regex
	SensitivePathDeny   []string // path prefixes
	WorkspaceOnlyReads  bool     // if true, out-of-workspace reads deny
	DangerouslyAllowAll bool     // skip all permission checks globally
	AllowAllSession     bool     // skip all permission checks for this session only

	// Step 4: call-scoped authority.
	// RequiresAuthority forces Ask decisions to consult an external Authority
	// for a single-use ruling bound to the exact call hash.
	RequiresAuthority bool
	// Authority is the external authority consulted when RequiresAuthority is
	// true and the gate returns Ask. Empty = no authority configured.
	Authority any

	// Admission controls (Step 3).
	// NoUntrustedOutput denies admission when output trust is Untrusted.
	NoUntrustedOutput bool
	// RequireLabeledOutput denies admission (Ask) when the realized output
	// carries no trust rank at all. Off by default: gradual typing exists so
	// unannotated results can flow as unresolved sources instead of blocking.
	RequireLabeledOutput bool
	// RequiredEffects are effect tokens that must appear in EffectLog before
	// admission is granted. Empty = no effect check.
	RequiredEffects []string
}

// DefaultPolicy returns the interactive defaults from task-002.
func DefaultPolicy() Policy {
	return Policy{
		Mode:                ModeInteractive,
		WorkspaceOnlyWrites: true,
		AskTimeoutSec:       120,
		BashDefault:         Ask,
		BashAllow:           defaultBashAllow,
		BashDeny:            defaultBashDeny,
		SensitivePathDeny:   defaultSensitivePaths(),
		WorkspaceOnlyReads:  true,
	}
}

// ChildPolicy is the default gate for sub-agents: no interactive Ask, bash
// allowed unless it matches the hard deny list. Mode still folds writes
// (Readonly) or Ask leftovers (HeadlessStrict).
func ChildPolicy(mode Mode) Policy {
	p := DefaultPolicy()
	p.Mode = mode
	p.BashDefault = Allow
	return p
}
