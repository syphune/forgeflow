#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root/backend"
compose="docker compose -p forgeflow-test -f ../infra/compose.test.yaml"
cleanup() {
  $compose down -v
}
trap cleanup EXIT INT TERM

$compose up -d --wait postgres
DATABASE_URL='postgres://forgeflow:forgeflow@localhost:55432/forgeflow?sslmode=disable' MIGRATIONS_PATH=db/migrations go run ./cmd/migrate up
DATABASE_URL='postgres://forgeflow:forgeflow@localhost:55432/forgeflow?sslmode=disable' go test -race -tags=integration ./...
