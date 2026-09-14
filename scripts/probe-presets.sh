#!/usr/bin/env bash
# Dev-only live probe for verified HTTP MCP presets.
# POSTs initialize to each verified remote URL; expects 200, 401, 406, or 307.
# Not run in CI — operators run before tagging a release that touches the catalog.
#
# Usage:  ./scripts/probe-presets.sh
# Exit:   0 if all verified remotes look alive; 1 if any unexpected status.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CATALOG="$ROOT/frontend/src/data/catalog.ts"

if [[ ! -f "$CATALOG" ]]; then
  echo "catalog not found: $CATALOG" >&2
  exit 2
fi

ENTRIES=()
while IFS= read -r line; do
  [[ -n "$line" ]] && ENTRIES+=("$line")
done < <(CATALOG="$CATALOG" node --input-type=module -e '
import { readFileSync } from "node:fs";
const src = readFileSync(process.env.CATALOG, "utf8");
const blocks = src.split(/\{\s*\n\s*id:/).slice(1);
for (const b of blocks) {
  const id = (b.match(/^\s*['\''"]([^'\''"]+)['\''"]/) || [])[1];
  const transport = (b.match(/transport:\s*['\''"]([^'\''"]+)['\''"]/) || [])[1];
  const head = b.split(/auth:/)[0] || b;
  const verified = /verified:\s*true/.test(head);
  const base = (b.match(/base_url:\s*['\''"]([^'\''"]+)['\''"]/) || [])[1];
  if (id && transport === "http" && verified && base) console.log(id + "|" + base);
}
')

if [[ ${#ENTRIES[@]} -eq 0 ]]; then
  echo "no verified HTTP presets parsed from catalog" >&2
  exit 2
fi

INIT_BODY='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"tokencontrolplane-probe","version":"0.0.0"}}}'

printf '%-20s %-50s %s\n' 'ID' 'URL' 'STATUS'
printf '%-20s %-50s %s\n' '---' '---' '------'

fail=0
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

for entry in "${ENTRIES[@]}"; do
  id="${entry%%|*}"
  url="${entry#*|}"
  code=$(curl -sS -o "$tmpdir/body" -w '%{http_code}' \
    --max-time 15 \
    -X POST "$url" \
    -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    -d "$INIT_BODY" || echo '000')
  ok='FAIL'
  case "$code" in
    200|401|406|307) ok='ok' ;;
  esac
  printf '%-20s %-50s %s (%s)\n' "$id" "$url" "$code" "$ok"
  if [[ "$ok" != 'ok' ]]; then
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  echo >&2
  echo "One or more verified presets returned an unexpected status." >&2
  echo "Update verified flags in frontend/src/data/catalog.ts or fix URLs." >&2
  exit 1
fi
echo
echo "All verified remote presets look alive."
