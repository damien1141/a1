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
        Trust   Trust    // for trust_floor: minimum required trust level
        Readers []string // for recipient_cover: required audience
        Token   string   // for prior/no_prior: effect token name
}

// Check evaluates whether this precondition holds against the prospective label
// and effect log. It returns (true, "") if satisfied, (false, reason) otherwise.
func (p Precondition) Check(prospective Label, log *EffectLog) (bool, string) {
        switch p.Kind {
        case PreconditionTrustFloor:
                // A trust floor demands data *at least as trusted* as the floor. A
                // more restrictive rank fails it.
                if TrustOrder(prospective.Trust) > TrustOrder(p.Trust) {
                        return false, fmt.Sprintf("trust floor %s not met: got %s", p.Trust, prospective.Trust)
                }
                if !IsKnownTrust(prospective.Trust) {
                        return false, fmt.Sprintf("trust floor %s not met: trust is unestablished", p.Trust)
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
//
// The audit (J5) extended this switch to cover every registered tool so the
// APPA trajectory tracking reasons about contribution trust correctly:
//   - read-only analysis tools (code, context, docsync, vector_search,
//     secret, ask_user) — User-trusted args, no effect tokens, same shape
//     as `read`/`search`. They read workspace state and report it back.
//   - network-fetch tools (fetch, browser_read) — Untrusted args/output,
//     because the data comes from outside the workspace. The trust
//     distinction matters when the agent later writes that data into a
//     file: a `fetch` output written via `write` should not elevate the
//     fetched content's trust above Untrusted without explicit user
//     approval.
//   - binary-running tools (test) — Untrusted, same shape as `bash` — its
//     output (test logs, coverage) is environment-derived, not user-
//     authored.
//   - file-writing tools (document) — User-trusted args (the prompt is
//     user-supplied), commits a "write" effect token so subsequent edits
//     to the same file can be traced through the effect log.
//   - meta / browser-side-effect tools (browser, browser_navigate,
//     browser_interact, permission) — Untrusted, no effect tokens. The
//     `permission` tool is special: it mutates the live permission mode,
//     which is agent state, not workspace state — so it stays Untrusted
//     without effect tokens (no APPA label algebra applies).
func DefaultContract(tool string) Contract {
        switch tool {
        case "read", "grep", "find", "ls", "search":
                // `search` folds the old grep/find/graph/rank tools via mode=grep|
                // find|graph|rank; all are read-only analysis. Treat the same way
                // the old tools were treated — user-trusted args, no effect tokens.
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: User, ReaderSet: TopReaderSet},
                }
        case "code", "context", "docsync", "vector_search", "session_search", "secret", "ask_user", "contract", "budget":
                // Read-only analysis tools collapsed under mode-dispatched parents
                // (audit J5). Same shape as `read`: user-supplied args, top readers,
                // no committed effect tokens — these tools inspect state without
                // mutating it. `contract` declares done-criteria in engine state
                // (never the workspace); the harness validates every id, so its
                // args stay user-trusted. `budget` declares a tool-round budget in
                // engine state — same shape.
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: User, ReaderSet: TopReaderSet},
                }
        case "fetch", "browser_read":
                // Network fetch tools. Output is Untrusted because the data came
                // from outside the workspace — a fetched web page or browser-read
                // content should not be elevated above Untrusted without explicit
                // user approval (e.g. writing it into a project file via `write`
                // with the user's blessing).
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: Untrusted, ReaderSet: TopReaderSet},
                }
        case "test":
                // `test` runs binaries (go test / pytest / etc.). Same shape as
                // `bash`: Untrusted args + output, no effect tokens. The test
                // runner's environment is not user-authored.
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: Untrusted, ReaderSet: TopReaderSet},
                }
        case "browser", "browser_navigate", "browser_interact", "permission":
                // Side-effecting tools. `browser*` drive an external browser (can
                // submit forms, click buttons, navigate to URLs); `permission`
                // mutates the live permission mode. All Untrusted — their args
                // may carry network-sourced content and their actions have side
                // effects outside the workspace.
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: Untrusted, ReaderSet: TopReaderSet},
                }
        case "write", "edit":
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: User, ReaderSet: TopReaderSet},
                        EffectTokens: []string{"write"},
                }
        case "document":
                // `document` writes a rendered file to disk → commits a "write"
                // effect token so subsequent edits to the same file can be traced
                // through the effect log. User-trusted args (the prompt is user-
                // supplied).
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: User, ReaderSet: TopReaderSet},
                        EffectTokens: []string{"write"},
                }
        case "bash":
                return Contract{
                        Tool:         tool,
                        Contribution: Label{Trust: Untrusted, ReaderSet: TopReaderSet},
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
//
// Pre-collapse, only 8 of 18 tools were registered here — the rest fell back
// to DefaultContract(tool) which returns an empty Contract (no contribution,
// no effect tokens). The audit (J5) extended DefaultContract itself to cover
// every tool, so the Lookup fallback now produces a meaningful Contract.
// Registering here is still preferred because it makes the contracts
// enumerable (e.g. for a future /api/contracts debug endpoint).
func DefaultContracts() *ContractRegistry {
        r := NewContractRegistry()
        // Legacy + post-collapse read/search family.
        r.Register("read", DefaultContract("read"))
        r.Register("grep", DefaultContract("grep"))
        r.Register("find", DefaultContract("find"))
        r.Register("ls", DefaultContract("ls"))
        r.Register("search", DefaultContract("search"))
        // Write family.
        r.Register("write", DefaultContract("write"))
        r.Register("edit", DefaultContract("edit"))
        r.Register("document", DefaultContract("document"))
        // Bash + binary-running family.
        r.Register("bash", DefaultContract("bash"))
        r.Register("test", DefaultContract("test"))
        // Read-only analysis tools (audit J5).
        r.Register("code", DefaultContract("code"))
        r.Register("context", DefaultContract("context"))
        r.Register("docsync", DefaultContract("docsync"))
        r.Register("vector_search", DefaultContract("vector_search"))
        r.Register("session_search", DefaultContract("session_search"))
        r.Register("secret", DefaultContract("secret"))
        r.Register("ask_user", DefaultContract("ask_user"))
        // Network fetch + browser family.
        r.Register("fetch", DefaultContract("fetch"))
        r.Register("browser", DefaultContract("browser"))
        r.Register("browser_navigate", DefaultContract("browser_navigate"))
        r.Register("browser_interact", DefaultContract("browser_interact"))
        r.Register("browser_read", DefaultContract("browser_read"))
        // Meta.
        r.Register("permission", DefaultContract("permission"))
        return r
}

// String returns a human-readable summary of the contract.
func (c Contract) String() string {
        var parts []string
        parts = append(parts, fmt.Sprintf("dτ=%s", c.Contribution))
        if len(c.EffectTokens) > 0 {
                parts = append(parts, "Kτ="+strings.Join(c.EffectTokens, ","))
        }
        if len(c.Requires) > 0 {
                var reqs []string
                for _, r := range c.Requires {
                        reqs = append(reqs, string(r.Kind))
                }
                parts = append(parts, "requires="+strings.Join(reqs, ","))
        }
        return fmt.Sprintf("Contract{%s}", strings.Join(parts, " "))
}
