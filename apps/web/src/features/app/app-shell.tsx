"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ReactNode, useEffect, useMemo, useState } from "react";
import type { Actor, AuthorizationContext, Organization, Project, Workspace } from "@forgeflow/api-client";
import { APIError } from "@forgeflow/api-client";
import { translate as t, type Locale } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI, getSelectedOrganizationID, rememberOrganizationID } from "./api";
import { ForgeSelect } from "./forge-select";
import { ThemeControl } from "./theme-control";

type Props = { children: ReactNode };
type SessionState = "checking" | "signed-in" | "signed-out";

function pathContext(pathname: string, searchParams: { get(name: string): string | null }): { organizationID?: string; workspaceID?: string; projectID?: string; navigationProjectID?: string; scope?: "organization" | "workspace" | "project" } {
  const navigationProjectID = searchParams.get("project_id") || undefined;
  const project = pathname.match(/\/app\/orgs\/([^/]+)\/workspaces\/([^/]+)\/projects\/([^/]+)/);
  if (project) return { organizationID: project[1], workspaceID: project[2], projectID: project[3], navigationProjectID, scope: "project" };
  const workspace = pathname.match(/\/app\/orgs\/([^/]+)\/workspaces\/([^/]+)\/settings/);
  if (workspace) return { organizationID: workspace[1], workspaceID: workspace[2], navigationProjectID, scope: "workspace" };
  const organization = pathname.match(/\/app\/orgs\/([^/]+)\/settings/);
  if (organization) return { organizationID: organization[1], navigationProjectID, scope: "organization" };
  return { navigationProjectID };
}

