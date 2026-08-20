"use client";

import { ChangeEvent, FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AcceptanceCriterion, Attachment, GitHubRepository, RepositoryContext, RepositorySnapshot, RepositoryTreeEntry, ReproductionStep, SnapshotSymbol, Specification, SpecificationResponse, VerificationRequest, WorkItem } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { optimizeAttachment } from "../../app/attachment-optimization";
import { ForgeSelect } from "../app/forge-select";
import type { SelectOption } from "../app/forge-select";
import { uiTone } from "../app/types";

type Props = {
  projectID: string;
  item: WorkItem;
  response: SpecificationResponse | null;
  onSaved: (response: SpecificationResponse) => void;
  onError: (message: string) => void;
};

type DraftStep = { action: string; expected_result: string; observed_result: string; evidence_refs: string };
type DraftCriterion = { statement: string };
type DraftRegression = { scenario: string; expected_result: string };
type DraftContextRef = { module: string; file: string; symbol: string; commit: string; pull_request: string; rationale: string };
type RepositoryContextData = { repositoryID: string; context: RepositoryContext; tree: RepositoryTreeEntry[]; snapshot: RepositorySnapshot | null; symbols: SnapshotSymbol[] };
const manualOptionValue = "__manual__";
const repositoryCacheTTL = 5 * 60 * 1000;
const repositoryListCache = new Map<string, { expiresAt: number; items: GitHubRepository[] }>();
const repositoryContextCache = new Map<string, { expiresAt: number; data: RepositoryContextData }>();
const attachmentAccept = "image/*,application/pdf,video/*,text/plain,application/json,text/csv,.log,.har";

function formatAttachmentSize(size: number) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

const bugFields = [
  ["PROBLEM_STATEMENT", "spec.problem-statement", "spec.problem-statement-help"],
  ["EXPECTED_BEHAVIOR", "spec.expected-behavior", "spec.expected-behavior-help"],
  ["ACTUAL_BEHAVIOR", "spec.actual-behavior", "spec.actual-behavior-help"],
  ["ENVIRONMENT", "spec.environment", "spec.environment-help"],
  ["PRECONDITIONS", "spec.preconditions", "spec.preconditions-help"],
  ["FREQUENCY", "spec.frequency", "spec.frequency-help"],
  ["AFFECTED_VERSION", "spec.affected-version", "spec.affected-version-help"],
] as const;

const taskFields = [
  ["GOAL", "spec.goal", "spec.goal-help"],
  ["PRECONDITIONS", "spec.constraints", "spec.constraints-help"],
  ["BUSINESS_IMPACT", "spec.business-impact", "spec.business-impact-help"],
  ["NO_CODE_CHANGE_RATIONALE", "spec.no-code-rationale", "spec.no-code-rationale-help"],
] as const;

function draftStep(step?: ReproductionStep): DraftStep {
  return { action: step?.action ?? "", expected_result: step?.expected_result ?? "", observed_result: step?.observed_result ?? "", evidence_refs: step?.evidence_refs?.join("\n") ?? "" };
}

function draftCriterion(criterion?: AcceptanceCriterion): DraftCriterion {
  return { statement: criterion?.statement ?? "" };
}

function draftRegression(testCase?: { scenario?: string; expected_result?: string }): DraftRegression {
  return { scenario: testCase?.scenario ?? "", expected_result: testCase?.expected_result ?? "" };
}

function draftContextRef(ref?: NonNullable<Specification["context_refs"]>[number]): DraftContextRef {
  return { module: ref?.module ?? "", file: ref?.file ?? "", symbol: ref?.symbol ?? "", commit: ref?.commit ?? "", pull_request: ref?.pull_request ?? "", rationale: ref?.rationale ?? "" };
}

function verificationLabel(status?: string) {
  return status === "HUMAN_VERIFIED" ? t("spec.human-verified") : t("spec.needs-verification");
}

function githubPathPart(value: string) {
  return encodeURIComponent(value);
}

function contextFileURL(repository: GitHubRepository, contextRef: DraftContextRef, symbol?: SnapshotSymbol) {
  if (!contextRef.file || !repository.full_name) return "";
  const revision = contextRef.commit || repository.default_branch;
  const line = symbol?.start_line ? `#L${symbol.start_line}` : "";
  return `https://github.com/${repository.full_name}/blob/${githubPathPart(revision)}/${contextRef.file.split("/").map(githubPathPart).join("/")}${line}`;
}

function FieldLabel({ label, help, status, statusTone }: { label: string; help?: string; status?: string; statusTone?: "success" | "warning" }) {
  return <span className="app-v2-field-label"><span>{label}</span>{help ? <span className="app-v2-tooltip" tabIndex={0} title={help} data-tooltip={help} aria-label={help}>?</span> : null}{status ? <small className={statusTone ? `app-v2-field-status is-${statusTone}` : ""}> · {status}</small> : null}</span>;
}

