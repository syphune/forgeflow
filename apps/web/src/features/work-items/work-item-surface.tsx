"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AgentRun, AuthorizationContext, Comment, CreateAgentRunRequest, Member, SpecificationResponse, WorkItem, WorkflowResponse } from "@forgeflow/api-client";
import { readinessLabel, statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { ForgeSelect } from "../app/forge-select";
import { SpecificationEditor } from "./specification-editor";
import { AgentRunsReviewPanel } from "./agent-runs-review";
import { AutonomousWorkflowPanel } from "./autonomous-workflow";
import { rolesForCapabilities, transitionPermissions } from "./workflow-responsibilities";
import { uiTone } from "../app/types";

type Props = { projectID: string; itemID: string; basePath: string; modal?: boolean };

function label(value: string | undefined) {
  return statusLabel(value ?? "");
}

const permissionKeys: Record<string, string> = {
  "work_item.create": "work.permission-create",
  "work_item.assign": "work.permission-assign",
  "work_item.transition": "work.permission-transition",
  "specification.verify": "work.permission-verify",
  "agent.execute": "work.permission-agent-execute",
  "agent.approve": "work.permission-agent-approve",
};

const ruleKeys: Record<string, string> = {
  require_specification_ready: "work.rule-specification-ready",
  require_human_verification: "work.rule-human-verification",
  require_assignee: "work.rule-assignee",
  require_repository: "work.rule-repository",
  require_pull_request: "work.rule-pull-request",
  require_ci_success: "work.rule-ci-success",
  require_permission: "work.rule-permission",
};
const activeAgentRunStatuses = ["QUEUED", "PREPARING", "PLANNING", "INVESTIGATING", "IMPLEMENTING", "TESTING"];

function memberLabel(id: string | null | undefined, members: Member[]): string {
  if (!id) return t("work.unassigned");
  const member = members.find((candidate) => candidate.id === id);
  return member ? `${member.display_name}${member.login ? ` (@${member.login})` : ""}` : id;
}

function permissionLabel(permission: string): string {
  return t(permissionKeys[permission] ?? "work.permission-unknown", { permission });
}

function transitionRoles(transition: WorkflowResponse["transitions"][number]): string {
  const permissions = transitionPermissions(transition.required_permissions);
  const roles = rolesForCapabilities(permissions);
  return roles.length ? roles.map((role) => label(role)).join(", ") : permissions.map(permissionLabel).join(", ");
}

function transitionRules(transition: WorkflowResponse["transitions"][number]): string {
  const rules = transition.required_rules ?? [];
  return rules.length ? rules.map((rule) => t(ruleKeys[rule] ?? "work.rule-unknown", { rule })).join(", ") : t("work.rule-none");
}

function LegacyAgentRunsPanel({ projectID, basePath, item, authorization, specificationVersion }: { projectID: string; basePath: string; item: WorkItem; authorization: AuthorizationContext | null; specificationVersion?: number }) {
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
    if (!runs.some((run) => activeAgentRunStatuses.includes(run.status))) return;
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, [load, runs]);

  const canExecute = authorization?.capabilities.includes("agent.execute") ?? false;
  const canApprove = authorization?.capabilities.includes("agent.approve") ?? false;

  async function createRun(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!item.repository_id || !prompt.trim() || busyID || !canExecute) return;
    setBusyID("create");
    setError("");
    try {
      const input: CreateAgentRunRequest = {
        project_id: projectID,
        work_item_id: item.id,
        repository_id: item.repository_id,
        agent_provider: provider.trim(),
        agent_name: agentName.trim() || "Forgeflow Agent",
        execution_inputs: { prompt: prompt.trim(), specification_version: specificationVersion },
        execution_policy: { mode: "approved-local-run" },
      };
      const created = await client.createAgentRun(input, projectID);
      setRuns((current) => [created, ...current]);
      setFormOpen(false);
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

  return <section className="app-v2-detail-stack" aria-labelledby="agent-runs-heading"><div className="app-v2-surface-card"><div className="app-v2-card-heading"><div><h3 id="agent-runs-heading">{t("work.agent-runs")}</h3><p>{t("work.agent-runs-description")}</p></div><button className="button button-primary" type="button" onClick={() => setFormOpen((current) => !current)} disabled={!canExecute || !item.repository_id}>{formOpen ? t("backlog.close") : t("work.new-agent-run")}</button></div>{!item.repository_id ? <div className="app-v2-inline-note">{t("work.agent-repository-required")} <Link href={`${basePath}/repositories`}>{t("nav.repositories")}</Link></div> : null}{!canExecute ? <div className="app-v2-inline-note">{t("work.agent-execute-read-only")}</div> : null}{error ? <div className="app-v2-error-panel" role="alert"><strong>{t("work.agent-runs-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{formOpen ? <form className="app-v2-run-form" onSubmit={createRun}><label className="app-v2-dialog-field"><span>{t("work.agent-provider")}</span><input value={provider} onChange={(event) => setProvider(event.target.value)} required /></label><label className="app-v2-dialog-field"><span>{t("work.agent-name")}</span><input value={agentName} onChange={(event) => setAgentName(event.target.value)} required /></label><label className="app-v2-dialog-field app-v2-run-prompt"><span>{t("work.agent-prompt")}</span><textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} rows={5} required /></label><div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={busyID === "create" || !prompt.trim()}>{busyID === "create" ? t("work.creating-run") : t("work.create-run")}</button><button className="button button-secondary" type="button" onClick={() => setFormOpen(false)}>{t("backlog.cancel")}</button></div></form> : null}{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("work.agent-runs-loading")}</p></div> : !runs.length ? <div className="app-v2-empty"><strong>{t("work.no-agent-runs")}</strong><p>{t("work.no-agent-runs-description")}</p></div> : <div className="app-v2-run-list">{runs.map((run) => { const active = ["QUEUED", "PREPARING", "PLANNING", "INVESTIGATING", "IMPLEMENTING", "TESTING"].includes(run.status); return <article className="app-v2-run-card" key={run.id}><div className="app-v2-run-card-heading"><div><span className="app-v2-key">{run.agent_name}</span><h4>{run.agent_provider}</h4></div><span className={`app-v2-run-status is-${run.status.toLowerCase()}`}>{statusLabel(run.status)}</span></div><p>{run.execution_inputs?.prompt || t("work.no-agent-prompt")}</p><div className="app-v2-run-meta"><span>{run.approved ? t("work.run-approved") : t("work.run-awaiting-approval")}</span><span>{run.created_at ? new Intl.DateTimeFormat(typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(run.created_at)) : ""}</span></div><div className="app-v2-run-actions">{!run.approved ? <button className="button button-secondary" type="button" onClick={() => void runAction(run, "approve")} disabled={!canApprove || Boolean(busyID)}>{busyID === run.id ? t("work.processing") : t("work.approve-run")}</button> : null}{run.approved && ["QUEUED", "INTERRUPTED"].includes(run.status) ? <button className="button button-primary" type="button" onClick={() => void runAction(run, run.status === "INTERRUPTED" ? "resume" : "start")} disabled={!canExecute || Boolean(busyID)}>{run.status === "INTERRUPTED" ? t("work.resume-run") : t("work.start-run")}</button> : null}{active ? <button className="button button-quiet is-danger" type="button" onClick={() => void runAction(run, "cancel")} disabled={!canExecute || Boolean(busyID)}>{t("work.cancel-run")}</button> : null}</div></article>; })}</div>}</div></section>;
}

export function WorkItemSurface({ projectID, itemID, basePath, modal = false }: Props) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const router = useRouter();
  const actionsRef = useRef<HTMLDivElement>(null);
  const [item, setItem] = useState<WorkItem | null>(null);
  const [specification, setSpecification] = useState<SpecificationResponse | null>(null);
  const [comments, setComments] = useState<Comment[]>([]);
  const [workflow, setWorkflow] = useState<WorkflowResponse | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [membersLoading, setMembersLoading] = useState(true);
  const [assigneeDraft, setAssigneeDraft] = useState("");
  const [specificationLoading, setSpecificationLoading] = useState(false);
  const [comment, setComment] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<"overview" | "definition" | "activity" | "runs">("overview");
  const [commentBusy, setCommentBusy] = useState(false);
  const [reviewBusy, setReviewBusy] = useState(false);
  const [transitionBusy, setTransitionBusy] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [assignmentBusy, setAssignmentBusy] = useState(false);
  const [actionMessage, setActionMessage] = useState("");
  const [transitionKey, setTransitionKey] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    queueMicrotask(() => {
      if (!controller.signal.aborted) {
        setLoading(true);
        setError("");
      }
    });
    void Promise.all([
      client.request<WorkItem>(`/work-items/${encodeURIComponent(itemID)}`, { projectID, signal: controller.signal }),
      client.request<{ items: Comment[] }>(`/work-items/${encodeURIComponent(itemID)}/comments`, { projectID, signal: controller.signal }),
      client.request<WorkflowResponse>("/workflows/current", { projectID, signal: controller.signal }),
    ]).then(([loaded, loadedComments, loadedWorkflow]) => {
      setItem(loaded);
      setAssigneeDraft(loaded.assignee_id ?? "");
      setComments(loadedComments.items ?? []);
      setWorkflow(loadedWorkflow);
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) setError(apiErrorMessage(cause));
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return () => controller.abort();
  }, [client, itemID, projectID]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      setMembers([]);
      setAuthorization(null);
      setMembersLoading(true);
    });
    void Promise.allSettled([
      client.listProjectMembers(projectID),
      client.getProjectAuthorization(projectID),
    ]).then(([membersResult, authorizationResult]) => {
      if (!active) return;
      if (membersResult.status === "fulfilled") setMembers(membersResult.value.items ?? []);
      if (authorizationResult.status === "fulfilled") setAuthorization(authorizationResult.value);
      setMembersLoading(false);
    });
    return () => {
      active = false;
    };
  }, [client, projectID]);

  useEffect(() => {
    if (tab !== "definition" && tab !== "runs") return;
    const controller = new AbortController();
    queueMicrotask(() => {
      if (!controller.signal.aborted) setSpecificationLoading(true);
    });
    void client.request<SpecificationResponse>(`/work-items/${encodeURIComponent(itemID)}/specification`, { projectID, signal: controller.signal }).then(setSpecification).catch((cause: unknown) => {
      if (!controller.signal.aborted) setError(apiErrorMessage(cause));
    }).finally(() => {
      if (!controller.signal.aborted) setSpecificationLoading(false);
    });
    return () => controller.abort();
  }, [client, itemID, projectID, tab]);

  useEffect(() => {
    if (!actionsOpen) return;
    function closeOnOutside(event: MouseEvent) {
      if (!actionsRef.current?.contains(event.target as Node)) setActionsOpen(false);
    }
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") setActionsOpen(false);
    }
    document.addEventListener("mousedown", closeOnOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("mousedown", closeOnOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [actionsOpen]);

  async function addComment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!comment.trim()) return;
    setCommentBusy(true);
    try {
      const created = await client.createComment(itemID, { body: comment.trim() }, projectID);
      setComments((current) => [...current, created]);
      setComment("");
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setCommentBusy(false);
    }
  }

  async function reviewSpecification() {
    const version = specification?.specification?.version;
    if (!version) return;
    setReviewBusy(true);
    setError("");
    try {
      await client.reviewSpecification(itemID, { expected_version: version }, projectID);
      setSpecification(await client.getSpecification(itemID, projectID));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setReviewBusy(false);
    }
  }

  async function transitionWorkItem() {
    if (!item || !transitionKey) return;
    setTransitionBusy(true);
    setError("");
    try {
      const updated = await client.transitionWorkItem(itemID, { transition_key: transitionKey, expected_version: item.version }, projectID);
      setItem(updated);
      setTransitionKey("");
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setTransitionBusy(false);
    }
  }

  async function saveAssignment() {
    if (!item || assignmentBusy || !authorization?.capabilities.includes("work_item.assign") || assigneeDraft === (item.assignee_id ?? "")) return;
    setAssignmentBusy(true);
    setError("");
    setActionMessage("");
    try {
      const updated = await client.assignWorkItem(itemID, { assignee_id: assigneeDraft || null, expected_version: item.version }, projectID);
      setItem(updated);
      setAssigneeDraft(updated.assignee_id ?? "");
      setActionMessage(t("work.assignment-saved"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setAssignmentBusy(false);
    }
  }

  async function copyLink() {
    setActionMessage("");
    try {
      try {
        await navigator.clipboard.writeText(window.location.href);
      } catch {
        const input = document.createElement("textarea");
        input.value = window.location.href;
        input.style.position = "fixed";
        input.style.opacity = "0";
        document.body.appendChild(input);
        input.select();
        const copied = document.execCommand("copy");
        input.remove();
        if (!copied) throw new Error(t("work.clipboard-unavailable"));
      }
      setActionMessage(t("work.link-copied"));
      setActionsOpen(false);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    }
  }

  async function archiveOrRestoreWorkItem() {
    if (!item || actionBusy) return;
    const archived = Boolean(item.archived_at);
    if (!archived && !window.confirm(t("work.archive-confirm", { key: item.key }))) return;
    setActionBusy(true);
    setError("");
    setActionMessage("");
    try {
      if (archived) {
        const restored = await client.request<WorkItem>(`/work-items/${encodeURIComponent(item.id)}/restore`, { method: "POST", projectID, body: { expected_version: item.version } });
        setItem(restored);
        setActionMessage(t("work.restored"));
        setActionsOpen(false);
      } else {
        await client.request<void>(`/work-items/${encodeURIComponent(item.id)}?expected_version=${item.version}`, { method: "DELETE", projectID });
        router.push(`${basePath}/backlog`);
      }
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setActionBusy(false);
    }
  }

  if (loading) return <div className={`app-v2-detail ${modal ? "is-modal" : ""}`} aria-busy="true"><div className="app-v2-loading"><span /><span /><span /></div></div>;
  if (error || !item) return <div className={`app-v2-detail ${modal ? "is-modal" : ""}`} role="alert"><div className="app-v2-error-panel"><strong>{t("work.load-error")}</strong><span>{error || t("work.not-found")}</span><Link className="button button-secondary" href={`${basePath}/backlog`}>{t("work.back")}</Link></div></div>;

  const availableTransitions = workflow?.transitions.filter((transition) => transition.from_status === item.status) ?? [];
  const workflowStatuses = new Map((workflow?.statuses ?? []).map((status) => [status.key, status.display_name]));
  const workflowPositions = new Map((workflow?.statuses ?? []).map((status) => [status.key, status.position]));
  const orderedStatuses = [...(workflow?.statuses ?? [])].sort((left, right) => left.position - right.position);
  const orderedTransitions = [...(workflow?.transitions ?? [])].sort((left, right) => (workflowPositions.get(left.from_status) ?? Number.MAX_SAFE_INTEGER) - (workflowPositions.get(right.from_status) ?? Number.MAX_SAFE_INTEGER) || (workflowPositions.get(left.to_status) ?? Number.MAX_SAFE_INTEGER) - (workflowPositions.get(right.to_status) ?? Number.MAX_SAFE_INTEGER) || left.key.localeCompare(right.key));
  const selectedTransition = availableTransitions.find((transition) => transition.key === transitionKey) ?? availableTransitions.find((transition) => transition.to_status !== "CANCELLED") ?? availableTransitions[0];
  const canAssign = authorization?.capabilities.includes("work_item.assign") ?? false;
  const assignmentChanged = assigneeDraft !== (item.assignee_id ?? "");
  const creatorRoles = rolesForCapabilities(["work_item.create"]).map((role) => label(role)).join(", ");
  const nextHandler = memberLabel(assigneeDraft, members);

  return <article className={`app-v2-detail ${modal ? "is-modal" : ""}`} aria-labelledby="work-item-heading">
    <header className="app-v2-detail-heading"><div><Link className="app-v2-back-link" href={`${basePath}/backlog`}>← {t("work.back")}</Link><div className="app-v2-item-meta"><span className="app-v2-key">{item.key}</span><span className={`app-v2-status-pill is-${uiTone(item.status)}`}>{label(item.status)}</span><span className={`app-v2-chip is-${uiTone(item.type)}`}>{label(item.type)}</span></div><h2 id="work-item-heading">{item.title}</h2></div><div className="app-v2-detail-actions"><div className="app-v2-action-menu" ref={actionsRef}><button className="button button-secondary" type="button" aria-haspopup="menu" aria-expanded={actionsOpen} onClick={() => setActionsOpen((open) => !open)} disabled={actionBusy}>{actionBusy ? t("work.processing") : t("work.more-actions")}</button>{actionsOpen ? <div className="app-v2-action-menu-panel" role="menu"><button className="app-v2-action-menu-item" type="button" role="menuitem" onClick={() => void copyLink()}>{t("work.copy-link")}</button><button className="app-v2-action-menu-item is-danger" type="button" role="menuitem" onClick={() => void archiveOrRestoreWorkItem()}>{item.archived_at ? t("work.restore") : t("work.archive")}</button></div> : null}</div></div></header>
    {orderedStatuses.length ? <div className="app-v2-workflow-strip" aria-label={t("work.workflow-progress")}>{orderedStatuses.map((status) => <div className={`app-v2-workflow-strip-step is-${uiTone(status.key)} ${status.key === item.status ? "is-current" : ""} ${(workflowPositions.get(status.key) ?? 0) < (workflowPositions.get(item.status) ?? 0) ? "is-complete" : ""}`} key={status.key}><span aria-hidden="true" /> <strong>{status.display_name}</strong></div>)}</div> : null}
    {actionMessage ? <p className="app-v2-action-status" role="status">{actionMessage}</p> : null}
    <nav className="app-v2-tabs" aria-label={t("work.details")}><button className={tab === "overview" ? "is-active" : ""} type="button" onClick={() => setTab("overview")}>{t("work.overview")}</button><button className={tab === "definition" ? "is-active" : ""} type="button" onClick={() => setTab("definition")}>{t("work.definition")}</button><button className={tab === "activity" ? "is-active" : ""} type="button" onClick={() => setTab("activity")}>{t("work.activity")}</button><button className={tab === "runs" ? "is-active" : ""} type="button" onClick={() => setTab("runs")}>{t("work.agent-runs")}</button></nav>
    {tab === "overview" ? <div className="app-v2-detail-grid"><section className="app-v2-detail-main"><div className="app-v2-surface-card"><h3>{t("work.context")}</h3><p className="app-v2-prose">{item.description || t("work.no-context-added")}</p></div><section className="app-v2-surface-card app-v2-responsibility-card" aria-labelledby="responsibility-heading"><div className="app-v2-card-heading"><div><h3 id="responsibility-heading">{t("work.responsibility")}</h3><p>{t("work.responsibility-description")}</p></div></div><div className="app-v2-responsibility-grid"><div><span>{t("work.creator")}</span><strong>{memberLabel(item.reporter_id, members)}</strong><small>{t("work.can-create", { roles: creatorRoles })}</small></div><div><span>{t("work.current-handler")}</span><div className="app-v2-assignee-control"><select aria-label={t("work.current-handler")} value={assigneeDraft} onChange={(event) => setAssigneeDraft(event.target.value)} disabled={!canAssign || membersLoading}><option value="">{t("work.unassigned")}</option>{members.map((member) => <option value={member.id} key={member.id}>{memberLabel(member.id, members)}</option>)}{assigneeDraft && !members.some((member) => member.id === assigneeDraft) ? <option value={assigneeDraft}>{assigneeDraft}</option> : null}</select>{canAssign && assignmentChanged ? <button className="button button-secondary" type="button" onClick={() => void saveAssignment()} disabled={assignmentBusy}>{assignmentBusy ? t("work.assignment-saving") : t("work.save-assignment")}</button> : null}</div><small>{canAssign ? t("work.assignee-help") : t("work.assignee-read-only")}</small></div></div><div className="app-v2-handoff"><span>{t("work.next-step")}</span>{selectedTransition ? <><strong>{selectedTransition.display_name} → {label(workflowStatuses.get(selectedTransition.to_status) ?? selectedTransition.to_status)}</strong><small>{t("work.next-handler", { handler: nextHandler })}</small><small>{t("work.transition-actors", { roles: transitionRoles(selectedTransition) })}</small><small>{t("work.transition-requirements", { requirements: transitionRules(selectedTransition) })}</small></> : availableTransitions.length ? <small>{t("work.choose-transition-to-see-actors")}</small> : <small>{t("work.no-next-step")}</small>}</div><details className="app-v2-responsibility-map"><summary>{t("work.show-responsibility-map")}</summary><div className="app-v2-responsibility-list">{orderedTransitions.map((transition) => <div className={transition.from_status === item.status ? "is-current" : ""} key={transition.key}><strong>{workflowStatuses.get(transition.from_status) ?? label(transition.from_status)} → {workflowStatuses.get(transition.to_status) ?? label(transition.to_status)}</strong><span>{transition.display_name}</span><small>{t("work.transition-actors", { roles: transitionRoles(transition) })}</small><small>{t("work.transition-requirements", { requirements: transitionRules(transition) })}</small></div>)}</div></details></section><div className="app-v2-surface-card"><h3>{t("work.next-action")}</h3><p>{t("work.next-action-description")}</p><div className="app-v2-transition-control"><ForgeSelect ariaLabel={`${t("work.choose-transition")} ${t("work.workflow")}`} value={transitionKey} options={[{ value: "", label: t("work.choose-transition") }, ...availableTransitions.map((transition) => ({ value: transition.key, label: transition.display_name }))]} placeholder={t("work.choose-transition")} onChange={setTransitionKey} /><button className="button button-primary" type="button" onClick={() => void transitionWorkItem()} disabled={transitionBusy || !transitionKey}>{transitionBusy ? t("work.moving") : t("work.move")}</button></div></div></section><aside className="app-v2-detail-aside"><div><span>{t("work.status")}</span><strong>{label(item.status)}</strong></div><div><span>{t("work.priority")}</span><strong>{label(item.priority)}</strong></div><div><span>{t("work.version")}</span><strong>{item.version}</strong></div><div><span>{t("work.repository")}</span><strong>{item.repository_id ? t("work.linked") : t("work.not-linked")}</strong></div></aside></div> : null}
    {tab === "definition" ? <section className="app-v2-detail-stack">{specificationLoading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("work.loading-specification")}</p></div> : specification ? <><div className="app-v2-surface-card"><div className="app-v2-card-heading"><div><h3>{t("work.specification-readiness")}</h3><p>{t("work.specification-description")}</p></div><span className={`app-v2-readiness ${specification.readiness?.ready ? "is-ready" : ""}`}>{specification.readiness?.ready ? t("work.ready") : t("work.gaps", { count: specification.readiness?.missing?.length ?? 0 })}</span></div>{specification.readiness?.missing?.length ? <ul className="app-v2-missing">{specification.readiness.missing.map((missing, index) => <li key={`${missing}-${index}`}>{readinessLabel(missing)}</li>)}</ul> : null}<h4>{item.type === "BUG" ? t("work.problem") : t("work.goal")}</h4><p className="app-v2-prose">{specification.specification?.summary || specification.specification?.fields?.PROBLEM_STATEMENT?.value || specification.specification?.fields?.GOAL?.value || t("work.no-definition")}</p>{specification.readiness?.missing?.includes("HUMAN_REVIEW") ? <button className="button button-primary" type="button" onClick={() => void reviewSpecification()} disabled={reviewBusy}>{reviewBusy ? t("work.reviewing") : t("work.review-specification", { version: specification.specification?.version ?? "" })}</button> : null}</div><SpecificationEditor projectID={projectID} item={item} response={specification} onSaved={setSpecification} onError={setError} /></> : <div className="app-v2-error-panel" role="alert"><strong>{t("work.specification-load-error")}</strong><span>{error || t("work.no-specification-returned")}</span></div>}</section> : null}
    {tab === "activity" ? <section className="app-v2-detail-stack"><div className="app-v2-surface-card"><h3>{t("work.comments")}</h3><div className="app-v2-comments">{comments.length ? comments.map((entry) => <div className="app-v2-comment" key={entry.id}><p>{entry.body}</p><small>{new Intl.DateTimeFormat(typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(entry.created_at))}</small></div>) : <p>{t("work.no-comments")}</p>}</div><form className="app-v2-comment-form" onSubmit={addComment}><textarea value={comment} onChange={(event) => setComment(event.target.value)} rows={3} placeholder={t("work.comment-placeholder")} aria-label={t("work.comments")} /><button className="button button-secondary" type="submit" disabled={commentBusy || !comment.trim()}>{commentBusy ? t("work.saving") : t("work.comment")}</button></form></div></section> : null}
    {tab === "runs" ? <><AutonomousWorkflowPanel projectID={projectID} item={item} authorization={authorization} /><AgentRunsReviewPanel projectID={projectID} basePath={basePath} item={item} authorization={authorization} specificationVersion={specification?.specification?.version} regressionCases={specification?.specification?.regression_test_cases ?? []} /></> : null}
  </article>;
}
