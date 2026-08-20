# MCP client integration

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
FORGEFLOW_MCP_PROJECT_ID=local-project \
./backend/bin/forgeflow-mcp
```

For a real workspace, use a short-lived PAT with only the required capabilities and set `DATABASE_URL`, `FORGEFLOW_MCP_TOKEN`, `FORGEFLOW_MCP_ORGANIZATION_ID`, and `FORGEFLOW_MCP_PROJECT_ID`. Never put the token in command arguments or commit it to this repository.

## Team setup from the web UI

Open **Developer → MCP connections**, choose a project and client, and generate a short-lived token. The page produces the project-scoped MCP endpoint and client configuration. The token is shown only once; keep it in the client environment and never commit it.

The hosted Streamable HTTP endpoint accepts the project scope in the URL, so Codex does not need a locally built bridge:

```sh
export FORGEFLOW_MCP_TOKEN='paste-the-token-once'
codex mcp add forgeflow-hrm \
	--url "https://forgeflow.example.com/api/v1/mcp?project_id=PROJECT_ID" \
	--bearer-token-env-var FORGEFLOW_MCP_TOKEN
```

The PAT must include the required role-approved scopes, including `autonomous.start`, `autonomous.retry`, and `autonomous.cancel` for autonomous workflow control. The UI-generated connection includes them.

## Codex CLI bridge fallback

Register the bridge with an absolute path. The command stores the server entry in the local Codex config; use a scoped PAT rather than the development actor:

```sh
codex mcp add forgeflow \
	--env FORGEFLOW_MCP_URL="https://forgeflow.example.com/api/v1/mcp" \
	--env FORGEFLOW_MCP_TOKEN="$FORGEFLOW_MCP_TOKEN" \
	--env FORGEFLOW_MCP_PROJECT_ID="$FORGEFLOW_MCP_PROJECT_ID" \
	-- /absolute/path/to/Forgeflow/backend/bin/forgeflow-mcp-bridge
codex mcp get forgeflow
codex mcp list
```

Codex and the repository conformance client should both see `work_item.list` and `specification.verify_field`. The latter remains human-only at the application-service boundary, even if an agent invokes it.

Agents can submit `specification.propose_analysis` with a root-cause hypothesis, blast radius, implementation plan, test plan, evidence references, and confidence. The server stores it as `AI_HYPOTHESIS`; it cannot become verified through MCP.

For implementation follow-up, `agent_run.create` accepts `execution_inputs.test_case_positions`, which scopes a run to unresolved regression cases. After an agent or reviewer checks a case, `agent_run.record_test_results` stores `PASS`, `FAIL`, `BLOCKED` or `NOT_RUN`, plus a required note for failures/blockers and optional evidence references. Recording one case merges with the existing run result, so previously passed cases remain available and are not reset.

The server also advertises scoped resource templates: `task://{project_key}/{number}`, `repo://{repository_id}/{topic}`, and `repo://{repository_id}/module/{path}`. Resource reads use the same project authorization as tools and cap JSON/file payloads.

## Autonomous workflow

The MCP catalog includes `autonomous.start`, `autonomous.get`, `autonomous.resume`, `autonomous.retry`, `autonomous.cancel`, `autonomous.add_feedback`, and `autonomous.record_test_results`. A manager or leader can provide one objective; Forgeflow creates or reuses the work item, waits for the specification quality gate, starts the configured Codex/Claude runner, and preserves the workflow gate and feedback.

The retry contract is incremental: the server stores `PASS`, `FAIL`, `BLOCKED`, and `NOT_RUN` per regression position. Reviewers add a note and retry only `unresolved_positions`; passed cases are not reset. Specification verification and production deployment approval are human-only capabilities.

For a Docker server runner, keep provider credentials in the runner environment and use a separate long random token between worker and runner:

```sh
FORGEFLOW_RUNNER_URL=http://runner:8090 \
FORGEFLOW_RUNNER_TOKEN="$(openssl rand -hex 32)" \
docker compose --env-file .env.local --profile autonomous -f infra/compose.dev.yaml up --build
```

The runner image is an execution boundary, not a trust boundary for unreviewed repository content: deploy it with a dedicated workspace volume, least-privilege credentials, network restrictions, and no production secrets. The default Compose stack leaves the runner profile disabled.

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
		  "FORGEFLOW_MCP_TOKEN": "replace-with-a-short-lived-pat",
		  "FORGEFLOW_MCP_PROJECT_ID": "replace-with-project-id"
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
- The API also exposes authenticated Streamable HTTP at `/api/v1/mcp`; use a Bearer PAT and `X-Project-ID` or the `project_id` URL query for remote clients.
- The bridge only forwards the upstream tool/resource catalog and bearer token; it does not access PostgreSQL or add capabilities.
- Set `FORGEFLOW_MCP_URL=http://localhost:18080/api/v1/mcp` for the local Docker deployment.
