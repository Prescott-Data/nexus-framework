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

# OpenAPI info.version — the first 2-space-indented `version:` key (under info:)
stamp_openapi() {
  local file="$1"
  [[ -f "$file" ]] || return 0
  sed -i -E "0,/^  version:.*/s//  version: ${VERSION}/" "$file"
}

stamp_openapi "$ROOT/openapi.yaml"
stamp_openapi "$ROOT/nexus-broker/openapi.yaml"

# mkdocs extra.version (quoted string)
sed -i -E "s/^  version: \".*\"/  version: \"${VERSION}\"/" "$ROOT/mkdocs.yml"

echo "Stamped version ${VERSION} into: openapi.yaml, nexus-broker/openapi.yaml, mkdocs.yml"
