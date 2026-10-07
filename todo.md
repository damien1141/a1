# APPA Phase 2 & 3 Implementation Plan

## Goal
Implement the full APPA system from the paper: recovery graph, call-scoped authority logging, trajectory confinement (fork + attest-schema), and authority resolver backend.

## Phase 2: Core Recovery & Conformance

### 1. Recovery Graph (`internal/permission/recovery.go`)
- [x] Define `RecoveryState(L, E_supp, Γ)` where Γ is residual gaps for blocked call
- [x] Define transitions:
  - `admit`: admit tool τ → `(L ∧ dτ, E ∪ Kτ)`
  - `transform`: apply cast/sanitizer λ → `L ∧ λ`
  - `branch`: execute narrowing in child → parent `(L, E ∪ Kτ)`, child gets `L0c := Lp`
  - `rule`: consume authority ruling → remove covered gap from Γ
- [x] Implement bounded BFS search over GC with visited set
- [x] Implement `RecoveryPath` with ordered transitions and explanations

### 2. Authority Ruling Logging (`internal/permission/gate.go`, `trajectory.go`)
- [x] Add `Ruling` struct: callHash, gaps covered, timestamp, authority name
- [x] Add `TrajectoryState.Rulings` append-only log
- [x] Update `consultAuthority` to log consumed rulings atomically with dispatch
- [x] Verify ruling covers specific gaps before allowing dispatch

### 3. Trajectory Conformance (`internal/permission/confinement.go`)
- [x] Define `ChildTrajectory` with inherited label `L0c := Lp`
- [x] Define `AttestSchema` validator: shape-bounded return channel
- [x] Implement `ForkTransition` and `AttestTransition` for recovery graph
- [x] Implement branch outcome merge: abandon/standard/sanitized

### 4. Gate Integration (`internal/permission/gate.go`)
- [x] When prospective check fails, invoke recovery search
- [x] Report available recovery paths in refusal reason
- [x] Support `accept` transition for narrowing acceptance
- [x] Support `authority` transition for call-scoped rulings
- [x] Support `transform` transition for casts/sanitizers
- [x] Support `fork` + `attest-schema` transitions

### 5. Executor Integration (`internal/agent/executor.go`)
- [x] Log authority rulings in trajectory when consultAuthority approves
- [ ] Support recovery path execution: run prerequisite tool, apply cast, spawn child
- [ ] Emit recovery events to UI

### 6. Permission Tool (`internal/tools/permissiontool/permissiontool.go`)
- [ ] Add `trajectory` action: show current L, E, accepted narrowing, pending rulings
- [ ] Add `recovery` action: show available recovery paths for last blocked call
- [ ] Add `authority` action: list registered authorities and mandates

## Phase 3: Authority Backend & Polish

### 7. Authority Resolver Backend
- [x] Define `Resolver` interface with `Resolve(ctx, callHash, req) → (Decision, reason)`
- [x] Implement deterministic rule-based resolver (`RuleBasedResolver`)
- [x] Implement human-review resolver (async callback, `HumanReviewResolver`)
- [x] Implement LLM-as-judge resolver with mandate checking (`LLMJudgeResolver`)
- [x] Wire resolver into `StaticGate` via `SetResolver`/`Resolver`

### 8. Attest-Schema Validator
- [x] Define schema types: bool, int (bounded), decimal (fixed), enum, bounded array/object
- [x] Implement structural validation
- [x] Reject free text, unbounded numbers, open collections
- [x] Return validated fields with declared merge label

### 9. Tests
- [x] `recovery_test.go` - graph search finds valid paths, terminates on dead ends
- [x] `confinement_test.go` - fork inherits label, attest-schema validates shape
- [ ] `authority_test.go` - rulings logged, call-scoped, do not modify L/E
- [ ] `integration_test.go` - end-to-end recovery flow

## Design Decisions
- Recovery graph search is best-effort: bounded depth (max 10) with visited set
- Authority rulings are logged but not persisted across sessions (session-local)
- Fork creates lightweight child trajectory, not full sub-agent
- Attest-schema is structural only; semantic declassification is explicit TCB decision
- Mode folding preserved on top of all APPA checks

## Out of Scope (Future)
- Persistent ruling storage/audit log
- Multi-party approval orchestration
- Resolver-backed dynamic log views (spend accumulators, etc.)
- Full LLM judge integration (use stub for now)
