"use client";

import { useMemo, useState } from "react";
import type { PersonalAccessToken } from "@forgeflow/api-client";
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

function endpointFor() {
	return `${publicAPIOrigin()}/api/v1/mcp`;
}

function configurationFor(client: MCPClient) {
	const endpoint = endpointFor();
	const name = "forgeflow";
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

export function MCPConnections({ onTokenChanged }: { onTokenChanged?: () => void }) {
	const client = useMemo(() => browserAPI(), []);
	const [provider, setProvider] = useState<MCPClient>("codex");
  const [expires, setExpires] = useState("90");
  const [connection, setConnection] = useState<PersonalAccessToken | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
	const [message, setMessage] = useState("");
	const configuration = configurationFor(provider);

  function resetConnection() {
    setConnection(null);
    setMessage("");
  }

  async function generate() {
		if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    setConnection(null);
    try {
      const token = await client.createPersonalAccessToken({
			name: `MCP ${provider}`,
        scopes: autonomousScopes,
        expires_in_days: Math.min(Math.max(Number(expires) || 90, 1), 365),
      });
      setConnection(token);
      onTokenChanged?.();
      setMessage(t("developer.mcp-generated"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function revokeConnection() {
    if (busy || !connection?.id || !window.confirm(t("developer.revoke-confirm", { name: connection.name ?? connection.prefix ?? "MCP token" }))) return;
    setBusy(true);
    setError("");
    try {
      await client.revokePersonalAccessToken(connection.id);
      setConnection(null);
      onTokenChanged?.();
      setMessage(t("developer.token-revoked"));
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
	    {error ? <div className="app-v2-error-panel" role="alert"><span>{error}</span></div> : null}
	    {message ? <p className="app-v2-action-status" role="status">{message}</p> : null}
	    <div className="app-v2-form-grid">
	      <label className="app-v2-dialog-field"><span>{t("developer.mcp-client")}</span><select value={provider} onChange={(event) => { setProvider(event.target.value as MCPClient); resetConnection(); }}><option value="codex">Codex</option><option value="cursor">Cursor</option><option value="claude">Claude</option></select></label>
	      <label className="app-v2-dialog-field"><span>{t("developer.mcp-expires")}</span><input type="number" min={1} max={365} value={expires} onChange={(event) => { setExpires(event.target.value); resetConnection(); }} /></label>
	    </div>
	    <div className="app-v2-editor-actions"><button className="button button-primary" type="button" onClick={() => void generate()} disabled={busy}>{busy ? t("developer.mcp-generating") : t("developer.mcp-generate")}</button></div>
    {connection?.token ? <div className="app-v2-token-secret" role="status"><strong>{t("developer.token-once")}</strong><p>{t("developer.mcp-token-warning")}</p><code>{connection.token}</code><button className="button button-secondary" type="button" onClick={() => void copy(connection.token ?? "", "developer.token-copied")}>{t("developer.copy-token")}</button><button className="button button-quiet is-danger" type="button" onClick={() => void revokeConnection()} disabled={busy}>{t("developer.revoke")}</button></div> : null}
	    {connection ? <div className="app-v2-mcp-setup"><div className="app-v2-card-heading"><div><h4>{t("developer.mcp-configuration")}</h4><p>{t("developer.mcp-env-note")}</p></div><button className="button button-secondary" type="button" onClick={() => void copy(configuration, "developer.mcp-copied")}>{t("developer.mcp-copy-configuration")}</button></div><pre className="app-v2-code-block"><code>{configuration}</code></pre><small className="app-v2-mcp-endpoint"><strong>{t("developer.mcp-endpoint")}</strong> {endpointFor()}</small></div> : null}
  </section>;
}
