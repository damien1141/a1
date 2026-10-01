# Everything past this point is completed:

2. clean up and make teh web config better- its looks very jank
- right now looks very messy

better TUI UI/UX still janky in some places 
- history navigation with arrow keys- it needs to save teh current draft before moving up so that teh user can move back down again
- needs to have a more clear turn ended indicator

# integrate APPA paper into permission system: ./2607.24625v2

## step 1 — minimal label/trust model + enriched types

status: complete

changes:
- `internal/permission/label.go` — new file with `Trust` enum, `Label` struct, trust helpers
- `internal/permission/policy.go` — `Request.InputLabels []Label`, `Request.OutputLabel`
- `internal/permission/extract.go` — attaches default input labels on extraction
- `internal/permission/label_test.go` — trust/label tests

acceptance:
- `go test ./internal/permission` passes
- existing gate tests still pass unchanged
- new tests cover trust descent and unknown fallback

## step 2 — effect event log

status: complete

changes:
- `internal/permission/effect.go` — new `EffectLog` with atomic commits, `Has`/`Count`/`Entries` accessors, `CheckRequiredEffects`, `CheckNoPrior`
- `internal/permission/effect_test.go` — coverage for commit, snapshot, missing-effect checks, concurrency

acceptance:
- `go test ./internal/permission` passes
- effect log is append-only, nil-safe, and concurrent-safe

## step 3 — post-execution admission gate

status: complete

changes:
- `internal/permission/label.go` — added `JoinTrust`, `MeetTrust`, `Label.Merge`, `Label.Meet`
- `internal/permission/policy.go` — added `Request.OutputLabel`, `Policy.NoUntrustedOutput`, `Policy.RequiredEffects`
- `internal/permission/gate.go` — added `Gate.Admit`, `AdmissionGate` interface, `StaticGate.Admit`, `AllowAll.Admit`
- `internal/permission/bypass.go` — added `BypassGate.Admit`
- `internal/permission/admission_test.go` — admission tests

acceptance:
- `go test ./internal/permission` passes
- unlabeled outputs pass through
- untrusted output blocked when configured
- required effects trigger Ask when no log attached

## step 4 — call-scoped authority

status: complete

changes:
- `internal/permission/authority.go` — `CallHash`, `Authority` interface, `AuthorityFunc`
- `internal/permission/policy.go` — `RequiresAuthority bool`, `Authority any`
- `internal/agent/executor.go` — `consultAuthority` wires single-use authority into `Check`
- `internal/permission/authority_test.go` — call hash/authority tests

acceptance:
- `go test ./internal/agent/ -run TestExecutor` passes
- permission tests pass
- pre-existing `engine_test.go` overflow tests unchanged

## step 5 — trajectory confinement for sub-agents

status: complete

changes:
- `internal/permission/trajectory.go` — `Trajectory`, `ConfinementPolicy`, `ConfinementGate`
- `internal/permission/trajectory_test.go` — step/whitelist tests
- `internal/agent/child_spec.go` — `Confinement *permission.ConfinementPolicy` per role
- `internal/agent/engine_runner.go` — wraps child gate when spec has confinement

acceptance:
- `go test ./internal/permission/...` passes
- `go test ./internal/agent/ -run TestExecutor` passes

## step 6 — remove BypassGate / replace with scoped override

status: complete

changes:
- `internal/permission/bypass.go` — replaced `BypassGate` with `SessionAllowGate`
- `internal/permission/policy.go` — added `AllowAllSession bool`; updated `ModeOf` unwrapping
- `internal/permission/gate.go` — `StaticGate.Check` short-circuits for `DangerouslyAllowAll || AllowAllSession`; `NewGate` clears regexes/restrictions when either flag is set
- `internal/permission/perm_test.go` — migrated to `SessionAllowGate`
- `internal/permission/mode_test.go` — migrated to `SessionAllowGate`
- `internal/tui/controller/controller.go` — migrated init/mode-switch to `SessionAllowGate`
- `internal/permission/appa_integration_test.go` — end-to-end APPA integration tests covering all 6 steps

acceptance:
- `go test ./internal/permission/...` passes
- `go test ./internal/agent/ -run TestExecutor` passes
- pre-existing `engine_test.go` overflow tests unchanged
- `BypassGate` no longer exists in the codebase
