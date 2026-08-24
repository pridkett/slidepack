#!/usr/bin/env bash
#
# The canonical verification gate for slidepack.
#
# Runs everything CI runs, in the order that fails fastest: formatting, then
# static analysis, then Go tests, then a real end-to-end exercise of the built
# binary, then the browser suites in Chromium and Firefox.
#
# The browser tests are not optional. A packed presentation is only correct if
# two real engines agree that it renders, so anything that replaces them with a
# mock has stopped verifying the thing that matters.
#
# Usage:
#   ./scripts/verify.sh              # everything
#   ./scripts/verify.sh --no-browser # skip the browser suites
#   ./scripts/verify.sh --short      # skip the multi-megabyte scale test
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"
BROWSER_TESTS=1
GO_TEST_FLAGS=()

for arg in "$@"; do
  case "$arg" in
    --no-browser) BROWSER_TESTS=0 ;;
    --short)      GO_TEST_FLAGS+=("-short") ;;
    -h|--help)
      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "verify.sh: unknown option $arg" >&2
      exit 2
      ;;
  esac
done

FAILURES=()
step() {
  printf '\n\033[1m==> %s\033[0m\n' "$1"
}
note() {
  printf '    %s\n' "$1"
}

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "verify.sh: required tool '$1' is not installed" >&2
    exit 1
  fi
}

require go

# ---------------------------------------------------------------------------
step "gofmt"
# ---------------------------------------------------------------------------
unformatted="$(gofmt -l . 2>/dev/null || true)"
if [[ -n "$unformatted" ]]; then
  echo "These files are not gofmt-clean:" >&2
  echo "$unformatted" >&2
  echo >&2
  echo "Run: gofmt -w ." >&2
  FAILURES+=("gofmt")
else
  note "clean"
fi

# ---------------------------------------------------------------------------
step "go vet"
# ---------------------------------------------------------------------------
if go vet ./...; then
  note "clean"
else
  FAILURES+=("go vet")
fi

# ---------------------------------------------------------------------------
step "go build"
# ---------------------------------------------------------------------------
BIN="$(mktemp -d)/slidepack"
if go build -o "$BIN" ./cmd/slidepack; then
  note "built $(basename "$BIN")"
else
  FAILURES+=("go build")
  echo "verify.sh: cannot continue without a binary" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
step "go test (unit and integration)"
# ---------------------------------------------------------------------------
if go test "${GO_TEST_FLAGS[@]+"${GO_TEST_FLAGS[@]}"}" ./...; then
  note "passed"
else
  FAILURES+=("go test")
fi

# ---------------------------------------------------------------------------
step "command-line smoke test"
# ---------------------------------------------------------------------------
# Exercises the built binary the way a person would, so that a mistake in
# argument handling or exit codes cannot hide behind the Go tests.
SMOKE="$(mktemp -d)"
smoke_ok=1

run_expect() {
  local want=$1; shift
  set +e
  "$@" >"$SMOKE/out.txt" 2>"$SMOKE/err.txt"
  local got=$?
  set -e
  if [[ "$got" != "$want" ]]; then
    echo "  FAIL: expected exit $want, got $got from: $*" >&2
    sed 's/^/        /' "$SMOKE/err.txt" >&2
    smoke_ok=0
  fi
}

run_expect 0 "$BIN" version
run_expect 0 "$BIN" --version
run_expect 0 "$BIN" -v
run_expect 0 "$BIN" --help
run_expect 0 "$BIN" -h
run_expect 0 "$BIN" help
run_expect 0 "$BIN" help pack
run_expect 0 "$BIN" help --json
run_expect 0 "$BIN" help --all
run_expect 2 "$BIN" help nosuchcommand
run_expect 2 "$BIN" nosuchcommand
run_expect 0 "$BIN" pack --help
run_expect 0 "$BIN" unpack --help
run_expect 0 "$BIN" validate --help
run_expect 0 "$BIN" inspect --help
run_expect 0 "$BIN" validate testdata/basic
run_expect 3 "$BIN" validate testdata/invalid/missing-resource
run_expect 0 "$BIN" pack testdata/basic -o "$SMOKE/basic.html"
run_expect 1 "$BIN" pack testdata/basic -o "$SMOKE/basic.html"          # refuses to overwrite
run_expect 0 "$BIN" pack testdata/basic -o "$SMOKE/basic.html" --force
run_expect 0 "$BIN" validate "$SMOKE/basic.html"
run_expect 0 "$BIN" inspect "$SMOKE/basic.html"
run_expect 0 "$BIN" inspect --json "$SMOKE/basic.html"
run_expect 0 "$BIN" unpack "$SMOKE/basic.html" -o "$SMOKE/restored"

