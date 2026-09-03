"use client";

import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { AuthorizationContext, AutonomousRun, ProviderAuthStatus, WorkItem } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { AutonomousRunProgress, isAutonomousRunActive } from "../app/autonomous-run-progress";
import { ForgeSelect } from "../app/forge-select";

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
  repositoryID,
  authorization,
}: {
  projectID: string;
  item: WorkItem;
  repositoryID: string;
  authorization: AuthorizationContext | null;
}) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [runs, setRuns] = useState<AutonomousRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [provider, setProvider] = useState<"codex" | "claude">("codex");
  const [model, setModel] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState("");
  const [providerStatus, setProviderStatus] = useState<ProviderAuthStatus | null>(null);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [catalogError, setCatalogError] = useState("");
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
    if (!runs.some((run) => isAutonomousRunActive(run.status))) return;
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, [load, runs]);

  const canStart = authorization?.capabilities.includes("autonomous.start") ?? false;
  const canRetry = authorization?.capabilities.includes("autonomous.retry") ?? false;
  const canCancel = authorization?.capabilities.includes("autonomous.cancel") ?? false;

  useEffect(() => {
    let active = true;
    if (!formOpen || !canStart) return () => { active = false; };
    queueMicrotask(() => {
      if (!active) return;
      setCatalogLoading(true);
      setCatalogError("");
    });
    void client.getProjectProviderAuth(projectID).then((result) => {
      if (!active) return;
      setProviderStatus((result.items ?? []).find((entry) => entry.provider === provider) ?? null);
    }).catch((cause: unknown) => {
      if (!active) return;
      setProviderStatus(null);
      setCatalogError(apiErrorMessage(cause));
    }).finally(() => {
      if (active) setCatalogLoading(false);
    });
    return () => { active = false; };
  }, [canStart, client, formOpen, projectID, provider]);

  async function start(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canStart || busyID || !objective.trim() || !repositoryID) return;
    setBusyID("create");
    setError("");
    try {
      const created = await client.startAutonomousRun({
        work_item_id: item.id,
        repository_id: repositoryID,
        objective: objective.trim(),
        agent_provider: provider,
        model: model.trim() || undefined,
        reasoning_effort: reasoningEffort.trim() || undefined,
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

  const providerModels = providerStatus?.models ?? [];
  const selectedModel = providerModels.find((entry) => entry.id === model) ?? (model ? undefined : providerModels.find((entry) => entry.is_default));
  const reasoningOptions = selectedModel?.reasoning_efforts?.length ? selectedModel.reasoning_efforts : providerStatus?.reasoning_efforts ?? [];

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
        <button className="button button-primary" type="button" onClick={() => setFormOpen((current) => !current)} disabled={!canStart || !repositoryID}>
          {formOpen ? t("backlog.close") : t("work.start-autonomous")}
        </button>
      </div>
      {!repositoryID ? <div className="app-v2-inline-note">{t("work.agent-repository-required")}</div> : null}
      {!canStart ? <div className="app-v2-inline-note">{t("work.autonomous-read-only")}</div> : null}
      {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("work.autonomous-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
      {message ? <p className="app-v2-action-status" role="status">{message}</p> : null}
      {formOpen ? <form className="app-v2-run-form" onSubmit={start}>
        <div className="app-v2-dialog-field"><span>{t("work.agent-provider")}</span><ForgeSelect ariaLabel={t("work.agent-provider")} value={provider} options={[{ value: "codex", label: "Codex" }, { value: "claude", label: "Claude" }]} placeholder={t("work.agent-provider")} onChange={(value) => { setProvider(value as "codex" | "claude"); setProviderStatus(null); setModel(""); setReasoningEffort(""); }} /></div>
        <div className="app-v2-dialog-field"><span>{t("work.autonomous-model")}</span>{providerModels.length ? <ForgeSelect ariaLabel={t("work.autonomous-model")} value={model} options={[{ value: "", label: t("work.autonomous-model-default") }, ...providerModels.map((entry) => ({ value: entry.id, label: `${entry.display_name || entry.id}${entry.is_default ? " · default" : ""}` }))]} placeholder={t("work.autonomous-model-default")} onChange={(value) => { setModel(value); setReasoningEffort(""); }} /> : <input value={model} onChange={(event) => setModel(event.target.value)} maxLength={120} placeholder={t("work.autonomous-model-placeholder")} />}{catalogLoading ? <small>{t("work.autonomous-model-catalog-loading")}</small> : providerStatus?.model_catalog_message ? <small>{t("work.autonomous-model-catalog-manual")}</small> : catalogError ? <small>{catalogError}</small> : null}</div>
        <div className="app-v2-dialog-field"><span>{t("work.autonomous-reasoning")}</span><ForgeSelect ariaLabel={t("work.autonomous-reasoning")} value={reasoningEffort} options={[{ value: "", label: t("work.autonomous-reasoning-default") }, ...reasoningOptions.map((entry) => ({ value: entry.value, label: `${entry.value}${entry.description ? ` — ${entry.description}` : ""}` }))]} placeholder={t("work.autonomous-reasoning-default")} disabled={!reasoningOptions.length} onChange={setReasoningEffort} />{!reasoningOptions.length && !catalogLoading ? <small>{t("work.autonomous-reasoning-unavailable")}</small> : null}</div>
        <label className="app-v2-dialog-field app-v2-run-prompt"><span>{t("work.autonomous-objective")}</span><textarea value={objective} onChange={(event) => setObjective(event.target.value)} rows={5} required /></label>
        <div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={busyID === "create" || !objective.trim()}>{busyID === "create" ? t("work.autonomous-starting") : t("work.start-autonomous")}</button><button className="button button-secondary" type="button" onClick={() => setFormOpen(false)}>{t("backlog.cancel")}</button></div>
      </form> : null}
      {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("work.autonomous-loading")}</p></div> : !runs.length ? <div className="app-v2-empty"><strong>{t("work.no-autonomous-runs")}</strong><p>{t("work.no-autonomous-runs-description")}</p></div> : <div className="app-v2-run-list">
        {runs.map((run) => {
          const canResume = ["WAITING_SPEC_REVIEW", "PAUSED", "FAILED", "WAITING_TEST_FEEDBACK"].includes(run.status);
          const canRetryRun = ["WAITING_TEST_FEEDBACK", "WAITING_PR_REVIEW", "FAILED"].includes(run.status);
          const retryNeedsFeedback = run.status === "WAITING_TEST_FEEDBACK";
          const details = feedbackItems[run.id] ?? [];
          return <article className="app-v2-run-card" key={run.id}>
            <div className="app-v2-run-card-heading"><div><span className="app-v2-key">{run.agent_name}</span><h4>{workflowLabel(run.phase)}</h4></div><span className={`app-v2-run-status is-${run.status.toLowerCase()}`}>{workflowLabel(run.status)}</span></div>
            <p>{run.objective}</p>
            <div className="app-v2-run-meta"><span>{t("work.autonomous-attempt", { attempt: run.attempt, max: run.max_attempts })}</span>{run.gate ? <span>{t("work.autonomous-gate", { gate: workflowLabel(run.gate) })}</span> : null}{run.model ? <span>{t("work.autonomous-model-value", { model: run.model })}</span> : null}{run.reasoning_effort ? <span>{t("work.autonomous-reasoning-value", { effort: run.reasoning_effort })}</span> : null}</div>
            {(isAutonomousRunActive(run.status) || run.status === "WAITING_SPEC_REVIEW") ? <AutonomousRunProgress run={run} /> : null}
            {run.last_error ? <div className="app-v2-inline-note is-danger">{run.last_error}</div> : null}
            <div className="app-v2-run-actions">
              <button className="button button-secondary" type="button" onClick={() => void toggleDetails(run)}>{expandedID === run.id ? t("backlog.close") : t("work.autonomous-feedback")}</button>
              {canResume ? <button className="button button-secondary" type="button" onClick={() => void runAction(run, "resume")} disabled={!canRetry || Boolean(busyID)}>{t("work.autonomous-resume")}</button> : null}
              {canRetryRun ? <button className="button button-primary" type="button" onClick={() => void retry(run)} disabled={!canRetry || Boolean(busyID) || (retryNeedsFeedback && !feedback[run.id]?.trim())}>{t("work.autonomous-retry")}</button> : null}
              {isAutonomousRunActive(run.status) ? <button className="button button-quiet is-danger" type="button" onClick={() => void runAction(run, "cancel")} disabled={!canCancel || Boolean(busyID)}>{t("work.cancel-run")}</button> : null}
            </div>
            {canRetryRun ? <label className="app-v2-dialog-field"><span>{t("work.autonomous-feedback-prompt")}</span><textarea value={feedback[run.id] ?? ""} onChange={(event) => setFeedback((current) => ({ ...current, [run.id]: event.target.value }))} rows={3} placeholder={t("work.autonomous-feedback-placeholder")} /></label> : null}
            {expandedID === run.id ? <div className="app-v2-run-feedback" aria-live="polite">{details.length ? details.map((entry, index) => <div className="app-v2-comment" key={`${run.id}-${index}`}><strong>{workflowLabel(entry.source)}</strong><p>{entry.note}</p></div>) : <p>{t("work.autonomous-no-feedback")}</p>}</div> : null}
          </article>;
        })}
      </div>}
    </div>
  </section>;
}
