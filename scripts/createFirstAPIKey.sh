#!/usr/bin/env bash
# =============================================================================
# createFirstAPIKey.sh
#
# Bootstrap script: creates your first API key via the CDN API, then prints
# a ready-to-paste curl cheat-sheet covering every available endpoint.
#
# Usage:
#   ./scripts/createFirstAPIKey.sh [--name NAME] [--host HOST]
#
# Optional flags:
#   --name NAME   Human-readable label for the API key (default: "bootstrap")
#   --host HOST   API base URL, no trailing slash
#                 (default: reads API_PREFIX & APP_PORT from .env, else
#                  falls back to http://localhost:4500/api/v1)
#   -h, --help    Show this help
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── Load .env ─────────────────────────────────────────────────────────────────
ENV_FILE="$PROJECT_ROOT/.env"
if [[ -f "$ENV_FILE" ]]; then
    set -a; source "$ENV_FILE"; set +a
fi

# ── Defaults ──────────────────────────────────────────────────────────────────
APP_PORT="${APP_PORT:-4500}"
API_PREFIX="${API_PREFIX:-/api/v1}"
DEFAULT_HOST="http://localhost:${APP_PORT}${API_PREFIX}"

KEY_NAME="bootstrap"
BASE_URL="$DEFAULT_HOST"

# ── Parse arguments ───────────────────────────────────────────────────────────
usage() {
    grep '^#' "$0" | sed 's/^# \{0,2\}//' | head -20
    exit 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --name)   KEY_NAME="$2"; shift 2 ;;
        --host)   BASE_URL="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "❌ Unknown option: $1"; usage ;;
    esac
done

# ── Dependency check ──────────────────────────────────────────────────────────
for dep in curl jq; do
    if ! command -v "$dep" >/dev/null 2>&1; then
        echo "❌ Required tool not found: $dep"
        exit 1
    fi
done

# ── Create the API key ────────────────────────────────────────────────────────
echo ""
echo "=========================================="
echo "🚀  Creating API key '${KEY_NAME}'"
echo "    ${BASE_URL}/api-keys"
echo "=========================================="
echo ""

RESPONSE=$(curl -sf -X POST "${BASE_URL}/api-keys" \
    -H "Content-Type: application/json" \
    -d "{\"name\": \"${KEY_NAME}\"}" \
) || {
    echo "❌  Request failed. Is the server running at ${BASE_URL}?"
    echo "   Start it with:  go run ./cmd/server"
    exit 1
}

# ── Parse response ────────────────────────────────────────────────────────────
# The API wraps responses in { "data": { ... } }
RAW_KEY=$(echo "$RESPONSE" | jq -r '.data.rawKey // .data.key // empty')
KEY_ID=$(echo "$RESPONSE"  | jq -r '.data.id   // empty')

if [[ -z "$RAW_KEY" ]]; then
    echo "❌  Could not extract rawKey from response:"
    echo "$RESPONSE" | jq .
    exit 1
fi

# ── Print summary ─────────────────────────────────────────────────────────────
echo "✅  API key created!"
echo ""
echo "  ID  : ${KEY_ID}"
echo "  KEY : ${RAW_KEY}"
echo ""
echo "⚠️   Copy the key now — it will NOT be shown again."
echo ""

# ── Cheat-sheet ───────────────────────────────────────────────────────────────
cat <<CHEATSHEET
══════════════════════════════════════════════════════════════════
 curl cheat-sheet  (BASE_URL=${BASE_URL})
══════════════════════════════════════════════════════════════════

Export your key first:
  export CDN_KEY="${RAW_KEY}"
  export BASE="${BASE_URL}"

──────────────────────────────────────────────────────────────────
 API KEYS  (/api-keys)
──────────────────────────────────────────────────────────────────
# List all keys
curl -s "\$BASE/api-keys" | jq .

# Create another key
curl -s -X POST "\$BASE/api-keys" \\
  -H "Content-Type: application/json" \\
  -d '{"name":"my-key","scopes":[]}'  | jq .

# Revoke a key
curl -s -X DELETE "\$BASE/api-keys/<keyId>" | jq .

──────────────────────────────────────────────────────────────────
 BUCKETS  (/buckets)         — requires Authorization header
──────────────────────────────────────────────────────────────────
AUTH='-H "Authorization: Bearer \$CDN_KEY"'

