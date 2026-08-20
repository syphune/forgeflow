"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { ReactNode, useEffect, useMemo, useState } from "react";
import type { Actor, AuthorizationContext, Organization, Project, Workspace } from "@forgeflow/api-client";
import { APIError } from "@forgeflow/api-client";
import { translate as t, type Locale } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "./api";
import { ForgeSelect } from "./forge-select";
import { ThemeControl } from "./theme-control";

type Props = { children: ReactNode };
type SessionState = "checking" | "signed-in" | "signed-out";

function pathContext(pathname: string): { organizationID?: string; workspaceID?: string; projectID?: string; scope?: "organization" | "workspace" | "project" } {
  const project = pathname.match(/\/app\/orgs\/([^/]+)\/workspaces\/([^/]+)\/projects\/([^/]+)/);
  if (project) return { organizationID: project[1], workspaceID: project[2], projectID: project[3], scope: "project" };
  const workspace = pathname.match(/\/app\/orgs\/([^/]+)\/workspaces\/([^/]+)\/settings/);
  if (workspace) return { organizationID: workspace[1], workspaceID: workspace[2], scope: "workspace" };
  const organization = pathname.match(/\/app\/orgs\/([^/]+)\/settings/);
  if (organization) return { organizationID: organization[1], scope: "organization" };
  return {};
}

