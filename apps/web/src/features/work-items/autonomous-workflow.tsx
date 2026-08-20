"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { AuthorizationContext, AutonomousRun, WorkItem } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

const activeStatuses = [
  "QUEUED",
  "INTAKE",
  "READY_FOR_EXECUTION",
  "EXECUTING",
  "FIXING",
  "DEPLOYING",
];

function workflowLabel(value: string): string {
  return value
    .toLowerCase()
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function AutonomousWorkflowPanel({
  projectID,
  item,
  authorization,
}: {
  projectID: string;
  item: WorkItem;
  authorization: AuthorizationContext | null;
}) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [runs, setRuns] = useState<AutonomousRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [provider, setProvider] = useState("codex");
  const [objective, setObjective] = useState(item.description ?? item.title);
  const [feedback, setFeedback] = useState<Record<string, string>>({});
  const [expandedID, setExpandedID] = useState("");
  const [feedbackItems, setFeedbackItems] = useState<Record<string, { note: string; source: string }[]>>({});

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    setError("");
    try {
      const result = await client.listAutonomousRuns(projectID, item.id);
      setRuns(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      if (!silent) setLoading(false);
    }
  }, [client, item.id, projectID]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  useEffect(() => {
    if (!runs.some((run) => activeStatuses.includes(run.status))) return;
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, [load, runs]);

  const canStart = authorization?.capabilities.includes("autonomous.start") ?? false;
  const canRetry = authorization?.capabilities.includes("autonomous.retry") ?? false;
  const canCancel = authorization?.capabilities.includes("autonomous.cancel") ?? false;

  async function start(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canStart || busyID || !objective.trim() || !item.repository_id) return;
    setBusyID("create");
    setError("");
    try {
      const created = await client.startAutonomousRun({
        work_item_id: item.id,
        repository_id: item.repository_id,
        objective: objective.trim(),
        agent_provider: provider as "codex" | "claude",
      }, projectID);
      setRuns((current) => [created, ...current]);
      setFormOpen(false);
      setMessage(t("work.autonomous-started"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function runAction(run: AutonomousRun, action: "resume" | "cancel") {
    if (busyID || (action === "resume" && !canRetry) || (action === "cancel" && !canCancel)) return;
    setBusyID(run.id);
    setError("");
    try {
      const updated = action === "resume"
        ? await client.resumeAutonomousRun(run.id, projectID)
        : await client.cancelAutonomousRun(run.id, projectID);
      setRuns((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(action === "resume" ? t("work.autonomous-resumed") : t("work.autonomous-cancelled"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function retry(run: AutonomousRun) {
    if (!canRetry || busyID) return;
    setBusyID(run.id);
    setError("");
    try {
      const updated = await client.retryAutonomousRun(run.id, {
        feedback: feedback[run.id]?.trim() || undefined,
        test_case_positions: run.unresolved_positions?.length ? run.unresolved_positions : undefined,
      }, projectID);
      setRuns((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setFeedback((current) => ({ ...current, [run.id]: "" }));
      setMessage(t("work.autonomous-retried"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function toggleDetails(run: AutonomousRun) {
    if (expandedID === run.id) {
      setExpandedID("");
      return;
    }
    setExpandedID(run.id);
    if (feedbackItems[run.id]) return;
    try {
      const detail = await client.getAutonomousRun(run.id, projectID);
      setFeedbackItems((current) => ({
        ...current,
        [run.id]: (detail.feedback ?? []).map((entry) => ({ note: entry.note, source: entry.source ?? "" })),
      }));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    }
  }

  return <section className="app-v2-detail-stack" aria-labelledby="autonomous-workflow-heading">
    <div className="app-v2-surface-card">
      <div className="app-v2-card-heading">
        <div>
          <h3 id="autonomous-workflow-heading">{t("work.autonomous-workflow")}</h3>
          <p>{t("work.autonomous-workflow-description")}</p>
        </div>
        <button className="button button-primary" type="button" onClick={() => setFormOpen((current) => !current)} disabled={!canStart || !item.repository_id}>
          {formOpen ? t("backlog.close") : t("work.start-autonomous")}
        </button>
      </div>
      {!item.repository_id ? <div className="app-v2-inline-note">{t("work.agent-repository-required")}</div> : null}
      {!canStart ? <div className="app-v2-inline-note">{t("work.autonomous-read-only")}</div> : null}
      {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("work.autonomous-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
      {message ? <p className="app-v2-action-status" role="status">{message}</p> : null}
      {formOpen ? <form className="app-v2-run-form" onSubmit={start}>
        <label className="app-v2-dialog-field"><span>{t("work.agent-provider")}</span><select value={provider} onChange={(event) => setProvider(event.target.value)}><option value="codex">Codex</option><option value="claude">Claude</option></select></label>
        <label className="app-v2-dialog-field app-v2-run-prompt"><span>{t("work.autonomous-objective")}</span><textarea value={objective} onChange={(event) => setObjective(event.target.value)} rows={5} required /></label>
        <div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={busyID === "create" || !objective.trim()}>{busyID === "create" ? t("work.autonomous-starting") : t("work.start-autonomous")}</button><button className="button button-secondary" type="button" onClick={() => setFormOpen(false)}>{t("backlog.cancel")}</button></div>
      </form> : null}
      {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("work.autonomous-loading")}</p></div> : !runs.length ? <div className="app-v2-empty"><strong>{t("work.no-autonomous-runs")}</strong><p>{t("work.no-autonomous-runs-description")}</p></div> : <div className="app-v2-run-list">
        {runs.map((run) => {
          const canResume = ["WAITING_SPEC_REVIEW", "PAUSED", "FAILED", "WAITING_TEST_FEEDBACK"].includes(run.status);
          const canRetryRun = ["WAITING_TEST_FEEDBACK", "WAITING_PR_REVIEW", "FAILED"].includes(run.status);
          const details = feedbackItems[run.id] ?? [];
          return <article className="app-v2-run-card" key={run.id}>
            <div className="app-v2-run-card-heading"><div><span className="app-v2-key">{run.agent_name}</span><h4>{workflowLabel(run.phase)}</h4></div><span className={`app-v2-run-status is-${run.status.toLowerCase()}`}>{workflowLabel(run.status)}</span></div>
            <p>{run.objective}</p>
            <div className="app-v2-run-meta"><span>{t("work.autonomous-attempt", { attempt: run.attempt, max: run.max_attempts })}</span>{run.gate ? <span>{t("work.autonomous-gate", { gate: workflowLabel(run.gate) })}</span> : null}</div>
            {run.last_error ? <div className="app-v2-inline-note is-danger">{run.last_error}</div> : null}
            <div className="app-v2-run-actions">
              <button className="button button-secondary" type="button" onClick={() => void toggleDetails(run)}>{expandedID === run.id ? t("backlog.close") : t("work.autonomous-feedback")}</button>
              {canResume ? <button className="button button-secondary" type="button" onClick={() => void runAction(run, "resume")} disabled={!canRetry || Boolean(busyID)}>{t("work.autonomous-resume")}</button> : null}
              {canRetryRun ? <button className="button button-primary" type="button" onClick={() => void retry(run)} disabled={!canRetry || Boolean(busyID) || !feedback[run.id]?.trim()}>{t("work.autonomous-retry")}</button> : null}
              {activeStatuses.includes(run.status) ? <button className="button button-quiet is-danger" type="button" onClick={() => void runAction(run, "cancel")} disabled={!canCancel || Boolean(busyID)}>{t("work.cancel-run")}</button> : null}
            </div>
            {canRetryRun ? <label className="app-v2-dialog-field"><span>{t("work.autonomous-feedback-prompt")}</span><textarea value={feedback[run.id] ?? ""} onChange={(event) => setFeedback((current) => ({ ...current, [run.id]: event.target.value }))} rows={3} placeholder={t("work.autonomous-feedback-placeholder")} /></label> : null}
            {expandedID === run.id ? <div className="app-v2-run-feedback" aria-live="polite">{details.length ? details.map((entry, index) => <div className="app-v2-comment" key={`${run.id}-${index}`}><strong>{workflowLabel(entry.source)}</strong><p>{entry.note}</p></div>) : <p>{t("work.autonomous-no-feedback")}</p>}</div> : null}
          </article>;
        })}
      </div>}
    </div>
  </section>;
}
