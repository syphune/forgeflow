"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import type { AuthorizationContext, Member, Organization, Workspace } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";

type OrganizationProps = { organizationID: string; section: string };
type WorkspaceProps = { organizationID: string; workspaceID: string; section: string };
const roles = ["owner", "admin", "project_manager", "developer", "qa", "viewer"];

function TenantFrame({ eyebrow, title, description, backHref, links, children }: { eyebrow: string; title: string; description: string; backHref: string; links: Array<{ href: string; label: string }>; children: React.ReactNode }) {
  const pathname = usePathname();
  const currentSection = pathname.split("/").pop() ?? "general";
  return <section className="app-v2-page" aria-labelledby="tenant-settings-heading"><div className="app-v2-page-heading"><div><p className="eyebrow">{eyebrow}</p><h2 id="tenant-settings-heading">{title}</h2><p>{description}</p></div><Link className="button button-secondary" href={backHref}>{t("settings.back-to-work")}</Link></div><nav className="app-v2-settings-tabs" aria-label={t("settings.tenant-navigation")}>{links.map((link) => <Link className={link.href.endsWith(`/${currentSection}`) ? "is-active" : ""} href={link.href} key={link.href}>{link.label}</Link>)}</nav>{children}</section>;
}

function ErrorNotice({ error, retry }: { error: string; retry?: () => void }) {
  return error ? <div className="app-v2-error-panel" role="alert"><strong>{t("settings.load-error")}</strong><span>{error}</span>{retry ? <button type="button" onClick={retry}>{t("app.retry")}</button> : null}</div> : null;
}

export function OrganizationSettingsPage({ organizationID, section }: OrganizationProps) {
  const client = useMemo(() => browserAPI(), []);
  const [organization, setOrganization] = useState<Organization | null>(null);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [login, setLogin] = useState("");
  const [role, setRole] = useState("developer");

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const [org, auth, memberResult] = await Promise.all([
        client.request<Organization>("/organizations/current"),
        client.getOrganizationAuthorization(organizationID),
        client.listOrganizationMembers(),
      ]);
      setOrganization(org); setAuthorization(auth); setMembers(memberResult.items ?? []);
    } catch (cause) { setError(apiErrorMessage(cause)); }
    finally { setLoading(false); }
  }, [client, organizationID]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);

  const canManage = authorization?.capabilities.includes("organization.manage") ?? false;

  async function addMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManage || busyID || !login.trim()) return;
    setBusyID("add"); setError("");
    try {
      const member = await client.addOrganizationMember({ login: login.trim(), role_key: role as "owner" | "admin" | "project_manager" | "developer" | "qa" | "viewer" });
      setMembers((current) => [...current, member]); setLogin(""); setMessage(t("settings.organization-member-added"));
    } catch (cause) { setError(apiErrorMessage(cause)); }
    finally { setBusyID(""); }
  }

  async function updateMember(member: Member, nextRole: string) {
    if (!canManage || busyID) return;
    setBusyID(member.id); setError("");
    try {
      const updated = await client.setOrganizationMemberRole(member.id, { user_id: member.id, role_key: nextRole as "owner" | "admin" | "project_manager" | "developer" | "qa" | "viewer" });
      setMembers((current) => current.map((entry) => entry.id === updated.id ? updated : entry)); setMessage(t("settings.organization-member-updated"));
    } catch (cause) { setError(apiErrorMessage(cause)); }
    finally { setBusyID(""); }
  }

  async function removeMember(member: Member) {
    if (!canManage || busyID || !window.confirm(t("settings.remove-member-confirm", { name: member.display_name || member.login }))) return;
    setBusyID(member.id); setError("");
    try {
      await client.removeOrganizationMember(member.id);
      setMembers((current) => current.filter((entry) => entry.id !== member.id)); setMessage(t("settings.organization-member-removed"));
    } catch (cause) { setError(apiErrorMessage(cause)); }
    finally { setBusyID(""); }
  }

  const links = [{ href: `/app/orgs/${organizationID}/settings/general`, label: t("settings.organization-general") }, { href: `/app/orgs/${organizationID}/settings/members`, label: t("settings.organization-members") }];
  const content = section === "members" ? <div className="app-v2-settings-stack"><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.organization-members")}</h3><p>{t("settings.organization-members-description")}</p></div></div>{canManage ? <form className="app-v2-settings-create-grid" onSubmit={addMember}><label className="app-v2-dialog-field"><span>{t("settings.member-login")}</span><input value={login} onChange={(event) => setLogin(event.target.value)} placeholder="octocat" required /></label><label className="app-v2-dialog-field"><span>{t("settings.role")}</span><select value={role} onChange={(event) => setRole(event.target.value)}>{roles.map((entry) => <option value={entry} key={entry}>{statusLabel(entry)}</option>)}</select></label><button className="button button-primary" type="submit" disabled={busyID === "add"}>{busyID === "add" ? t("settings.saving") : t("settings.add-member")}</button></form> : <div className="app-v2-inline-note">{t("settings.read-only")}</div>}</div><div className="app-v2-surface-card app-v2-settings-card">{members.length ? <div className="app-v2-member-list">{members.map((member) => <div className="app-v2-member-row" key={member.id}><div><strong>{member.display_name || member.login}</strong><small>{member.login}</small></div><div className="app-v2-member-actions"><select aria-label={`${t("settings.role")} ${member.display_name || member.login}`} value={member.role_key} onChange={(event) => void updateMember(member, event.target.value)} disabled={!canManage || busyID === member.id}>{roles.map((entry) => <option value={entry} key={entry}>{statusLabel(entry)}</option>)}</select><button className="button button-quiet is-danger" type="button" onClick={() => void removeMember(member)} disabled={!canManage || busyID === member.id}>{t("settings.remove")}</button></div></div>)}</div> : <div className="app-v2-empty"><strong>{t("settings.no-members")}</strong><p>{t("settings.no-members-description")}</p></div>}</div></div> : <div className="app-v2-settings-stack"><div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{organization?.display_name ?? t("settings.organization-general")}</h3><p>{t("settings.organization-general-description")}</p></div></div><dl className="app-v2-detail-aside app-v2-tenant-facts"><div><dt>{t("settings.organization-id")}</dt><dd>{organization?.id}</dd></div><div><dt>{t("settings.member-count")}</dt><dd>{members.length}</dd></div></dl></div></div>;
  return <TenantFrame eyebrow={t("settings.organization-eyebrow")} title={section === "members" ? t("settings.organization-members") : t("settings.organization-general")} description={section === "members" ? t("settings.organization-members-description") : t("settings.organization-general-description")} backHref="/app" links={links}>{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : <><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{content}</>}</TenantFrame>;
}

