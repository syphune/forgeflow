"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { AgentRun, AuthorizationContext, CreateAgentRunRequest, WorkItem } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

type RegressionCase = { position?: number; scenario?: string; expected_result?: string };
type AgentTestStatus = "NOT_RUN" | "PASS" | "FAIL" | "BLOCKED";
type AgentTestResult = { position: number; status: AgentTestStatus; note: string };

const activeRunStatuses = ["QUEUED", "PREPARING", "PLANNING", "INVESTIGATING", "IMPLEMENTING", "TESTING"];

function testResultsForRun(run: AgentRun): Map<number, AgentTestResult> {
  const raw = run.result?.test_cases;
  if (!Array.isArray(raw)) return new Map();
  const results = new Map<number, AgentTestResult>();
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const record = entry as Record<string, unknown>;
    const position = Number(record.position);
    const status = record.status;
    if (!Number.isInteger(position) || position < 1 || !["NOT_RUN", "PASS", "FAIL", "BLOCKED"].includes(String(status))) continue;
    results.set(position, { position, status: status as AgentTestStatus, note: typeof record.note === "string" ? record.note : "" });
  }
  return results;
}

function reviewNoteForRun(run: AgentRun): string {
  return typeof run.result?.test_review_note === "string" ? run.result.test_review_note : "";
}

