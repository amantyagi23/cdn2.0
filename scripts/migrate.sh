#!/usr/bin/env bash
# migrate.sh — run golang-migrate against the project database.
#
# Usage:
#   ./scripts/migrate.sh up              # apply all pending migrations
#   ./scripts/migrate.sh down [n]        # roll back n migrations (default 1)
#   ./scripts/migrate.sh version         # print current migration version
#   ./scripts/migrate.sh force VERSION   # force-set version (clears dirty flag)
#   ./scripts/migrate.sh drop            # drop everything (asks for confirmation)
#   ./scripts/migrate.sh seed            # run 0003_seed_plans.sql via psql
#   ./scripts/migrate.sh fresh           # drop + up + seed in one shot

set -euo pipefail

# ──────────────────────────────────────────
# Paths
# ──────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MIGRATIONS_PATH="$PROJECT_ROOT/migrations"

# ──────────────────────────────────────────
# Load .env
# ──────────────────────────────────────────
ENV_FILE="$PROJECT_ROOT/.env"

if [[ -f "$ENV_FILE" ]]; then
    echo "📄 Loading environment from $ENV_FILE"
    set -a
    # shellcheck source=/dev/null
    source "$ENV_FILE"
    set +a
else
    echo "⚠️  No .env file found at $ENV_FILE — using existing environment variables"
fi

# ──────────────────────────────────────────
# Database configuration
# (MIGRATE_DB_HOST lets you override the host for docker vs local)
# ──────────────────────────────────────────
DB_HOST="${MIGRATE_DB_HOST:-${POSTGRES_HOST:-localhost}}"
DB_PORT="${MIGRATE_DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${POSTGRES_USER:-postgres}"
DB_PASSWORD="${POSTGRES_PASSWORD:-}"
DB_NAME="${POSTGRES_DB:-cdn_network}"
DB_SSLMODE="${POSTGRES_SSLMODE:-disable}"

DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}"
# psql-style DSN (used for seed / psql operations)
PSQL_URL="postgresql://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}"

# ──────────────────────────────────────────
# Dependency checks
# ──────────────────────────────────────────
if ! command -v migrate >/dev/null 2>&1; then
    echo ""
    echo "❌  'migrate' CLI not found."
    echo "    Install via:"
    echo "    go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest"
    echo "    or: brew install golang-migrate"
    exit 1
fi

if [[ ! -d "$MIGRATIONS_PATH" ]]; then
    echo "❌  Migrations directory not found: $MIGRATIONS_PATH"
    exit 1
fi

# ──────────────────────────────────────────
# Helpers
# ──────────────────────────────────────────
print_header() {
    echo ""
    echo "=========================================="
    echo "🚀  Database Migration"
    echo "=========================================="
    echo "  Database : $DB_NAME"
    echo "  Host     : $DB_HOST:$DB_PORT"
    echo "  User     : $DB_USER"
    echo "  SSL      : $DB_SSLMODE"
    echo "  Path     : $MIGRATIONS_PATH"
    echo "  Command  : ${COMMAND}"
    echo "=========================================="
    echo ""
}

run_migrate() {
    migrate \
        -path   "$MIGRATIONS_PATH" \
        -database "$DATABASE_URL" \
        "$@"
}

run_seed() {
    SEED_FILE="$MIGRATIONS_PATH/0003_seed_plans.sql"
    if [[ ! -f "$SEED_FILE" ]]; then
        echo "⚠️  Seed file not found: $SEED_FILE — skipping."
        return
    fi
    if ! command -v psql >/dev/null 2>&1; then
        echo "❌  'psql' not found — cannot run seed. Install postgresql-client."
        exit 1
    fi
    echo "🌱  Running seed: $SEED_FILE"
    PGPASSWORD="$DB_PASSWORD" psql \
        --host="$DB_HOST" \
        --port="$DB_PORT" \
        --username="$DB_USER" \
        --dbname="$DB_NAME" \
        --file="$SEED_FILE"
    echo "✅  Seed complete."
}

# ──────────────────────────────────────────
# Command dispatch
# ──────────────────────────────────────────
COMMAND="${1:-up}"
print_header

case "$COMMAND" in

    up)
        echo "⬆️   Applying all pending migrations…"
        run_migrate up
        echo "✅  Migrations applied."
        ;;

    down)
        COUNT="${2:-1}"
        echo "⬇️   Rolling back ${COUNT} migration(s)…"
        run_migrate down "$COUNT"
        echo "✅  Rollback complete."
        ;;

    version)
        echo "🔍  Current schema version:"
        run_migrate version
        ;;

    force)
        VERSION="${2:-}"
        if [[ -z "$VERSION" ]]; then
            echo "❌  Usage: $0 force VERSION"
            echo "    Example: $0 force 1"
            exit 1
        fi
        echo "⚡  Force-setting version to ${VERSION} (clears dirty flag)…"
        run_migrate force "$VERSION"
        echo "✅  Version forced to ${VERSION}."
        ;;

    drop)
        echo "⚠️   WARNING: This will drop ALL tables and types in '$DB_NAME'."
        read -r -p "    Type 'yes' to confirm: " CONFIRM
        if [[ "$CONFIRM" != "yes" ]]; then
            echo "    Aborted."
            exit 0
        fi
        echo "🗑️   Dropping all schema objects…"
        run_migrate drop -f
        echo "✅  Drop complete."
        ;;

    seed)
        run_seed
        ;;

    fresh)
        echo "♻️   Fresh: drop → up → seed"
        echo ""
        echo "⚠️   WARNING: This will destroy all data in '$DB_NAME'."
        read -r -p "    Type 'yes' to confirm: " CONFIRM
        if [[ "$CONFIRM" != "yes" ]]; then
            echo "    Aborted."
            exit 0
        fi
        echo ""
        echo "🗑️  [1/3] Dropping schema…"
        run_migrate drop -f || true
        echo ""
        echo "⬆️  [2/3] Running all migrations…"
        run_migrate up
        echo ""
        echo "🌱  [3/3] Seeding plans…"
        run_seed
        echo ""
        echo "✅  Fresh database ready."
        ;;

    *)
        echo "❌  Unknown command: $COMMAND"
        echo ""
        echo "Usage:"
        echo "  $0 up                  Apply all pending migrations"
        echo "  $0 down [n]            Roll back n migration(s) (default: 1)"
        echo "  $0 version             Print current migration version"
        echo "  $0 force VERSION       Force-set version (clears dirty flag)"
        echo "  $0 drop                Drop all tables/types (destructive, asks confirmation)"
        echo "  $0 seed                Run 0003_seed_plans.sql via psql"
        echo "  $0 fresh               drop + up + seed (full reset, asks confirmation)"
        exit 1
        ;;

esac
