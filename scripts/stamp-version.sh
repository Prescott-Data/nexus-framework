#!/usr/bin/env bash
#
# Single source of truth: the root VERSION file.
# This script propagates that version into every file that must stay in
# lockstep with a release (OpenAPI specs and the docs site). Binaries derive
# their version separately via ldflags (see docker-compose build args and the
# service Dockerfiles/Makefiles), also reading the same VERSION file.
#
# Usage:
#   scripts/stamp-version.sh            # write the version into all targets
#
# To detect drift in CI, run this script and then `git diff --exit-code`
# (see the `check-version` target in the root Makefile).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"

if [[ -z "$VERSION" ]]; then
  echo "error: root VERSION file is empty" >&2
  exit 1
fi

# In-place edit that works with both GNU and BSD sed. BSD sed reads the argument
# after -i as a backup suffix, so `sed -i -E` there writes a file named `-E` and
# leaves the original untouched, which stamps nothing and looks like it worked.
edit_in_place() {
  local expression="$1" file="$2" temporary
  temporary="$(mktemp)"
  sed -E "$expression" "$file" > "$temporary" && mv "$temporary" "$file"
}

# OpenAPI info.version — the first 2-space-indented `version:` key (under info:)
# Uses awk rather than sed's `0,/re/` address, which is a GNU extension that BSD
# sed rejects, so the stamp silently did nothing on macOS.
stamp_openapi() {
  local file="$1" temporary
  [[ -f "$file" ]] || return 0
  temporary="$(mktemp)"
  awk -v version="$VERSION" '
    !stamped && /^  version:/ { print "  version: " version; stamped = 1; next }
    { print }
  ' "$file" > "$temporary" && mv "$temporary" "$file"
}

stamp_openapi "$ROOT/openapi.yaml"
stamp_openapi "$ROOT/nexus-broker/openapi.yaml"

# mkdocs extra.version (quoted string)
edit_in_place "s/^  version: \".*\"/  version: \"${VERSION}\"/" "$ROOT/mkdocs.yml"

echo "Stamped version ${VERSION} into: openapi.yaml, nexus-broker/openapi.yaml, mkdocs.yml"