export function AgentRunsReviewPanel({
  projectID,
  basePath,
  item,
  authorization,
  specificationVersion,
  regressionCases,
}: {
  projectID: string;
  basePath: string;
  item: WorkItem;
  authorization: AuthorizationContext | null;
  specificationVersion?: number;
  regressionCases: RegressionCase[];
}) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [provider, setProvider] = useState("local");
  const [agentName, setAgentName] = useState("Forgeflow Agent");
  const [prompt, setPrompt] = useState(item.description ?? "");
  const [selectedRunID, setSelectedRunID] = useState("");
  const [noteDrafts, setNoteDrafts] = useState<Record<string, string>>({});
  const [reviewDrafts, setReviewDrafts] = useState<Record<string, string>>({});
  const [followUpPositions, setFollowUpPositions] = useState<number[] | null>(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    setError("");
    try {
      const result = await client.listAgentRuns(projectID, item.id);
      setRuns(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      if (!silent) setLoading(false);
    }
  }, [client, item.id, projectID]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  useEffect(() => {
    if (!runs.some((run) => activeRunStatuses.includes(run.status))) return;
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, [load, runs]);

  const canExecute = authorization?.capabilities.includes("agent.execute") ?? false;
  const canApprove = authorization?.capabilities.includes("agent.approve") ?? false;
  const canRecordResults = authorization?.capabilities.includes("work_item.edit") || canApprove;
  const canCreateRun = item.status === "READY" || (item.status === "IN_PROGRESS" && Boolean(followUpPositions?.length));

  async function createRun(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!item.repository_id || !prompt.trim() || busyID || !canExecute || !canCreateRun) return;
    setBusyID("create");
    setError("");
    try {
      const input: CreateAgentRunRequest = {
        project_id: projectID,
        work_item_id: item.id,
        repository_id: item.repository_id,
        agent_provider: provider.trim(),
        agent_name: agentName.trim() || "Forgeflow Agent",
        execution_inputs: {
          prompt: prompt.trim(),
          specification_version: specificationVersion,
          test_case_positions: followUpPositions ?? undefined,
        },
        execution_policy: { mode: "approved-local-run" },
      };
      const created = await client.createAgentRun(input, projectID);
      setRuns((current) => [created, ...current]);
      setFormOpen(false);
      setFollowUpPositions(null);
      setMessage(t("work.run-created"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function runAction(run: AgentRun, action: "approve" | "start" | "resume" | "cancel") {
    if (busyID || (action === "approve" && !canApprove) || (action !== "approve" && !canExecute)) return;
    setBusyID(run.id);
    setError("");
    setMessage("");
    try {
      const updated = action === "approve" ? await client.approveAgentRun(run.id, projectID) : action === "start" ? await client.startAgentRun(run.id, projectID) : action === "resume" ? await client.resumeAgentRun(run.id, projectID) : await client.cancelAgentRun(run.id, projectID);
      setRuns((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(t(`work.run-${action}-saved`));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function recordTestResult(run: AgentRun, position: number, status: AgentTestStatus) {
    if (!canRecordResults || run.status === "CANCELLED" || busyID) return;
    const existing = testResultsForRun(run).get(position);
    const note = noteDrafts[`${run.id}:${position}`] ?? existing?.note ?? "";
    if ((status === "FAIL" || status === "BLOCKED") && !note.trim()) {
      setError(t("work.test-note-required"));
      return;
    }
    setBusyID(`test:${run.id}:${position}`);
    setError("");
    try {
      const updated = await client.recordAgentRunTestResults(run.id, { test_cases: [{ position, status, note: note.trim() }] }, projectID);
      setRuns((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(t("work.test-result-saved"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function saveReviewNote(run: AgentRun) {
    if (!canRecordResults || run.status === "CANCELLED" || busyID) return;
    const note = reviewDrafts[run.id] ?? reviewNoteForRun(run);
    if (!note.trim()) return;
    setBusyID(`review:${run.id}`);
    setError("");
    try {
      const updated = await client.recordAgentRunTestResults(run.id, { review_note: note.trim() }, projectID);
      setRuns((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(t("work.test-review-saved"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  function prepareFollowUp(run: AgentRun) {
    const results = testResultsForRun(run);
    const unresolved = regressionCases.filter((testCase, index) => results.get(testCase.position ?? index + 1)?.status !== "PASS");
    if (!unresolved.length) {
      setMessage(t("work.all-tests-passed"));
      return;
    }
    const positions = unresolved.map((testCase, index) => testCase.position ?? index + 1);
    const scope = unresolved.map((testCase, index) => `${testCase.position ?? index + 1}. ${testCase.scenario?.trim() || t("work.untitled-test")} — ${testCase.expected_result?.trim() || t("work.expected-result-missing")}`).join("\n");
    setFollowUpPositions(positions);
    setPrompt(`${t("work.follow-up-prompt")}\n\n${scope}`);
    setFormOpen(true);
    setMessage(t("work.follow-up-prepared", { count: positions.length }));
  }

  return <section className="app-v2-detail-stack" aria-labelledby="agent-runs-heading">
    <div className="app-v2-surface-card">
      <div className="app-v2-card-heading"><div><h3 id="agent-runs-heading">{t("work.agent-runs")}</h3><p>{t("work.agent-runs-description")}</p></div><button className="button button-primary" type="button" onClick={() => setFormOpen((current) => !current)} disabled={!canExecute || !item.repository_id || !canCreateRun}>{formOpen ? t("backlog.close") : t("work.new-agent-run")}</button></div>
      {!item.repository_id ? <div className="app-v2-inline-note">{t("work.agent-repository-required")} <Link href={`${basePath}/repositories`}>{t("nav.repositories")}</Link></div> : null}
      {item.status === "IN_PROGRESS" && !followUpPositions?.length ? <div className="app-v2-inline-note">{t("work.follow-up-required")}</div> : null}
      {!canExecute ? <div className="app-v2-inline-note">{t("work.agent-execute-read-only")}</div> : null}
      {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("work.agent-runs-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
      {message ? <p className="app-v2-action-status" role="status">{message}</p> : null}
      {formOpen ? <form className="app-v2-run-form" onSubmit={createRun}>
        {followUpPositions?.length ? <div className="app-v2-inline-note">{t("work.follow-up-scope", { count: followUpPositions.length })}</div> : null}
        <label className="app-v2-dialog-field"><span>{t("work.agent-provider")}</span><input value={provider} onChange={(event) => setProvider(event.target.value)} required /></label>
        <label className="app-v2-dialog-field"><span>{t("work.agent-name")}</span><input value={agentName} onChange={(event) => setAgentName(event.target.value)} required /></label>
        <label className="app-v2-dialog-field app-v2-run-prompt"><span>{t("work.agent-prompt")}</span><textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} rows={5} required /></label>
        <div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={busyID === "create" || !prompt.trim()}>{busyID === "create" ? t("work.creating-run") : t("work.create-run")}</button><button className="button button-secondary" type="button" onClick={() => setFormOpen(false)}>{t("backlog.cancel")}</button></div>
      </form> : null}
      {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("work.agent-runs-loading")}</p></div> : !runs.length ? <div className="app-v2-empty"><strong>{t("work.no-agent-runs")}</strong><p>{t("work.no-agent-runs-description")}</p></div> : <div className="app-v2-run-list">
        {runs.map((run) => {
          const active = activeRunStatuses.includes(run.status);
          const results = testResultsForRun(run);
          const passedCount = regressionCases.filter((testCase, index) => results.get(testCase.position ?? index + 1)?.status === "PASS").length;
          const scopedCases = run.execution_inputs?.test_case_positions ?? [];
          const selected = selectedRunID === run.id;
          return <article className="app-v2-run-card" key={run.id}>
            <div className="app-v2-run-card-heading"><div><span className="app-v2-key">{run.agent_name}</span><h4>{run.agent_provider}</h4></div><span className={`app-v2-run-status is-${run.status.toLowerCase()}`}>{statusLabel(run.status)}</span></div>
            <p>{run.execution_inputs?.prompt || t("work.no-agent-prompt")}</p>
            <div className="app-v2-run-meta"><span>{run.approved ? t("work.run-approved") : t("work.run-awaiting-approval")}</span>{regressionCases.length ? <span>{t("work.test-progress", { passed: passedCount, total: regressionCases.length })}</span> : null}{scopedCases.length ? <span>{t("work.follow-up-scope", { count: scopedCases.length })}</span> : null}<span>{run.created_at ? new Intl.DateTimeFormat(typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(run.created_at)) : ""}</span></div>
            <div className="app-v2-run-actions"><button className="button button-secondary" type="button" onClick={() => setSelectedRunID((current) => current === run.id ? "" : run.id)} disabled={!regressionCases.length}>{selected ? t("backlog.close") : t("work.review-tests")}</button>{!run.approved ? <button className="button button-secondary" type="button" onClick={() => void runAction(run, "approve")} disabled={!canApprove || Boolean(busyID)}>{busyID === run.id ? t("work.processing") : t("work.approve-run")}</button> : null}{run.approved && ["QUEUED", "INTERRUPTED"].includes(run.status) ? <button className="button button-primary" type="button" onClick={() => void runAction(run, run.status === "INTERRUPTED" ? "resume" : "start")} disabled={!canExecute || Boolean(busyID)}>{run.status === "INTERRUPTED" ? t("work.resume-run") : t("work.start-run")}</button> : null}{active ? <button className="button button-quiet is-danger" type="button" onClick={() => void runAction(run, "cancel")} disabled={!canExecute || Boolean(busyID)}>{t("work.cancel-run")}</button> : null}</div>
            {selected ? <div className="app-v2-test-review" aria-label={t("work.test-checklist")}>
              <div className="app-v2-test-review-heading"><div><h4>{t("work.test-checklist")}</h4><p>{t("work.test-checklist-help")}</p></div><button className="button button-secondary" type="button" onClick={() => prepareFollowUp(run)} disabled={!canExecute}>{t("work.prepare-follow-up")}</button></div>
              {regressionCases.map((testCase, index) => {
                const position = testCase.position ?? index + 1;
                const result = results.get(position);
                const status = result?.status ?? "NOT_RUN";
                const noteKey = `${run.id}:${position}`;
                const note = noteDrafts[noteKey] ?? result?.note ?? "";
                const testBusy = busyID === `test:${run.id}:${position}`;
                return <div className={`app-v2-test-case is-${status.toLowerCase()}`} key={`${run.id}-${position}`}>
                  <div className="app-v2-test-case-heading"><label><input type="checkbox" checked={status === "PASS"} onChange={() => void recordTestResult(run, position, status === "PASS" ? "NOT_RUN" : "PASS")} disabled={!canRecordResults || run.status === "CANCELLED" || Boolean(busyID)} /><strong>{position}. {testCase.scenario?.trim() || t("work.untitled-test")}</strong></label><span className="app-v2-test-status">{t(`work.test-status-${status.toLowerCase()}`)}</span></div>
                  <p>{testCase.expected_result?.trim() || t("work.expected-result-missing")}</p>
                  <textarea value={note} onChange={(event) => setNoteDrafts((current) => ({ ...current, [noteKey]: event.target.value }))} placeholder={t("work.test-note")} rows={2} disabled={!canRecordResults || run.status === "CANCELLED" || Boolean(busyID)} aria-label={`${t("work.test-note")} ${position}`} />
                  <div className="app-v2-test-case-actions"><button className="button button-quiet" type="button" onClick={() => void recordTestResult(run, position, "FAIL")} disabled={!canRecordResults || run.status === "CANCELLED" || Boolean(busyID)}>{testBusy ? t("work.saving") : t("work.test-fail")}</button><button className="button button-quiet" type="button" onClick={() => void recordTestResult(run, position, "BLOCKED")} disabled={!canRecordResults || run.status === "CANCELLED" || Boolean(busyID)}>{t("work.test-blocked")}</button></div>
                </div>;
              })}
              <label className="app-v2-dialog-field"><span>{t("work.test-review-note")}</span><textarea value={reviewDrafts[run.id] ?? reviewNoteForRun(run)} onChange={(event) => setReviewDrafts((current) => ({ ...current, [run.id]: event.target.value }))} rows={3} disabled={!canRecordResults || run.status === "CANCELLED" || Boolean(busyID)} /></label>
              <div className="app-v2-editor-actions"><span>{t("work.test-review-note-help")}</span><button className="button button-secondary" type="button" onClick={() => void saveReviewNote(run)} disabled={!canRecordResults || run.status === "CANCELLED" || Boolean(busyID) || !(reviewDrafts[run.id] ?? reviewNoteForRun(run)).trim()}>{t("work.save-test-review")}</button></div>
            </div> : null}
          </article>;
        })}
      </div>}
      {!regressionCases.length ? <div className="app-v2-inline-note">{t("work.no-regression-cases")}</div> : null}
    </div>
  </section>;
}
