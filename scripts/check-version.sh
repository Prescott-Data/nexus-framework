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
VERSION="$(sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//' "$ROOT/VERSION" | head -1)"
fail=0

# 1. Stamped surfaces: stamping must be a no-op. Compared by content, not
#    against git, so the check behaves the same on a dirty working tree.
stamped_files=(
  openapi.yaml nexus-broker/openapi.yaml mkdocs.yml
  nexus-sdk-ts/package.json nexus-sdk-ts/package-lock.json
  nexus-sdk-python/pyproject.toml nexus-sdk-python/nexus_sdk/__init__.py
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

# 1b. Each surface must actually carry the field. A no-op stamp proves nothing
#     if the field was deleted or reformatted so that no expression matched.
expect() {
  local file="$1" pattern="$2"
  if ! grep -qE "$pattern" "$ROOT/$file"; then
    echo "❌ ${file}: expected a line matching /${pattern}/ carrying VERSION=${VERSION}." >&2
    fail=1
  fi
}
v="$(printf '%s' "$VERSION" | sed 's/\./\\./g')"
expect openapi.yaml                           "^  version: ${v}\$"
expect nexus-broker/openapi.yaml              "^  version: ${v}\$"
expect mkdocs.yml                             "^  version: \"${v}\"\$"
expect nexus-sdk-ts/package.json              "^  \"version\": \"${v}\",?\$"
expect nexus-sdk-ts/package-lock.json         "^  \"version\": \"${v}\",?\$"
# The root package entry is the object keyed "" under "packages"; its version
# is read in place so a dependency with the same number cannot satisfy the check.
lock_root_version="$(awk '
  /^    "": \{/ { inroot = 1; next }
  inroot && /^    \}/ { exit }
  inroot && /^      "version": "/ { gsub(/.*"version": "|",?$/, ""); print; exit }
' "$ROOT/nexus-sdk-ts/package-lock.json")"
if [[ "$lock_root_version" != "$VERSION" ]]; then
  echo "❌ nexus-sdk-ts/package-lock.json: packages[\"\"].version is '${lock_root_version:-missing}', VERSION is '${VERSION}'." >&2
  fail=1
fi
expect nexus-sdk-python/pyproject.toml        "^version = \"${v}\"\$"
expect nexus-sdk-python/nexus_sdk/__init__.py "^__version__ = \"${v}\"\$"

# 2. Changelog top section
top="$(grep -m1 -E '^## ' "$ROOT/docs/CHANGELOG.md" | sed -E 's/^## ([^ <]+).*/\1/')"
if [[ "$top" != "$VERSION" ]]; then
  echo "❌ docs/CHANGELOG.md: first release heading is '${top}', VERSION is '${VERSION}'. Document this release under '## ${VERSION}'." >&2
  fail=1
fi

# 3. Tags newer than VERSION. Compared numerically in awk; `sort -V` is not
#    available on every BSD/busybox sort.
semver_gt() { # semver_gt A B: true when A > B
  awk -v a="$1" -v b="$2" 'BEGIN {
    split(a, x, "."); split(b, y, ".")
    for (i = 1; i <= 3; i++) { if (x[i]+0 > y[i]+0) exit 0; if (x[i]+0 < y[i]+0) exit 1 }
    exit 1
  }'
}
latest_tag=""
for t in $(git -C "$ROOT" tag -l 'v*' | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' || true); do
  if [[ -z "$latest_tag" ]] || semver_gt "$t" "$latest_tag"; then latest_tag="$t"; fi
done
if [[ -n "$latest_tag" ]] && semver_gt "$latest_tag" "$VERSION"; then
  echo "❌ Tag v${latest_tag} is newer than VERSION=${VERSION}. A release was tagged outside the release workflow. Bump VERSION past it." >&2
  fail=1
fi

# 3b. VERSION equal to an existing tag is fine only when that release is in
#     this branch's history (the normal state of every non-release PR). A tag
#     with the same number that is not an ancestor means VERSION was set to a
#     number already used elsewhere; the release workflow would refuse it.
if git -C "$ROOT" rev-parse -q --verify "refs/tags/v${VERSION}^{commit}" >/dev/null 2>&1; then
  if ! git -C "$ROOT" merge-base --is-ancestor "refs/tags/v${VERSION}^{commit}" HEAD; then
    echo "❌ Tag v${VERSION} already exists on a commit that is not in this branch's history. Tags are never moved; bump VERSION." >&2
    fail=1
  fi
fi

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
echo "✅ Version ${VERSION} is in sync: stamped files, changelog, tags."
