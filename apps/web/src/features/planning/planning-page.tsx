"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { AuthorizationContext, CreateSprintRequest, Sprint } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

type Props = { projectID: string; basePath: string };
type SprintForm = { name: string; goal: string; startsAt: string; endsAt: string };

const emptyForm: SprintForm = { name: "", goal: "", startsAt: "", endsAt: "" };

function dateValue(value?: string | null) {
  return value ? value.slice(0, 10) : "";
}

function dateLabel(value?: string | null) {
  if (!value) return "";
  return new Intl.DateTimeFormat(typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN", { dateStyle: "medium" }).format(new Date(value));
}

function statusClass(status: string) {
  return status === "ACTIVE" ? "is-active" : status === "COMPLETED" ? "is-complete" : "is-planned";
}

export function PlanningPage({ projectID, basePath }: Props) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [sprints, setSprints] = useState<Sprint[]>([]);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [form, setForm] = useState<SprintForm>(emptyForm);
  const [editingID, setEditingID] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [busyID, setBusyID] = useState("");
  const [saving, setSaving] = useState(false);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError("");
    try {
      const [sprintResult, auth] = await Promise.all([
        client.listSprints(projectID),
        client.getProjectAuthorization(projectID),
      ]);
      if (signal?.aborted) return;
      setSprints(sprintResult.items ?? []);
      setAuthorization(auth);
    } catch (cause) {
      if (!signal?.aborted) setError(apiErrorMessage(cause));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, [client, projectID]);

  useEffect(() => {
    const controller = new AbortController();
    queueMicrotask(() => {
      if (!controller.signal.aborted) void load(controller.signal);
    });
    return () => controller.abort();
  }, [load]);

  const canManage = authorization?.capabilities.includes("sprint.manage") ?? false;
  const activeSprint = sprints.find((sprint) => sprint.status === "ACTIVE");

  function updateForm(key: keyof SprintForm, value: string) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  function beginCreate() {
    setEditingID("");
    setForm(emptyForm);
    setMessage("");
    setError("");
    setFormOpen(true);
  }

  function beginEdit(sprint: Sprint) {
    setEditingID(sprint.id);
    setForm({ name: sprint.name, goal: sprint.goal, startsAt: dateValue(sprint.starts_at), endsAt: dateValue(sprint.ends_at) });
    setMessage("");
    setError("");
    setFormOpen(true);
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!form.name.trim() || saving || !canManage) return;
    setSaving(true);
    setError("");
    setMessage("");
    const input = {
      name: form.name.trim(),
      goal: form.goal.trim(),
      starts_at: form.startsAt || null,
      ends_at: form.endsAt || null,
    };
    try {
      const saved = editingID
        ? await client.updateSprint(editingID, input, projectID)
        : await client.createSprint({ project_id: projectID, ...input } satisfies CreateSprintRequest, projectID);
      setSprints((current) => editingID ? current.map((sprint) => sprint.id === saved.id ? saved : sprint) : [saved, ...current]);
      setFormOpen(false);
      setForm(emptyForm);
      setEditingID("");
      setMessage(t(editingID ? "planning.updated" : "planning.created"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function transition(sprint: Sprint) {
    if (busyID || !canManage) return;
    setBusyID(sprint.id);
    setError("");
    setMessage("");
    try {
      const updated = sprint.status === "PLANNED" ? await client.startSprint(sprint.id, projectID) : await client.completeSprint(sprint.id, projectID);
      setSprints((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(t(updated.status === "ACTIVE" ? "planning.started" : "planning.completed", { name: updated.name }));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function remove(sprint: Sprint) {
    if (busyID || !canManage || sprint.status !== "PLANNED" || !window.confirm(t("planning.delete-confirm", { name: sprint.name }))) return;
    setBusyID(sprint.id);
    setError("");
    try {
      await client.deleteSprint(sprint.id, projectID);
      setSprints((current) => current.filter((entry) => entry.id !== sprint.id));
      setMessage(t("planning.deleted"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  return <section className="app-v2-page" aria-labelledby="planning-heading">
    <div className="app-v2-page-heading">
      <div><p className="eyebrow">{t("planning.eyebrow")}</p><h2 id="planning-heading">{t("planning.title")}</h2><p>{t("planning.description")}</p></div>
      <div className="app-v2-page-actions"><Link className="button button-secondary" href={`${basePath}/backlog`}>{t("planning.open-backlog")}</Link><button className="button button-primary" type="button" onClick={beginCreate} disabled={!canManage}>{t("planning.new-sprint")}</button></div>
    </div>
    {activeSprint ? <div className="app-v2-surface-card app-v2-planning-active"><div><span className="app-v2-eyebrow-small">{t("planning.active")}</span><h3>{activeSprint.name}</h3><p>{activeSprint.goal || t("planning.no-goal")}</p></div><span className="app-v2-readiness is-ready">{statusLabel(activeSprint.status)}</span></div> : null}
    {message ? <p className="app-v2-action-status" role="status">{message}</p> : null}
    {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("planning.load-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
    {!canManage && !loading ? <div className="app-v2-inline-note" role="status">{t("planning.read-only")}</div> : null}
    {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("planning.loading")}</p></div> : null}
    {!loading && !error && !sprints.length ? <div className="app-v2-empty"><strong>{t("planning.empty")}</strong><p>{t("planning.empty-description")}</p>{canManage ? <button className="button button-secondary" type="button" onClick={beginCreate}>{t("planning.new-sprint")}</button> : null}</div> : null}
    {!loading && sprints.length ? <div className="app-v2-planning-list">{sprints.map((sprint) => <article className="app-v2-surface-card app-v2-sprint-card" key={sprint.id}><div className="app-v2-sprint-card-heading"><div><span className={`app-v2-sprint-status ${statusClass(sprint.status)}`}>{statusLabel(sprint.status)}</span><h3>{sprint.name}</h3></div><span className="app-v2-chip is-neutral">{sprint.starts_at ? `${dateLabel(sprint.starts_at)}${sprint.ends_at ? ` → ${dateLabel(sprint.ends_at)}` : ""}` : t("planning.no-dates")}</span></div><p>{sprint.goal || t("planning.no-goal")}</p><div className="app-v2-sprint-actions">{sprint.status !== "COMPLETED" ? <button className="button button-secondary" type="button" onClick={() => void transition(sprint)} disabled={!canManage || busyID === sprint.id}>{busyID === sprint.id ? t("planning.updating") : sprint.status === "PLANNED" ? t("planning.start") : t("planning.complete")}</button> : null}{sprint.status === "PLANNED" ? <><button className="button button-quiet" type="button" onClick={() => beginEdit(sprint)} disabled={!canManage || Boolean(busyID)}>{t("planning.edit")}</button><button className="button button-quiet is-danger" type="button" onClick={() => void remove(sprint)} disabled={!canManage || Boolean(busyID)}>{t("planning.delete")}</button></> : null}</div></article>)}</div> : null}
    {formOpen ? <div className="app-v2-inline-form-wrap"><form className="app-v2-surface-card app-v2-planning-form" onSubmit={save}><div className="app-v2-card-heading"><div><h3>{editingID ? t("planning.edit-title") : t("planning.new-title")}</h3><p>{t("planning.form-description")}</p></div><button className="button button-quiet" type="button" onClick={() => setFormOpen(false)}>{t("backlog.close")}</button></div><div className="app-v2-form-grid"><label className="app-v2-dialog-field"><span>{t("planning.name")}</span><input value={form.name} onChange={(event) => updateForm("name", event.target.value)} required maxLength={160} autoFocus /></label><label className="app-v2-dialog-field"><span>{t("planning.goal")}</span><textarea value={form.goal} onChange={(event) => updateForm("goal", event.target.value)} rows={3} maxLength={1000} /></label><label className="app-v2-dialog-field"><span>{t("planning.starts")}</span><input type="date" value={form.startsAt} onChange={(event) => updateForm("startsAt", event.target.value)} /></label><label className="app-v2-dialog-field"><span>{t("planning.ends")}</span><input type="date" value={form.endsAt} onChange={(event) => updateForm("endsAt", event.target.value)} /></label></div><div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={saving || !form.name.trim()}>{saving ? t("planning.saving") : t("planning.save")}</button><button className="button button-secondary" type="button" onClick={() => setFormOpen(false)}>{t("backlog.cancel")}</button></div></form></div> : null}
  </section>;
}
