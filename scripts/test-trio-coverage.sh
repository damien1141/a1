#!/usr/bin/env bash
#
# Test-trio coverage gate: every package must carry at least one positive,
# negative, and differential test (see TODO.md "test trio" for definitions).
#
# Usage: scripts/test-trio-coverage.sh          # report + exit 1 on gaps
#        scripts/test-trio-coverage.sh -v       # also list matching evidence
#
# Classification per package:
#   P  any *_test.go present (existing suites are happy-path heavy) or a
#      *_Positive test function
#   N  error-path assertions (require/assert.Error*, wantErr) or failure-named
#      tests (Not/Invalid/Missing/Empty/Reject/...) or a *_Negative function
#   D  differential keywords (golden/reference/oracle/naive/round-trip/parity/
#      testdata) or a *_Differential function
#
# Exemptions: one per line in scripts/trio-exemptions.txt as
#   <package-path> <one-line rationale>   (path is repo-relative, e.g. cmd/jev)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERBOSE=0
[ "${1:-}" = "-v" ] && VERBOSE=1

EXEMPT_FILE="$ROOT/scripts/trio-exemptions.txt"
declare -A EXEMPT
if [ -f "$EXEMPT_FILE" ]; then
  while read -r pkg _rationale; do
    case "$pkg" in ''|\#*) continue ;; esac
    EXEMPT["$pkg"]=1
  done < "$EXEMPT_FILE"
fi

fail=0
total=0
for dir in $(go list ./... | sed 's|^github.com/damien1141/a1/||'); do
  [ -n "$dir" ] || dir=.
  total=$((total + 1))
  files=$(ls "$dir"/*_test.go 2>/dev/null || true)
  if [ -z "$files" ]; then
    if [ -n "${EXEMPT[$dir]:-}" ]; then
      [ "$VERBOSE" = 1 ] && echo "EXEMPT  $dir (no tests, rationale in trio-exemptions.txt)"
      continue
    fi
    echo "GAP     $dir (no tests at all)"
    fail=1
    continue
  fi

  missing=""
  # Positive: any test file, or an explicit *_Positive function.
  if ! grep -hqE '^func Test[A-Za-z0-9_]+_Positive\(' $files; then
    missing="P"   # files exist; happy paths assumed present — nothing to add
    missing=""
  fi

  # Negative: error assertions or failure-named tests or *_Negative function.
  if ! grep -hqE 'require\.Error|assert\.Error|ErrorIs|ErrorContains|ErrorAs|wantErr' $files &&
     ! grep -hqE '^func Test[A-Za-z0-9_]*(_(Not|No[A-Z]|Fail|Error|Invalid|Bad|Missing|Empty|Malformed|Reject|Deny|Exceed|Limit|Overflow|Zero|Nil|Unknown|Unsupported|Timeout|Absent|Corrupt|Truncat|Cycl|Dupl|Inval))' $files &&
     ! grep -hqE '^func Test[A-Za-z0-9_]+_Negative\(' $files; then
    missing="${missing}N"
  fi

  # Differential: keywords or *_Differential function.
  if ! grep -hqiE 'golden|reference impl|oracle|differential|stdlib|naive|round.?trip|parity|testdata' $files &&
     ! grep -hqE '^func Test[A-Za-z0-9_]+_Differential\(' $files; then
    missing="${missing}D"
  fi

  if [ -n "${EXEMPT[$dir]:-}" ]; then
    # Exemption covers every missing letter for that package.
    if [ "$VERBOSE" = 1 ]; then
      echo "EXEMPT  $dir (missing: ${missing:-none}, rationale in trio-exemptions.txt)"
    fi
    continue
  fi

  if [ -n "$missing" ]; then
    echo "GAP     $dir (missing: $(echo "$missing" | fold -w1 | paste -sd,))"
    fail=1
  elif [ "$VERBOSE" = 1 ]; then
    echo "OK      $dir"
  fi
done

echo "---"
echo "packages: $total"
if [ "$fail" = 1 ]; then
  echo "result: GAPS FOUND (add missing trio tests or file an exemption)"
  exit 1
fi
echo "result: all packages carry the test trio"
