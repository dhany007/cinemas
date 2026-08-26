#!/bin/sh

set -eu

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL

for migration in /migrations/*.up.sql; do
    version=$(basename "$migration" .up.sql)
    applied=$(psql "$DATABASE_URL" -At -v ON_ERROR_STOP=1 \
        -c "SELECT 1 FROM schema_migrations WHERE version = '$version'")
    if [ "$applied" = "1" ]; then
        continue
    fi

    psql "$DATABASE_URL" --single-transaction -v ON_ERROR_STOP=1 -f "$migration"
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
        -c "INSERT INTO schema_migrations (version) VALUES ('$version')"
done