# Exactly one file, no companion directory (AC-002).
produced=$(find "$SMOKE" -maxdepth 1 -name 'basic*' | wc -l | tr -d ' ')
if [[ "$produced" != "1" ]]; then
  echo "  FAIL: pack produced $produced entries named basic*, expected exactly 1" >&2
  smoke_ok=0
fi

# Round trip is byte-exact (AC-013, AC-014).
if ! diff -r testdata/basic "$SMOKE/restored" >/dev/null; then
  echo "  FAIL: unpack(pack(testdata/basic)) differs from the source" >&2
  diff -r testdata/basic "$SMOKE/restored" | sed 's/^/        /' >&2
  smoke_ok=0
fi

# Determinism survives an mtime change (AC-016, AC-017).
cp -R testdata/basic "$SMOKE/mtime-src"
"$BIN" pack "$SMOKE/mtime-src" -o "$SMOKE/one.html" --quiet
touch "$SMOKE/mtime-src/index.html"
"$BIN" pack "$SMOKE/mtime-src" -o "$SMOKE/two.html" --quiet
if ! cmp -s "$SMOKE/one.html" "$SMOKE/two.html"; then
  echo "  FAIL: packed output changed when only an mtime changed" >&2
  smoke_ok=0
fi

# Colour must never leak into a pipe, and must never appear in JSON.
if "$BIN" validate testdata/basic | grep -q $'\033'; then
  echo "  FAIL: colour escapes appeared in piped output" >&2
  smoke_ok=0
fi
if "$BIN" validate --json testdata/basic --color always | grep -q $'\033'; then
  echo "  FAIL: colour escapes appeared in --json output" >&2
  smoke_ok=0
fi
if ! "$BIN" validate testdata/basic --color always | grep -q $'\033'; then
  echo "  FAIL: --color always produced no colour" >&2
  smoke_ok=0
fi
if NO_COLOR=1 "$BIN" validate testdata/basic --color always | grep -q $'\033'; then
  echo "  FAIL: NO_COLOR did not override --color always" >&2
  smoke_ok=0
fi

# The archive is a single payload readable by an independent tar (AC-041).
python3 - "$SMOKE/basic.html" "$SMOKE/payload.tar.gz" <<'PY'
import base64, sys
html = open(sys.argv[1], encoding="utf-8").read()
n = html.count('type="application/octet-stream"')
assert n == 1, f"expected exactly one payload element, found {n}"
i = html.index('id="slidepack-payload"')
s = html.index(">", i) + 1
e = html.index("</script", s)
open(sys.argv[2], "wb").write(base64.b64decode(html[s:e].strip()))
PY
if ! tar tf "$SMOKE/payload.tar.gz" >/dev/null 2>&1; then
  echo "  FAIL: the embedded payload is not a tar archive an independent tool can read" >&2
  smoke_ok=0
fi

if [[ "$smoke_ok" == "1" ]]; then
  note "passed"
else
  FAILURES+=("cli smoke test")
fi
rm -rf "$SMOKE"

# ---------------------------------------------------------------------------
step "machine-readable interface"
# ---------------------------------------------------------------------------
# help/validate/inspect --json are a contract that agents depend on. Checking
# the shape here means a refactor that drops a field fails a build rather than
# somebody's automation.
if ! python3 ./scripts/check-interface.py "$BIN"; then
  FAILURES+=("interface check")
fi

# ---------------------------------------------------------------------------
if [[ "$BROWSER_TESTS" == "1" ]]; then
  step "browser end-to-end (Chromium and Firefox)"
  require node
  require npm
  cd "$ROOT/tests/browser"

  if [[ ! -d node_modules ]]; then
    note "installing browser test dependencies"
    npm ci --no-audit --no-fund 2>/dev/null || npm install --no-audit --no-fund
  fi
  note "ensuring browser binaries are present"
  npx --no-install playwright install chromium firefox >/dev/null

  browsers_ok=1
  for project in chromium firefox; do
    printf '\n    --- %s ---\n' "$project"
    if ! npx --no-install playwright test --project="$project" --reporter=list; then
      browsers_ok=0
      FAILURES+=("browser: $project")
    fi
  done
  cd "$ROOT"
  if [[ "$browsers_ok" == "1" ]]; then
    note "both engines passed"
  fi
else
  step "browser end-to-end"
  note "SKIPPED (--no-browser)"
  echo
  echo "WARNING: the browser suites are the only check that a packed file" >&2
  echo "actually renders. Do not treat a --no-browser run as verification." >&2
fi

# ---------------------------------------------------------------------------
printf '\n'
if [[ ${#FAILURES[@]} -eq 0 ]]; then
  printf '\033[32m==> verification passed\033[0m\n'
  exit 0
fi
printf '\033[31m==> verification FAILED\033[0m\n'
for f in "${FAILURES[@]}"; do
  printf '    - %s\n' "$f"
done
exit 1