export function AppShell({ children }: Props) {
  const pathname = usePathname();
  const router = useRouter();
  const routeContext = useMemo(() => pathContext(pathname), [pathname]);
  const client = useMemo(() => browserAPI(routeContext.projectID), [routeContext.projectID]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [actor, setActor] = useState<Actor | null>(null);
  const [sessionState, setSessionState] = useState<SessionState>("checking");
  const [contextLoading, setContextLoading] = useState(true);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
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
      try {
        const currentActor = await client.request<Actor>("/me", { signal: controller.signal });
        if (!active) return;
        setActor(currentActor);
        setSessionState("signed-in");
        const [orgs, spaces, projectList, count, auth] = await Promise.all([
          client.listOrganizations(),
          client.listWorkspaces(),
          client.listProjects(),
          client.getUnreadNotificationCount(),
          routeContext.scope === "project" && routeContext.projectID
            ? client.getProjectAuthorization(routeContext.projectID)
            : routeContext.scope === "workspace" && routeContext.workspaceID
              ? client.getWorkspaceAuthorization(routeContext.workspaceID)
              : routeContext.scope === "organization" && routeContext.organizationID
                ? client.getOrganizationAuthorization(routeContext.organizationID)
                : Promise.resolve(null),
        ]);
        if (!active) return;
        setOrganizations(orgs.items ?? []);
        setWorkspaces(spaces.items ?? []);
        setProjects(projectList.items ?? []);
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
  const projectOptions = projects.map((project) => {
    const organization = organizations.find((item) => item.id === project.organization_id);
    const workspace = workspaces.find((item) => item.id === project.workspace_id);
    const scope = [organization?.display_name, workspace?.display_name].filter(Boolean).join(" / ");
    return { value: project.id, label: `${scope ? `${scope} · ` : ""}${project.key} · ${project.display_name}` };
  });
  const canonicalContextMismatch = Boolean(
    authorization && (
      authorization.scope !== routeContext.scope ||
      authorization.organization_id !== routeContext.organizationID ||
      (routeContext.scope !== "organization" && authorization.workspace_id !== routeContext.workspaceID) ||
      (routeContext.scope === "project" && authorization.project_id !== routeContext.projectID)
    ),
  );
  const baseProjectPath = !canonicalContextMismatch && routeContext.organizationID && routeContext.workspaceID && routeContext.projectID
    ? `/app/orgs/${routeContext.organizationID}/workspaces/${routeContext.workspaceID}/projects/${routeContext.projectID}`
    : "/app";

  function selectProject(projectID: string) {
    const project = projects.find((item) => item.id === projectID);
    if (!project) return;
    const organizationID = routeContext.organizationID ?? project.organization_id ?? organizations[0]?.id;
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
            <ForgeSelect ariaLabel={t("nav.project-switcher")} disabled={contextLoading || sessionState !== "signed-in"} value={routeContext.projectID ?? ""} options={projectOptions} placeholder={t("app.choose-project")} searchable onChange={selectProject} />
            <small className="app-v2-context-hint">{selectedOrganization?.display_name ?? t("nav.organization")} / {selectedWorkspace?.display_name ?? t("nav.workspace")}</small>
          </div>
        </div>
        <nav className="app-v2-nav" aria-label={t("nav.project-navigation")}>
          {routeContext.scope === "project" ? <>
            <Link className={pathname.endsWith("/backlog") || pathname.includes("/work-items/") ? "is-active" : ""} href={`${baseProjectPath}/backlog`} onClick={() => setMobileOpen(false)}><span>▦</span>Backlog</Link>
            <Link className={pathname.includes("/planning") ? "is-active" : ""} href={`${baseProjectPath}/planning`} onClick={() => setMobileOpen(false)}><span>◷</span>{t("nav.planning")}</Link>
            <Link className={pathname.includes("/repositories") ? "is-active" : ""} href={`${baseProjectPath}/repositories`} onClick={() => setMobileOpen(false)}><span>⌘</span>{t("nav.repositories")}</Link>
          </> : null}
          <Link className={pathname.includes("/inbox") ? "is-active" : ""} href="/app/inbox" onClick={() => setMobileOpen(false)}><span>◌</span>{t("nav.inbox")}{unreadCount > 0 ? <b className="app-v2-badge">{unreadCount > 99 ? "99+" : unreadCount}</b> : null}</Link>
        </nav>
        <div className="app-v2-sidebar-bottom">
          <div className="app-v2-account" aria-live="polite">
            <span className="app-v2-account-label">{sessionState === "signed-in" ? `${t("auth.signed-in")}${actor?.source ? ` · ${actor.source}` : ""}` : sessionState === "signed-out" ? t("auth.required") : t("auth.checking")}</span>
            {sessionState === "signed-in" ? <button className="app-v2-sign-out" type="button" onClick={() => void signOut()} disabled={logoutBusy}>{logoutBusy ? t("auth.logging-out") : t("auth.sign-out")}</button> : null}
            {sessionState === "signed-out" ? <a className="app-v2-sign-in" href={signInURL}>{t("auth.sign-in")} <span aria-hidden="true">↗</span></a> : null}
          </div>
          {routeContext.organizationID ? <Link href={`/app/orgs/${routeContext.organizationID}/settings/general`} onClick={() => setMobileOpen(false)}><span>◎</span>{t("nav.organization-settings")}</Link> : null}
          {routeContext.organizationID && routeContext.workspaceID ? <Link href={`/app/orgs/${routeContext.organizationID}/workspaces/${routeContext.workspaceID}/settings/general`} onClick={() => setMobileOpen(false)}><span>◫</span>{t("nav.workspace-settings")}</Link> : null}
          {routeContext.scope === "project" ? <Link href={`${baseProjectPath}/settings/workflow`} onClick={() => setMobileOpen(false)}><span>⚙</span>{t("nav.project-settings")}</Link> : null}
          <Link href="/app/account/developer" onClick={() => setMobileOpen(false)}><span>⌘</span>{t("nav.developer")}</Link>
          <div className="app-v2-locale-switcher" aria-label={t("nav.language")}>
            <span>{t("nav.language")}</span>
            <button type="button" className={locale === "vi" ? "is-active" : ""} aria-pressed={locale === "vi"} onClick={() => changeLocale("vi")}>VI</button>
            <button type="button" className={locale === "en" ? "is-active" : ""} aria-pressed={locale === "en"} onClick={() => changeLocale("en")}>EN</button>
          </div>
          <ThemeControl className="app-v2-theme-switcher" />
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
