package permission

import (
	"fmt"
	"strings"
)

// Contract formalizes a tool's declared information contribution and requirements.
// It corresponds to the paper's tool contract: (i) prospective label contribution dτ,
// (ii) effect tokens Kτ committed upon success, (iii) preconditions over labels/history.
type Contract struct {
	// Tool is the tool name this contract applies to.
	Tool string
	// Contribution is the prospective label dτ this tool contributes when admitted.
	Contribution Label

	// EffectTokens are Kτ committed to the effect log upon successful execution.
	EffectTokens []string

	// Requires lists preconditions that must hold for dispatch.
	// Empty means no preconditions beyond the prospective label check.
	Requires []Precondition
}

// PreconditionKind identifies the type of precondition.
type PreconditionKind string

const (
	// PreconditionTrustFloor requires the prospective label's trust to be at least floor.
	PreconditionTrustFloor PreconditionKind = "trust_floor"
	// PreconditionRecipientCover requires the label's reader set to include all recipients.
	PreconditionRecipientCover PreconditionKind = "recipient_cover"
	// PreconditionPrior requires the effect token to exist in the effect log.
	PreconditionPrior PreconditionKind = "prior"
	// PreconditionNoPrior requires the effect token to NOT exist in the effect log.
	PreconditionNoPrior PreconditionKind = "no_prior"
)

// Precondition describes a requirement over prospective labels or history.
type Precondition struct {
	Kind    PreconditionKind
	Trust   Trust      // for trust_floor: minimum required trust level
	Readers []string   // for recipient_cover: required audience
	Token   string     // for prior/no_prior: effect token name
}

// Check evaluates whether this precondition holds against the prospective label
// and effect log. It returns (true, "") if satisfied, (false, reason) otherwise.
func (p Precondition) Check(prospective Label, log *EffectLog) (bool, string) {
	switch p.Kind {
	case PreconditionTrustFloor:
		if TrustOrder(prospective.Trust) < TrustOrder(p.Trust) {
			return false, fmt.Sprintf("trust floor %s not met: got %s", p.Trust, prospective.Trust)
		}
		return true, ""

	case PreconditionRecipientCover:
		logSet := make(map[string]struct{}, len(prospective.ReaderSet))
		for _, r := range prospective.ReaderSet {
			logSet[r] = struct{}{}
		}
		for _, want := range p.Readers {
			if _, ok := logSet[want]; !ok {
				return false, fmt.Sprintf("recipient %q not covered by label readers %v", want, prospective.ReaderSet)
			}
		}
		return true, ""

	case PreconditionPrior:
		if !log.Has(p.Token) {
			return false, fmt.Sprintf("required prior effect %q not found", p.Token)
		}
		return true, ""

	case PreconditionNoPrior:
		if log.Has(p.Token) {
			return false, fmt.Sprintf("no_prior violated: effect %q already committed", p.Token)
		}
		return true, ""

	default:
		return false, fmt.Sprintf("unknown precondition kind: %s", p.Kind)
	}
}

// DefaultContract returns a default contract for the given tool name.
// Unannotated tools return an empty contract (no contribution, no requirements).
func DefaultContract(tool string) Contract {
	switch tool {
	case "read", "grep", "find", "ls":
		return Contract{
			Tool:        tool,
			Contribution: Label{Trust: User, ReaderSet: []string{"user"}},
		}
	case "write", "edit":
		return Contract{
			Tool:        tool,
			Contribution: Label{Trust: User, ReaderSet: []string{"user"}},
			EffectTokens: []string{"write"},
		}
	case "bash":
		return Contract{
			Tool:        tool,
			Contribution: Label{Trust: Untrusted, ReaderSet: []string{}},
		}
	default:
		return Contract{Tool: tool}
	}
}

// ContractRegistry stores tool contracts for lookup during gate evaluation.
type ContractRegistry struct {
	contracts map[string]Contract
}

// NewContractRegistry creates an empty registry.
func NewContractRegistry() *ContractRegistry {
	return &ContractRegistry{contracts: make(map[string]Contract)}
}

// Register adds or updates a contract for tool.
func (r *ContractRegistry) Register(tool string, c Contract) {
	if c.Tool == "" {
		c.Tool = tool
	}
	r.contracts[tool] = c
}

// Lookup returns the contract for tool. If not registered, the default is returned.
func (r *ContractRegistry) Lookup(tool string) Contract {
	if c, ok := r.contracts[tool]; ok {
		return c
	}
	return DefaultContract(tool)
}

// DefaultContracts returns a registry pre-populated with the standard tool contracts.
func DefaultContracts() *ContractRegistry {
	r := NewContractRegistry()
	r.Register("read", DefaultContract("read"))
	r.Register("grep", DefaultContract("grep"))
	r.Register("find", DefaultContract("find"))
	r.Register("ls", DefaultContract("ls"))
	r.Register("write", DefaultContract("write"))
	r.Register("edit", DefaultContract("edit"))
	r.Register("bash", DefaultContract("bash"))
	return r
}

// String returns a human-readable summary of the contract.
func (c Contract) String() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("dτ=%s", c.Contribution))
	if len(c.EffectTokens) > 0 {
		parts = append(parts, fmt.Sprintf("Kτ=%s", strings.Join(c.EffectTokens, ",")))
	}
	if len(c.Requires) > 0 {
		var reqs []string
		for _, r := range c.Requires {
			reqs = append(reqs, string(r.Kind))
		}
		parts = append(parts, fmt.Sprintf("requires=%s", strings.Join(reqs, ",")))
	}
	return fmt.Sprintf("Contract{%s}", strings.Join(parts, " "))
}
