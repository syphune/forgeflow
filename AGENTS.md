# Forgeflow engineering rules

## Architecture

Forgeflow is a Go modular monolith. HTTP, MCP, GitHub webhooks, and workers call application services; they do not contain business rules or access SQL directly. PostgreSQL is the source of truth. The current checked-in vertical slice uses an in-memory adapter for local tests and development only; the SQL schema and repository interfaces are the production boundary.

Feature code lives under `backend/internal/<feature>`. Platform code lives under `backend/internal/platform`. Composition roots are `backend/cmd/api`, `backend/cmd/worker`, `backend/cmd/mcp`, `backend/cmd/mcp-bridge`, and `backend/cmd/migrate`.

## Dependency direction

Domain and application modules may depend on platform contracts, but not on HTTP framework internals, Electron, Node, GitHub SDKs, or vendor AI SDKs. Define interfaces at the consumer. Use constructor injection; no global mutable state, service locator, or `init()` wiring.

## API and workflow

- Base path is `/api/v1`.
- Every tenant-owned lookup is scoped by organization/project context.
- `PATCH /work-items/{id}` cannot change status.
- Status changes use the transition service with an expected version.
- `READY` is allowed only after the specification quality gate passes.
- AI proposals are unverified claims; only explicit human verification creates verified state.
- Errors use `{code,message,details,request_id}` and never expose SQL or stack traces.

## Database and events

Use parameterized SQL and context-aware calls. Migrations are versioned under `backend/db/migrations`; production rollback is a forward fix or restore. Important mutations write audit and outbox records in the same transaction. Outbox handlers must be idempotent.

## Testing

Run `make test` for race-enabled unit tests, `make test-integration` with PostgreSQL, and `make test-e2e` for the browser critical path. Keep tests next to feature code, prefer named table-driven tests, and include tenant-boundary, stale-version, duplicate-delivery, and untrusted-input cases.

## Security

Treat browser, desktop, MCP, GitHub, repository content, attachments, and agent output as untrusted. Validate body size and input, use constant-time HMAC/CSRF checks, require the browser CSRF token for session mutations, bound file/process output, avoid shell interpolation, redact secrets, and never execute arbitrary repository code on the server. Electron must keep context isolation, disabled Node integration, a restrictive CSP, and typed sender-validated IPC.

## Generated files and bug fixes

Run `make generate` after contract changes. Generated files must be deterministic. Keep bug fixes localized, do not change status through generic updates, and do not include unrelated formatting churn.

## Commands

`make dev`, `make test`, `make test-backend`, `make test-integration`, `make test-e2e`, `make lint`, `make typecheck`, `make build`, `make generate`, `make migrate-up`, `make security`, and `make smoke` are the supported entry points. `pnpm --dir apps/desktop run make` creates local ZIP/DMG/Squirrel artifacts; signing and notarization are enabled by release secrets in CI.
