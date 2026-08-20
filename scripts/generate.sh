#!/usr/bin/env sh
set -eu

PNPM="pnpm"
if ! command -v pnpm >/dev/null 2>&1; then
  if command -v corepack >/dev/null 2>&1; then
    PNPM="corepack pnpm"
  elif command -v npm >/dev/null 2>&1; then
    PNPM="npm exec --yes --package=pnpm@10.14.0 -- pnpm"
  else
    printf '%s\n' "pnpm, corepack, or npm is required for contract generation" >&2
    exit 1
  fi
fi
$PNPM exec openapi-typescript contracts/openapi/openapi.yaml -o packages/api-client/src/generated.ts
