# Forgeflow

Forgeflow is a Go modular monolith with a Next.js web client and an Electron desktop client. PostgreSQL is the source of truth. The checked-in product covers scoped identity/PAT sessions, organizations/projects/members, Jira-style work items, workflow transitions, structured bug specifications and readiness gates, comments/links/labels/attachments/custom fields, sprints/boards, AI proposals and analysis hypotheses, AgentRun approvals/evidence, GitHub App/webhook intake, MCP tool/resource mapping, bounded repository context, project automation/notifications, audit/outbox records, and desktop process/path safety.

## Quick start

Requirements: Go 1.26+, Node 22+, Docker, and pnpm 10.14+ (or `corepack pnpm`).

```sh
make dev
```

`make dev` starts the PostgreSQL, migration, API, worker, OpenTelemetry Collector, and Web containers with Docker Compose, loading `.env.local` for interpolation as well as container environment. The API is exposed on `:18080` and Web on `http://localhost:13000`. It uses local OAuth settings; add `http://localhost:18080/api/v1/auth/github/callback` to the GitHub OAuth App when testing sign-in locally. Use `make dev-db` for only PostgreSQL or `make dev-host` for the non-container host runner.

Without `DATABASE_URL`, the API uses an in-memory adapter for local tests. With `DATABASE_URL`, startup selects PostgreSQL repositories. `FORGEFLOW_DEV_AUTH=true` enables spoofable header actors only for local development; production uses GitHub OAuth sessions or scoped PATs. Set `FORGEFLOW_GITHUB_OAUTH_CLIENT_ID`, `FORGEFLOW_GITHUB_OAUTH_CLIENT_SECRET`, and `FORGEFLOW_GITHUB_OAUTH_REDIRECT_URL` to enable login. Set `FORGEFLOW_GITHUB_WEBHOOK_SECRET` to enable signed webhook intake.

## GitHub App repository picker

The repository picker uses a GitHub App installation token, not the OAuth client secret. Configure the App ID, URL slug, and downloaded private-key PEM before starting the API:

```sh
FORGEFLOW_GITHUB_APP_ID=123456
FORGEFLOW_GITHUB_APP_SLUG=forgeflow
FORGEFLOW_GITHUB_APP_PRIVATE_KEY_FILE=/absolute/path/to/forgeflow-app.private-key.pem
FORGEFLOW_GITHUB_APP_CALLBACK_URL=https://forgeflow.example.com/api/v1/integrations/github/install/callback
```

Set the App webhook URL to `https://forgeflow.example.com/api/v1/integrations/github/webhooks` and use the same webhook secret as `FORGEFLOW_GITHUB_WEBHOOK_SECRET`. In the workspace, choose a project, select **Connect GitHub**, finish the GitHub App installation, then check one or more repositories to link them. The API syncs repository metadata and keeps project links tenant-scoped.

Health endpoints are `/health/live` and `/health/ready`; readiness verifies PostgreSQL connectivity and migration state. Set `FORGEFLOW_METRICS_TOKEN` to expose the low-cardinality Prometheus-compatible `/metrics` endpoint with a bearer token. In production, browser requests use the secure session cookie and the workspace organization switcher; `X-Organization-ID` and `X-Actor-ID` are only accepted when `FORGEFLOW_DEV_AUTH=true` for local development.

Attachments are private, bounded to 10 MiB per file, and stored under `FORGEFLOW_ATTACHMENTS_DIR` (the Docker development stack mounts a persistent volume). Untrusted files are downloaded with attachment disposition and `nosniff`.

## Repository map

- `backend/internal/workitem`: work-item use cases and HTTP mapping.
- `backend/internal/attachment`: bounded private work-item file uploads and downloads.
- `backend/internal/workflow`: status and transition rules.
- `backend/internal/specification`: provenance and deterministic quality gate.
- `backend/internal/auth`, `tenant`: GitHub OAuth/session/PAT and tenant-scoped project APIs.
- `backend/internal/planning`, `board`: sprint lifecycle and status-projection board.
- `backend/internal/agentrun`: approval-gated run state and structured artifacts.
- `backend/internal/github`: bounded, HMAC-verified, idempotent webhook persistence plus linked repository tree/file/search/context reads.
- `backend/internal/intelligence`: fixed-root, bounded lexical/symbol repository index.
- `backend/internal/mcp`: static tool/resource registry, application-service adapter, and remote-to-stdio bridge.
- `backend/internal/outbox`: transactional-event contract and in-memory test writer.
- `backend/db/migrations`: forward SQL migrations.
- `contracts/openapi`: API source contract and generated client types.
- `apps/web`: Next.js App Router shell.
- `apps/desktop`: Electron main/preload/renderer shell.

The desktop runner executes providers locally only: it accepts an explicit human approval, runs an allowlisted provider with fixed arguments, bounds output/time, and confines worktrees to the app-managed root. It creates a `forgeflow/*` branch, lets the user inspect and explicitly commit/push it, and never auto-merges. For a platform-tracked run, create and approve an AgentRun in the web workspace, then provide its project ID, run ID, and API URL in the desktop sync panel. You can sign in with GitHub in the panel (the session is stored with OS-encrypted `safeStorage`) or provide a short-lived PAT. The desktop advances the server run through preparation, records the bounded result, and leaves successful work in `REVIEWING`; credentials are never passed to the agent process. A server-side autonomous runner is optional behind the Docker `autonomous` profile, hydrates the linked GitHub repository into a per-run workspace, and must be isolated with dedicated credentials and workspace/network policy.

Run `make test-e2e` for the browser critical path. Run `pnpm --dir apps/desktop run make` to create desktop packages locally; release CI supplies the platform signing/notarization credentials when configured.

See [AGENTS.md](AGENTS.md) for repository rules and [docs/architecture](docs/architecture/README.md) for the implementation boundary.

For end-user operation, see the [Forgeflow user guide](docs/user-guide/README.md), including web navigation, backlog/board workflow, readiness gates, notifications, permissions, and the desktop Agent Runner flow.

For Codex or Claude, build the remote-authenticated stdio bridge with `make mcp-bridge` and follow [docs/development/mcp.md](docs/development/mcp.md). The bridge uses a short-lived PAT and never connects to PostgreSQL.
