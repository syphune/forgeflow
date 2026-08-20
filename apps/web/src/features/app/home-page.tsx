"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { Project } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "./api";

export function HomePage() {
  const client = useMemo(() => browserAPI(), []);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await client.listProjects();
      setProjects(result.items ?? []);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  return (
    <section className="app-v2-page app-v2-context-home" aria-labelledby="app-home-heading">
      <p className="eyebrow">{t("app.home-eyebrow")}</p>
      <h2 id="app-home-heading">{t("app.choose-project-start")}</h2>
      <p>{t("app.use-project-picker")}</p>
      {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("app.projects-load-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
      {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("app.loading-projects")}</p></div> : null}
      {!loading && !error && !projects.length ? <div className="app-v2-empty"><strong>{t("app.no-projects")}</strong><p>{t("app.no-projects-description")}</p></div> : null}
      {!loading && !error && projects.length ? <div className="app-v2-home-grid" aria-label={t("app.projects")}>
        {projects.map((project) => {
          const basePath = `/app/orgs/${project.organization_id}/workspaces/${project.workspace_id}/projects/${project.id}`;
          return <article className="app-v2-home-card" key={project.id}>
            <div><span className="app-v2-key">{project.key}</span><h3>{project.display_name}</h3><p>{t("app.project-next-step")}</p></div>
            <div className="app-v2-home-card-actions"><Link className="button button-primary" href={`${basePath}/backlog`}>{t("app.open-backlog")}</Link><Link className="button button-secondary" href={`${basePath}/planning`}>{t("nav.planning")}</Link></div>
          </article>;
        })}
      </div> : null}
    </section>
  );
}