export function AppShell({ children }: Props) {
  const pathname = usePathname();
  const router = useRouter();
  const searchParams = useSearchParams();
  const routeContext = useMemo(() => pathContext(pathname, searchParams), [pathname, searchParams]);
  const [selectedOrganizationID, setSelectedOrganizationID] = useState(() => getSelectedOrganizationID());
  const organizationID = routeContext.organizationID || selectedOrganizationID;
  const client = useMemo(() => browserAPI(routeContext.projectID, organizationID), [organizationID, routeContext.projectID]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [actor, setActor] = useState<Actor | null>(null);
  const [sessionState, setSessionState] = useState<SessionState>("checking");
  const [contextLoading, setContextLoading] = useState(true);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [canCreateProject, setCanCreateProject] = useState(false);
  const [unreadCount, setUnreadCount] = useState(0);
  const [error, setError] = useState("");
  const [mobileOpen, setMobileOpen] = useState(false);
  const [logoutBusy, setLogoutBusy] = useState(false);
  const [locale, setLocale] = useState<Locale>(() => typeof document !== "undefined" && document.documentElement.lang === "en" ? "en" : "vi");
  const signInURL = `${process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? ""}/api/v1/auth/github/start`;

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    const load = async () => {
      setError("");
      setContextLoading(true);
      setCanCreateProject(false);
      try {
        const bootstrapClient = browserAPI(undefined, null);
        const currentActor = await bootstrapClient.request<Actor>("/me", { signal: controller.signal });
        const orgs = await bootstrapClient.listOrganizations();
        const availableOrganizationIDs = new Set((orgs.items ?? []).map((organization) => organization.id));
        const storedOrganizationID = getSelectedOrganizationID();
        const nextOrganizationID = routeContext.organizationID && availableOrganizationIDs.has(routeContext.organizationID)
          ? routeContext.organizationID
          : availableOrganizationIDs.has(storedOrganizationID)
            ? storedOrganizationID
            : orgs.items?.[0]?.id ?? "";
        if (!active) return;
        if (!nextOrganizationID) {
          rememberOrganizationID("");
          setSelectedOrganizationID("");
          setActor(currentActor);
          setSessionState("signed-in");
          setOrganizations([]);
          setWorkspaces([]);
          setProjects([]);
          setUnreadCount(0);
          setAuthorization(null);
          return;
        }
        rememberOrganizationID(nextOrganizationID);
        setSelectedOrganizationID(nextOrganizationID);
        const scopedClient = browserAPI(routeContext.projectID, nextOrganizationID);
        const organizationContexts = await Promise.all((orgs.items ?? []).map(async (organization) => {
          const organizationClient = browserAPI(undefined, organization.id);
          const [organizationAuthorization, spaces, projectList] = await Promise.all([
            organizationClient.getOrganizationAuthorization(organization.id),
            organizationClient.listWorkspaces(),
            organizationClient.listProjects(),
          ]);
          return { authorization: organizationAuthorization, spaces: spaces.items ?? [], projects: projectList.items ?? [] };
        }));
        const [count, auth] = await Promise.all([
          scopedClient.getUnreadNotificationCount(),
          routeContext.scope === "project" && routeContext.projectID
            ? scopedClient.getProjectAuthorization(routeContext.projectID)
            : routeContext.scope === "workspace" && routeContext.workspaceID
              ? scopedClient.getWorkspaceAuthorization(routeContext.workspaceID)
              : routeContext.scope === "organization" && routeContext.organizationID
                ? scopedClient.getOrganizationAuthorization(routeContext.organizationID)
                : Promise.resolve(null),
        ]);
        if (!active) return;
        setActor(currentActor);
        setSessionState("signed-in");
        setOrganizations(orgs.items ?? []);
        setWorkspaces(organizationContexts.flatMap((context) => context.spaces));
        setProjects(organizationContexts.flatMap((context) => context.projects));
        setCanCreateProject(organizationContexts.some((context) => context.authorization.capabilities?.includes("project.manage")));
        setUnreadCount(count.unread_count ?? 0);
        setAuthorization(auth);
      } catch (cause) {
        if (!active || controller.signal.aborted) return;
        if (cause instanceof APIError && cause.status === 401) {
          setActor(null);
          setSessionState("signed-out");
          setAuthorization(null);
          setOrganizations([]);
          setWorkspaces([]);
          setProjects([]);
          setCanCreateProject(false);
          return;
        }
        setError(apiErrorMessage(cause));
      } finally {
        if (active) setContextLoading(false);
      }
    };
    void load();
    return () => {
      active = false;
      controller.abort();
    };
  }, [client, routeContext.organizationID, routeContext.projectID, routeContext.scope, routeContext.workspaceID]);

  useEffect(() => {
    async function refreshUnreadCount() {
      try {
        const result = await client.getUnreadNotificationCount();
        setUnreadCount(result.unread_count ?? 0);
      } catch {
        // The inbox owns the actionable error; the shell keeps the last known badge.
      }
    }
    window.addEventListener("forgeflow:notifications-changed", refreshUnreadCount);
    return () => window.removeEventListener("forgeflow:notifications-changed", refreshUnreadCount);
  }, [client]);

  const selectedOrganization = organizations.find((item) => item.id === routeContext.organizationID);
  const selectedWorkspace = workspaces.find((item) => item.id === routeContext.workspaceID);
  const selectedProject = projects.find((item) => item.id === routeContext.projectID);
  const navigationProject = projects.find((project) => project.id === (routeContext.projectID ?? routeContext.navigationProjectID)
    && (!routeContext.organizationID || project.organization_id === routeContext.organizationID)
    && (!routeContext.workspaceID || project.workspace_id === routeContext.workspaceID));
  const projectNavigationPath = navigationProject
    ? `/app/orgs/${navigationProject.organization_id}/workspaces/${navigationProject.workspace_id}/projects/${navigationProject.id}`
    : "";
  const projectNavigationQuery = navigationProject ? `?project_id=${encodeURIComponent(navigationProject.id)}` : "";
  const projectOptions = projects.map((project) => {
    const organization = organizations.find((item) => item.id === project.organization_id);
    const workspace = workspaces.find((item) => item.id === project.workspace_id);
    const scope = [organization?.display_name, workspace?.display_name].filter(Boolean).join(" / ");
    return { value: project.id, label: `${project.key} · ${project.display_name}${scope ? ` · ${scope}` : ""}` };
  });
  const accountName = actor?.display_name?.trim() || actor?.login?.trim() || actor?.id || "";
  const accountLogin = actor?.login?.trim() ? `@${actor.login.trim()}` : "";
  const accountInitial = (accountName || "?").slice(0, 1).toUpperCase();
  const canonicalContextMismatch = Boolean(
    routeContext.scope && !contextLoading && (!authorization || (
      authorization.scope !== routeContext.scope ||
      authorization.organization_id !== routeContext.organizationID ||
      (routeContext.scope !== "organization" && authorization.workspace_id !== routeContext.workspaceID) ||
      (routeContext.scope === "project" && authorization.project_id !== routeContext.projectID)
    )),
  );
  const baseProjectPath = !canonicalContextMismatch && routeContext.organizationID && routeContext.workspaceID && routeContext.projectID
    ? `/app/orgs/${routeContext.organizationID}/workspaces/${routeContext.workspaceID}/projects/${routeContext.projectID}`
    : "/app";

  function selectProject(projectID: string) {
    const project = projects.find((item) => item.id === projectID);
    if (!project) return;
    const organizationID = project.organization_id ?? routeContext.organizationID ?? organizations[0]?.id;
    if (!organizationID || !project.workspace_id) return;
    const targetPath = `/app/orgs/${organizationID}/workspaces/${project.workspace_id}/projects/${project.id}/backlog`;
    if (project.id !== routeContext.projectID) setContextLoading(true);
    router.push(targetPath);
    setMobileOpen(false);
  }

  function changeLocale(nextLocale: Locale) {
    document.cookie = `forgeflow_locale=${nextLocale}; path=/; max-age=31536000; samesite=lax`;
    document.documentElement.lang = nextLocale;
    setLocale(nextLocale);
    window.location.reload();
  }

  async function signOut() {
    setLogoutBusy(true);
    try {
      await client.request<void>("/auth/logout", { method: "POST" });
    } catch {
      // Clear the local session view even when the server session already expired.
    } finally {
      setActor(null);
      setSessionState("signed-out");
      setAuthorization(null);
      setOrganizations([]);
      setWorkspaces([]);
      setProjects([]);
      setUnreadCount(0);
      setLogoutBusy(false);
      router.replace("/app");
    }
  }

  return (
    <div className="app-v2">
      <button className="app-v2-mobile-toggle" type="button" aria-expanded={mobileOpen} onClick={() => setMobileOpen((value) => !value)}>
        <span aria-hidden="true">☰</span><span>{t("nav.menu")}</span>
      </button>
      <aside className={`app-v2-sidebar ${mobileOpen ? "is-open" : ""}`} aria-label={t("nav.application-navigation")}>
        <div className="app-v2-brand-row">
          <Link className="brand" href="/app" onClick={() => setMobileOpen(false)}>
            <span className="brand-mark" aria-hidden="true"><span /></span><span>forgeflow</span>
          </Link>
          <button className="app-v2-close" type="button" onClick={() => setMobileOpen(false)} aria-label={t("nav.close-navigation")}>×</button>
        </div>
        <div className="app-v2-context-picker">
          <div className="app-v2-context-field">
            <span>{t("nav.project")}</span>
            <div className="app-v2-project-picker-row">
              <ForgeSelect ariaLabel={t("nav.project-switcher")} searchLabel={t("nav.project")} disabled={contextLoading || sessionState !== "signed-in"} value={navigationProject?.id ?? ""} options={projectOptions} placeholder={t("app.choose-project")} searchable onChange={selectProject} />
              {canCreateProject ? <Link className="app-v2-add-project" href="/app#create-project" aria-label={t("app.add-project")} title={t("app.add-project")}>+</Link> : null}
            </div>
            <small className="app-v2-context-hint">{selectedOrganization?.display_name ?? t("nav.organization")} / {selectedWorkspace?.display_name ?? t("nav.workspace")}</small>
          </div>
        </div>
        <nav className="app-v2-nav" aria-label={t("nav.project-navigation")}>
          {projectNavigationPath ? <>
            <Link className={pathname.endsWith("/backlog") || pathname.includes("/work-items/") ? "is-active" : ""} href={`${projectNavigationPath}/backlog`} onClick={() => setMobileOpen(false)}><span>▦</span>Backlog</Link>
            <Link className={pathname.includes("/planning") ? "is-active" : ""} href={`${projectNavigationPath}/planning`} onClick={() => setMobileOpen(false)}><span>◷</span>{t("nav.planning")}</Link>
            <Link className={pathname.includes("/repositories") ? "is-active" : ""} href={`${projectNavigationPath}/repositories`} onClick={() => setMobileOpen(false)}><span>⌘</span>{t("nav.repositories")}</Link>
          </> : null}
          <Link className={pathname.includes("/inbox") ? "is-active" : ""} href="/app/inbox" onClick={() => setMobileOpen(false)}><span>◌</span>{t("nav.inbox")}{unreadCount > 0 ? <b className="app-v2-badge">{unreadCount > 99 ? "99+" : unreadCount}</b> : null}</Link>
        </nav>
        <div className="app-v2-sidebar-bottom">
          <div className="app-v2-account" aria-live="polite">
            {sessionState === "signed-in" ? <><div className="app-v2-account-identity"><span className="app-v2-account-avatar" aria-hidden="true">{accountInitial}</span><span className="app-v2-account-copy"><strong className="app-v2-account-name" title={accountName}>{accountName}</strong><span className="app-v2-account-login">{accountLogin || t("auth.signed-in")}</span></span></div><button className="app-v2-sign-out" type="button" onClick={() => void signOut()} disabled={logoutBusy}>{logoutBusy ? t("auth.logging-out") : t("auth.sign-out")}</button></> : <><span className="app-v2-account-status">{sessionState === "signed-out" ? t("auth.required") : t("auth.checking")}</span>{sessionState === "signed-out" ? <a className="app-v2-sign-in" href={signInURL}>{t("auth.sign-in")} <span aria-hidden="true">↗</span></a> : null}</>}
          </div>
          {routeContext.organizationID ? <Link href={`/app/orgs/${routeContext.organizationID}/settings/general${projectNavigationQuery}`} onClick={() => setMobileOpen(false)}><span>◎</span>{t("nav.organization-settings")}</Link> : null}
          {routeContext.organizationID && routeContext.workspaceID ? <Link href={`/app/orgs/${routeContext.organizationID}/workspaces/${routeContext.workspaceID}/settings/general${projectNavigationQuery}`} onClick={() => setMobileOpen(false)}><span>◫</span>{t("nav.workspace-settings")}</Link> : null}
          {projectNavigationPath ? <Link href={`${projectNavigationPath}/settings/workflow`} onClick={() => setMobileOpen(false)}><span>⚙</span>{t("nav.project-settings")}</Link> : null}
          <Link href="/app/account/developer" onClick={() => setMobileOpen(false)}><span>⌘</span>{t("nav.developer")}</Link>
          <div className="app-v2-sidebar-preferences">
            <div className="app-v2-locale-switcher" aria-label={t("nav.language")}>
              <span>{t("nav.language")}</span>
              <button type="button" className={locale === "vi" ? "is-active" : ""} aria-pressed={locale === "vi"} onClick={() => changeLocale("vi")}>VI</button>
              <button type="button" className={locale === "en" ? "is-active" : ""} aria-pressed={locale === "en"} onClick={() => changeLocale("en")}>EN</button>
            </div>
            <ThemeControl className="app-v2-theme-switcher" />
          </div>
        </div>
      </aside>
      <div className="app-v2-main" aria-busy={contextLoading}>
        <header className="app-v2-header">
          <div><p className="app-v2-breadcrumb">{selectedOrganization?.display_name ?? "Forgeflow"} / {selectedWorkspace?.display_name ?? t("nav.workspace")}</p><h1>{selectedProject ? `${selectedProject.key} · ${selectedProject.display_name}` : t("app.engineering-workspace")}</h1></div>
          <div className="app-v2-header-actions">{contextLoading && sessionState === "signed-in" ? <span className="app-v2-header-loading" role="status" aria-live="polite">{t("app.loading-project")}</span> : routeContext.scope === "project" && authorization?.capabilities.includes("work_item.create") ? <Link className="button button-primary" href={`${baseProjectPath}/backlog?create=1`}>{t("app.new-work-item")}</Link> : null}</div>
        </header>
        {contextLoading && sessionState === "signed-in" ? <div className="app-v2-route-loading" role="status" aria-live="polite"><div className="app-v2-loading"><span /><span /><span /><p>{t("app.loading-context")}</p></div></div> : null}
        {error ? <div className="app-v2-inline-error" role="status">{error} <button type="button" onClick={() => window.location.reload()}>{t("app.retry")}</button></div> : null}
        <main className="app-v2-content">
          {sessionState === "checking" ? <div className="app-v2-auth-gate" role="status" aria-live="polite"><strong>{t("app.checking-session")}</strong><p>{t("app.preparing-workspace")}</p></div> : null}
          {sessionState === "signed-out" ? <div className="app-v2-auth-gate" role="alert"><strong>{t("app.sign-in-workspace")}</strong><p>{t("app.private-context")}</p><a className="button button-primary" href={signInURL}>{t("app.continue-github")} <span aria-hidden="true">↗</span></a></div> : null}
          {sessionState === "signed-in" && (canonicalContextMismatch ? <div className="app-v2-error-state" role="alert"><strong>{t("app.context-not-found")}</strong><p>{t("app.context-mismatch")}</p><Link className="button button-secondary" href="/app">{t("app.return-home")}</Link></div> : children)}
        </main>
      </div>
    </div>
  );
}
