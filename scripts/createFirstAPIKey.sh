#!/usr/bin/env bash
# =============================================================================
# createFirstAPIKey.sh
#
# Bootstrap script: creates your first API key by inserting directly into
# PostgreSQL (bypassing the HTTP API), then prints a ready-to-paste curl
# cheat-sheet covering every available endpoint.
#
# Key format (must match apikey.NewAPIKey / generateRawKey in Go):
#   raw    = "ak_" + base64.RawURLEncoding(32 random bytes)   -> 46 chars
#   prefix = raw[:12]
#   hash   = hex(sha256(raw))                                 -> 64 chars
# Only the hash + prefix are stored; the raw key is printed once.
#
# Usage:
#   ./scripts/createFirstAPIKey.sh [--name NAME] [--host HOST]
#
# Optional flags:
#   --name NAME   Human-readable label for the API key (default: "bootstrap")
#   --host HOST   API base URL used in the cheat-sheet, no trailing slash
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

PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5432}"
PG_USER="${POSTGRES_USER:-aman}"
PG_PASSWORD="${POSTGRES_PASSWORD:-12345678}"
DB_NAME="${POSTGRES_DB:-tomatotrails}"

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
for dep in psql openssl; do
    if ! command -v "$dep" >/dev/null 2>&1; then
        echo "❌ Required tool not found: $dep"
        exit 1
    fi
done

# ── PostgreSQL connection settings ────────────────────────────────────────────
export PGPASSWORD="$PG_PASSWORD"
trap 'unset PGPASSWORD' EXIT

PSQL=(psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$DB_NAME" \
      -v ON_ERROR_STOP=1)

echo ""
echo "=========================================="
echo "🚀  Creating API key '${KEY_NAME}'"
echo "    ${PG_USER}@${PG_HOST}:${PG_PORT}/${DB_NAME}"
echo "=========================================="
echo ""

# ── Sanity checks ─────────────────────────────────────────────────────────────
if ! "${PSQL[@]}" -tAc "SELECT 1;" >/dev/null 2>&1; then
    echo "❌  Could not connect to PostgreSQL at ${PG_HOST}:${PG_PORT}/${DB_NAME}."
    echo "    Check POSTGRES_* in .env and that the database exists."
    exit 1
fi

if ! "${PSQL[@]}" -tAc "SELECT 1 FROM api_keys LIMIT 1;" >/dev/null 2>&1; then
    echo "❌  Table 'api_keys' not found in '${DB_NAME}'."
    echo "    Apply the schema first."
    exit 1
fi

# ── Generate the raw API key exactly like apikey.generateRawKey() ────────────
# Go: base64.RawURLEncoding (URL-safe, no padding) of 32 random bytes.
#     rawKeyPrefix = "ak_", displayLen = 12.
RAW_B64="$(openssl rand -base64 32 | tr -d '\n')"
RAW_URLSAFE="$(printf '%s' "$RAW_B64" | tr '+/' '-_' | tr -d '=')"
RAW_KEY="ak_${RAW_URLSAFE}"
KEY_PREFIX="${RAW_KEY:0:12}"
KEY_HASH="$(printf '%s' "$RAW_KEY" | openssl dgst -sha256 -hex | awk '{print $NF}')"

# Format assertions (fail fast if the platform's openssl/tr behaves oddly).
if [[ "${#RAW_KEY}" -ne 46 ]]; then
    echo "❌  Raw key has unexpected length ${#RAW_KEY} (want 46): $RAW_KEY"
    exit 1
fi
if [[ "${#KEY_HASH}" -ne 64 || ! "$KEY_HASH" =~ ^[0-9a-f]{64}$ ]]; then
    echo "❌  Hash has unexpected format: $KEY_HASH"
    exit 1
fi

# ── Insert into api_keys ─────────────────────────────────────────────────────
KEY_ID=$("${PSQL[@]}" -tA \
    -v name="$KEY_NAME" \
    -v prefix="$KEY_PREFIX" \
    -v hash="$KEY_HASH" <<'SQL'
INSERT INTO api_keys (name, key_prefix, key_hash, scopes, status)
VALUES (:'name', :'prefix', :'hash', '{}'::text[], 'active')
RETURNING id;
SQL
) || {
    echo "❌  Failed to insert API key into the database."
    exit 1
}

KEY_ID="$(printf '%s' "$KEY_ID" | tr -d '[:space:]')"
if [[ -z "$KEY_ID" ]]; then
    echo "❌  Insert returned no id."
    exit 1
fi

# ── Print summary ─────────────────────────────────────────────────────────────
echo "✅  API key created!"
echo ""
echo "  ID     : ${KEY_ID}"
echo "  PREFIX : ${KEY_PREFIX}"
echo "  KEY    : ${RAW_KEY}"
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