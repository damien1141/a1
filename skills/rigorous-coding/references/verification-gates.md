# Verification Gates

The 5-gate cognitive loop is the spine of `rigorous-coding`. This reference expands each gate and gives concrete verification commands per language so "I verified it" means something specific.

## Why "gates" and not "steps"

A step is something you do; a gate is something you must **pass** before proceeding. If you reach gate 4 and cannot produce the evidence gate 2 demanded, you go back, not forward. The discipline is in the order: scope before evidence, evidence before adversarial, adversarial before verify, verify before report. Skipping ahead is how "looks right" replaces "is right."

## Gate 1 — Scope

**Question:** What exactly am I changing, and why?

Write a one-paragraph scope statement **before** touching code. Include:

- The change in one sentence: "Add retry-with-backoff to the `fetchUser` call in `auth.ts`."
- The trigger: "User report: sporadic 503s on login during deploys."
- The boundary: "Touch only `fetchUser` and its tests. Do not modify the global fetch wrapper."
- The load-bearing assumption: "I am assuming the 503s come from the upstream identity provider, not from our own gateway. If false, retry makes things worse."

If you cannot state the load-bearing assumption, you do not understand the change well enough to make it. Go back and read.

### Scope anti-patterns

| Anti-pattern | Example | Fix |
|--------------|---------|-----|
| Vague change | "Improve auth" | Name the specific function and behavior change |
| Unbounded boundary | "Clean up the auth module while I'm here" | Refuse scope creep; file a separate ticket |
| No trigger | "Just feels fragile" | Either reproduce a real failure or close the task |
| Hidden assumption | (no assumption stated) | Force yourself to name at least one |

## Gate 2 — Evidence

**Question:** What evidence grounds this change?

Every claim about why the bug exists, what the code currently does, or what the spec requires must be tied to a source you can cite.

| Claim type | Acceptable evidence | Unacceptable evidence |
|------------|---------------------|----------------------|
| "The bug is X" | Stack trace, repro command, `file:line` of the failing assertion | "I read the code and it looks like…" |
| "The spec requires Y" | Link to spec section, ticket, or user story | "Users probably expect…" |
| "This function does Z" | Reading the actual function | "It should do Z" |
| "The API returns W" | Actual response captured via `curl`/Postman/repro | "The docs say it returns W" |
| "Performance is the bottleneck" | Profile output naming the function | "It feels slow" |

### Evidence-gathering commands

```bash
# Read the actual code, not your memory of it
Read src/auth.ts                 # then cite line numbers in your scope

# Capture the actual error
node repro.js 2>&1 | tee error.log
python -c "import repro; repro.run()" 2>&1 | tee error.log

# Capture the actual API response
curl -v -H "Authorization: Bearer $T" https://api.example.com/users/me

# Capture the actual stack trace with full locals
pytest tests/test_auth.py::test_login --pdb -x
# or
node --inspect-brk repro.js
```

If your evidence is "I think the code does X," run a print or a debugger and **see** what it does. Memory of code is not evidence of code.

## Gate 3 — Adversarial

**Question:** What input would break this, make it lie, or make it silently wrong?

Run the adversarial matrix. See `adversarial-cases.md` for the full treatment — this gate just demands that you do it. The five cases:

1. **Empty** — `[]`, `""`, `0`, `null`, `{}`
2. **Missing** — required field absent, optional header absent, env var unset
3. **Malformed** — wrong type, wrong encoding, malformed UTF-8, structurally invalid JSON
4. **Concurrent** — two callers racing, mutation during iteration, partial writes
5. **Large** — 10M-row file, 100k-key object, deeply nested recursion, overflow

For each case, decide: handled, provably impossible (with proof), or known gap (with risk noted in the report). "Probably won't happen" is not a decision; it is a deferred bug.

Also ask the **wrong-but-plausible** question: what input makes this code return a result that *looks* correct but is not? Off-by-ones, timezone-naive timestamps, locale-dependent parsing, and silent type coercion are the usual suspects.

