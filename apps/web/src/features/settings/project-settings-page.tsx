"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { AuthorizationContext, AutomationRule, AutonomousPolicy, CreateEnvironmentRequest, CustomFieldDefinition, Environment, Member, WorkflowResponse } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { uiTone } from "../app/types";

type Props = { projectID: string; basePath: string; section: string };
type WorkflowStatus = WorkflowResponse["statuses"][number];
type WorkflowTransition = WorkflowResponse["transitions"][number];
type FieldType = CustomFieldDefinition["value_type"];

const sections = ["workflow", "autonomous", "people", "fields", "automation"] as const;
const roles = ["project_manager", "developer", "qa", "viewer"];
const ruleOptions = [
  "require_specification_ready",
  "require_human_verification",
  "require_assignee",
  "require_repository",
  "require_pull_request",
  "require_ci_success",
  "require_permission",
];
const automationEvents = [
  "work_item.created",
  "work_item.updated",
  "work_item.assigned",
  "work_item.transitioned",
  "work_item.comment.created",
  "repository.linked",
  "repository.unlinked",
  "github.push",
  "github.pull_request.updated",
  "github.ci.updated",
];

function sectionTitle(section: string) {
  return section === "workflow" ? t("settings.workflow") : section === "autonomous" ? t("settings.autonomous") : section === "people" ? t("settings.people") : section === "fields" ? t("settings.fields") : t("settings.automation");
}

function SectionFrame({ projectID, basePath, section, children }: Props & { children: React.ReactNode }) {
  return <section className="app-v2-page" aria-labelledby="settings-heading"><div className="app-v2-page-heading"><div><p className="eyebrow">{t("settings.project-eyebrow")}</p><h2 id="settings-heading">{sectionTitle(section)}</h2><p>{t(`settings.${section}-description`)}</p></div><Link className="button button-secondary" href={`${basePath}/backlog`}>{t("settings.back-to-work")}</Link></div><nav className="app-v2-settings-tabs" aria-label={t("settings.project-navigation")}>{sections.map((entry) => <Link className={entry === section ? "is-active" : ""} href={`${basePath}/settings/${entry}`} key={entry}>{sectionTitle(entry)}</Link>)}</nav>{children}</section>;
}

function ErrorNotice({ error, retry }: { error: string; retry?: () => void }) {
  return error ? <div className="app-v2-error-panel" role="alert"><strong>{t("settings.load-error")}</strong><span>{error}</span>{retry ? <button type="button" onClick={retry}>{t("app.retry")}</button> : null}</div> : null;
}

function ReadOnlyNotice() {
  return <div className="app-v2-inline-note" role="status">{t("settings.read-only")}</div>;
}

