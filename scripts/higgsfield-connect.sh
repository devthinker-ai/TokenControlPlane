#!/usr/bin/env sh
# Quick-connect helper: Higgsfield MCP OAuth device flow.
# Prints a URL to open in your browser; after you sign in, prints the access token.
# Usage: sh scripts/higgsfield-connect.sh
set -eu

AUTH_BASE="https://fnf-device-auth.higgsfield.ai"
CLIENT_ID="tokencontrolplane"
SCOPE="openid email offline_access"

echo "Requesting device code..."
RESP=$(curl -s -X POST "$AUTH_BASE/authorize" -H 'Content-Type: application/json' \
  -d "{\"client_id\":\"$CLIENT_ID\",\"scope\":\"$SCOPE\"}")

DEVICE_CODE=$(printf '%s' "$RESP" | sed -n 's/.*"device_code":"\([^"]*\)".*/\1/p')
VERIFY_URL=$(printf '%s' "$RESP" | sed -n 's/.*"verification_uri":"\([^"]*\)".*/\1/p')
EXPIRES=$(printf '%s' "$RESP" | sed -n 's/.*"expires_in":\([0-9]*\).*/\1/p')
INTERVAL=$(printf '%s' "$RESP" | sed -n 's/.*"interval":\([0-9]*\).*/\1/p')

[ -n "$DEVICE_CODE" ] || { echo "Failed to get device code: $RESP"; exit 1; }

echo ""
echo "1. Open this URL in your browser and sign in to Higgsfield:"
echo ""
echo "   $VERIFY_URL"
echo ""
echo "   (code expires in ${EXPIRES:-900}s — polling now, every ${INTERVAL:-3}s)"
echo "2. After you sign in, the access token will be printed here."
echo ""
[ -n "${BROWSER:-}" ] && "$BROWSER" "$VERIFY_URL" 2>/dev/null || true

DEADLINE=$(( $(date +%s) + ${EXPIRES:-900} ))
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  sleep "${INTERVAL:-3}"
  TRESP=$(curl -s -X POST "$AUTH_BASE/token" -H 'Content-Type: application/json' \
    -d "{\"grant_type\":\"urn:ietf:params:oauth:grant-type:device_code\",\"device_code\":\"$DEVICE_CODE\",\"client_id\":\"$CLIENT_ID\"}")
  case "$TRESP" in
    *authorization_pending*) continue ;;
    *slow_down*) sleep 5; continue ;;
    *access_token*)
      echo ""
      echo "=== CONNECTED ==="
      printf '%s' "$TRESP" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p' | \
        { read -r AT; echo "Access token (paste into the server's auth value as: Bearer $AT):"; echo "$AT"; }
      echo ""
      echo "Note: access tokens expire. For a durable connection, see Phase 10 (dashboard device-flow connect with auto-refresh)."
      exit 0 ;;
    *expired_token*)
      echo "Device code expired before sign-in. Run the script again."; exit 1 ;;
    *)
      echo "Unexpected response: $TRESP"; exit 1 ;;
  esac
done
echo "Timed out waiting for sign-in."; exit 1
