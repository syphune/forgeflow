"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { GitHubInstallation, GitHubRepository, RepositoryContext, RepositorySearchMatch } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI, isRepositoryEmptyError } from "../app/api";

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
  const [repositorySearchQuery, setRepositorySearchQuery] = useState("");
  const [repositorySearchResults, setRepositorySearchResults] = useState<RepositorySearchMatch[]>([]);
  const [repositorySearchBusy, setRepositorySearchBusy] = useState(false);
  const [repositorySearchSubmitted, setRepositorySearchSubmitted] = useState(false);
  const [loading, setLoading] = useState(true);
  const [contextLoading, setContextLoading] = useState(false);
  const [repositoryEmpty, setRepositoryEmpty] = useState(false);
  const [repositoryInitializing, setRepositoryInitializing] = useState(false);
  const [busyRepositoryID, setBusyRepositoryID] = useState("");
  const [error, setError] = useState("");
  const [contextError, setContextError] = useState("");
  const [repositorySearchError, setRepositorySearchError] = useState("");
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
    if (selectedRepositoryID !== repository.id) {
      setRepositorySearchQuery("");
      setRepositorySearchResults([]);
      setRepositorySearchSubmitted(false);
      setRepositorySearchError("");
    }
    setSelectedRepositoryID(repository.id);
    setContextError("");
    setRepositoryEmpty(false);
    setMessage("");
    if (context?.repository.id === repository.id) return;
    setContextLoading(true);
    try {
      setContext(await client.getRepositoryContext(projectID, repository.id));
    } catch (cause) {
      setContext(null);
      setRepositoryEmpty(isRepositoryEmptyError(cause));
      setContextError(apiErrorMessage(cause));
    } finally {
      setContextLoading(false);
    }
  }

  async function searchRepositoryCode() {
    const query = repositorySearchQuery.trim();
    if (!selectedRepositoryID || !query || repositorySearchBusy) return;
    setRepositorySearchBusy(true);
    setRepositorySearchSubmitted(true);
    setRepositorySearchError("");
    try {
      const response = await client.searchRepository(projectID, selectedRepositoryID, query);
      setRepositorySearchResults(response.items ?? []);
    } catch (cause) {
      setRepositorySearchResults([]);
      setRepositorySearchError(apiErrorMessage(cause));
    } finally {
      setRepositorySearchBusy(false);
    }
  }

  async function initializeRepository() {
    const repository = repositories.find((item) => item.id === selectedRepositoryID);
    if (!repository || repositoryInitializing) return;
    if (!window.confirm(t("repo.initialize-empty-confirm"))) return;
    setRepositoryInitializing(true);
    setContextError("");
    try {
      await client.initializeRepository(projectID, repository.id, crypto.randomUUID());
      setContext(null);
      setRepositoryEmpty(false);
      await selectRepository(repository);
    } catch (cause) {
      setRepositoryEmpty(isRepositoryEmptyError(cause));
      setContextError(apiErrorMessage(cause));
    } finally {
      setRepositoryInitializing(false);
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
        setRepositoryEmpty(false);
        setRepositorySearchQuery("");
        setRepositorySearchResults([]);
        setRepositorySearchSubmitted(false);
        setRepositorySearchError("");
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
    {!loading && repositories.length ? <div className="app-v2-repository-layout"><div className="app-v2-repository-grid" aria-label={t("repo.github-repositories")}>{repositories.map((repository) => <article className={`app-v2-surface-card app-v2-repository-card ${repository.linked ? "is-linked" : ""}`} key={repository.id}><div className="app-v2-repository-card-heading"><div><span className="app-v2-key">{repository.installation_account || "GitHub"}</span><h3>{repositoryName(repository)}</h3></div><span className={`app-v2-readiness ${repository.linked ? "is-ready" : ""}`}>{repository.linked ? t("repo.linked") : t("repo.available")}</span></div><p>{t("repo.default-branch")} <strong>{repository.default_branch || t("repo.unknown")}</strong></p><div className="app-v2-repository-actions"><button className="button button-secondary" type="button" onClick={() => void toggleRepository(repository)} disabled={busyRepositoryID === repository.id}>{busyRepositoryID === repository.id ? t("work.saving") : repository.linked ? t("repo.unlink") : t("repo.link")}</button>{repository.linked ? <button className="button button-quiet" type="button" onClick={() => void selectRepository(repository)} disabled={contextLoading}>{selectedRepositoryID === repository.id && contextLoading ? t("repo.loading-context") : t("repo.view-context")}</button> : null}</div></article>)}</div><div className="app-v2-surface-card app-v2-repository-context" aria-live="polite"><h3>{t("repo.engineering-context")}</h3>{contextLoading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : null}{contextError ? <div className="app-v2-error-panel" role="alert"><span>{contextError}</span><div><button type="button" onClick={() => { const repository = repositories.find((item) => item.id === selectedRepositoryID); if (repository) void selectRepository(repository); }}>{t("app.retry")}</button>{repositoryEmpty ? <button className="button button-secondary" type="button" onClick={() => void initializeRepository()} disabled={repositoryInitializing}>{repositoryInitializing ? t("repo.initializing-repository") : t("repo.initialize-empty-repository")}</button> : null}</div></div> : null}{!contextLoading && !contextError && !context ? <p className="app-v2-muted">{t("repo.select-linked")}</p> : null}{context ? <><div className="app-v2-repository-context-title"><strong>{repositoryName(context.repository)}</strong><span>{context.repository.default_branch || t("repo.unknown-default-branch")}</span></div><div className="app-v2-context-stats"><div><strong>{context.branches.length}</strong><span>{t("repo.branch")}</span></div><div><strong>{context.pull_requests.length}</strong><span>{t("repo.pull-request")}</span></div><div><strong>{context.ci_runs.length}</strong><span>{t("repo.ci-run")}</span></div></div><Link className="button button-secondary" href={`${basePath}/backlog`}>{t("repo.open-backlog")}</Link><form className="repository-search-form" onSubmit={(event) => { event.preventDefault(); void searchRepositoryCode(); }}><input type="search" aria-label={t("repo.search-code")} value={repositorySearchQuery} onChange={(event) => { setRepositorySearchQuery(event.target.value); setRepositorySearchSubmitted(false); setRepositorySearchError(""); }} placeholder={t("repo.search-placeholder")} maxLength={256} /><button className="button button-secondary" type="submit" disabled={repositorySearchBusy || !repositorySearchQuery.trim()}>{repositorySearchBusy ? t("repo.searching-code") : t("repo.search-code")}</button></form>{repositorySearchError ? <p className="app-v2-inline-error" role="alert">{repositorySearchError}</p> : null}{repositorySearchSubmitted && !repositorySearchBusy && !repositorySearchResults.length && !repositorySearchError ? <p className="app-v2-muted">{t("repo.search-empty")}</p> : null}{repositorySearchResults.length ? <div className="repository-search-results" aria-label={t("repo.search-results")}>{repositorySearchResults.map((match, index) => <div className="repository-file-row" key={`${match.path}-${match.line}-${index}`}><span>{match.path}</span><small>{t("repo.search-match", { line: match.line, snippet: match.snippet })}</small></div>)}</div> : null}</> : null}</div></div> : null}
  </section>;
}