function AutonomousSettings({ projectID, canManage }: { projectID: string; canManage: boolean }) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [policy, setPolicy] = useState<AutonomousPolicy | null>(null);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [environment, setEnvironment] = useState<CreateEnvironmentRequest>({ key: "staging", name: "Staging", kind: "staging", auto_deploy: false, require_approval: true });

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [loadedPolicy, loadedEnvironments] = await Promise.all([client.getProjectAIPolicy(projectID), client.listProjectEnvironments(projectID)]);
      setPolicy(loadedPolicy);
      setEnvironments(loadedEnvironments.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client, projectID]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  function updatePolicy(patch: Partial<AutonomousPolicy>) {
    setPolicy((current) => current ? { ...current, ...patch } : current);
  }

  async function savePolicy(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManage || !policy || busy) return;
    setBusy(true);
    setError("");
    try {
      setPolicy(await client.updateProjectAIPolicy(projectID, policy));
      setMessage(t("settings.autonomous-saved"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function createEnvironment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManage || busy || !environment.key?.trim() || !environment.name?.trim()) return;
    setBusy(true);
    setError("");
    try {
      const created = await client.createProjectEnvironment(projectID, { ...environment, key: environment.key.trim().toLowerCase(), name: environment.name.trim() });
      setEnvironments((current) => [...current, created]);
      setMessage(t("settings.environment-created"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return <div className="app-v2-settings-stack"><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{!canManage ? <ReadOnlyNotice /> : null}{loading || !policy ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : <form className="app-v2-settings-stack" onSubmit={savePolicy}><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.autonomous")}</h3><p>{t("settings.autonomous-help")}</p></div></div><div className="app-v2-form-grid"><label className="app-v2-checkbox"><input type="checkbox" checked={policy.enabled} onChange={(event) => updatePolicy({ enabled: event.target.checked })} disabled={!canManage} />{t("settings.autonomous-enabled")}</label><label className="app-v2-dialog-field"><span>{t("settings.autonomous-runtime")}</span><select value={policy.runtime} onChange={(event) => updatePolicy({ runtime: event.target.value as AutonomousPolicy["runtime"] })} disabled={!canManage}><option value="server">Server runner</option><option value="desktop">Desktop runner</option><option value="auto">Automatic</option></select></label><label className="app-v2-dialog-field"><span>{t("settings.autonomous-provider")}</span><select multiple value={policy.providers} onChange={(event) => updatePolicy({ providers: Array.from(event.target.selectedOptions, (option) => option.value as "codex" | "claude") })} disabled={!canManage}><option value="codex">Codex</option><option value="claude">Claude</option></select></label><label className="app-v2-dialog-field"><span>{t("settings.autonomous-max-attempts")}</span><input type="number" min={1} max={10} value={policy.max_attempts} onChange={(event) => updatePolicy({ max_attempts: Number(event.target.value) })} disabled={!canManage} /></label><label className="app-v2-dialog-field"><span>{t("settings.autonomous-timeout")}</span><input type="number" min={60} max={86400} value={policy.timeout_seconds} onChange={(event) => setPolicy((current) => current ? { ...current, timeout_seconds: Number(event.target.value) } : current)} disabled={!canManage} /></label><label className="app-v2-dialog-field"><span>{t("settings.autonomous-test-scope")}</span><select value={policy.test_scope} onChange={(event) => updatePolicy({ test_scope: event.target.value as AutonomousPolicy["test_scope"] })} disabled={!canManage}><option value="unresolved_only">Unresolved only</option><option value="full_regression">Full regression</option></select></label><label className="app-v2-checkbox"><input type="checkbox" checked={policy.auto_retry} onChange={(event) => updatePolicy({ auto_retry: event.target.checked })} disabled={!canManage} />{t("settings.autonomous-auto-retry")}</label><label className="app-v2-checkbox"><input type="checkbox" checked={policy.auto_create_pr} onChange={(event) => updatePolicy({ auto_create_pr: event.target.checked })} disabled={!canManage} />{t("settings.autonomous-auto-pr")}</label></div><div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={!canManage || busy}>{busy ? t("settings.saving") : t("settings.save-autonomous")}</button></div></div></form>}<div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.environments")}</h3><p>{t("settings.environments-help")}</p></div></div>{canManage ? <form className="app-v2-settings-create-grid" onSubmit={createEnvironment}><label className="app-v2-dialog-field"><span>{t("settings.environment-key")}</span><input value={environment.key} onChange={(event) => setEnvironment((current) => ({ ...current, key: event.target.value }))} maxLength={64} required /></label><label className="app-v2-dialog-field"><span>{t("settings.environment-name")}</span><input value={environment.name} onChange={(event) => setEnvironment((current) => ({ ...current, key: current.key, name: event.target.value }))} maxLength={120} required /></label><label className="app-v2-dialog-field"><span>{t("settings.environment-kind")}</span><select value={environment.kind} onChange={(event) => setEnvironment((current) => ({ ...current, kind: event.target.value as CreateEnvironmentRequest["kind"] }))}><option value="preview">Preview</option><option value="development">Development</option><option value="staging">Staging</option><option value="production">Production</option></select></label><button className="button button-primary" type="submit" disabled={busy}>{t("settings.add-environment")}</button></form> : null}{environments.length ? <div className="app-v2-rule-list">{environments.map((entry) => <div className="app-v2-rule-row" key={entry.id}><div><strong>{entry.name}</strong><small>{entry.key} · {entry.kind}</small></div><div className="app-v2-member-actions"><span className={`app-v2-chip is-${entry.require_approval ? "warning" : "success"}`}>{entry.require_approval ? t("settings.environment-approval") : t("settings.environment-auto")}</span></div></div>)}</div> : <div className="app-v2-empty"><strong>{t("settings.no-environments")}</strong><p>{t("settings.no-environments-description")}</p></div>}</div></div>;
}

function WorkflowSettings({ projectID, canManage }: { projectID: string; canManage: boolean }) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [workflow, setWorkflow] = useState<WorkflowResponse | null>(null);
  const [statuses, setStatuses] = useState<WorkflowStatus[]>([]);
  const [transitions, setTransitions] = useState<WorkflowTransition[]>([]);
  const [editing, setEditing] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const loaded = await client.getWorkflow(projectID);
      setWorkflow(loaded);
      setStatuses(loaded.statuses.map((status) => ({ ...status })));
      setTransitions(loaded.transitions.map((transition) => ({ ...transition, required_rules: [...(transition.required_rules ?? [])], required_permissions: [...(transition.required_permissions ?? [])] })));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client, projectID]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  function reset() {
    if (!workflow) return;
    setStatuses(workflow.statuses.map((status) => ({ ...status })));
    setTransitions(workflow.transitions.map((transition) => ({ ...transition, required_rules: [...(transition.required_rules ?? [])], required_permissions: [...(transition.required_permissions ?? [])] })));
    setEditing(false);
    setError("");
  }

  function updateStatus(key: string, patch: Partial<WorkflowStatus>) {
    setStatuses((current) => current.map((status) => status.key === key ? { ...status, ...patch } : status));
  }

  function removeStatus(key: string) {
    if (key === "RAW") return;
    setStatuses((current) => current.filter((status) => status.key !== key));
    setTransitions((current) => current.filter((transition) => transition.from_status !== key && transition.to_status !== key));
  }

  function addStatus() {
    const existing = new Set(statuses.map((status) => status.key));
    let index = statuses.length + 1;
    while (existing.has(`STAGE_${index}`)) index += 1;
    setStatuses((current) => [...current, { key: `STAGE_${index}`, display_name: t("settings.new-status"), category: "TODO", position: Math.max(0, ...current.map((status) => status.position)) + 10, is_terminal: false }]);
  }

  function updateTransition(key: string, patch: Partial<WorkflowTransition> | { required_rules: string[] }) {
    setTransitions((current) => current.map((transition) => transition.key === key ? { ...transition, ...patch } as WorkflowTransition : transition));
  }

  function addTransition() {
    if (statuses.length < 2) return;
    const from = statuses[0].key;
    const to = statuses[1].key;
    const key = `move_${from.toLowerCase()}_${to.toLowerCase()}_${transitions.length + 1}`;
    setTransitions((current) => [...current, { key, from_status: from, to_status: to, display_name: t("settings.new-transition"), required_rules: [], required_permissions: [] }]);
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!workflow || busy || !canManage) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const updated = await client.request<WorkflowResponse>("/workflows/current", { method: "PUT", projectID, body: { name: workflow.name, statuses, transitions } });
      setWorkflow(updated);
      setStatuses(updated.statuses.map((status) => ({ ...status })));
      setTransitions(updated.transitions.map((transition) => ({ ...transition, required_rules: [...(transition.required_rules ?? [])], required_permissions: [...(transition.required_permissions ?? [])] })));
      setEditing(false);
      setMessage(t("settings.workflow-saved"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  if (loading) return <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("settings.loading")}</p></div>;
  if (!workflow) return <ErrorNotice error={error || t("settings.no-workflow")} retry={() => void load()} />;

  return <div className="app-v2-settings-stack"><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{!canManage ? <ReadOnlyNotice /> : null}<div className="app-v2-surface-card app-v2-settings-summary"><div><span className="app-v2-eyebrow-small">{t("settings.workflow-name")}</span><h3>{workflow.name}</h3><p>{t("settings.workflow-help")}</p></div><div className="app-v2-settings-summary-counts"><strong>{statuses.length}<small>{t("settings.statuses")}</small></strong><strong>{transitions.length}<small>{t("settings.transitions")}</small></strong></div></div>{!editing ? <><div className="app-v2-workflow-stepper">{statuses.slice().sort((left, right) => left.position - right.position).map((status) => <div className={`app-v2-workflow-step is-${uiTone(status.category)}`} key={status.key}><span>{status.key}</span><strong>{status.display_name}</strong><small>{statusLabel(status.category)}</small></div>)}</div><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.transitions")}</h3><p>{t("settings.transition-help")}</p></div><button className="button button-primary" type="button" onClick={() => setEditing(true)} disabled={!canManage}>{t("settings.edit-workflow")}</button></div>{transitions.length ? <div className="app-v2-transition-list">{transitions.map((transition) => <div className="app-v2-transition-row" key={transition.key}><strong>{transition.display_name}</strong><span>{transition.from_status} → {transition.to_status}</span><small>{transition.required_rules?.length ? transition.required_rules.join(", ") : t("settings.no-rules")}</small></div>)}</div> : <div className="app-v2-empty"><strong>{t("settings.no-transitions")}</strong><p>{t("settings.no-transitions-description")}</p></div>}</div></> : <form className="app-v2-settings-stack" onSubmit={save}><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.statuses")}</h3><p>{t("settings.status-help")}</p></div><button className="button button-secondary" type="button" onClick={addStatus} disabled={busy || statuses.length >= 50}>{t("settings.add-status")}</button></div><div className="app-v2-settings-table">{statuses.map((status) => <div className="app-v2-settings-table-row" key={status.key}><input aria-label={`${t("settings.status-key")} ${status.display_name}`} value={status.key} readOnly={status.key === "RAW"} onChange={(event) => updateStatus(status.key, { key: event.target.value.toUpperCase() })} /><input aria-label={`${t("settings.status-name")} ${status.display_name}`} value={status.display_name} onChange={(event) => updateStatus(status.key, { display_name: event.target.value })} /><select aria-label={`${t("settings.status-category")} ${status.display_name}`} value={status.category} onChange={(event) => updateStatus(status.key, { category: event.target.value as WorkflowStatus["category"] })}><option value="TODO">{statusLabel("TODO")}</option><option value="IN_PROGRESS">{statusLabel("IN_PROGRESS")}</option><option value="DONE">{statusLabel("DONE")}</option><option value="CANCELLED">{statusLabel("CANCELLED")}</option></select><input aria-label={`${t("settings.status-position")} ${status.display_name}`} type="number" value={status.position} onChange={(event) => updateStatus(status.key, { position: Number(event.target.value) })} /><label className="app-v2-checkbox"><input type="checkbox" checked={status.is_terminal} onChange={(event) => updateStatus(status.key, { is_terminal: event.target.checked })} />{t("settings.terminal")}</label><button className="button button-quiet is-danger" type="button" onClick={() => removeStatus(status.key)} disabled={busy || status.key === "RAW"}>{t("settings.remove")}</button></div>)}</div></div><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.transitions")}</h3><p>{t("settings.transition-help")}</p></div><button className="button button-secondary" type="button" onClick={addTransition} disabled={busy || statuses.length < 2}>{t("settings.add-transition")}</button></div><div className="app-v2-settings-stack">{transitions.map((transition) => <div className="app-v2-transition-editor" key={transition.key}><div className="app-v2-form-grid"><label className="app-v2-dialog-field"><span>{t("settings.transition-name")}</span><input value={transition.display_name} onChange={(event) => updateTransition(transition.key, { display_name: event.target.value })} /></label><label className="app-v2-dialog-field"><span>{t("settings.transition-key")}</span><input value={transition.key} readOnly /></label><label className="app-v2-dialog-field"><span>{t("settings.from-status")}</span><select value={transition.from_status} onChange={(event) => updateTransition(transition.key, { from_status: event.target.value })}>{statuses.map((status) => <option value={status.key} key={status.key}>{status.display_name}</option>)}</select></label><label className="app-v2-dialog-field"><span>{t("settings.to-status")}</span><select value={transition.to_status} onChange={(event) => updateTransition(transition.key, { to_status: event.target.value })}>{statuses.map((status) => <option value={status.key} key={status.key}>{status.display_name}</option>)}</select></label><label className="app-v2-dialog-field"><span>{t("settings.rules")}</span><select multiple value={transition.required_rules ?? []} onChange={(event) => updateTransition(transition.key, { required_rules: Array.from(event.target.selectedOptions, (option) => option.value) })}>{ruleOptions.map((rule) => <option value={rule} key={rule}>{rule}</option>)}</select></label><label className="app-v2-dialog-field"><span>{t("settings.permissions")}</span><input value={(transition.required_permissions ?? []).join(", ")} onChange={(event) => updateTransition(transition.key, { required_permissions: event.target.value.split(",").map((value) => value.trim()).filter(Boolean) })} placeholder="work_item.transition" /></label></div><button className="button button-quiet is-danger" type="button" onClick={() => setTransitions((current) => current.filter((entry) => entry.key !== transition.key))} disabled={busy}>{t("settings.remove")}</button></div>)}</div></div><div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={busy}>{busy ? t("settings.saving") : t("settings.save-workflow")}</button><button className="button button-secondary" type="button" onClick={reset} disabled={busy}>{t("backlog.cancel")}</button></div></form>}</div>;
}

function PeopleSettings({ projectID, canManage }: { projectID: string; canManage: boolean }) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await client.listProjectMembers(projectID);
      setMembers(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client, projectID]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  async function update(member: Member, role: string) {
    if (!canManage || busyID) return;
    setBusyID(member.id);
    setError("");
    try {
      const updated = await client.request<Member>(`/projects/${encodeURIComponent(projectID)}/members/${encodeURIComponent(member.id)}`, { method: "PUT", projectID, body: { user_id: member.id, role_key: role } });
      setMembers((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(t("settings.member-saved"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function remove(member: Member) {
    if (!canManage || busyID || !window.confirm(t("settings.remove-member-confirm", { name: member.display_name || member.login }))) return;
    setBusyID(member.id);
    setError("");
    try {
      await client.request<void>(`/projects/${encodeURIComponent(projectID)}/members/${encodeURIComponent(member.id)}`, { method: "DELETE", projectID });
      setMembers((current) => current.map((entry) => entry.id === member.id ? { ...entry, project_role: false } : entry));
      setMessage(t("settings.member-removed"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  return <div className="app-v2-settings-stack"><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{!canManage ? <ReadOnlyNotice /> : null}<div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.people")}</h3><p>{t("settings.people-help")}</p></div><span className="app-v2-chip is-success">{members.filter((member) => member.project_role).length} {t("settings.active-members")}</span></div>{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : members.length ? <div className="app-v2-member-list">{members.map((member) => <div className="app-v2-member-row" key={member.id}><div><strong>{member.display_name || member.login}</strong><small>{member.login} · {member.project_role ? t("settings.project-member") : t("settings.organization-member")}</small></div><div className="app-v2-member-actions"><select aria-label={`${t("settings.role")} ${member.display_name || member.login}`} value={member.project_role ? member.role_key : ""} onChange={(event) => { if (event.target.value) void update(member, event.target.value); }} disabled={!canManage || busyID === member.id}><option value="">{t("settings.not-in-project")}</option>{roles.map((role) => <option value={role} key={role}>{statusLabel(role)}</option>)}</select>{member.project_role ? <button className="button button-quiet is-danger" type="button" onClick={() => void remove(member)} disabled={!canManage || busyID === member.id}>{busyID === member.id ? t("settings.saving") : t("settings.remove")}</button> : null}</div></div>)}</div> : <div className="app-v2-empty"><strong>{t("settings.no-members")}</strong><p>{t("settings.no-members-description")}</p></div>}</div></div>;
}

function FieldsSettings({ projectID, canManage }: { projectID: string; canManage: boolean }) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [fields, setFields] = useState<CustomFieldDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [type, setType] = useState<FieldType>("TEXT");
  const [options, setOptions] = useState("");
  const [editingID, setEditingID] = useState("");
  const [editName, setEditName] = useState("");
  const [editOptions, setEditOptions] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await client.listCustomFields(projectID);
      setFields(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client, projectID]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManage || busy || !name.trim() || !key.trim()) return;
    setBusy(true);
    setError("");
    try {
      const created = await client.createCustomField({ key: key.trim().toUpperCase(), display_name: name.trim(), value_type: type, options: type === "SELECT" ? options.split(",").map((value) => value.trim()).filter(Boolean) : [], required: false }, projectID);
      setFields((current) => [...current, created]);
      setName(""); setKey(""); setOptions("");
      setMessage(t("settings.field-created"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function remove(field: CustomFieldDefinition) {
    if (!canManage || busy || !window.confirm(t("settings.delete-field-confirm", { name: field.display_name }))) return;
    setBusy(true);
    setError("");
    try {
      await client.deleteCustomField(field.id, projectID);
      setFields((current) => current.filter((entry) => entry.id !== field.id));
      setMessage(t("settings.field-deleted"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function saveEdit(field: CustomFieldDefinition) {
    if (!canManage || busy || !editName.trim()) return;
    setBusy(true);
    setError("");
    try {
      const updated = await client.updateCustomField(field.id, { display_name: editName.trim(), options: field.value_type === "SELECT" ? editOptions.split(",").map((value) => value.trim()).filter(Boolean) : undefined }, projectID);
      setFields((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setEditingID("");
      setMessage(t("settings.field-updated"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return <div className="app-v2-settings-stack"><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{!canManage ? <ReadOnlyNotice /> : null}<div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.fields")}</h3><p>{t("settings.fields-help")}</p></div></div>{canManage ? <form className="app-v2-settings-create-grid" onSubmit={create}><label className="app-v2-dialog-field"><span>{t("settings.field-key")}</span><input value={key} onChange={(event) => setKey(event.target.value.toUpperCase())} maxLength={32} required placeholder="RISK" /></label><label className="app-v2-dialog-field"><span>{t("settings.field-name")}</span><input value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required placeholder={t("settings.field-name-placeholder")} /></label><label className="app-v2-dialog-field"><span>{t("settings.field-type")}</span><select value={type} onChange={(event) => setType(event.target.value as FieldType)}><option value="TEXT">Text</option><option value="NUMBER">Number</option><option value="BOOLEAN">Boolean</option><option value="DATE">Date</option><option value="SELECT">Select</option></select></label>{type === "SELECT" ? <label className="app-v2-dialog-field"><span>{t("settings.field-options")}</span><input value={options} onChange={(event) => setOptions(event.target.value)} placeholder="low, medium, high" /></label> : null}<button className="button button-primary" type="submit" disabled={busy || !name.trim() || !key.trim()}>{busy ? t("settings.saving") : t("settings.add-field")}</button></form> : null}</div><div className="app-v2-surface-card app-v2-settings-card">{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : fields.length ? <div className="app-v2-field-list">{fields.map((field) => editingID === field.id ? <div className="app-v2-field-row" key={field.id}><div className="app-v2-field-edit"><input value={editName} onChange={(event) => setEditName(event.target.value)} /><small>{field.key} · {field.value_type}</small>{field.value_type === "SELECT" ? <input value={editOptions} onChange={(event) => setEditOptions(event.target.value)} placeholder="low, medium, high" /> : null}</div><div className="app-v2-member-actions"><button className="button button-secondary" type="button" onClick={() => void saveEdit(field)} disabled={busy}>{t("settings.save")}</button><button className="button button-quiet" type="button" onClick={() => setEditingID("")} disabled={busy}>{t("backlog.cancel")}</button></div></div> : <div className="app-v2-field-row" key={field.id}><div><strong>{field.display_name}</strong><small>{field.key} · {field.value_type}{field.required ? ` · ${t("settings.required")}` : ""}</small></div><div className="app-v2-member-actions"><button className="button button-quiet" type="button" onClick={() => { setEditingID(field.id); setEditName(field.display_name); setEditOptions(field.options?.join(", ") ?? ""); }} disabled={!canManage || busy}>{t("settings.edit")}</button><button className="button button-quiet is-danger" type="button" onClick={() => void remove(field)} disabled={!canManage || busy}>{t("settings.delete")}</button></div></div>)}</div> : <div className="app-v2-empty"><strong>{t("settings.no-fields")}</strong><p>{t("settings.no-fields-description")}</p></div>}</div></div>;
}

function AutomationSettings({ projectID, canManage }: { projectID: string; canManage: boolean }) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [rules, setRules] = useState<AutomationRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [name, setName] = useState("");
  const [eventType, setEventType] = useState(automationEvents[0]);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await client.listAutomationRules(projectID);
      setRules(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client, projectID]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManage || busyID || !name.trim()) return;
    setBusyID("create");
    setError("");
    try {
      const created = await client.createAutomationRule({ name: name.trim(), event_type: eventType, action_type: "notify", config: { title: "{event_type}", body: "{event_type} received for {aggregate_type} {aggregate_id}." } }, projectID);
      setRules((current) => [created, ...current]);
      setName("");
      setMessage(t("settings.rule-created"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function toggle(rule: AutomationRule) {
    if (!canManage || busyID) return;
    setBusyID(rule.id);
    setError("");
    try {
      const updated = await client.toggleAutomationRule(rule.id, !rule.enabled, projectID);
      setRules((current) => current.map((entry) => entry.id === updated.id ? updated : entry));
      setMessage(t("settings.rule-updated"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function remove(rule: AutomationRule) {
    if (!canManage || busyID || !window.confirm(t("settings.delete-rule-confirm", { name: rule.name }))) return;
    setBusyID(rule.id);
    setError("");
    try {
      await client.deleteAutomationRule(rule.id, projectID);
      setRules((current) => current.filter((entry) => entry.id !== rule.id));
      setMessage(t("settings.rule-deleted"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  return <div className="app-v2-settings-stack"><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{!canManage ? <ReadOnlyNotice /> : null}<div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.automation")}</h3><p>{t("settings.automation-help")}</p></div></div>{canManage ? <form className="app-v2-settings-create-grid" onSubmit={create}><label className="app-v2-dialog-field"><span>{t("settings.rule-name")}</span><input value={name} onChange={(event) => setName(event.target.value)} maxLength={160} required placeholder={t("settings.rule-name-placeholder")} /></label><label className="app-v2-dialog-field"><span>{t("settings.event")}</span><select value={eventType} onChange={(event) => setEventType(event.target.value)}>{automationEvents.map((event) => <option value={event} key={event}>{event}</option>)}</select></label><button className="button button-primary" type="submit" disabled={busyID === "create" || !name.trim()}>{busyID === "create" ? t("settings.saving") : t("settings.add-rule")}</button></form> : null}</div><div className="app-v2-surface-card app-v2-settings-card">{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : rules.length ? <div className="app-v2-rule-list">{rules.map((rule) => <div className="app-v2-rule-row" key={rule.id}><div><strong>{rule.name}</strong><small>{rule.event_type} · {rule.action_type}</small></div><div className="app-v2-member-actions"><button className={`button button-quiet ${rule.enabled ? "" : "is-disabled"}`} type="button" onClick={() => void toggle(rule)} disabled={!canManage || busyID === rule.id}>{rule.enabled ? t("settings.enabled") : t("settings.disabled")}</button><button className="button button-quiet is-danger" type="button" onClick={() => void remove(rule)} disabled={!canManage || Boolean(busyID)}>{t("settings.delete")}</button></div></div>)}</div> : <div className="app-v2-empty"><strong>{t("settings.no-automation-rules")}</strong><p>{t("settings.no-rules-description")}</p></div>}</div></div>;
}

export function ProjectSettingsPage({ projectID, basePath, section }: Props) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [authLoading, setAuthLoading] = useState(true);
  useEffect(() => {
    let active = true;
    void client.getProjectAuthorization(projectID).then((loaded) => { if (active) setAuthorization(loaded); }).catch(() => { if (active) setAuthorization(null); }).finally(() => { if (active) setAuthLoading(false); });
    return () => { active = false; };
  }, [client, projectID]);
  const canManage = !authLoading && (authorization?.capabilities.includes("project.manage") ?? false);
  const content = section === "autonomous" ? <AutonomousSettings projectID={projectID} canManage={canManage} /> : section === "people" ? <PeopleSettings projectID={projectID} canManage={canManage} /> : section === "fields" ? <FieldsSettings projectID={projectID} canManage={canManage} /> : section === "automation" ? <AutomationSettings projectID={projectID} canManage={canManage} /> : <WorkflowSettings projectID={projectID} canManage={canManage} />;
  return <SectionFrame projectID={projectID} basePath={basePath} section={sections.includes(section as typeof sections[number]) ? section : "workflow"}>{authLoading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : content}</SectionFrame>;
}