export function WorkspaceSettingsPage({ organizationID, workspaceID, section }: WorkspaceProps) {
  const client = useMemo(() => browserAPI(), []);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [name, setName] = useState("");

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const [workspaces, auth] = await Promise.all([client.listWorkspaces(), client.getWorkspaceAuthorization(workspaceID)]);
      const found = workspaces.items.find((entry) => entry.id === workspaceID) ?? null;
      setWorkspace(found); setName(found?.display_name ?? ""); setAuthorization(auth);
    } catch (cause) { setError(apiErrorMessage(cause)); }
    finally { setLoading(false); }
  }, [client, workspaceID]);
  useEffect(() => { queueMicrotask(() => void load()); }, [load]);
  const canManage = authorization?.capabilities.includes("workspace.manage") ?? false;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManage || busy || !name.trim()) return;
    setBusy(true); setError("");
    try { const updated = await client.updateWorkspace(workspaceID, { display_name: name.trim() }); setWorkspace(updated); setMessage(t("settings.workspace-saved")); }
    catch (cause) { setError(apiErrorMessage(cause)); }
    finally { setBusy(false); }
  }

  const links = [{ href: `/app/orgs/${organizationID}/workspaces/${workspaceID}/settings/general`, label: t("settings.workspace-general") }, { href: `/app/orgs/${organizationID}/workspaces/${workspaceID}/settings/members`, label: t("settings.workspace-members") }];
  const content = section === "members" ? <div className="app-v2-surface-card app-v2-settings-card"><h3>{t("settings.workspace-members")}</h3><p className="app-v2-prose">{t("settings.workspace-members-inherited")}</p><Link className="button button-secondary" href={`/app/orgs/${organizationID}/settings/members`}>{t("settings.open-organization-members")}</Link></div> : <div className="app-v2-surface-card app-v2-settings-card"><div className="app-v2-card-heading"><div><h3>{t("settings.workspace-general")}</h3><p>{t("settings.workspace-general-description")}</p></div></div>{canManage ? <form className="app-v2-settings-create-grid" onSubmit={save}><label className="app-v2-dialog-field"><span>{t("settings.workspace-name")}</span><input value={name} onChange={(event) => setName(event.target.value)} required /></label><button className="button button-primary" type="submit" disabled={busy || !name.trim()}>{busy ? t("settings.saving") : t("settings.save")}</button></form> : <ReadOnlyTenantNotice />}</div>;
  return <TenantFrame eyebrow={t("settings.workspace-eyebrow")} title={section === "members" ? t("settings.workspace-members") : t("settings.workspace-general")} description={section === "members" ? t("settings.workspace-members-description") : t("settings.workspace-general-description")} backHref="/app" links={links}>{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /></div> : <><ErrorNotice error={error} retry={() => void load()} />{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{content}</>}</TenantFrame>;
}

function ReadOnlyTenantNotice() { return <div className="app-v2-inline-note">{t("settings.read-only")}</div>; }
