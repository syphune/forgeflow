# Architecture boundary

The current implementation is a runnable vertical slice:

```text
HTTP/MCP -> application services -> PostgreSQL repositories
                         -> workflow/specification/readiness rules
                         -> audit + outbox contracts
GitHub webhook -> HMAC validation -> idempotent persistence
Electron -> typed IPC -> bounded local Git/provider runner
```

The local MCP bridge is a thin stdio client for the authenticated remote MCP endpoint; it forwards the upstream catalog and calls without adding authorization or database access.

The services own authorization context, optimistic version checks, transition validation, readiness rules, AgentRun approval, and tenant scoping. Adapters are replaceable and cannot bypass those rules. `DATABASE_URL` selects the PostgreSQL repositories plus SQL audit/outbox writers; no URL selects the deterministic in-memory adapter for local tests.

The current product surfaces also include bounded GitHub App tree/file/search reads, fixed-commit persisted repository snapshots with extracted symbols, repository knowledge revisions, MCP resource templates and analysis proposals, scoped PAT creation/revocation, private bounded attachments, project automation/notifications, structured regression/context references, and desktop-managed Git worktrees. The default development attachment store is local and volume-backed; S3-compatible storage is configurable for production. OpenTelemetry trace export is enabled when `FORGEFLOW_OTEL_ENDPOINT` is configured. Desktop packaging is covered by the Electron Forge release job; signing/notarization still require platform credentials, and load/security hardening remains deployment work.
