"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { GitHubInstallation, GitHubRepository, RepositoryContext } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

type Props = { projectID: string; basePath: string };

const apiBase = (process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? "").replace(/\/+$/, "");

function repositoryName(repository: GitHubRepository) {
  return repository.full_name || `${t("repo.title")} (${t("repo.unknown")})`;
}

export function RepositoryPage({ projectID, basePath }: Props) {
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const [repositories, setRepositories] = useState<GitHubRepository[]>([]);
  const [installations, setInstallations] = useState<GitHubInstallation[]>([]);
  const [selectedRepositoryID, setSelectedRepositoryID] = useState("");
  const [context, setContext] = useState<RepositoryContext | null>(null);
  const [loading, setLoading] = useState(true);
  const [contextLoading, setContextLoading] = useState(false);
  const [busyRepositoryID, setBusyRepositoryID] = useState("");
  const [error, setError] = useState("");
  const [contextError, setContextError] = useState("");
  const [message, setMessage] = useState("");
  const installURL = `${apiBase}/api/v1/integrations/github/install/start`;

  const loadRepositories = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError("");
    try {
      const [available, connected] = await Promise.all([
        client.request<{ items: GitHubRepository[] }>(`/integrations/github/repositories?project_id=${encodeURIComponent(projectID)}`, { projectID, signal }),
        client.request<{ items: GitHubInstallation[] }>("/integrations/github/installations", { signal }),
      ]);
      if (signal?.aborted) return;
      setRepositories(available.items ?? []);
      setInstallations(connected.items ?? []);
    } catch (cause) {
      if (!signal?.aborted) setError(apiErrorMessage(cause));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, [client, projectID]);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => void loadRepositories(controller.signal), 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [loadRepositories]);

  async function selectRepository(repository: GitHubRepository) {
    if (!repository.linked || contextLoading) return;
    setSelectedRepositoryID(repository.id);
    setContextError("");
    setMessage("");
    if (context?.repository.id === repository.id) return;
    setContextLoading(true);
    try {
      setContext(await client.getRepositoryContext(projectID, repository.id));
    } catch (cause) {
      setContext(null);
      setContextError(apiErrorMessage(cause));
    } finally {
      setContextLoading(false);
    }
  }

  async function toggleRepository(repository: GitHubRepository) {
    if (busyRepositoryID) return;
    const linked = !repository.linked;
    setBusyRepositoryID(repository.id);
    setError("");
    setMessage("");
    try {
      if (linked) await client.linkProjectRepository(projectID, repository.id);
      else await client.unlinkProjectRepository(projectID, repository.id);
      setRepositories((current) => current.map((item) => item.id === repository.id ? { ...item, linked } : item));
      if (!linked && selectedRepositoryID === repository.id) {
        setSelectedRepositoryID("");
        setContext(null);
      }
      setMessage(t(linked ? "repo.link-message" : "repo.unlink-message", { name: repositoryName(repository) }));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyRepositoryID("");
    }
  }

  const linkedCount = repositories.filter((repository) => repository.linked).length;

  return <section className="app-v2-page" aria-labelledby="repositories-heading">
    <div className="app-v2-page-heading"><div><p className="eyebrow">{t("repo.eyebrow")}</p><h2 id="repositories-heading">{t("repo.title")}</h2><p>{t("repo.description")}</p></div><div className="app-v2-page-actions"><button className="button button-secondary" type="button" onClick={() => void loadRepositories()} disabled={loading}>{loading ? t("repo.refreshing") : t("repo.refresh")}</button><a className="button button-primary" href={installURL}>{t("repo.connect-github")}</a></div></div>
    <div className="app-v2-surface-card app-v2-repository-intro"><div><strong>{t("repo.linked-count", { count: linkedCount })}</strong><p>{t("repo.link-help")}</p></div>{installations.length ? <span className="app-v2-chip is-info">{t("repo.installations", { count: installations.length })}</span> : null}</div>
    {message ? <p className="app-v2-action-status" role="status">{message}</p> : null}
    {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("repo.load-error")}</strong><span>{error}</span><button type="button" onClick={() => void loadRepositories()}>{t("app.retry")}</button></div> : null}
    {!loading && !installations.length ? <div className="app-v2-empty"><strong>{t("repo.install-first")}</strong><p>{t("repo.install-help")}</p><a className="button button-secondary" href={installURL}>{t("repo.install")}</a></div> : null}
    {!loading && installations.length > 0 && !repositories.length ? <div className="app-v2-empty"><strong>{t("repo.none")}</strong><p>{t("repo.none-help")}</p><a className="button button-secondary" href={installURL}>{t("repo.update-selection")}</a></div> : null}
    {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("repo.checking")}</p></div> : null}
    {!loading && repositories.length ? <div className="app-v2-repository-layout"><div className="app-v2-repository-grid" aria-label={t("repo.github-repositories")}>{repositories.map((repository) => <article className={`app-v2-surface-card app-v2-repository-card ${repository.linked ? "is-linked" : ""}`} key={repository.id}><div className="app-v2-repository-card-heading"><div><span className="app-v2-key">{repository.installation_account || "GitHub"}</span><h3>{repositoryName(repository)}</h3></div><span className={`app-v2-readiness ${repository.linked ? "is-ready" : ""}`}>{repository.linked ? t("repo.linked") : t("repo.available")}</span></div><p>{t("repo.default-branch")} <strong>{repository.default_branch || t("repo.unknown")}</strong></p><div className="app-v2-repository-actions"><button className="button button-secondary" type="button" onClick={() => void toggleRepository(repository)} disabled={busyRepositoryID === repository.id}>{busyRepositoryID === repository.id ? t("work.saving") : repository.linked ? t("repo.unlink") : t("repo.link")}</button>{repository.linked ? <button className="button button-quiet" type="button" onClick={() => void selectRepository(repository)} disabled={contextLoading}>{selectedRepositoryID === repository.id && contextLoading ? t("repo.loading-context") : t("repo.view-context")}</button> : null}</div></article>)}</div><div className="app-v2-surface-card app-v2-repository-context" aria-live="polite"><h3>{t("repo.engineering-context")}</h3>{contextLoading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : null}{contextError ? <div className="app-v2-error-panel" role="alert"><span>{contextError}</span><button type="button" onClick={() => { const repository = repositories.find((item) => item.id === selectedRepositoryID); if (repository) void selectRepository(repository); }}>{t("app.retry")}</button></div> : null}{!contextLoading && !contextError && !context ? <p className="app-v2-muted">{t("repo.select-linked")}</p> : null}{context ? <><div className="app-v2-repository-context-title"><strong>{repositoryName(context.repository)}</strong><span>{context.repository.default_branch || t("repo.unknown-default-branch")}</span></div><div className="app-v2-context-stats"><div><strong>{context.branches.length}</strong><span>{t("repo.branch")}</span></div><div><strong>{context.pull_requests.length}</strong><span>{t("repo.pull-request")}</span></div><div><strong>{context.ci_runs.length}</strong><span>{t("repo.ci-run")}</span></div></div><Link className="button button-secondary" href={`${basePath}/backlog`}>{t("repo.open-backlog")}</Link></> : null}</div></div> : null}
  </section>;
}