function FieldMediaPicker({ target, attachments, selectedIDs, busy, onChange, onUpload, onDownload }: { target: string; attachments: Attachment[]; selectedIDs: string[]; busy: boolean; onChange: (target: string, ids: string[]) => void; onUpload: (files: File[], target: string) => void; onDownload?: (attachment: Attachment) => void }) {
  const attachmentByID = new Map(attachments.map((attachment) => [attachment.id, attachment]));
  return <div className="app-v2-field-media app-v2-field-media-compact">
    <div className="app-v2-field-media-toolbar">
      <span>{t("spec.add-evidence")}</span>
      {attachments.length ? <select className="app-v2-attachment-reference-select" aria-label={t("spec.choose-media")} value="" onChange={(event) => { const id = event.target.value; if (id && !selectedIDs.includes(id)) onChange(target, [...selectedIDs, id]); }} disabled={busy}>
        <option value="">{t("spec.choose-media")}</option>
        {attachments.map((attachment) => <option value={attachment.id} key={attachment.id} disabled={selectedIDs.includes(attachment.id)}>{attachment.name}</option>)}
      </select> : null}
      <label className="button button-quiet app-v2-field-media-upload">{t("spec.add-media")}<input type="file" accept={attachmentAccept} multiple hidden onChange={(event) => { const files = Array.from(event.currentTarget.files ?? []); event.currentTarget.value = ""; if (files.length) onUpload(files, target); }} disabled={busy} /></label>
    </div>
    {selectedIDs.length ? <div className="app-v2-field-media-list">{selectedIDs.map((id) => { const attachment = attachmentByID.get(id); const name = attachment?.name ?? id; return <span className="app-v2-media-chip" key={id} title={id}><button className="app-v2-media-chip-name" type="button" onClick={() => { if (attachment && onDownload) onDownload(attachment); }} disabled={!attachment || !onDownload}>{name}</button><button type="button" aria-label={`${t("spec.remove-media")}: ${name}`} onClick={() => onChange(target, selectedIDs.filter((selectedID) => selectedID !== id))} disabled={busy}>×</button></span>; })}</div> : null}
  </div>;
}

function ContextSelect({ field, label, help, value, options, placeholder, onChange }: { field: keyof DraftContextRef; label: string; help: string; value: string; options: SelectOption[]; placeholder: string; onChange: (field: keyof DraftContextRef, value: string) => void }) {
  const [manual, setManual] = useState(false);
  const knownValue = options.some((option) => option.value === value);
  const selectOptions = [...(value && !knownValue ? [{ value, label: `${value} (${t("spec.saved-value")})` }] : []), ...options, { value: manualOptionValue, label: t("spec.manual-entry") }];
  return <div className="app-v2-editor-field"><FieldLabel label={label} help={help} /><ForgeSelect ariaLabel={label} value={manual ? manualOptionValue : value} options={selectOptions} placeholder={placeholder} searchable={field === "module" || field === "file" || field === "symbol"} onChange={(nextValue) => {
    if (nextValue === manualOptionValue) {
      setManual(true);
      return;
    }
    setManual(false);
    onChange(field, nextValue);
  }} />{manual ? <input aria-label={`${label} - ${t("spec.manual-value")}`} value={value} onChange={(event) => onChange(field, event.target.value)} placeholder={t("spec.enter-value", { label: label.toLowerCase() })} /> : null}</div>;
}

