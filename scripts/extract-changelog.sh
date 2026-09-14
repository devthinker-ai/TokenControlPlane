#!/usr/bin/env bash
# Extract the Keep-a-Changelog section for a version tag from CHANGELOG.md.
# Usage: scripts/extract-changelog.sh v1.2.3 [CHANGELOG.md]
# Fails (exit 1) when the section is missing — CI uses this to gate releases.
set -euo pipefail

TAG="${1:-}"
FILE="${2:-CHANGELOG.md}"
if [[ -z "$TAG" ]]; then
  echo "usage: $0 vX.Y.Z [CHANGELOG.md]" >&2
  exit 2
fi
VER="${TAG#v}"

if [[ ! -f "$FILE" ]]; then
  echo "CHANGELOG.md has no entry for v${VER} — add one (file missing: $FILE)" >&2
  exit 1
fi

# Print from "## [VER]" / "## [vVER]" through the line before the next "## ".
awk -v ver="$VER" '
  BEGIN { want1="## [" ver "]"; want2="## [v" ver "]"; found=0 }
  {
    if (!found) {
      if (index($0, want1) == 1 || index($0, want2) == 1) { found=1; print; next }
      next
    }
    if ($0 ~ /^## /) { exit }
    print
  }
  END {
    if (!found) {
      printf("CHANGELOG.md has no entry for v%s — add one\n", ver) > "/dev/stderr"
      exit 1
    }
  }
' "$FILE"
