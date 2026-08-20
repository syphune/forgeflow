"use client";

import Link from "next/link";
import { FormEvent, useEffect, useMemo, useState } from "react";
import type { AutonomousRun, GitHubRepository } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

const intakeTypes = ["TASK", "BUG", "STORY"] as const;
type IntakeType = typeof intakeTypes[number];

type Props = {
  projectID: string;
  basePath: string;
  canStart: boolean;
  onCreated: () => void;
};

export function AutonomousIntake({ projectID, basePath, canStart, onCreated }: Props) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [repositories, setRepositories] = useState<GitHubRepository[]>([]);
  const [repositoryID, setRepositoryID] = useState("");
  const [repositoryLoading, setRepositoryLoading] = useState(true);
  const [repositoryError, setRepositoryError] = useState("");
  const [workItemType, setWorkItemType] = useState<IntakeType>("TASK");
  const [provider, setProvider] = useState<"codex" | "claude">("codex");
  const [objective, setObjective] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [createdRun, setCreatedRun] = useState<AutonomousRun | null>(null);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      setRepositoryLoading(true);
      setRepositoryError("");
    });
    void client.listProjectRepositories(projectID).then((result) => {
      if (!active) return;
      const linked = (result.items ?? []).filter((repository) => repository.linked);
      setRepositories(linked);
      setRepositoryID((current) => linked.some((repository) => repository.id === current) ? current : linked[0]?.id ?? "");
    }).catch((cause: unknown) => {
      if (active) setRepositoryError(apiErrorMessage(cause));
    }).finally(() => {
      if (active) setRepositoryLoading(false);
    });
    return () => {
      active = false;
    };
  }, [client, projectID]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canStart || busy || !repositoryID || !objective.trim()) return;
    setBusy(true);
    setError("");
    setCreatedRun(null);
    try {
      const run = await client.startAutonomousRun({
        work_item_type: workItemType,
        repository_id: repositoryID,
        objective: objective.trim(),
        agent_provider: provider,
      }, projectID);
      setCreatedRun(run);
      setObjective("");
      onCreated();
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  const canSubmit = canStart && !busy && Boolean(repositoryID) && Boolean(objective.trim());

  return <section className="app-v2-surface-card app-v2-ai-intake" aria-labelledby="ai-intake-heading">
    <div className="app-v2-card-heading">
      <div>
        <p className="eyebrow">{t("backlog.ai-eyebrow")}</p>
        <h3 id="ai-intake-heading">{t("backlog.ai-title")}</h3>
        <p>{t("backlog.ai-description")}</p>
      </div>
      <span className="app-v2-chip is-info">Codex / Claude</span>
    </div>
    <div className="app-v2-inline-note">{t("backlog.ai-quality-note")}</div>
    {repositoryError ? <div className="app-v2-error-panel" role="alert"><strong>{t("backlog.ai-repository-error")}</strong><span>{repositoryError}</span></div> : null}
    {!repositoryLoading && !repositories.length ? <div className="app-v2-inline-note">{t("backlog.ai-no-repository")} <Link href={`${basePath}/repositories`}>{t("nav.repositories")}</Link></div> : null}
    {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("backlog.ai-error")}</strong><span>{error}</span></div> : null}
    {createdRun ? <div className="app-v2-inline-note" role="status"><strong>{t("backlog.ai-created")}</strong><span> {t("backlog.ai-created-detail", { status: statusLabel(createdRun.status) })}</span> <Link href={`${basePath}/work-items/${createdRun.work_item_id}`}>{t("backlog.ai-open-item")}</Link></div> : null}
    <form className="app-v2-run-form" onSubmit={submit}>
      <div className="app-v2-ai-intake-grid">
        <label className="app-v2-dialog-field"><span>{t("backlog.type")}</span><select value={workItemType} onChange={(event) => setWorkItemType(event.target.value as IntakeType)}><option value="TASK">{statusLabel("TASK")}</option><option value="BUG">{statusLabel("BUG")}</option><option value="STORY">{statusLabel("STORY")}</option></select></label>
        <label className="app-v2-dialog-field"><span>{t("backlog.ai-provider")}</span><select value={provider} onChange={(event) => setProvider(event.target.value as "codex" | "claude")}><option value="codex">Codex</option><option value="claude">Claude</option></select></label>
        <label className="app-v2-dialog-field"><span>{t("backlog.ai-repository")}</span><select value={repositoryID} onChange={(event) => setRepositoryID(event.target.value)} disabled={repositoryLoading || !repositories.length} required><option value="">{repositoryLoading ? t("app.loading-projects") : t("backlog.ai-repository-placeholder")}</option>{repositories.map((repository) => <option value={repository.id} key={repository.id}>{repository.full_name}</option>)}</select></label>
        <label className="app-v2-dialog-field app-v2-run-prompt"><span>{t("backlog.ai-objective")}</span><textarea value={objective} onChange={(event) => setObjective(event.target.value)} rows={6} maxLength={131072} required placeholder={t("backlog.ai-objective-placeholder")} /></label>
      </div>
      <div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={!canSubmit}>{busy ? t("backlog.ai-submitting") : t("backlog.ai-submit")}</button></div>
    </form>
  </section>;
}