function ContextReferenceEditor({ contextRef, repository, data, modules, mediaTarget, mediaAttachments, mediaIDs, mediaBusy, onMediaChange, onMediaUpload, onMediaDownload, onChange, onRemove }: { contextRef: DraftContextRef; repository?: GitHubRepository; data: RepositoryContextData | null; modules: string[]; mediaTarget: string; mediaAttachments: Attachment[]; mediaIDs: string[]; mediaBusy: boolean; onMediaChange: (target: string, ids: string[]) => void; onMediaUpload: (files: File[], target: string) => void; onMediaDownload: (attachment: Attachment) => void; onChange: (field: keyof DraftContextRef, value: string) => void; onRemove: () => void }) {
  const files = data?.tree.filter((entry) => entry.type === "blob" || entry.type === "file") ?? [];
  const visibleFiles = files.filter((entry) => !contextRef.module || entry.path === contextRef.module || entry.path.startsWith(`${contextRef.module}/`));
  const symbols = data?.symbols.filter((symbol) => !contextRef.file || symbol.path === contextRef.file) ?? [];
  const selectedSymbol = symbols.find((symbol) => (symbol.qualified_name || symbol.name) === contextRef.symbol);
  const contextCommits = data?.context.commits ?? [];
  const commits = data?.snapshot && !contextCommits.some((commit) => commit.sha === data.snapshot?.commit_sha) ? [...contextCommits, { sha: data.snapshot.commit_sha, message: t("spec.indexed-commit") }] : contextCommits;
  const selectedCommit = commits.find((commit) => commit.sha === contextRef.commit);
  const pullRequests = data?.context.pull_requests ?? [];
  const selectedPullRequest = pullRequests.find((pullRequest) => pullRequest.url === contextRef.pull_request || (pullRequest.number !== undefined && (contextRef.pull_request === `#${pullRequest.number}` || contextRef.pull_request === String(pullRequest.number))));
  const pullRequestURL = selectedPullRequest?.url || (/^https?:\/\//.test(contextRef.pull_request) ? contextRef.pull_request : "");
  const fileURL = repository ? contextFileURL(repository, contextRef, selectedSymbol) : "";
  return <div className="app-v2-editor-repeater">
    <ContextSelect field="module" label={t("spec.module")} help={t("spec.module-help")} value={contextRef.module} options={modules.map((module) => ({ value: module, label: module }))} placeholder={`${t("spec.module")}…`} onChange={onChange} />
    <ContextSelect field="file" label={t("spec.file")} help={t("spec.file-help")} value={contextRef.file} options={visibleFiles.map((entry) => ({ value: entry.path, label: entry.path }))} placeholder={files.length ? `${t("spec.file")}…` : data ? t("spec.no-files") : t("spec.repository-not-loaded")} onChange={onChange} />
    <ContextSelect field="symbol" label={t("spec.symbol")} help={t("spec.symbol-help")} value={contextRef.symbol} options={symbols.map((symbol) => ({ value: symbol.qualified_name || symbol.name, label: `${symbol.qualified_name || symbol.name} · ${symbol.kind}` }))} placeholder={symbols.length ? `${t("spec.symbol")}…` : data ? data.snapshot ? t("spec.no-indexed-symbols") : t("spec.repository-index-not-ready") : t("spec.repository-not-loaded")} onChange={onChange} />
    <ContextSelect field="commit" label={t("spec.commit")} help={t("spec.commit-help")} value={contextRef.commit} options={commits.map((commit) => ({ value: commit.sha ?? "", label: `${(commit.sha ?? "").slice(0, 8)} · ${(commit.message ?? "").split("\n")[0]}` }))} placeholder={commits.length ? `${t("spec.commit")}…` : data ? t("spec.no-synced-commits") : t("spec.repository-not-loaded")} onChange={onChange} />
    <ContextSelect field="pull_request" label={t("spec.pull-request")} help={t("spec.pull-request-help")} value={contextRef.pull_request} options={pullRequests.map((pullRequest) => ({ value: pullRequest.url || (pullRequest.number !== undefined ? `#${pullRequest.number}` : ""), label: `${pullRequest.number !== undefined ? `#${pullRequest.number} ` : ""}${pullRequest.title ?? t("spec.pull-request")}` }))} placeholder={pullRequests.length ? `${t("spec.pull-request")}…` : data ? t("spec.no-synced-pull-requests") : t("spec.repository-not-loaded")} onChange={onChange} />
    <div className="app-v2-editor-field"><FieldLabel label={t("spec.rationale")} help={t("spec.rationale-help")} /><textarea aria-label={t("spec.rationale")} value={contextRef.rationale} onChange={(event) => onChange("rationale", event.target.value)} rows={2} placeholder={t("spec.affected-help")} /><FieldMediaPicker target={mediaTarget} attachments={mediaAttachments} selectedIDs={mediaIDs} busy={mediaBusy} onChange={onMediaChange} onUpload={onMediaUpload} onDownload={onMediaDownload} /></div>
    {fileURL || pullRequestURL || (selectedCommit && repository) ? <div className="app-v2-context-links">{fileURL ? <a href={fileURL} target="_blank" rel="noreferrer">{t("spec.open-file")}</a> : null}{selectedCommit && repository ? <a href={`https://github.com/${repository.full_name}/commit/${encodeURIComponent(selectedCommit.sha ?? contextRef.commit)}`} target="_blank" rel="noreferrer">{t("spec.open-commit")}</a> : null}{pullRequestURL ? <a href={pullRequestURL} target="_blank" rel="noreferrer">{t("spec.open-pull-request")}</a> : null}</div> : null}
    <button className="button button-quiet is-danger" type="button" onClick={onRemove}>{t("spec.delete")}</button>
  </div>;
}

export function SpecificationEditor({ projectID, item, response, onSaved, onError }: Props) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const specification = response?.specification ?? null;
  const [repositories, setRepositories] = useState<GitHubRepository[]>([]);
  const [summary, setSummary] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [mediaRefs, setMediaRefs] = useState<Record<string, string[]>>({});
  const [steps, setSteps] = useState<DraftStep[]>([]);
  const [criteria, setCriteria] = useState<DraftCriterion[]>([]);
  const [regressionCases, setRegressionCases] = useState<DraftRegression[]>([]);
  const [contextRefs, setContextRefs] = useState<DraftContextRef[]>([]);
  const [repositoryID, setRepositoryID] = useState("");
  const [saving, setSaving] = useState(false);
  const [verifying, setVerifying] = useState("");
  const [dirty, setDirty] = useState(false);
  const [editorError, setEditorError] = useState("");
  const [editorMessage, setEditorMessage] = useState("");
  const [repositoryContextData, setRepositoryContextData] = useState<RepositoryContextData | null>(null);
  const [repositoryContextLoading, setRepositoryContextLoading] = useState(false);
  const [repositoryContextError, setRepositoryContextError] = useState("");
  const [repositoryIndexing, setRepositoryIndexing] = useState(false);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [attachmentLoading, setAttachmentLoading] = useState(false);
  const [attachmentBusy, setAttachmentBusy] = useState(false);
  const [attachmentError, setAttachmentError] = useState("");
  const [attachmentMessage, setAttachmentMessage] = useState("");
  const repositoryContextLoadingRef = useRef(false);
  const repositoryIndexingRef = useRef(false);
  const loadedSpecificationKey = useRef("");

  const fieldGroups = item.type === "BUG" ? [
    { title: "spec.problem-details", help: "spec.problem-details-help", fields: bugFields.slice(0, 3) },
    { title: "spec.problem-context", help: "spec.problem-context-help", fields: bugFields.slice(3) },
  ] : [{ title: "spec.task-details", help: "spec.task-details-help", fields: taskFields }];
  const selectedRepository = repositories.find((repository) => repository.id === repositoryID);
  const repositoryData = repositoryContextData?.repositoryID === repositoryID ? repositoryContextData : null;
  const repositoryFiles = useMemo(() => repositoryData?.tree.filter((entry) => entry.type === "blob" || entry.type === "file") ?? [], [repositoryData]);
  const moduleOptions = useMemo(() => {
    const modules = new Set<string>();
    for (const entry of repositoryFiles) {
      const parts = entry.path.split("/");
      for (let index = 1; index < parts.length; index += 1) modules.add(parts.slice(0, index).join("/"));
    }
    return [...modules].sort((left, right) => left.localeCompare(right));
  }, [repositoryFiles]);

  useEffect(() => {
    if (!specification) return;
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      const nextFields: Record<string, string> = {};
      for (const [key, field] of Object.entries(specification.fields ?? {})) nextFields[key] = field.value ?? "";
      setSummary(specification.summary ?? "");
      setFields(nextFields);
      setMediaRefs(Object.fromEntries(Object.entries(specification.media_refs ?? {}).map(([target, ids]) => [target, [...(ids ?? [])]])));
      setSteps((specification.reproduction_steps ?? []).map(draftStep));
      setCriteria((specification.acceptance_criteria ?? []).map(draftCriterion));
      setRegressionCases((specification.regression_test_cases ?? []).map(draftRegression));
      setContextRefs((specification.context_refs ?? []).map(draftContextRef));
      setRepositoryID(specification.repository_id ?? item.repository_id ?? "");
      setDirty(false);
      setEditorError("");
      const nextKey = `${item.id}:${specification.id}`;
      if (loadedSpecificationKey.current !== nextKey) {
        setEditorMessage("");
        loadedSpecificationKey.current = nextKey;
      }
    });
    return () => { active = false; };
  }, [item.id, item.repository_id, specification]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      setAttachmentLoading(true);
      setAttachmentError("");
    });
    void client.listWorkItemAttachments(item.id, projectID).then((result) => {
      if (active) setAttachments(result.items ?? []);
    }).catch((cause: unknown) => {
      if (active) setAttachmentError(apiErrorMessage(cause));
    }).finally(() => {
      if (active) setAttachmentLoading(false);
    });
    return () => { active = false; };
  }, [client, item.id, projectID]);

  useEffect(() => {
    const controller = new AbortController();
    const cached = repositoryListCache.get(projectID);
    if (cached && cached.expiresAt > Date.now()) {
      queueMicrotask(() => setRepositories(cached.items));
      return () => controller.abort();
    }
    void client.request<{ items: GitHubRepository[] }>(`/projects/${encodeURIComponent(projectID)}/repositories`, { projectID, signal: controller.signal }).then((result) => {
      if (!controller.signal.aborted) {
        const items = result.items ?? [];
        repositoryListCache.set(projectID, { items, expiresAt: Date.now() + repositoryCacheTTL });
        setRepositories(items);
      }
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) setEditorError(apiErrorMessage(cause));
    });
    return () => controller.abort();
  }, [client, projectID]);

  const loadRepositoryContext = useCallback(async (force = false) => {
    if (!repositoryID || repositoryContextLoadingRef.current) return;
    const requestedRepositoryID = repositoryID;
    const cacheKey = `${projectID}:${requestedRepositoryID}`;
    const cached = repositoryContextCache.get(cacheKey);
    if (!force && cached && cached.expiresAt > Date.now()) {
      setRepositoryContextData(cached.data);
      setRepositoryContextError("");
      return;
    }
    repositoryContextLoadingRef.current = true;
    setRepositoryContextLoading(true);
    setRepositoryContextError("");
    try {
      const [context, tree, snapshots] = await Promise.all([
        client.getRepositoryContext(projectID, requestedRepositoryID),
        client.listRepositoryTree(projectID, requestedRepositoryID),
        client.listRepositorySnapshots(projectID, requestedRepositoryID),
      ]);
      const snapshot = snapshots.items.find((entry) => entry.status === "READY") ?? null;
      const symbols = snapshot ? (await client.listRepositorySnapshotSymbols(projectID, requestedRepositoryID, snapshot.id, { limit: 100 })).items : [];
      const data = { repositoryID: requestedRepositoryID, context, tree: tree.items ?? [], snapshot, symbols: symbols ?? [] };
      repositoryContextCache.set(cacheKey, { data, expiresAt: Date.now() + repositoryCacheTTL });
      setRepositoryContextData(data);
    } catch (cause) {
      setRepositoryContextError(apiErrorMessage(cause));
    } finally {
      repositoryContextLoadingRef.current = false;
      setRepositoryContextLoading(false);
    }
  }, [client, projectID, repositoryID]);

  const indexRepository = useCallback(async () => {
    if (!repositoryID || repositoryContextLoadingRef.current || repositoryIndexingRef.current) return;
    repositoryIndexingRef.current = true;
    setRepositoryIndexing(true);
    setRepositoryContextError("");
    try {
      await client.refreshRepositorySnapshot(projectID, repositoryID);
      repositoryContextCache.delete(`${projectID}:${repositoryID}`);
      repositoryContextLoadingRef.current = false;
      await loadRepositoryContext(true);
    } catch (cause) {
      setRepositoryContextError(apiErrorMessage(cause));
    } finally {
      repositoryIndexingRef.current = false;
      setRepositoryIndexing(false);
    }
  }, [client, loadRepositoryContext, projectID, repositoryID]);

  useEffect(() => {
    if (!repositoryID || repositoryContextData?.repositoryID === repositoryID) return;
    queueMicrotask(() => void loadRepositoryContext());
  }, [loadRepositoryContext, repositoryContextData?.repositoryID, repositoryID]);

  function markDirty() {
    setDirty(true);
    setEditorMessage("");
  }

  function updateField(key: string, value: string) {
    setFields((current) => ({ ...current, [key]: value }));
    markDirty();
  }

  function updateMediaRefs(target: string, ids: string[]) {
    setMediaRefs((current) => ({ ...current, [target]: ids }));
    markDirty();
  }

  function updateContextRef(index: number, field: keyof DraftContextRef, value: string) {
    setContextRefs((current) => current.map((entry, position) => position === index ? { ...entry, [field]: value } : entry));
    markDirty();
  }

  function addEvidenceReference(stepIndex: number, attachmentID: string) {
    const reference = attachmentID.trim();
    if (!reference) return;
    setSteps((current) => current.map((step, index) => {
      if (index !== stepIndex) return step;
      const references = step.evidence_refs.split("\n").map((value) => value.trim()).filter(Boolean);
      return references.includes(reference) ? step : { ...step, evidence_refs: [...references, reference].join("\n") };
    }));
    markDirty();
  }

  async function uploadFiles(files: File[], target = "") {
    if (!files.length || attachmentBusy) return;
    setAttachmentBusy(true);
    setAttachmentError("");
    setAttachmentMessage("");
    const errors: string[] = [];
    const createdIDs: string[] = [];
    const optimizationMessages: string[] = [];
    try {
      for (const file of files) {
        try {
          const prepared = await optimizeAttachment(file);
          const created = await client.uploadWorkItemAttachment(item.id, prepared.file, projectID);
          setAttachments((current) => [created, ...current]);
          createdIDs.push(created.id);
          if (prepared.optimized) optimizationMessages.push(`${file.name}: ${formatAttachmentSize(prepared.originalSize)} → ${formatAttachmentSize(prepared.file.size)}`);
        } catch (cause) {
          errors.push(`${file.name}: ${apiErrorMessage(cause)}`);
        }
      }
      if (target && createdIDs.length) {
        setMediaRefs((current) => ({ ...current, [target]: [...new Set([...(current[target] ?? []), ...createdIDs])] }));
        markDirty();
      }
      if (errors.length) setAttachmentError(errors.join(" · "));
      if (optimizationMessages.length) setAttachmentMessage(t("spec.optimized-attachment", { details: optimizationMessages.join(" · ") }));
    } finally {
      setAttachmentBusy(false);
    }
  }

  async function uploadAttachments(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.currentTarget.files ?? []);
    event.currentTarget.value = "";
    await uploadFiles(files);
  }

  async function downloadAttachment(attachment: Attachment) {
    setAttachmentError("");
    try {
      const blob = await client.downloadWorkItemAttachment(item.id, attachment.id, projectID);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = attachment.name;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (cause) {
      setAttachmentError(apiErrorMessage(cause));
    }
  }

  async function deleteAttachment(attachment: Attachment) {
    if (attachmentBusy || !window.confirm(t("spec.delete-attachment-confirm", { name: attachment.name }))) return;
    setAttachmentBusy(true);
    setAttachmentError("");
    try {
      await client.deleteWorkItemAttachment(item.id, attachment.id, projectID);
      setAttachments((current) => current.filter((entry) => entry.id !== attachment.id));
      setMediaRefs((current) => Object.fromEntries(Object.entries(current).map(([target, ids]) => [target, ids.filter((id) => id !== attachment.id)])));
      markDirty();
    } catch (cause) {
      setAttachmentError(apiErrorMessage(cause));
    } finally {
      setAttachmentBusy(false);
    }
  }

  async function save(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!specification || saving) return;
    setSaving(true);
    setEditorError("");
    setEditorMessage("");
    try {
      const nextFields = Object.fromEntries(Object.entries(fields).map(([key, value]) => [key, value.trim()]));
      const nextSteps = steps.filter((step) => step.action.trim() || step.expected_result.trim() || step.observed_result.trim()).map((step) => ({
        action: step.action.trim(),
        expected_result: step.expected_result.trim(),
        observed_result: step.observed_result.trim(),
        evidence_refs: step.evidence_refs.split("\n").map((reference) => reference.trim()).filter(Boolean),
      }));
      const nextCriteria: AcceptanceCriterion[] = criteria.filter((criterion) => criterion.statement.trim()).map((criterion) => ({ statement: criterion.statement.trim() }));
      const nextRegressionCases = regressionCases.filter((testCase) => testCase.scenario.trim() || testCase.expected_result.trim()).map((testCase, index) => ({ position: index + 1, scenario: testCase.scenario.trim(), expected_result: testCase.expected_result.trim() }));
      const nextContextRefs = contextRefs.filter((ref) => ref.module.trim() || ref.file.trim() || ref.symbol.trim() || ref.commit.trim() || ref.pull_request.trim() || ref.rationale.trim()).map((ref) => ({ repository_id: repositoryID || undefined, module: ref.module.trim(), file: ref.file.trim(), symbol: ref.symbol.trim(), commit: ref.commit.trim(), pull_request: ref.pull_request.trim(), rationale: ref.rationale.trim() }));
      await client.updateSpecification(item.id, { expected_version: specification.version, summary: summary.trim(), fields: nextFields, reproduction_steps: nextSteps, acceptance_criteria: nextCriteria, regression_test_cases: nextRegressionCases, context_refs: nextContextRefs, media_refs: mediaRefs, repository_id: repositoryID }, projectID);
      const refreshed = await client.getSpecification(item.id, projectID);
      onSaved(refreshed);
      setEditorMessage(t("spec.saved-version", { version: refreshed.specification?.version ?? specification.version }));
    } catch (cause) {
      setEditorError(apiErrorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function verify(input: VerificationRequest, key: string) {
    if (dirty) {
      setEditorError(t("spec.save-before-verify"));
      return;
    }
    setVerifying(key);
    setEditorError("");
    try {
      await client.verifySpecification(item.id, input, projectID);
      const refreshed = await client.getSpecification(item.id, projectID);
      onSaved(refreshed);
      setEditorMessage(t("spec.verification-saved"));
    } catch (cause) {
      setEditorError(apiErrorMessage(cause));
    } finally {
      setVerifying("");
    }
  }

  if (!specification) return <div className="app-v2-empty"><strong>{t("spec.no-specification")}</strong><p>{t("spec.reload-before-edit")}</p></div>;
  const renderSpecificationField = ([key, title, help]: readonly [string, string, string]) => {
    const field = specification.fields?.[key];
    const target = `field:${key}`;
    return <div className="app-v2-editor-field" key={key}><FieldLabel label={t(title)} help={t(help)} status={field ? verificationLabel(field.verification_status) : ""} statusTone={field?.verification_status === "HUMAN_VERIFIED" ? "success" : "warning"} /><textarea aria-label={t(title)} value={fields[key] ?? ""} onChange={(event) => updateField(key, event.target.value)} rows={3} /><FieldMediaPicker target={target} attachments={attachments} selectedIDs={mediaRefs[target] ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, uploadTarget) => void uploadFiles(files, uploadTarget)} onDownload={(attachment) => void downloadAttachment(attachment)} />{field?.value?.trim() && field.verification_status !== "HUMAN_VERIFIED" ? <button className="button button-quiet" type="button" onClick={() => void verify({ kind: "field", field: key }, `field:${key}`)} disabled={Boolean(verifying) || dirty}>{verifying === `field:${key}` ? t("spec.verifying") : t("spec.verify")}</button> : null}</div>;
  };

  return <form className="app-v2-definition-editor" onSubmit={(event) => void save(event)}>
    <div className="app-v2-editor-heading"><div><h3>{t("spec.editor")}</h3><p>{t("spec.editor-description")}</p></div><span className="app-v2-chip is-info">{t("spec.version", { version: specification.version })}</span></div>
    {editorError ? <div className="app-v2-error-panel" role="alert"><span>{editorError}</span></div> : null}
    {editorMessage ? <p className="app-v2-action-status" role="status">{editorMessage}</p> : null}
    <section className="app-v2-editor-section app-v2-regression-editor"><div className="app-v2-editor-section-heading"><div><h4>{t("spec.regression-cases")}</h4><p>{t("spec.regression-cases-help")}</p></div><button className="button button-secondary" type="button" onClick={() => { setRegressionCases((current) => [...current, draftRegression()]); markDirty(); }}>{t("spec.add-regression-case")}</button></div>{regressionCases.map((testCase, index) => <div className="app-v2-editor-repeater app-v2-regression-row" key={`regression-${index}`}><strong>{t("spec.test-case", { index: index + 1 })}</strong><label className="app-v2-editor-field"><span>{t("spec.test-scenario")}</span><textarea value={testCase.scenario} onChange={(event) => { setRegressionCases((current) => current.map((entry, position) => position === index ? { ...entry, scenario: event.target.value } : entry)); markDirty(); }} rows={2} /></label><label className="app-v2-editor-field"><span>{t("spec.test-expected-result")}</span><textarea value={testCase.expected_result} onChange={(event) => { setRegressionCases((current) => current.map((entry, position) => position === index ? { ...entry, expected_result: event.target.value } : entry)); markDirty(); }} rows={2} /></label><div className="app-v2-repeater-actions">{specification.regression_test_cases?.[index]?.verification_status !== "HUMAN_VERIFIED" ? <button className="button button-quiet" type="button" onClick={() => void verify({ kind: "regression_case", position: index + 1 }, `regression:${index + 1}`)} disabled={Boolean(verifying) || dirty}>{verifying === `regression:${index + 1}` ? t("spec.verifying") : t("spec.verify")}</button> : <span className="app-v2-verified-label">{t("spec.human-verified")}</span>}<button className="button button-quiet is-danger" type="button" onClick={() => { setRegressionCases((current) => current.filter((_, position) => position !== index)); markDirty(); }}>{t("spec.remove")}</button></div></div>)}</section>
    <div className="app-v2-editor-field"><FieldLabel label={item.type === "BUG" ? t("spec.short-problem") : t("spec.summary")} help={t("spec.summary-help")} /><textarea aria-label={item.type === "BUG" ? t("spec.short-problem") : t("spec.summary")} value={summary} onChange={(event) => { setSummary(event.target.value); markDirty(); }} rows={3} placeholder={t("spec.summary-placeholder")} /><FieldMediaPicker target="summary" attachments={attachments} selectedIDs={mediaRefs.summary ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, target) => void uploadFiles(files, target)} onDownload={(attachment) => void downloadAttachment(attachment)} /></div>
    <div className="app-v2-editor-field-groups">{fieldGroups.map((group) => <section className="app-v2-editor-field-group" key={group.title}><div className="app-v2-editor-section-heading"><div><h4>{t(group.title)}</h4><p>{t(group.help)}</p></div></div><div className="app-v2-editor-field-columns"><div>{group.fields.filter((_, index) => index % 2 === 0).map(renderSpecificationField)}</div><div>{group.fields.filter((_, index) => index % 2 === 1).map(renderSpecificationField)}</div></div></section>)}</div>
    <div className="app-v2-editor-field"><FieldLabel label={t("spec.repository-context")} help={t("spec.repository-context-help")} /><ForgeSelect ariaLabel={t("spec.repository-context")} value={repositoryID} options={repositories.filter((repository) => repository.linked).map((repository) => ({ value: repository.id, label: repository.full_name }))} placeholder={t("spec.no-repository")} searchable onChange={(nextRepositoryID) => { setRepositoryID(nextRepositoryID); setRepositoryContextData(null); setRepositoryContextError(""); markDirty(); }} /><small>{t("spec.repository-help")}</small></div>
    <div className="app-v2-context-loader" aria-busy={repositoryContextLoading || repositoryIndexing}><div><strong>{selectedRepository ? selectedRepository.full_name : t("spec.repository-files")}</strong><small>{repositoryData ? `${repositoryFiles.length} ${t("spec.file").toLowerCase()} · ${repositoryData.symbols.length} ${t("spec.symbol").toLowerCase()}${repositoryData.snapshot ? ` · snapshot ${repositoryData.snapshot.commit_sha.slice(0, 8)}` : ` · ${t("spec.repository-index-not-ready")}`}` : repositoryID ? t("spec.repository-context") : `${t("spec.repository-help")} → ${t("nav.repositories")}.`}</small></div><button className="button button-secondary" type="button" onClick={() => void (repositoryData && !repositoryData.snapshot ? indexRepository() : loadRepositoryContext(true))} disabled={!repositoryID || repositoryContextLoading || repositoryIndexing}>{repositoryIndexing ? t("spec.indexing-repository") : repositoryContextLoading ? t("spec.loading-repository") : repositoryData && !repositoryData.snapshot ? t("spec.index-repository") : repositoryData ? t("spec.reload-repository") : t("spec.load-repository")}</button></div>
    {repositoryContextError ? <p className="app-v2-error-panel" role="alert">{repositoryContextError} {t("spec.affected-help")}</p> : null}
    <section className="app-v2-editor-section app-v2-attachments-section"><div className="app-v2-editor-section-heading"><div><h4>{t("spec.multimedia-evidence")}</h4><p>{t("spec.multimedia-help")}</p></div><label className="button button-secondary app-v2-attachment-upload">{attachmentBusy ? t("spec.uploading-attachment") : t("spec.upload-attachment")}<input type="file" accept={attachmentAccept} multiple hidden onChange={(event) => void uploadAttachments(event)} disabled={attachmentBusy} /></label></div><p className="app-v2-attachment-limit">{t("spec.attachment-limit")}</p>{attachmentError ? <p className="app-v2-error-panel" role="alert">{attachmentError}</p> : null}{attachmentMessage ? <p className="app-v2-action-status" role="status">{attachmentMessage}</p> : null}{attachmentLoading ? <p className="app-v2-attachment-status" role="status">{t("spec.loading-attachments")}</p> : attachments.length ? <div className="app-v2-attachment-list">{attachments.map((attachment) => <div className="app-v2-attachment-row" key={attachment.id}><button className="button button-quiet app-v2-attachment-name" type="button" onClick={() => void downloadAttachment(attachment)}>{attachment.name}</button><small>{formatAttachmentSize(attachment.size_bytes)}</small><button className="button button-quiet is-danger" type="button" disabled={attachmentBusy} onClick={() => void deleteAttachment(attachment)}>{t("spec.delete")}</button></div>)}</div> : <p className="app-v2-attachment-status">{t("spec.no-attachments")}</p>}</section>
    {item.type === "BUG" ? <section className="app-v2-editor-section"><div className="app-v2-editor-section-heading"><div><h4>{t("spec.reproduction-steps")}</h4><p>{t("spec.reproduction-help")}</p></div><button className="button button-secondary" type="button" onClick={() => { setSteps((current) => [...current, draftStep()]); markDirty(); }}>{t("spec.add-step")}</button></div>{steps.map((step, index) => <div className="app-v2-editor-repeater" key={`step-${index}`}><div className="app-v2-repeater-heading"><strong>{t("spec.step", { index: index + 1 })}</strong>{step.action || step.expected_result || step.observed_result ? <span className={`app-v2-chip is-${uiTone(specification.reproduction_steps?.[index]?.verification_status)}`}>{verificationLabel(specification.reproduction_steps?.[index]?.verification_status)}</span> : null}</div><div className="app-v2-editor-field"><FieldLabel label={t("spec.action")} help={t("spec.action-help")} /><textarea aria-label={t("spec.action")} value={step.action} onChange={(event) => { setSteps((current) => current.map((entry, position) => position === index ? { ...entry, action: event.target.value } : entry)); markDirty(); }} rows={2} /><FieldMediaPicker target={`step:${index + 1}:action`} attachments={attachments} selectedIDs={mediaRefs[`step:${index + 1}:action`] ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, target) => void uploadFiles(files, target)} /></div><div className="app-v2-editor-field"><FieldLabel label={t("spec.expected-result")} help={t("spec.expected-result-help")} /><textarea aria-label={t("spec.expected-result")} value={step.expected_result} onChange={(event) => { setSteps((current) => current.map((entry, position) => position === index ? { ...entry, expected_result: event.target.value } : entry)); markDirty(); }} rows={2} /><FieldMediaPicker target={`step:${index + 1}:expected_result`} attachments={attachments} selectedIDs={mediaRefs[`step:${index + 1}:expected_result`] ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, target) => void uploadFiles(files, target)} /></div><div className="app-v2-editor-field"><FieldLabel label={t("spec.observed-result")} help={t("spec.observed-result-help")} /><textarea aria-label={t("spec.observed-result")} value={step.observed_result} onChange={(event) => { setSteps((current) => current.map((entry, position) => position === index ? { ...entry, observed_result: event.target.value } : entry)); markDirty(); }} rows={2} /><FieldMediaPicker target={`step:${index + 1}:observed_result`} attachments={attachments} selectedIDs={mediaRefs[`step:${index + 1}:observed_result`] ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, target) => void uploadFiles(files, target)} /></div><div className="app-v2-editor-field"><FieldLabel label={t("spec.evidence")} help={t("spec.evidence-help")} /><textarea aria-label={t("spec.evidence")} value={step.evidence_refs} onChange={(event) => { setSteps((current) => current.map((entry, position) => position === index ? { ...entry, evidence_refs: event.target.value } : entry)); markDirty(); }} rows={2} /><FieldMediaPicker target={`step:${index + 1}:evidence`} attachments={attachments} selectedIDs={mediaRefs[`step:${index + 1}:evidence`] ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, target) => void uploadFiles(files, target)} />{attachments.length ? <select className="app-v2-attachment-reference-select" aria-label={t("spec.attach-to-step")} value="" onChange={(event) => addEvidenceReference(index, event.target.value)}><option value="">{t("spec.attach-to-step")}</option>{attachments.map((attachment) => <option value={attachment.id} key={attachment.id}>{attachment.name}</option>)}</select> : null}</div><div className="app-v2-repeater-actions">{specification.reproduction_steps?.[index]?.verification_status !== "HUMAN_VERIFIED" ? <button className="button button-quiet" type="button" onClick={() => void verify({ kind: "reproduction_step", position: index + 1 }, `step:${index + 1}`)} disabled={Boolean(verifying) || dirty}>{verifying === `step:${index + 1}` ? t("spec.verifying") : t("spec.verify")}</button> : <span className="app-v2-verified-label">{t("spec.human-verified")}</span>}<button className="button button-quiet is-danger" type="button" onClick={() => { setSteps((current) => current.filter((_, position) => position !== index)); markDirty(); }}>{t("spec.remove")}</button></div></div>)}</section> : null}
    <section className="app-v2-editor-section"><div className="app-v2-editor-section-heading"><div><h4>{t("spec.acceptance")}</h4><p>{t("spec.acceptance-help")}</p></div><button className="button button-secondary" type="button" onClick={() => { setCriteria((current) => [...current, draftCriterion()]); markDirty(); }}>{t("spec.add-criterion")}</button></div>{criteria.map((criterion, index) => { const target = `acceptance:${index + 1}`; return <div className="app-v2-editor-repeater app-v2-editor-repeater-row" key={`criterion-${index}`}><div className="app-v2-editor-field"><FieldLabel label={t("spec.criterion", { index: index + 1 })} help={t("spec.criterion-help")} /><textarea aria-label={t("spec.criterion", { index: index + 1 })} value={criterion.statement} onChange={(event) => { setCriteria((current) => current.map((entry, position) => position === index ? { statement: event.target.value } : entry)); markDirty(); }} rows={2} /><FieldMediaPicker target={target} attachments={attachments} selectedIDs={mediaRefs[target] ?? []} busy={attachmentBusy} onChange={updateMediaRefs} onUpload={(files, uploadTarget) => void uploadFiles(files, uploadTarget)} /></div>{specification.acceptance_criteria?.[index]?.verification_status !== "HUMAN_VERIFIED" ? <button className="button button-quiet" type="button" onClick={() => void verify({ kind: "acceptance_criterion", position: index + 1 }, `criterion:${index + 1}`)} disabled={Boolean(verifying) || dirty}>{verifying === `criterion:${index + 1}` ? t("spec.verifying") : t("spec.verify")}</button> : <span className="app-v2-verified-label">{t("spec.human-verified")}</span>}<button className="button button-quiet is-danger" type="button" onClick={() => { setCriteria((current) => current.filter((_, position) => position !== index)); markDirty(); }}>{t("spec.remove")}</button></div>; })}</section>
    <section className="app-v2-editor-section"><div className="app-v2-editor-section-heading"><div><h4>{t("spec.affected-component")}</h4><p>{t("spec.affected-help")}</p></div><button className="button button-secondary" type="button" onClick={() => { setContextRefs((current) => [...current, draftContextRef()]); markDirty(); }}>{t("spec.add-component")}</button></div>{contextRefs.map((contextRef, index) => { const target = `context:${index + 1}:rationale`; return <ContextReferenceEditor key={`context-${index}`} contextRef={contextRef} repository={selectedRepository} data={repositoryData} modules={moduleOptions} mediaTarget={target} mediaAttachments={attachments} mediaIDs={mediaRefs[target] ?? []} mediaBusy={attachmentBusy} onMediaChange={updateMediaRefs} onMediaUpload={(files, uploadTarget) => void uploadFiles(files, uploadTarget)} onMediaDownload={(attachment) => void downloadAttachment(attachment)} onChange={(field, value) => updateContextRef(index, field, value)} onRemove={() => { setContextRefs((current) => current.filter((_, position) => position !== index)); markDirty(); }} />; })}</section>
    <div className="app-v2-editor-actions"><span>{dirty ? t("spec.unsaved") : t("spec.saved")}</span><button className="button button-primary" type="submit" disabled={saving || attachmentBusy || !dirty}>{saving ? t("spec.saving") : t("spec.save")}</button></div>
  </form>;
}
