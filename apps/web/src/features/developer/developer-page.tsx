"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { PersonalAccessToken } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { MCPConnections } from "./mcp-connections";

export function DeveloperPage() {
  const client = useMemo(() => browserAPI(), []);
  const [tokens, setTokens] = useState<PersonalAccessToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [name, setName] = useState("Forgeflow desktop");
  const [profile, setProfile] = useState<"read" | "local">("local");
  const [expiry, setExpiry] = useState("90");
  const [newToken, setNewToken] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await client.listPersonalAccessTokens();
      setTokens(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !name.trim()) return;
    setBusy(true);
    setError("");
    setMessage("");
    setNewToken("");
    try {
      const scopes = profile === "read" ? ["project.read", "repository.read"] : ["project.read", "work_item.create", "work_item.edit", "work_item.assign", "work_item.transition", "comment.create", "repository.read", "specification.propose", "agent.execute", "autonomous.start", "autonomous.retry", "autonomous.cancel"];
      const token = await client.createPersonalAccessToken({ name: name.trim(), scopes, expires_in_days: Math.min(Math.max(Number(expiry) || 90, 1), 365) });
      setTokens((current) => [token, ...current]);
      setNewToken(token.token ?? "");
      setMessage(t("developer.token-created"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function revoke(token: PersonalAccessToken) {
    if (busy || !token.id || !window.confirm(t("developer.revoke-confirm", { name: token.name ?? token.prefix ?? "token" }))) return;
    setBusy(true);
    setError("");
    try {
      await client.revokePersonalAccessToken(token.id);
      setTokens((current) => current.filter((entry) => entry.id !== token.id));
      setMessage(t("developer.token-revoked"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function copyToken() {
    if (!newToken) return;
    try {
      await navigator.clipboard.writeText(newToken);
      setMessage(t("developer.token-copied"));
    } catch {
      setError(t("developer.copy-failed"));
    }
  }

  return <section className="app-v2-page" aria-labelledby="developer-heading"><div className="app-v2-page-heading"><div><p className="eyebrow">{t("developer.eyebrow")}</p><h2 id="developer-heading">{t("developer.title")}</h2><p>{t("developer.description")}</p></div></div><MCPConnections />{error ? <div className="app-v2-error-panel" role="alert"><strong>{t("developer.load-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{newToken ? <div className="app-v2-surface-card app-v2-token-secret" role="status"><strong>{t("developer.token-once")}</strong><p>{t("developer.token-once-description")}</p><code>{newToken}</code><button className="button button-secondary" type="button" onClick={() => void copyToken()}>{t("developer.copy-token")}</button></div> : null}<div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("developer.create-token")}</h3><p>{t("developer.create-token-description")}</p></div></div><form className="app-v2-settings-create-grid" onSubmit={create}><label className="app-v2-dialog-field"><span>{t("developer.token-name")}</span><input value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required /></label><label className="app-v2-dialog-field"><span>{t("developer.profile")}</span><select value={profile} onChange={(event) => setProfile(event.target.value as "read" | "local")}><option value="read">{t("developer.read-profile")}</option><option value="local">{t("developer.local-profile")}</option></select></label><label className="app-v2-dialog-field"><span>{t("developer.expires")}</span><input type="number" min={1} max={365} value={expiry} onChange={(event) => setExpiry(event.target.value)} /></label><button className="button button-primary" type="submit" disabled={busy || !name.trim()}>{busy ? t("developer.creating") : t("developer.create")}</button></form></div><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("developer.active-tokens")}</h3><p>{t("developer.active-tokens-description")}</p></div></div>{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : tokens.length ? <div className="app-v2-token-list">{tokens.map((token) => <div className="app-v2-token-row" key={token.id ?? token.prefix}><div><strong>{token.name || token.prefix}</strong><small>{token.prefix} · {token.scopes?.join(", ") || t("developer.no-scopes")}{token.expires_at ? ` · ${new Intl.DateTimeFormat(typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN", { dateStyle: "medium" }).format(new Date(token.expires_at))}` : ""}</small></div><button className="button button-quiet is-danger" type="button" onClick={() => void revoke(token)} disabled={busy}>{t("developer.revoke")}</button></div>)}</div> : <div className="app-v2-empty"><strong>{t("developer.no-tokens")}</strong><p>{t("developer.no-tokens-description")}</p></div>}</div></section>;
}
