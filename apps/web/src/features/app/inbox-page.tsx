"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import type { Notification, Organization, Project, Workspace } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "./api";

export function InboxPage() {
  const client = useMemo(() => browserAPI(), []);
  const [items, setItems] = useState<Notification[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [markAllBusy, setMarkAllBusy] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([
      client.listNotifications(50),
      client.listProjects(),
      client.listWorkspaces(),
      client.listOrganizations(),
    ]).then(([notifications, projectResult, workspaceResult, organizationResult]) => {
      if (controller.signal.aborted) return;
      setItems(notifications.items ?? []);
      setProjects(projectResult.items ?? []);
      setWorkspaces(workspaceResult.items ?? []);
      setOrganizations(organizationResult.items ?? []);
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) setError(apiErrorMessage(cause));
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return () => controller.abort();
  }, [client]);

  function notificationHref(item: Notification) {
    if (item.resource_type !== "WORK_ITEM" || !item.resource_id || !item.project_id) return "";
    const project = projects.find((entry) => entry.id === item.project_id);
    if (!project) return "";
    const organization = organizations.find((entry) => entry.id === project.organization_id);
    const workspace = workspaces.find((entry) => entry.id === project.workspace_id);
    if (!organization || !workspace) return "";
    return `/app/orgs/${organization.id}/workspaces/${workspace.id}/projects/${project.id}/backlog?item=${encodeURIComponent(item.resource_id)}`;
  }

  async function markRead(item: Notification) {
    if (item.read_at || busyID) return;
    setBusyID(item.id);
    setError("");
    try {
      await client.markNotificationRead(item.id);
      setItems((current) => current.map((entry) => entry.id === item.id ? { ...entry, read_at: new Date().toISOString() } : entry));
      window.dispatchEvent(new Event("forgeflow:notifications-changed"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function markAllRead() {
    if (markAllBusy || !items.some((item) => !item.read_at)) return;
    setMarkAllBusy(true);
    setError("");
    setMessage("");
    try {
      await client.markAllNotificationsRead();
      const now = new Date().toISOString();
      setItems((current) => current.map((item) => ({ ...item, read_at: item.read_at ?? now })));
      setMessage(t("inbox.marked-all"));
      window.dispatchEvent(new Event("forgeflow:notifications-changed"));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setMarkAllBusy(false);
    }
  }

  const unread = items.filter((item) => !item.read_at).length;
  const dateLocale = typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN";

  return <section className="app-v2-page" aria-labelledby="inbox-heading"><div className="app-v2-page-heading"><div><p className="eyebrow">{t("inbox.eyebrow")}</p><h2 id="inbox-heading">{t("inbox.title")}</h2><p>{t("inbox.description")}</p></div><div className="app-v2-page-actions"><button className="button button-secondary" type="button" onClick={() => void markAllRead()} disabled={markAllBusy || !unread}>{markAllBusy ? t("inbox.marking") : t("inbox.mark-all")}</button></div></div>{message ? <p className="app-v2-action-status" role="status">{message}</p> : null}{error ? <div className="app-v2-error-panel" role="alert"><strong>{t("inbox.load-error")}</strong><span>{error}</span><button type="button" onClick={() => window.location.reload()}>{t("app.retry")}</button></div> : null}{loading ? <div className="app-v2-loading" aria-busy="true" role="status"><span /><span /><span /><p>{t("inbox.loading")}</p></div> : null}{!loading && !error && !items.length ? <div className="app-v2-empty"><strong>{t("inbox.caught-up")}</strong><p>{t("inbox.empty-description")}</p></div> : null}<div className="app-v2-notifications">{items.map((item) => { const href = notificationHref(item); const content = <><div className="app-v2-notification-copy"><strong>{item.title}</strong><p>{item.body}</p><small>{href ? t("inbox.open-resource") : t("inbox.no-resource")}</small></div><div className="app-v2-notification-meta"><time dateTime={item.created_at}>{new Intl.DateTimeFormat(dateLocale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(item.created_at))}</time>{!item.read_at ? <button className="button button-quiet" type="button" onClick={(event) => { event.preventDefault(); event.stopPropagation(); void markRead(item); }} disabled={busyID === item.id}>{busyID === item.id ? t("inbox.marking") : t("inbox.mark-read")}</button> : <span className="app-v2-read-label">{t("inbox.read")}</span>}</div></>; return href ? <Link className={`app-v2-notification ${item.read_at ? "is-read" : ""}`} key={item.id} href={href} onClick={() => void markRead(item)}>{content}</Link> : <article className={`app-v2-notification ${item.read_at ? "is-read" : ""}`} key={item.id}>{content}</article>; })}</div></section>;
}
