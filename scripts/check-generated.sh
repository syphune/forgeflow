#!/usr/bin/env sh
set -eu

./scripts/generate.sh
git diff --exit-code -- packages/api-client/src/generated.ts
