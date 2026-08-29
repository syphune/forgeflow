"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import type { AuthorizationContext, Organization, Project, Workspace } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI, rememberOrganizationID } from "./api";

type OrganizationContext = { organization: Organization; authorization: AuthorizationContext; workspaces: Workspace[]; projects: Project[] };

export function HomePage() {
  const client = useMemo(() => browserAPI(undefined, null), []);
  const [organizationContexts, setOrganizationContexts] = useState<OrganizationContext[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [organizationError, setOrganizationError] = useState("");
  const [createError, setCreateError] = useState("");
  const [message, setMessage] = useState("");
  const [organizationBusy, setOrganizationBusy] = useState(false);
  const [organizationName, setOrganizationName] = useState("");
  const [organizationSlug, setOrganizationSlug] = useState("");
  const [createOrganizationID, setCreateOrganizationID] = useState("");
  const [createWorkspaceID, setCreateWorkspaceID] = useState("");
  const [projectKey, setProjectKey] = useState("APP");
  const [projectName, setProjectName] = useState("");
  const [createBusy, setCreateBusy] = useState(false);

  const projects = useMemo(() => organizationContexts.flatMap((context) => context.projects), [organizationContexts]);
  const creatableContexts = organizationContexts.filter((context) => context.authorization.capabilities?.includes("project.manage"));
  const createContext = creatableContexts.find((context) => context.organization.id === createOrganizationID) ?? creatableContexts[0];
  const selectedWorkspaceID = createContext?.workspaces.some((workspace) => workspace.id === createWorkspaceID)
    ? createWorkspaceID
    : createContext?.workspaces[0]?.id ?? "";

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const organizations = await client.listOrganizations();
      const contexts = await Promise.all((organizations.items ?? []).map(async (organization) => {
        const scopedClient = browserAPI(undefined, organization.id);
        const [authorization, workspaces, projectList] = await Promise.all([
          scopedClient.getOrganizationAuthorization(organization.id),
          scopedClient.listWorkspaces(),
          scopedClient.listProjects(),
        ]);
        return { organization, authorization, workspaces: workspaces.items ?? [], projects: projectList.items ?? [] };
      }));
      setOrganizationContexts(contexts);
      const creatable = contexts.filter((context) => context.authorization.capabilities?.includes("project.manage"));
      setCreateOrganizationID((current) => creatable.some((context) => context.organization.id === current) ? current : creatable[0]?.organization.id ?? "");
      setCreateWorkspaceID((current) => current || creatable[0]?.workspaces[0]?.id || "");
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setLoading(false);
    }
  }, [client]);

  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  async function createOrganization(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (organizationBusy || !organizationSlug.trim() || !organizationName.trim()) return;
    setOrganizationBusy(true);
    setOrganizationError("");
    try {
      const organization = await client.createOrganization({ slug: organizationSlug.trim(), display_name: organizationName.trim() });
      rememberOrganizationID(organization.id);
      window.location.reload();
    } catch (cause) {
      setOrganizationError(apiErrorMessage(cause));
    } finally {
      setOrganizationBusy(false);
    }
  }

  async function createProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (createBusy || !createContext || !selectedWorkspaceID || !projectKey.trim() || !projectName.trim()) return;
    setCreateBusy(true);
    setCreateError("");
    setMessage("");
    try {
      const project = await browserAPI(undefined, createContext.organization.id).createProject({
        workspace_id: selectedWorkspaceID,
        key: projectKey.trim(),
        display_name: projectName.trim(),
      });
      setOrganizationContexts((current) => current.map((context) => context.organization.id === createContext.organization.id
        ? { ...context, projects: [...context.projects, project] }
        : context));
      setProjectKey("APP");
      setProjectName("");
      setMessage(t("app.project-created"));
    } catch (cause) {
      setCreateError(apiErrorMessage(cause));
    } finally {
      setCreateBusy(false);
    }
  }

  return (
    <section className="app-v2-page app-v2-context-home" aria-labelledby="app-home-heading">
      <p className="eyebrow">{t("app.home-eyebrow")}</p>
      <h2 id="app-home-heading">{t("app.choose-project-start")}</h2>
      <p>{t("app.use-project-picker")}</p>
      {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("app.projects-load-error")}</strong><span>{error}</span><button type="button" onClick={() => void load()}>{t("app.retry")}</button></div> : null}
      {loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("app.loading-projects")}</p></div> : null}
      {!loading && !error && !organizationContexts.length ? <div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("app.create-organization")}</h3><p>{t("app.create-organization-description")}</p></div></div><form className="app-v2-settings-create-grid" onSubmit={createOrganization}><label className="app-v2-dialog-field"><span>{t("app.organization-name")}</span><input value={organizationName} onChange={(event) => setOrganizationName(event.target.value)} maxLength={160} required /></label><label className="app-v2-dialog-field"><span>{t("app.organization-slug")}</span><input value={organizationSlug} onChange={(event) => setOrganizationSlug(event.target.value.toLowerCase())} maxLength={63} pattern="[a-z0-9][a-z0-9-]*[a-z0-9]?" required /></label><button className="button button-primary" type="submit" disabled={organizationBusy || !organizationSlug.trim() || !organizationName.trim()}>{organizationBusy ? t("settings.saving") : t("app.create-organization-submit")}</button></form>{organizationError ? <div className="app-v2-error-panel" role="alert"><strong>{t("app.organization-create-error")}</strong><span>{organizationError}</span></div> : null}</div> : null}
      {creatableContexts.length ? <div id="create-project" className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("app.create-project")}</h3><p>{t("app.create-project-description")}</p></div></div><form className="app-v2-settings-create-grid" onSubmit={createProject}><label className="app-v2-dialog-field"><span>{t("app.create-project-organization")}</span><select value={createContext?.organization.id ?? ""} onChange={(event) => { setCreateOrganizationID(event.target.value); setCreateWorkspaceID(""); }}>{creatableContexts.map((context) => <option value={context.organization.id} key={context.organization.id}>{context.organization.display_name}</option>)}</select></label><label className="app-v2-dialog-field"><span>{t("app.create-project-workspace")}</span><select value={selectedWorkspaceID} onChange={(event) => setCreateWorkspaceID(event.target.value)} disabled={!createContext?.workspaces.length}>{createContext?.workspaces.map((workspace) => <option value={workspace.id} key={workspace.id}>{workspace.display_name}</option>)}</select></label><label className="app-v2-dialog-field"><span>{t("app.create-project-key")}</span><input value={projectKey} onChange={(event) => setProjectKey(event.target.value.toUpperCase())} maxLength={32} required /></label><label className="app-v2-dialog-field"><span>{t("app.create-project-name")}</span><input value={projectName} onChange={(event) => setProjectName(event.target.value)} maxLength={160} required /></label><button className="button button-primary" type="submit" disabled={createBusy || !selectedWorkspaceID || !projectKey.trim() || !projectName.trim()}>{createBusy ? t("settings.saving") : t("app.create-project-submit")}</button></form>{createContext && !createContext.workspaces.length ? <p className="app-v2-inline-note">{t("app.project-create-no-workspace")}</p> : null}{createError ? <div className="app-v2-error-panel" role="alert"><strong>{t("app.projects-load-error")}</strong><span>{createError}</span></div> : null}{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}</div> : null}
      {!loading && !error && !projects.length ? <div className="app-v2-empty"><strong>{t("app.no-projects")}</strong><p>{t("app.no-projects-description")}</p></div> : null}
      {!loading && !error && projects.length ? <div className="app-v2-home-grid" aria-label={t("app.projects")}>
        {projects.map((project) => {
          const basePath = `/app/orgs/${project.organization_id}/workspaces/${project.workspace_id}/projects/${project.id}`;
          return <article className="app-v2-home-card" key={project.id}>
            <div><span className="app-v2-key">{project.key}</span><h3>{project.display_name}</h3><p>{t("app.project-next-step")}</p></div>
            <div className="app-v2-home-card-actions"><Link className="button button-primary" href={`${basePath}/backlog`}>{t("app.open-backlog")}</Link><Link className="button button-secondary" href={`${basePath}/repositories`}>{t("app.connect-repository")}</Link><Link className="button button-secondary" href={`${basePath}/planning`}>{t("nav.planning")}</Link></div>
          </article>;
        })}
      </div> : null}
    </section>
  );
}
