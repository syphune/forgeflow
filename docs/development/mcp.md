# MCP client integration

> Muốn dùng MCP theo từng bước và xem ví dụ tiếng Việt, đọc [Hướng dẫn dùng MCP trong user guide](../user-guide/backlog-workflow.md#72-dùng-mcp-từ-codex-claude-hoặc-cursor). Phần dưới dành cho cấu hình local, bridge và autonomous nâng cao.

Forgeflow exposes two local MCP stdio options. `backend/cmd/mcp` runs against the local application/database for development; `backend/cmd/mcp-bridge` is the recommended Codex/Claude entrypoint because it forwards stdio to the authenticated remote Streamable HTTP API. Both speak the official MCP protocol, keep protocol frames on stdout, and write diagnostics to stderr.

## Build and verify

```sh
cd backend
go build -o ./bin/forgeflow-mcp ./cmd/mcp
go build -o ./bin/forgeflow-mcp-bridge ./cmd/mcp-bridge
go test -race ./internal/mcp
```

The server requires a scoped `FORGEFLOW_MCP_TOKEN` by default. For an explicitly local, in-memory smoke run only, set `FORGEFLOW_DEV_AUTH=true` and provide organization/project scope:

```sh
FORGEFLOW_DEV_AUTH=true \
FORGEFLOW_MCP_ORGANIZATION_ID=local-org \
./backend/bin/forgeflow-mcp
```

For a real workspace, use a short-lived PAT with only the required capabilities and set `DATABASE_URL`, `FORGEFLOW_MCP_TOKEN`, and optionally `FORGEFLOW_MCP_ORGANIZATION_ID`. The connection is organization-wide; every project-scoped tool call must provide `project_id`. Never put the token in command arguments or commit it to this repository.

## Team setup from the web UI

Open **Developer → MCP connections**, choose a client, and generate a short-lived token. The page produces an organization-wide MCP endpoint and client configuration. When a request does not identify exactly one project, the agent should call `project.list` and ask the user to choose one before acting. The token is shown only once; keep it in the client environment and never commit it.

The hosted Streamable HTTP endpoint is organization-wide, so Codex does not need a locally built bridge:

```sh
export FORGEFLOW_MCP_TOKEN='paste-the-token-once'
codex mcp add forgeflow-hrm \
	--url "https://forgeflow.example.com/api/v1/mcp" \
	--bearer-token-env-var FORGEFLOW_MCP_TOKEN
```

The PAT must include the required role-approved scopes, including `autonomous.start`, `autonomous.retry`, and `autonomous.cancel` for autonomous workflow control. The UI-generated connection includes them.

## Codex CLI bridge fallback

Register the bridge with an absolute path. The command stores the server entry in the local Codex config; use a scoped PAT rather than the development actor:

```sh
codex mcp add forgeflow \
	--env FORGEFLOW_MCP_URL="https://forgeflow.example.com/api/v1/mcp" \
	--env FORGEFLOW_MCP_TOKEN="$FORGEFLOW_MCP_TOKEN" \
	-- /absolute/path/to/Forgeflow/backend/bin/forgeflow-mcp-bridge
codex mcp get forgeflow
codex mcp list
```

Codex and the repository conformance client should both see `sprint.list`, `sprint.get_context`, `work_item.list`, and `specification.verify_field`. Use `sprint.list` to obtain the sprint ID and brief, then `sprint.get_context` for the iteration work items. The latter remains human-only at the application-service boundary, even if an agent invokes it.

Agents can submit `specification.propose_analysis` with a root-cause hypothesis, blast radius, implementation plan, test plan, evidence references, and confidence. The server stores it as `AI_HYPOTHESIS`; it cannot become verified through MCP.

For implementation follow-up, `agent_run.create` accepts `execution_inputs.test_case_positions`, which scopes a run to unresolved regression cases. After an agent or reviewer checks a case, `agent_run.record_test_results` stores `PASS`, `FAIL`, `BLOCKED` or `NOT_RUN`, plus a required note for failures/blockers and optional evidence references. Recording one case merges with the existing run result, so previously passed cases remain available and are not reset.

The server also advertises the server-managed `forgeflow://agent-skill` resource. The MCP `initialize` instructions point the client at this resource before project work begins, so a remote agent does not need a machine-local `~/.codex/skills` installation. The full skill is embedded in the MCP binary; rebuild and redeploy the MCP server after changing it. The other scoped resource templates are `task://{project_key}/{number}`, `sprint://{sprint_id}?project_id={project_id}`, `repo://{repository_id}/{topic}?project_id={project_id}`, and `repo://{repository_id}/module/{path}?project_id={project_id}`. Resource reads use the same project authorization as tools and cap JSON/file payloads.

## Repository source of truth

MCP repository tools always resolve the repository link in the selected Forgeflow project, then use the connected GitHub App installation. Repository identity, current tree/file/search/symbol candidates, related files, and commit/PR history come from the linked GitHub repository/default branch; Forgeflow-only CI and knowledge projections remain labeled application data. MCP does not read a local checkout or persisted repository snapshot. An unlinked repository is rejected.

MCP is read-only for GitHub repositories: it does not create a branch, commit, push, or pull request. Code changes go through the local Desktop Agent Runner, where the user chooses a local code folder, selects a discovered Git repository, reviews the diff, and explicitly approves the agent before execution. The hosted MCP server cannot access the user's filesystem or browser `localStorage`.

The Desktop Runner stores the selected local project path and execution fields in machine-local `localStorage` so the next run can reuse them. System rules require Forgeflow work branches to use `forgeflow/*` and commit subjects to follow Conventional Commits. The autonomous runner may still clone GitHub into an isolated workspace only when its project policy explicitly enables server execution; that workspace is not used for MCP lookup.

## Autonomous workflow

The MCP catalog includes `autonomous.start`, `autonomous.get`, `autonomous.resume`, `autonomous.retry`, `autonomous.cancel`, `autonomous.add_feedback`, and `autonomous.record_test_results`. A manager or leader can provide one objective; Forgeflow creates or reuses the work item, waits for the specification quality gate, starts the configured Codex/Claude runner, and preserves the workflow gate and feedback.

The retry contract is incremental: the server stores `PASS`, `FAIL`, `BLOCKED`, and `NOT_RUN` per regression position. Reviewers add a note and retry only `unresolved_positions`; passed cases are not reset. Specification verification and production deployment approval are human-only capabilities.

For a Docker server runner, keep provider credentials in the runner environment and use a separate long random token between worker and runner:

```sh
FORGEFLOW_RUNNER_URL=http://runner:8090 \
FORGEFLOW_RUNNER_TOKEN="$(openssl rand -hex 32)" \
docker compose --env-file .env.local --profile autonomous -f infra/compose.dev.yaml up --build
```

The runner image is an execution boundary, not a trust boundary for unreviewed repository content: deploy it with a dedicated workspace volume, least-privilege credentials, network restrictions, and no production secrets. Docker jobs use a read-only root filesystem, non-root UID, dropped capabilities, `no-new-privileges`, a `noexec,nosuid` temporary filesystem, CPU/RAM/PID/timeout limits, and bounded/redacted output. Network defaults to `none`; use `bridge` only behind an egress firewall/allowlist when the provider API needs outbound access. Use a version-pinned image or digest, never `latest`.

When the worker and Docker daemon do not share the same host path, set `FORGEFLOW_RUNNER_DOCKER_WORKSPACE_ROOT` to the absolute path visible to the daemon. The `/var/run/docker.sock` mount gives the runner near-root control of the host; keep it on a dedicated host/VM or replace it with a rootless/isolated daemon for a production pilot. Production release approval and verification remain human-only, and the runner has no production secrets by default.

When the autonomous worker dispatches a linked repository, the runner validates the HTTPS clone URL, clones it into the per-run workspace, checks out the requested base SHA/branch, and reuses that workspace on retries. Public GitHub repositories work without extra configuration; private repositories require a short-lived `FORGEFLOW_GIT_TOKEN` mounted only in the runner. The worker never sends provider or Git credentials in the job payload.

## Claude Desktop

Claude Desktop uses the same stdio transport. Add a server entry to its local configuration and restart the app:

```json
{
  "mcpServers": {
    "forgeflow": {
		"command": "/absolute/path/to/Forgeflow/backend/bin/forgeflow-mcp-bridge",
		"args": [],
		"env": {
		  "FORGEFLOW_MCP_URL": "https://forgeflow.example.com/api/v1/mcp",
		  "FORGEFLOW_MCP_TOKEN": "replace-with-a-short-lived-pat"
		}
    }
  }
}
```

Claude Desktop was not installed in the local test environment, so live desktop discovery is not claimed here. The official SDK stdio conformance test is the protocol compatibility check for both clients; client-specific setup is intentionally kept in their own configuration.

## Scope and safety

- MCP adapters call application services; they do not access the database directly.
- PAT scopes are intersected with role capabilities, never added to them.
- Repository and AI content is returned as untrusted content.
- Tool names, descriptions, and schemas are static and covered by a snapshot test.
- The API also exposes authenticated Streamable HTTP at `/api/v1/mcp`; use a Bearer PAT and pass `project_id` to each project-scoped tool. `X-Project-ID` and the `project_id` URL query remain supported for legacy pinned connections.
- The bridge only forwards the upstream tool/resource catalog and bearer token; it does not access PostgreSQL or add capabilities.
- Set `FORGEFLOW_MCP_URL=http://localhost:18080/api/v1/mcp` for the local Docker deployment.
