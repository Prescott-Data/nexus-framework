#!/usr/bin/env bash
#
# Version drift guard. Fails when any version surface disagrees with VERSION.
#
#   1. Stamping VERSION into every surface must produce no diff.
#   2. The first release heading in docs/CHANGELOG.md must be VERSION. A PR
#      that changes behaviour bumps VERSION and documents it under that
#      number; there is no "Unreleased" section, because merging is releasing.
#   3. No git tag may be newer than VERSION. A tag the file does not know
#      about means a release was cut by hand, outside the release workflow.
#
# Run locally with `make check-version`. CI runs it on every pull request.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
fail=0

# 1. Stamped surfaces: stamping must be a no-op. Compared by content, not
#    against git, so the check behaves the same on a dirty working tree.
stamped_files=(
  openapi.yaml nexus-broker/openapi.yaml mkdocs.yml
  nexus-sdk-ts/package.json nexus-sdk-python/pyproject.toml nexus-sdk-python/nexus_sdk/__init__.py
)
fingerprint() { shasum -a 256 "$ROOT/$1" | cut -d' ' -f1; }
# Indexed arrays only: stock macOS bash is 3.2 and has no associative arrays.
before=()
for f in "${stamped_files[@]}"; do before+=("$(fingerprint "$f")"); done
"$ROOT/scripts/stamp-version.sh" >/dev/null
drifted=()
for i in "${!stamped_files[@]}"; do
  f="${stamped_files[$i]}"
  [[ "${before[$i]}" == "$(fingerprint "$f")" ]] || drifted+=("$f")
done
if (( ${#drifted[@]} > 0 )); then
  echo "❌ Version drift: these files did not carry VERSION=${VERSION}. They have been re-stamped; review and commit:" >&2
  printf '   %s\n' "${drifted[@]}" >&2
  fail=1
fi

# 2. Changelog top section
top="$(grep -m1 -E '^## ' "$ROOT/docs/CHANGELOG.md" | sed -E 's/^## ([^ <]+).*/\1/')"
if [[ "$top" != "$VERSION" ]]; then
  echo "❌ docs/CHANGELOG.md: first release heading is '${top}', VERSION is '${VERSION}'. Document this release under '## ${VERSION}'." >&2
  fail=1
fi

# 3. Tags newer than VERSION
latest_tag="$(git -C "$ROOT" tag -l 'v*' | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)"
if [[ -n "$latest_tag" ]]; then
  newest="$(printf '%s\n%s\n' "$latest_tag" "$VERSION" | sort -V | tail -1)"
  if [[ "$newest" != "$VERSION" ]]; then
    echo "❌ Tag v${latest_tag} is newer than VERSION=${VERSION}. A release was tagged outside the release workflow. Bump VERSION past it." >&2
    fail=1
  fi
fi

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
echo "✅ Version ${VERSION} is in sync: stamped files, changelog, tags."
