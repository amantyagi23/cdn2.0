#!/usr/bin/env bash

set -euo pipefail

# ==========================================
# create_admin.sh
# Creates a superadmin row in the `operators` table.
# ==========================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ----------------------------------------
# Load .env
# ----------------------------------------

ENV_FILE="$PROJECT_ROOT/.env"

if [[ -f "$ENV_FILE" ]]; then
    echo "📄 Loading environment from $ENV_FILE"
    set -a
    source "$ENV_FILE"
    set +a
else
    echo "❌ .env file not found: $ENV_FILE"
    exit 1
fi

# ----------------------------------------
# PostgreSQL configuration (same vars as migrate.sh)
# ----------------------------------------

DB_HOST="${MIGRATE_DB_HOST:-localhost}"
DB_PORT="${MIGRATE_DB_PORT:-5432}"
DB_USER="${POSTGRES_USER:-postgres}"
DB_PASSWORD="${POSTGRES_PASSWORD:-}"
DB_NAME="${POSTGRES_DB:-cdn_network}"
DB_SSLMODE="${POSTGRES_SSLMODE:-disable}"

# ----------------------------------------
# Checks
# ----------------------------------------

if ! command -v psql >/dev/null 2>&1; then
    echo "❌ psql is not installed"
    exit 1
fi

# ----------------------------------------
# Usage
# ----------------------------------------

usage() {
    cat <<EOF
Usage: ./create_admin.sh [options]

Required:
  --name NAME                     Operator display name
  --email EMAIL                   Operator email (must be unique)
  --wallet-callback-url URL       Wallet callback URL (NOT NULL in schema)

Optional (auto-generated if omitted):
  --password-hash HASH            Pre-hashed password (login credential)
  --api-key KEY                   API key (random hex generated if omitted)
  --api-secret-hash HASH          Pre-hashed API secret
                                   (if omitted, a random secret is generated,
                                    its SHA-256 hash is stored, and the
                                    PLAINTEXT secret is printed ONCE)
  --wallet-secret-hash HASH       Pre-hashed wallet secret

Optional (defaults shown):
  --currency CUR                  Default: CHIPS
  --revenue-share-pct PCT         Default: 0
  --status STATUS                 Default: active
  --traffic-mode MODE             Default: live
  --feed-visibility VIS           Default: live_only

  -h, --help                      Show this help

Role is always set to 'superadmin'.
EOF
    exit 1
}

# ----------------------------------------
# Parse arguments
# ----------------------------------------

NAME=""
EMAIL=""
WALLET_CALLBACK_URL=""
PASSWORD_HASH=""
API_KEY=""
API_SECRET_HASH=""
WALLET_SECRET_HASH=""
CURRENCY="CHIPS"
REVENUE_SHARE_PCT="0"
STATUS="active"
TRAFFIC_MODE="live"
FEED_VISIBILITY="live_only"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --name) NAME="$2"; shift 2 ;;
        --email) EMAIL="$2"; shift 2 ;;
        --wallet-callback-url) WALLET_CALLBACK_URL="$2"; shift 2 ;;
        --password-hash) PASSWORD_HASH="$2"; shift 2 ;;
        --api-key) API_KEY="$2"; shift 2 ;;
        --api-secret-hash) API_SECRET_HASH="$2"; shift 2 ;;
        --wallet-secret-hash) WALLET_SECRET_HASH="$2"; shift 2 ;;
        --currency) CURRENCY="$2"; shift 2 ;;
        --revenue-share-pct) REVENUE_SHARE_PCT="$2"; shift 2 ;;
        --status) STATUS="$2"; shift 2 ;;
        --traffic-mode) TRAFFIC_MODE="$2"; shift 2 ;;
        --feed-visibility) FEED_VISIBILITY="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "❌ Unknown option: $1"; usage ;;
    esac
done

if [[ -z "$NAME" || -z "$EMAIL" || -z "$WALLET_CALLBACK_URL" ]]; then
    echo "❌ --name, --email, and --wallet-callback-url are required"
    echo ""
    usage
fi

# ----------------------------------------
# Auto-generate API key / secret hash if not provided
# ----------------------------------------

GENERATED_SECRET_NOTICE=""

if [[ -z "$API_KEY" ]]; then
    API_KEY="ak_$(openssl rand -hex 24)"
fi

if [[ -z "$API_SECRET_HASH" ]]; then
    PLAINTEXT_SECRET="$(openssl rand -hex 32)"
    API_SECRET_HASH="$(printf '%s' "$PLAINTEXT_SECRET" | sha256sum | awk '{print $1}')"
    GENERATED_SECRET_NOTICE="$PLAINTEXT_SECRET"
fi

# ----------------------------------------
# Run insert
# ----------------------------------------

echo ""
echo "=========================================="
echo "🚀 Creating superadmin operator"
echo "=========================================="
echo "Database : $DB_NAME"
echo "Host     : $DB_HOST:$DB_PORT"
echo "Name     : $NAME"
echo "Email    : $EMAIL"
echo "Role     : superadmin"
echo "=========================================="
echo ""

PGPASSWORD="$DB_PASSWORD" psql \
    -h "$DB_HOST" \
    -p "$DB_PORT" \
    -U "$DB_USER" \
    -d "$DB_NAME" \
    -v ON_ERROR_STOP=1 \
    -v v_name="$NAME" \
    -v v_email="$EMAIL" \
    -v v_wallet_callback_url="$WALLET_CALLBACK_URL" \
    -v v_password_hash="$PASSWORD_HASH" \
    -v v_api_key="$API_KEY" \
    -v v_api_secret_hash="$API_SECRET_HASH" \
    -v v_wallet_secret_hash="$WALLET_SECRET_HASH" \
    -v v_currency="$CURRENCY" \
    -v v_revenue_share_pct="$REVENUE_SHARE_PCT" \
    -v v_status="$STATUS" \
    -v v_traffic_mode="$TRAFFIC_MODE" \
    -v v_feed_visibility="$FEED_VISIBILITY" \
    <<'SQL'
INSERT INTO operators (
    name,
    email,
    api_key,
    api_secret_hash,
    password_hash,
    wallet_callback_url,
    wallet_secret_hash,
    status,
    traffic_mode,
    feed_visibility,
    role,
    currency,
    revenue_share_pct
) VALUES (
    :'v_name',
    :'v_email',
    :'v_api_key',
    :'v_api_secret_hash',
    NULLIF(:'v_password_hash', ''),
    :'v_wallet_callback_url',
    NULLIF(:'v_wallet_secret_hash', ''),
    :'v_status',
    :'v_traffic_mode',
    :'v_feed_visibility',
    'superadmin',
    :'v_currency',
    :'v_revenue_share_pct'
)
RETURNING id, name, email, role, created_at;
SQL

echo ""
echo "✅ Superadmin operator created."

if [[ -n "$GENERATED_SECRET_NOTICE" ]]; then
    echo ""
    echo "⚠️  No --api-secret-hash was provided, so a secret was generated for you."
    echo "   Store this now — it is NOT recoverable later (only its SHA-256 hash was saved):"
    echo ""
    echo "   API_SECRET (plaintext): $GENERATED_SECRET_NOTICE"
    echo ""
fi

echo "   API_KEY: $API_KEY"