## Gate 4 — Verify

**Question:** Did I run something, and did it pass?

Verification is mechanical. It is not a feeling. List the commands and their actual output.

### Verification commands by language

**Python**

```bash
# Type check
mypy src/                           # or: pyright src/
pyright --pythonversion 3.11 src/

# Lint
ruff check src/ tests/
ruff format --check src/ tests/

# Tests (fast unit subset + the new test for the change)
pytest tests/test_dedup.py -q
pytest -x --ff                      # failed-first, stop on first failure

# Coverage delta (did the new test actually exercise the new code?)
pytest --cov=src/dedup --cov-report=term-missing tests/test_dedup.py

# Repro the original bug is now fixed
python repro_bug_1234.py
```

**TypeScript / JavaScript**

```bash
# Type check (no emit — types only)
tsc --noEmit
# or per-project
tsc -p tsconfig.json --noEmit

# Lint
eslint . --max-warnings=0
biome check src/

# Tests
pnpm test                           # or: npm test / yarn test
pnpm test -- --run                  # vitest, no watch
pnpm test src/dedup.test.ts         # one file

# Repro
pnpm tsx repro.ts
```

**Go**

```bash
go vet ./...
go build ./...
go test ./... -race -count=1        # -race for concurrent code, -count=1 disables cache
go test -run TestDedup ./pkg/dedup -v

# Reproduce
go run ./cmd/repro
```

**Rust**

```bash
cargo check --all-targets
cargo clippy --all-targets -- -D warnings
cargo fmt --check
cargo test --package dedup -- --nocapture

# Reproduce
cargo run --example repro
```

**Generic — universal checks**

```bash
# Did the diff actually change what I think it changed?
git diff --stat
git diff src/

# Did I leave debug prints / commented-out code / TODOs behind?
git diff | grep -E "^\+" | grep -iE "console\.log|print\(|debugger|TODO|FIXME|XXX"

# Are there failing tests I "fixed" by deleting them?
git diff -- tests/
```

### "It compiles" is not verification

Compiling tells you the syntax and types are internally consistent. It tells you nothing about whether the code does the right thing. A function that returns the wrong answer can compile perfectly. Verification requires at least one of:

- A test that exercises the new behavior and passes
- A repro of the original bug that now produces the correct output
- A property-based test that fails to find a counterexample after N runs
- A manual check whose commands and output you cite

If you have only "it compiles" or "the types check," say so: `VERIFIED: tsc --noEmit → no type errors. ASSUMED: behavior is correct, no test written yet.`

## Gate 5 — Report

**Question:** What did I verify, and what am I still only assuming?

Use the Output Template in `SKILL.md` and the labels in `honest-exit.md`. The report is the contract between you and the next reader (reviewer, oncall, future-you). If the report says VERIFIED, the reader should be able to rerun your command and get your result. If the report says ASSUMED, the reader knows what to double-check.

## Gate ordering matters

| Wrong order | Symptom |
|-------------|---------|
| Verify before adversarial | Tests pass but the bug is still there because you didn't test the case that triggers it |
| Report before verify | "Done!" followed by "actually, the tests fail" |
| Adversarial before evidence | You imagine edge cases that cannot occur given the real input shape, wasting effort |
| Scope skipped | The change balloons; you "fix" three bugs and verify none of them |

When in doubt, return to gate 1 and re-state scope. Most stuck debugging sessions are scope drift in disguise.

## Gate-checklist quick reference

```
[ ] Scope  : One-paragraph scope statement + named load-bearing assumption
[ ] Evidence: Every claim cited to file:line, repro command, or external spec
[ ] Adversarial: empty / missing / malformed / concurrent / large — each decided
[ ] Verify : ≥1 command run, output cited; not just "compiles"
[ ] Report : VERIFIED / ASSUMED labels applied; root cause or symptom-patch noted
```

If any checkbox is empty, the task is not done — it is in progress at best.
