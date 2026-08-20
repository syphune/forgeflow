"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import type { PersonalAccessToken, Project } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

type MCPClient = "codex" | "cursor" | "claude";

const autonomousScopes = [
  "project.read",
  "work_item.create",
  "work_item.edit",
  "work_item.assign",
  "work_item.transition",
  "comment.create",
  "repository.read",
  "specification.propose",
  "agent.execute",
  "autonomous.start",
  "autonomous.retry",
  "autonomous.cancel",
];

function publicAPIOrigin() {
  const configured = (process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? "")
    .replace(/\/api\/v1\/?$/, "")
    .replace(/\/+$/, "");
  return configured || (typeof window === "undefined" ? "" : window.location.origin);
}

function serverName(project: Project) {
  const key = project.key.toLowerCase().replace(/[^a-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "") || "project";
  return `forgeflow-${key}`;
}

function endpointFor(project: Project) {
  return `${publicAPIOrigin()}/api/v1/mcp?project_id=${encodeURIComponent(project.id)}`;
}

function configurationFor(client: MCPClient, project: Project) {
  const endpoint = endpointFor(project);
  const name = serverName(project);
  if (client === "codex") {
    return `# Set the token in your shell, then register Forgeflow.\nexport FORGEFLOW_MCP_TOKEN='PASTE_THE_TOKEN_HERE'\n\ncodex mcp add ${name} \\\n  --url "${endpoint}" \\\n  --bearer-token-env-var FORGEFLOW_MCP_TOKEN`;
  }
  return `# Set FORGEFLOW_MCP_TOKEN in the environment used by ${client === "cursor" ? "Cursor" : "Claude"}.\n${JSON.stringify({
    mcpServers: {
      [name]: {
        url: endpoint,
        headers: { Authorization: "Bearer ${FORGEFLOW_MCP_TOKEN}" },
      },
    },
  }, null, 2)}`;
}

export function MCPConnections() {
  const client = useMemo(() => browserAPI(), []);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState("");
  const [provider, setProvider] = useState<MCPClient>("codex");
  const [expires, setExpires] = useState("90");
  const [connection, setConnection] = useState<PersonalAccessToken | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await client.listProjects();
      const items = (result.items ?? []).slice().sort((left, right) => left.key.localeCompare(right.key));
      setProjects(items);
      const requestedProjectID = typeof window === "undefined" ? "" : new URLSearchParams(window.location.search).get("project_id") ?? "";
      setProjectID((current) => current && items.some((item) => item.id === current) ? current : requestedProjectID && items.some((item) => item.id === requestedProjectID) ? requestedProjectID : items[0]?.id ?? "");
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  const project = projects.find((item) => item.id === projectID) ?? null;
  const configuration = project ? configurationFor(provider, project) : "";

  function resetConnection() {
    setConnection(null);
    setMessage("");
  }

  async function generate() {
    if (!project || busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    setConnection(null);
    try {
      const token = await client.createPersonalAccessToken({
        name: `MCP ${provider} · ${project.key}`,
        scopes: autonomousScopes,
        expires_in_days: Math.min(Math.max(Number(expires) || 90, 1), 365),
      });
      setConnection(token);
      setMessage(t("developer.mcp-generated"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function copy(value: string, successKey: "developer.token-copied" | "developer.mcp-copied") {
    try {
      await navigator.clipboard.writeText(value);
      setMessage(t(successKey));
    } catch {
      setError(t("developer.copy-failed"));
    }
  }

  return <section id="mcp-connections" className="app-v2-surface-card app-v2-settings-card app-v2-mcp-connection" aria-labelledby="mcp-connections-heading">
    <div className="app-v2-card-heading">
      <div>
        <p className="eyebrow">{t("developer.mcp-eyebrow")}</p>
        <h3 id="mcp-connections-heading">{t("developer.mcp-title")}</h3>
        <p>{t("developer.mcp-description")}</p>
      </div>
    </div>
    {error ? <div className="app-v2-error-panel" role="alert"><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
    <div className="app-v2-form-grid">
      <label className="app-v2-dialog-field"><span>{t("developer.mcp-client")}</span><select value={provider} onChange={(event) => { setProvider(event.target.value as MCPClient); resetConnection(); }}><option value="codex">Codex</option><option value="cursor">Cursor</option><option value="claude">Claude</option></select></label>
      <label className="app-v2-dialog-field"><span>{t("developer.mcp-project")}</span><select value={projectID} onChange={(event) => { setProjectID(event.target.value); resetConnection(); }} disabled={loading || !projects.length}><option value="">{loading ? t("app.loading-projects") : t("developer.mcp-select-project")}</option>{projects.map((item) => <option value={item.id} key={item.id}>{item.key} · {item.display_name}</option>)}</select></label>
      <label className="app-v2-dialog-field"><span>{t("developer.mcp-expires")}</span><input type="number" min={1} max={365} value={expires} onChange={(event) => { setExpires(event.target.value); resetConnection(); }} /></label>
    </div>
    {!loading && !projects.length ? <div className="app-v2-inline-note">{t("developer.mcp-no-projects")}</div> : null}
    <div className="app-v2-editor-actions"><button className="button button-primary" type="button" onClick={() => void generate()} disabled={busy || !project}>{busy ? t("developer.mcp-generating") : t("developer.mcp-generate")}</button></div>
    {connection?.token ? <div className="app-v2-token-secret" role="status"><strong>{t("developer.token-once")}</strong><p>{t("developer.mcp-token-warning")}</p><code>{connection.token}</code><button className="button button-secondary" type="button" onClick={() => void copy(connection.token ?? "", "developer.token-copied")}>{t("developer.copy-token")}</button></div> : null}
    {connection && project ? <div className="app-v2-mcp-setup"><div className="app-v2-card-heading"><div><h4>{t("developer.mcp-configuration")}</h4><p>{t("developer.mcp-env-note")}</p></div><button className="button button-secondary" type="button" onClick={() => void copy(configuration, "developer.mcp-copied")}>{t("developer.mcp-copy-configuration")}</button></div><pre className="app-v2-code-block"><code>{configuration}</code></pre><small className="app-v2-mcp-endpoint"><strong>{t("developer.mcp-endpoint")}</strong> {endpointFor(project)}</small></div> : null}
  </section>;
}