# Create a bucket
curl -s -X POST "\$BASE/buckets" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"name":"my-bucket","region":"us-east-1","isPublic":false}' | jq .

# List buckets
curl -s "\$BASE/buckets" \$AUTH | jq .

# Get a bucket
curl -s "\$BASE/buckets/<bucketId>" \$AUTH | jq .

# Update a bucket
curl -s -X PATCH "\$BASE/buckets/<bucketId>" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"isPublic":true}' | jq .

# Delete a bucket
curl -s -X DELETE "\$BASE/buckets/<bucketId>" \$AUTH | jq .

──────────────────────────────────────────────────────────────────
 FILES  (/buckets/:bucketId/files)
──────────────────────────────────────────────────────────────────
# List files in a bucket
curl -s "\$BASE/buckets/<bucketId>/files" \$AUTH | jq .

# Request a presigned upload URL
curl -s -X POST "\$BASE/buckets/<bucketId>/files/upload-url" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"filename":"photo.jpg","contentType":"image/jpeg","size":204800}' | jq .

# Register a file (after direct upload)
curl -s -X POST "\$BASE/buckets/<bucketId>/files" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"filename":"photo.jpg","contentType":"image/jpeg","size":204800}' | jq .

# Complete a multipart upload
curl -s -X POST "\$BASE/buckets/<bucketId>/files/<fileId>/complete" \$AUTH | jq .

# Get file metadata
curl -s "\$BASE/buckets/<bucketId>/files/<fileId>" \$AUTH | jq .

# Delete a file
curl -s -X DELETE "\$BASE/buckets/<bucketId>/files/<fileId>" \$AUTH | jq .

──────────────────────────────────────────────────────────────────
 CDN DOMAINS  (/buckets/:bucketId/cdn-domains)
──────────────────────────────────────────────────────────────────
# Create a CDN domain
curl -s -X POST "\$BASE/buckets/<bucketId>/cdn-domains" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"domain":"cdn.example.com"}' | jq .

# List CDN domains
curl -s "\$BASE/buckets/<bucketId>/cdn-domains" \$AUTH | jq .

# Verify a CDN domain
curl -s -X POST "\$BASE/buckets/<bucketId>/cdn-domains/<domainId>/verify" \$AUTH | jq .

# Enable / Disable a CDN domain
curl -s -X POST "\$BASE/buckets/<bucketId>/cdn-domains/<domainId>/enable"  \$AUTH | jq .
curl -s -X POST "\$BASE/buckets/<bucketId>/cdn-domains/<domainId>/disable" \$AUTH | jq .

# Delete a CDN domain
curl -s -X DELETE "\$BASE/buckets/<bucketId>/cdn-domains/<domainId>" \$AUTH | jq .

──────────────────────────────────────────────────────────────────
 CDN SETTINGS  (/buckets/:bucketId/cdn-settings)
──────────────────────────────────────────────────────────────────
# Get CDN settings
curl -s "\$BASE/buckets/<bucketId>/cdn-settings" \$AUTH | jq .

# Upsert CDN settings
curl -s -X PUT "\$BASE/buckets/<bucketId>/cdn-settings" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"cacheControl":"public, max-age=86400","httpsRedirect":true}' | jq .

──────────────────────────────────────────────────────────────────
 USAGE EVENTS  (/usage-events)
──────────────────────────────────────────────────────────────────
# Record a usage event
curl -s -X POST "\$BASE/usage-events" \$AUTH \\
  -H "Content-Type: application/json" \\
  -d '{"bucketId":"<bucketId>","bytes":1024,"eventType":"upload"}' | jq .

# List usage events
curl -s "\$BASE/usage-events" \$AUTH | jq .

# Usage summary
curl -s "\$BASE/usage-events/summary" \$AUTH | jq .

──────────────────────────────────────────────────────────────────
 CDN DELIVERY  (root-mounted, no auth required)
──────────────────────────────────────────────────────────────────
curl -s "http://localhost:${APP_PORT}/cdn/<path>" | jq .

──────────────────────────────────────────────────────────────────
 HEALTH
──────────────────────────────────────────────────────────────────
curl -s "\$BASE/health"       | jq .
curl -s "\$BASE/health/live"  | jq .
curl -s "\$BASE/health/ready" | jq .
══════════════════════════════════════════════════════════════════
CHEATSHEET
