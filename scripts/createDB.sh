#!/bin/bash

set -e

# ----------------------------------------
# Find project root
# ----------------------------------------

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
# PostgreSQL Configuration
# ----------------------------------------

PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5432}"
PG_USER="${POSTGRES_USER:-aman}"
PG_PASSWORD="${POSTGRES_PASSWORD:-12345678}"

DEFAULT_DB="${PG_DEFAULT_DB:-postgres}"
DB_NAME="${POSTGRES_DB:-tomatotrails}"
DB_USER="${POSTGRES_USER:-aman}"

export PGPASSWORD="$PG_PASSWORD"

echo "🔌 PostgreSQL: $PG_HOST:$PG_PORT"
echo "👤 User: $PG_USER"
echo "🗄️ Database: $DB_NAME"

# Your psql commands here...

# ----------------------------------------
# Validate database name
# ----------------------------------------

if [[ -z "$DB_NAME" ]]; then
    echo "❌ POSTGRES_DB is not set."
    exit 1
fi

# ----------------------------------------
# Check PostgreSQL connection
# ----------------------------------------

echo "🔌 Connecting to PostgreSQL..."

psql \
    -h "$PG_HOST" \
    -p "$PG_PORT" \
    -U "$PG_USER" \
    -d "$DEFAULT_DB" \
    -c "SELECT version();" > /dev/null

echo " PostgreSQL connection successful."

# ----------------------------------------
# Check if database exists
# ----------------------------------------

DB_EXISTS=$(psql \
    -h "$PG_HOST" \
    -p "$PG_PORT" \
    -U "$PG_USER" \
    -d "$DEFAULT_DB" \
    -tAc "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME';")

# ----------------------------------------
# Create database
# ----------------------------------------

if [[ "$DB_EXISTS" != "1" ]]; then

    echo "🗄️ Creating database '$DB_NAME'..."

    # PostgreSQL CREATE DATABASE cannot run inside a transaction.
    psql \
        -h "$PG_HOST" \
        -p "$PG_PORT" \
        -U "$PG_USER" \
        -d "$DEFAULT_DB" \
        -c "CREATE DATABASE \"$DB_NAME\";"

    echo " Database '$DB_NAME' created."

else

    echo "ℹ️ Database '$DB_NAME' already exists."

fi

# ----------------------------------------
# Grant database privileges
# ----------------------------------------

echo "🔐 Granting privileges..."

psql \
    -h "$PG_HOST" \
    -p "$PG_PORT" \
    -U "$PG_USER" \
    -d "$DEFAULT_DB" \
    -c "GRANT ALL PRIVILEGES ON DATABASE \"$DB_NAME\" TO \"$DB_USER\";"

echo " Granted ALL privileges on '$DB_NAME' to '$DB_USER'."

# ----------------------------------------
# Cleanup
# ----------------------------------------

unset PGPASSWORD

echo "🎉 PostgreSQL setup completed successfully."