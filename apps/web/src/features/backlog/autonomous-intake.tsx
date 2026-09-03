"use client";

import Link from "next/link";
import { FormEvent, useEffect, useMemo, useState } from "react";
import type { AutonomousRun, GitHubRepository, ProviderAuthStatus } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { AutonomousRunProgress, isAutonomousRunActive } from "../app/autonomous-run-progress";
import { ForgeSelect } from "../app/forge-select";

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
  const [model, setModel] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState("");
  const [providerStatus, setProviderStatus] = useState<ProviderAuthStatus | null>(null);
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [catalogError, setCatalogError] = useState("");
  const [objective, setObjective] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [createdRun, setCreatedRun] = useState<AutonomousRun | null>(null);
  const createdRunID = createdRun?.id;
  const createdRunStatus = createdRun?.status;

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

  useEffect(() => {
    let active = true;
    if (!canStart) {
      queueMicrotask(() => {
        if (!active) return;
        setProviderStatus(null);
        setCatalogLoading(false);
      });
      return () => { active = false; };
    }
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
  }, [canStart, client, projectID, provider]);

  useEffect(() => {
    if (!createdRunID || !createdRunStatus || !isAutonomousRunActive(createdRunStatus)) return;
    let active = true;
    const timer = window.setInterval(() => {
      void client.getAutonomousRun(createdRunID, projectID).then((result) => {
        if (active) setCreatedRun(result.run);
      }).catch(() => {
        // Keep the last known state and retry on the next poll.
      });
    }, 3000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [client, createdRunID, createdRunStatus, projectID]);

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
        model: model.trim() || undefined,
        reasoning_effort: reasoningEffort.trim() || undefined,
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
  const providerModels = providerStatus?.models ?? [];
  const selectedModel = providerModels.find((entry) => entry.id === model) ?? (model ? undefined : providerModels.find((entry) => entry.is_default));
  const reasoningOptions = selectedModel?.reasoning_efforts?.length ? selectedModel.reasoning_efforts : providerStatus?.reasoning_efforts ?? [];

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
    {createdRun ? <><div className="app-v2-inline-note" role="status"><strong>{t("backlog.ai-created")}</strong><span> {t("backlog.ai-created-detail", { status: statusLabel(createdRun.status) })}</span> <Link href={`${basePath}/work-items/${createdRun.work_item_id}`}>{t("backlog.ai-open-item")}</Link></div><AutonomousRunProgress run={createdRun} /></> : null}
    <form className="app-v2-run-form" onSubmit={submit}>
      <div className="app-v2-ai-intake-grid">
        <div className="app-v2-dialog-field"><span>{t("backlog.type")}</span><ForgeSelect ariaLabel={t("backlog.type")} value={workItemType} options={intakeTypes.map((value) => ({ value, label: statusLabel(value) }))} placeholder={t("backlog.type")} onChange={(value) => setWorkItemType(value as IntakeType)} /></div>
        <div className="app-v2-dialog-field"><span>{t("backlog.ai-provider")}</span><ForgeSelect ariaLabel={t("backlog.ai-provider")} value={provider} options={[{ value: "codex", label: "Codex" }, { value: "claude", label: "Claude" }]} placeholder={t("backlog.ai-provider")} onChange={(value) => { setProvider(value as "codex" | "claude"); setProviderStatus(null); setModel(""); setReasoningEffort(""); }} /></div>
        <div className="app-v2-dialog-field"><span>{t("backlog.ai-model")}</span>{providerModels.length ? <ForgeSelect ariaLabel={t("backlog.ai-model")} value={model} options={[{ value: "", label: t("backlog.ai-model-default") }, ...providerModels.map((entry) => ({ value: entry.id, label: `${entry.display_name || entry.id}${entry.is_default ? " · default" : ""}` }))]} placeholder={t("backlog.ai-model-default")} onChange={(value) => { setModel(value); setReasoningEffort(""); }} /> : <input value={model} onChange={(event) => setModel(event.target.value)} maxLength={120} placeholder={t("backlog.ai-model-placeholder")} />}{catalogLoading ? <small>{t("backlog.ai-model-catalog-loading")}</small> : providerStatus?.model_catalog_message ? <small>{t("backlog.ai-model-catalog-manual")}</small> : catalogError ? <small>{catalogError}</small> : null}</div>
        <div className="app-v2-dialog-field"><span>{t("backlog.ai-reasoning")}</span><ForgeSelect ariaLabel={t("backlog.ai-reasoning")} value={reasoningEffort} options={[{ value: "", label: t("backlog.ai-reasoning-default") }, ...reasoningOptions.map((entry) => ({ value: entry.value, label: `${entry.value}${entry.description ? ` — ${entry.description}` : ""}` }))]} placeholder={t("backlog.ai-reasoning-default")} disabled={!reasoningOptions.length} onChange={setReasoningEffort} />{!reasoningOptions.length && !catalogLoading ? <small>{t("backlog.ai-reasoning-unavailable")}</small> : null}</div>
        <div className="app-v2-dialog-field"><span>{t("backlog.ai-repository")}</span><ForgeSelect ariaLabel={t("backlog.ai-repository")} value={repositoryID} options={[{ value: "", label: repositoryLoading ? t("app.loading-projects") : t("backlog.ai-repository-placeholder") }, ...repositories.map((repository) => ({ value: repository.id, label: repository.full_name }))]} placeholder={t("backlog.ai-repository-placeholder")} disabled={repositoryLoading || !repositories.length} onChange={setRepositoryID} /></div>
        <label className="app-v2-dialog-field app-v2-run-prompt"><span>{t("backlog.ai-objective")}</span><textarea value={objective} onChange={(event) => setObjective(event.target.value)} rows={6} maxLength={131072} required placeholder={t("backlog.ai-objective-placeholder")} /></label>
      </div>
      <div className="app-v2-editor-actions"><button className="button button-primary" type="submit" disabled={!canSubmit}>{busy ? t("backlog.ai-submitting") : t("backlog.ai-submit")}</button></div>
    </form>
  </section>;
}
