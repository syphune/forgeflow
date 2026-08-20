# Development

Use Go 1.26+, Node 22, and the commands in the root `Makefile`. Start PostgreSQL with `docker compose -f infra/compose.dev.yaml up postgres`, then run migrations with `DATABASE_URL=... make migrate-up`.

The frontend lockfile should be generated with the pinned workspace package manager: `corepack pnpm install --lockfile-only`. Header actors are enabled only by the in-memory dev adapter or an explicit `FORGEFLOW_DEV_AUTH=true`; never use that flag in production.
