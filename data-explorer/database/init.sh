#!/bin/bash

set -e

# Configuration
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-indexer}"
DB_PASSWORD="${DB_PASSWORD:-indexer_password}"
DB_NAME="${DB_NAME:-blockchain_explorer}"

echo "Initializing database: $DB_NAME"
echo "Host: $DB_HOST:$DB_PORT"

echo "Waiting for PostgreSQL to be ready..."
until PGPASSWORD=$DB_PASSWORD psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c '\q' 2>/dev/null; do
  echo "PostgreSQL is unavailable - sleeping"
  sleep 2
done

echo "PostgreSQL is ready!"

# Apply schema
echo "Applying schema..."
PGPASSWORD=$DB_PASSWORD psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -f "$(dirname "$0")/schema.sql"

echo "Database initialization complete!"
