#!/usr/bin/env bash
# migrate.sh — run golang-migrate against the project database.
#
# Works from any directory: the migrations folder is found by searching,
# in order:
#   1. --migrations PATH
#   2. $SCRIPT_DIR/../migrations   (normal layout: scripts/migrate.sh)
#   3. $SCRIPT_DIR/migrations
#   4. $PWD/migrations             (invoked from project root)
#   5. ancestors of $PWD
#
# Usage:
#   ./scripts/migrate.sh [-y|--yes] [-m|--migrations DIR] <command> [args]
#   ./migrate.sh         [-y|--yes] [-m|--migrations DIR] <command> [args]
#
# Commands:
#   up                  Apply all pending migrations
#   down [n]            Roll back n migrations (default 1)
#   version             Print current migration version
#   force VERSION       Force-set version (clears dirty flag)
#   drop                Drop all schema objects (asks for confirmation)
#   seed                Run 0003_seed_plans.sql via psql
#   fresh               drop + up + seed (full reset, asks for confirmation)
#
# Flags:
#   -y, --yes           Skip confirmation prompts (for CI)
#   -m, --migrations D  Explicit path to the migrations directory
#   -h, --help          Show this help

set -euo pipefail

# ──────────────────────────────────────────
# Resolve script location
# (handles symlinks, piping, `sh script`, sourcing)
# ──────────────────────────────────────────
resolve_script_dir() {
    local src="${BASH_SOURCE[0]:-}"
    if [[ -z "$src" ]]; then
        # Piped execution (`bash < migrate.sh`) or POSIX sh — no reliable
        # $BASH_SOURCE. Fall back to $PWD; find_migrations_dir() below
        # will still locate the folder in the common case.
        pwd -P
        return
    fi
    while [[ -L "$src" ]]; do
        local dir
        dir="$(cd -P -- "$(dirname -- "$src")" && pwd -P)"
        src="$(readlink -- "$src")"
        [[ "$src" != /* ]] && src="$dir/$src"
    done
    cd -P -- "$(dirname -- "$src")" && pwd -P
}

SCRIPT_DIR="$(resolve_script_dir)"
PROJECT_ROOT="$(cd -P -- "$SCRIPT_DIR/.." && pwd -P)"

# ──────────────────────────────────────────
# Load .env  (project-root first, then $PWD as fallback)
# ──────────────────────────────────────────
ENV_FILE="$PROJECT_ROOT/.env"
if [[ ! -f "$ENV_FILE" && -f "$PWD/.env" ]]; then
    ENV_FILE="$PWD/.env"
fi
if [[ -f "$ENV_FILE" ]]; then
    echo "📄 Loading environment from $ENV_FILE"
    set -a
    # shellcheck source=/dev/null
    source "$ENV_FILE"
    set +a
else
    echo "⚠️  No .env file found (looked in $PROJECT_ROOT and $PWD) — using existing environment variables"
fi

# ──────────────────────────────────────────
# Database configuration
# ──────────────────────────────────────────
DB_HOST="${MIGRATE_DB_HOST:-${POSTGRES_HOST:-localhost}}"
DB_PORT="${MIGRATE_DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${POSTGRES_USER:-aman}"
DB_PASSWORD="${POSTGRES_PASSWORD:-}"
DB_NAME="${POSTGRES_DB:-tomatotrails}"
DB_SSLMODE="${POSTGRES_SSLMODE:-disable}"

urlencode() {
    local s="$1" out="" c i
    for (( i=0; i<${#s}; i++ )); do
        c="${s:i:1}"
        case "$c" in
            [a-zA-Z0-9.~_-]) out+="$c" ;;
            *) printf -v c '%%%02X' "'$c"; out+="$c" ;;
        esac
    done
    printf '%s' "$out"
}

DB_USER_ENC="$(urlencode "$DB_USER")"
DB_PASS_ENC="$(urlencode "$DB_PASSWORD")"
DB_NAME_ENC="$(urlencode "$DB_NAME")"
DATABASE_URL="postgres://${DB_USER_ENC}:${DB_PASS_ENC}@${DB_HOST}:${DB_PORT}/${DB_NAME_ENC}?sslmode=${DB_SSLMODE}"

# ──────────────────────────────────────────
# Helpers
# ──────────────────────────────────────────
usage() {
    cat <<'EOF'
Usage:
  migrate.sh [-y|--yes] [-m|--migrations DIR] <command> [args]

Commands:
  up                  Apply all pending migrations
  down [n]            Roll back n migrations (default 1)
  version             Print current migration version
  force VERSION       Force-set version (clears dirty flag)
  drop                Drop all schema objects (asks for confirmation)
  seed                Run 0003_seed_plans.sql via psql
  fresh               drop + up + seed (full reset, asks for confirmation)

Flags:
  -y, --yes           Skip confirmation prompts (for CI)
  -m, --migrations D  Explicit path to the migrations directory
  -h, --help          Show this help
EOF
    exit 0
}

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

confirm() {
    if [[ "$SKIP_CONFIRM" == "1" ]]; then
        echo "    (--yes: skipping confirmation)"
        return 0
    fi
    local reply=""
    read -r -p "    $1 Type 'yes' to confirm: " reply || reply=""
    [[ "$reply" == "yes" ]]
}

require_uint() {
    if ! [[ "$1" =~ ^[0-9]+$ ]]; then
        echo "❌  $2 must be a non-negative integer (got: '$1')"
        exit 1
    fi
}

run_migrate() {
    migrate -path "$MIGRATIONS_PATH" -database "$DATABASE_URL" "$@"
}

run_seed() {
    local SEED_FILE="$MIGRATIONS_PATH/0003_seed_plans.sql"
    if [[ ! -f "$SEED_FILE" ]]; then
        echo "⚠️  Seed file not found: $SEED_FILE — skipping."
        return
    fi
    command -v psql >/dev/null 2>&1 \
        || { echo "❌  'psql' not found — cannot run seed. Install postgresql-client."; exit 1; }
    echo "🌱  Running seed: $SEED_FILE"
    PGPASSWORD="$DB_PASSWORD" psql -v ON_ERROR_STOP=1 \
        --host="$DB_HOST" --port="$DB_PORT" \
        --username="$DB_USER" --dbname="$DB_NAME" \
        --file="$SEED_FILE"
    echo "✅  Seed complete."
}

# ──────────────────────────────────────────
# Parse flags
# ──────────────────────────────────────────
SKIP_CONFIRM=0
MIGRATIONS_OVERRIDE=""
ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        -y|--yes)         SKIP_CONFIRM=1; shift ;;
        -m|--migrations)  MIGRATIONS_OVERRIDE="${2:-}"; shift 2 ;;
        -h|--help)        usage ;;
        --)               shift; ARGS+=("$@"); break ;;
        -*)               echo "❌  Unknown flag: $1"; usage ;;
        *)                ARGS+=("$1"); shift ;;
    esac
done

COMMAND="${ARGS[0]:-up}"
ARG1="${ARGS[1]:-}"

case "$COMMAND" in
    up|down|version|force|drop|seed|fresh) ;;
    *) echo "❌  Unknown command: $COMMAND"; echo; usage ;;
esac

# ──────────────────────────────────────────
# Locate the migrations directory
# ──────────────────────────────────────────
find_migrations_dir() {
    local candidates=()
    [[ -n "$MIGRATIONS_OVERRIDE" ]] && candidates+=("$MIGRATIONS_OVERRIDE")
    candidates+=(
        "$PROJECT_ROOT/migrations"
        "$SCRIPT_DIR/migrations"
        "$PWD/migrations"
    )
    local c
    for c in "${candidates[@]}"; do
        if [[ -d "$c" ]]; then
            (cd -P -- "$c" && pwd -P)
            return 0
        fi
    done
    local d="$PWD"
    while [[ "$d" != "/" && "$d" != "" ]]; do
        if [[ -d "$d/migrations" ]]; then
            (cd -P -- "$d/migrations" && pwd -P)
            return 0
        fi
        d="$(dirname -- "$d")"
    done
    return 1
}

if ! MIGRATIONS_PATH="$(find_migrations_dir)"; then
    echo ""
    echo "❌  Could not locate a 'migrations' directory."
    echo "    Searched:"
    [[ -n "$MIGRATIONS_OVERRIDE" ]] && echo "      --migrations $MIGRATIONS_OVERRIDE"
    echo "      $PROJECT_ROOT/migrations"
    echo "      $SCRIPT_DIR/migrations"
    echo "      $PWD/migrations"
    echo "      ancestors of $PWD"
    echo ""
    echo "    Pass --migrations PATH to override, or run from the project root."
    exit 1
fi

# ──────────────────────────────────────────
# Dependency checks
# ──────────────────────────────────────────
if ! command -v migrate >/dev/null 2>&1; then
    echo ""
    echo "❌  'migrate' CLI not found."
    echo "    go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest"
    echo "    or: brew install golang-migrate"
    exit 1
fi

print_header

# ──────────────────────────────────────────
# Dispatch
# ──────────────────────────────────────────
case "$COMMAND" in
    up)
        echo "⬆️   Applying all pending migrations…"
        run_migrate up
        echo "✅  Migrations applied."
        ;;
    down)
        COUNT="${ARG1:-1}"
        require_uint "$COUNT" "down count"
        echo "⬇️   Rolling back ${COUNT} migration(s)…"
        run_migrate down "$COUNT"
        echo "✅  Rollback complete."
        ;;
    version)
        echo "🔍  Current schema version:"
        run_migrate version
        ;;
    force)
        VERSION="$ARG1"
        if [[ -z "$VERSION" ]]; then
            echo "❌  Usage: $0 force VERSION"
            echo "    Example: $0 force 1"
            exit 1
        fi
        require_uint "$VERSION" "force version"
        echo "⚡  Force-setting version to ${VERSION} (clears dirty flag)…"
        run_migrate force "$VERSION"
        echo "✅  Version forced to ${VERSION}."
        ;;
    drop)
        echo "⚠️   WARNING: This will drop ALL tables and types in '$DB_NAME'."
        if ! confirm ""; then echo "    Aborted."; exit 0; fi
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
        if ! confirm ""; then echo "    Aborted."; exit 0; fi
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
esac