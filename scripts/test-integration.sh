#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root/backend"
compose="docker compose -p forgeflow-test -f ../infra/compose.test.yaml"
test_port=${FORGEFLOW_TEST_POSTGRES_PORT:-55432}
database_url="postgres://forgeflow:forgeflow@localhost:${test_port}/forgeflow?sslmode=disable"
cleanup() {
  $compose down -v
}
trap cleanup EXIT INT TERM

$compose up -d --wait postgres
DATABASE_URL="$database_url" MIGRATIONS_PATH=db/migrations go run ./cmd/migrate up
DATABASE_URL="$database_url" go test -race -tags=integration ./...
