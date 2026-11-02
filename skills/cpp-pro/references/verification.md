# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**.

```
VERIFIED:
  - cmake --build build-asan      → built without warnings
  - clang-tidy -p build src/*.cpp → no findings
  - ctest --test-dir build-asan --output-on-failure
    Test  #1: parser_test ......... Passed
    100% tests passed, 2 tests passed
  - ASan run of myapp_test         → no leaks, no overflows
ASSUMED:
  - Performance under production load (bench in CI only, no real data)
  - MSVC behavior (only GCC/Clang in CI)
  - ThreadSanitizer not run (single-threaded code path tested)
Lingering risk:
  - reinterpret_cast in src/hash.cpp:42 (NOLINT with reason: pointer hashing)
```

## VERIFIED — what counts

Only what you ran and saw:

- A clean build with `-Werror`
- `clang-tidy` no findings (or justified `NOLINT`s)
- `ctest` pass count
- ASan/UBSan/TSan runs with no errors
- A benchmark you actually ran

## ASSUMED — what to flag

- Cross-compiler behavior you didn't run
- Performance claims without a benchmark
- Concurrency correctness not exercised by TSan
- Behavior on platforms not in CI (e.g. Windows if you only tested Linux)
- Third-party library behavior you didn't run

## The verification loop

```bash
cmake --preset asan
cmake --build --preset asan --parallel
ctest --preset asan
ASAN_OPTIONS=detect_leaks=1:abort_on_error=1 ./build-asan/myapp_test
clang-tidy -p build-asan src/*.cpp
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top — an earlier step may now break

Never:
- Add `NOLINT` without a reason
- `#pragma GCC diagnostic ignored` without a reason
- Disable a test with `EXPECT_DEATH` or skip to go green
- Weaken `-Werror` to silence warnings

## When you cannot run a gate

If TSan isn't available or MSVC isn't installed:

```
ASSUMED:
  - TSan not run (no ThreadSanitizer in this environment); ASan + UBSan ran clean
  - MSVC build not verified (Linux-only CI); code uses standard C++ only
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- run: cmake --preset asan
- run: cmake --build --preset asan --parallel
- run: ctest --preset asan
- run: ASAN_OPTIONS=detect_leaks=1 ./build-asan/myapp_test
- run: clang-tidy -p build-asan src/*.cpp
```

Run a separate job with TSan for any code touching `std::thread`/`std::atomic`/`std::mutex`.

## Sanitizer recipes

### ASan + UBSan (combined)
```bash
cmake -B build -DCMAKE_CXX_FLAGS="-fsanitize=address,undefined -fno-omit-frame-pointer -g" \
              -DCMAKE_EXE_LINKER_FLAGS="-fsanitize=address,undefined"
cmake --build build
ASAN_OPTIONS=detect_leaks=1:abort_on_error=1 UBSAN_OPTIONS=print_stacktrace=1 ./build/myapp
```

### TSan
```bash
cmake -B build-tsan -DCMAKE_CXX_FLAGS="-fsanitize=thread -g" \
                    -DCMAKE_EXE_LINKER_FLAGS="-fsanitize=thread"
cmake --build build-tsan
TSAN_OPTIONS=second_deadlock_stack=1 ./build-tsan/myapp
```

### LeakSanitizer (part of ASan)
```bash
ASAN_OPTIONS=detect_leaks=1 ./build/myapp
```

If LSan can't run (e.g. on some macOS setups), use Valgrind:
```bash
valgrind --leak-check=full --error-exitcode=1 ./build/myapp
```

## `NOLINT` audit

```bash
grep -rn "NOLINT" src/ | grep -v "// .*reason"
```

Every `NOLINT` should have a reason comment. clang-tidy's `HeaderFilterRegex: '.'` will check headers too.

## Pre-commit hooks

```bash
#!/bin/sh
# .git/hooks/pre-commit
clang-format --dry-run --Werror $(git diff --cached --name-only --diff-filter=ACM | grep -E '\.(cpp|h|hpp)$') || exit 1
```

Catches formatting issues before they reach CI. Not a substitute for the explicit verification gate.

## The exit checklist

Before writing the final report:

- [ ] `-Wall -Wextra -Wpedantic -Werror` clean
- [ ] `clang-tidy` clean or justified `NOLINT`s
- [ ] `ctest` green
- [ ] ASan + UBSan runs clean
- [ ] TSan run for concurrent code
- [ ] No new `NOLINT` without reason
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
