#!/usr/bin/env bash
#
# Renders the Homebrew formula for a release from the archives the release
# workflow just built.
#
# The checksums come from the archives themselves rather than from the
# published release page: the artifacts are already on disk in the job that
# needs them, and reading them directly removes any window where the formula
# could describe assets that have not finished propagating.
#
# Usage:
#   ./scripts/render-formula.sh <version> <archive-dir> [output-file]
#
# <version> may be given with or without a leading "v". The output goes to
# stdout when no output file is named.
set -euo pipefail

VERSION="${1:?usage: render-formula.sh <version> <archive-dir> [output-file]}"
ARCHIVE_DIR="${2:?usage: render-formula.sh <version> <archive-dir> [output-file]}"
OUTPUT="${3:-}"

VERSION="${VERSION#v}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATE="$ROOT/.github/homebrew/slidepack.rb.tmpl"

[ -f "$TEMPLATE" ] || { echo "no formula template at $TEMPLATE" >&2; exit 1; }
[ -d "$ARCHIVE_DIR" ] || { echo "no such directory: $ARCHIVE_DIR" >&2; exit 1; }

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

formula="$(cat "$TEMPLATE")"
formula="${formula//@@VERSION@@/$VERSION}"

# Windows ships as a .zip and has no formula; the four unix targets are all
# required, because a tap that silently omits a platform is worse than a tap
# that fails to publish.
for target in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
  archive="$ARCHIVE_DIR/slidepack_${VERSION}_${target}.tar.gz"
  if [ ! -f "$archive" ]; then
    echo "missing release archive: $archive" >&2
    exit 1
  fi

  sum="$(sha256_of "$archive")"
  if [ -z "$sum" ]; then
    echo "could not checksum $archive" >&2
    exit 1
  fi

  placeholder="@@SHA_$(printf '%s' "$target" | tr '[:lower:]' '[:upper:]')@@"
  formula="${formula//$placeholder/$sum}"
done

case "$formula" in
  *@@*)
    echo "formula still contains unsubstituted placeholders" >&2
    printf '%s\n' "$formula" | grep -n '@@' >&2
    exit 1
    ;;
esac

if [ -n "$OUTPUT" ]; then
  printf '%s\n' "$formula" > "$OUTPUT"
else
  printf '%s\n' "$formula"
fi
