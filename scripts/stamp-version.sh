#!/usr/bin/env bash
#
# Single source of truth: the root VERSION file.
#
# This script writes that version into every file that carries one, so a
# release is one edit to VERSION followed by `make stamp`. Binaries get the
# same value through the VERSION build argument in the Dockerfiles and
# docker-compose, which the image workflow reads from this file as well.
#
# Stamped surfaces:
#   openapi.yaml, nexus-broker/openapi.yaml   info.version
#   mkdocs.yml                                extra.version
#   nexus-sdk-ts/package.json                 "version"
#   nexus-sdk-ts/package-lock.json            root "version" and packages[""].version
#   nexus-sdk-python/pyproject.toml           version
#   nexus-sdk-python/nexus_sdk/__init__.py    __version__
#
# Usage:
#   scripts/stamp-version.sh            # write the version into all targets
#
# scripts/check-version.sh runs this and fails on any resulting diff, on a
# changelog whose top section is not VERSION, and on a git tag newer than
# VERSION. CI runs that check on every pull request.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"

if [[ -z "$VERSION" ]]; then
  echo "error: root VERSION file is empty" >&2
  exit 1
fi
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: VERSION must be MAJOR.MINOR.PATCH, got '${VERSION}'" >&2
  exit 1
fi

# In-place edit that works with both GNU and BSD sed. BSD sed reads the argument
# after -i as a backup suffix, so `sed -i -E` there writes a file named `-E` and
# leaves the original untouched, which stamps nothing and looks like it worked.
edit_in_place() {
  local expression="$1" file="$2" temporary
  [[ -f "$file" ]] || return 0
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

# TypeScript SDK: top-level "version" in package.json
edit_in_place "s/^  \"version\": \"[^\"]*\"/  \"version\": \"${VERSION}\"/" "$ROOT/nexus-sdk-ts/package.json"

# TypeScript SDK lockfile: the top-level "version" (2-space indent) and the
# "version" inside the root package entry, the object keyed "" under
# "packages". Only lines inside that object are touched; dependency entries
# further down are left alone even if the root field is missing.
stamp_lockfile() {
  local file="$1" temporary
  [[ -f "$file" ]] || return 0
  temporary="$(mktemp)"
  awk -v version="$VERSION" '
    !top && /^  "version": "/            { print "  \"version\": \"" version "\","; top = 1; next }
    /^    "": \{/                          { inroot = 1 }
    inroot && /^    \}/                   { inroot = 0 }
    inroot && !root && /^      "version": "/ { print "      \"version\": \"" version "\","; root = 1; next }
    { print }
  ' "$file" > "$temporary" && mv "$temporary" "$file"
}
stamp_lockfile "$ROOT/nexus-sdk-ts/package-lock.json"

# Python SDK: pyproject [project] version and the package __version__
edit_in_place "s/^version = \"[^\"]*\"/version = \"${VERSION}\"/" "$ROOT/nexus-sdk-python/pyproject.toml"
edit_in_place "s/^__version__ = \"[^\"]*\"/__version__ = \"${VERSION}\"/" "$ROOT/nexus-sdk-python/nexus_sdk/__init__.py"

echo "Stamped version ${VERSION} into: openapi.yaml, nexus-broker/openapi.yaml, mkdocs.yml, nexus-sdk-ts/package.json, nexus-sdk-ts/package-lock.json, nexus-sdk-python/pyproject.toml, nexus-sdk-python/nexus_sdk/__init__.py"
