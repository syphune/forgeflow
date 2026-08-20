"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { DragEvent, FormEvent, KeyboardEvent, MouseEvent, useEffect, useMemo, useState } from "react";
import type { AuthorizationContext, BoardColumn, BoardResponse, MoveWorkItemRequest, WorkItem, WorkItemList, WorkItemType, WorkflowResponse } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";
import { apiErrorMessage, browserAPI } from "../app/api";
import { ForgeSelect } from "../app/forge-select";
import { rolesForCapabilities } from "../work-items/workflow-responsibilities";
import { WorkItemModal } from "../work-items/work-item-modal";
import { WorkItemSurface } from "../work-items/work-item-surface";
import { AutonomousIntake } from "./autonomous-intake";
import { uiTone } from "../app/types";

type Props = { projectID: string; basePath: string; createOnMount?: boolean };

const types: WorkItemType[] = ["TASK", "STORY", "BUG", "EPIC", "SUB_TASK"];
const priorities = ["HIGHEST", "HIGH", "MEDIUM", "LOW", "LOWEST"];

function label(value: string | undefined) {
  return statusLabel(value ?? "");
}

export function BacklogPage({ projectID, basePath, createOnMount = false }: Props) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const client = useMemo(() => browserAPI(projectID), [projectID]);
  const view = searchParams.get("view") === "board" ? "board" : "list";
  const query = searchParams.get("q") ?? "";
  const status = searchParams.get("status") ?? "";
  const type = searchParams.get("type") ?? "";
  const priority = searchParams.get("priority") ?? "";
  const cursor = searchParams.get("cursor") ?? "";
  const selectedItemID = searchParams.get("item") ?? "";
  const [items, setItems] = useState<WorkItem[]>([]);
  const [board, setBoard] = useState<BoardResponse | null>(null);
  const [workflow, setWorkflow] = useState<WorkflowResponse | null>(null);
  const [authorization, setAuthorization] = useState<AuthorizationContext | null>(null);
  const [authorizationLoading, setAuthorizationLoading] = useState(true);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [moveError, setMoveError] = useState("");
  const [pendingMoveID, setPendingMoveID] = useState("");
  const [draggingID, setDraggingID] = useState("");
  const [keyboardGrabbedID, setKeyboardGrabbedID] = useState("");
  const [announcement, setAnnouncement] = useState("");
  const [reloadNonce, setReloadNonce] = useState(0);
  const [createOpen, setCreateOpen] = useState(createOnMount || searchParams.get("create") === "1");
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState("");
  const [newTitle, setNewTitle] = useState("");
  const [newType, setNewType] = useState<WorkItemType>("TASK");
  const [newDescription, setNewDescription] = useState("");
  const [restoreNonce, setRestoreNonce] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    queueMicrotask(() => {
      if (!controller.signal.aborted) {
        setBusy(true);
        setError("");
      }
    });
    if (view === "board") {
      void Promise.all([
        client.request<BoardResponse>("/boards/current", { projectID, signal: controller.signal }),
        client.request<WorkflowResponse>("/workflows/current", { projectID, signal: controller.signal }),
      ]).then(([loadedBoard, loadedWorkflow]) => {
        setBoard(loadedBoard);
        setWorkflow(loadedWorkflow);
        setItems(loadedBoard.columns.flatMap((column) => column.items));
        setNextCursor(null);
      }).catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(apiErrorMessage(cause));
      }).finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    } else {
      const params = new URLSearchParams();
      params.set("sort", "backlog");
      params.set("limit", "100");
      if (cursor) params.set("cursor", cursor);
      if (query) params.set("q", query);
      if (status) params.set("status", status);
      if (type) params.set("type", type);
      if (priority) params.set("priority", priority);
      void client.request<WorkItemList>(`/work-items?${params.toString()}`, { projectID, signal: controller.signal })
        .then((result) => {
          setItems(result.items ?? []);
          setNextCursor(result.next_cursor ?? null);
          setBoard(null);
          setWorkflow(null);
        })
        .catch((cause: unknown) => {
          if (!controller.signal.aborted) setError(apiErrorMessage(cause));
        })
        .finally(() => {
          if (!controller.signal.aborted) setBusy(false);
        });
    }
    return () => controller.abort();
  }, [client, cursor, priority, projectID, query, reloadNonce, status, type, view]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) {
        setAuthorization(null);
        setAuthorizationLoading(true);
      }
    });
    void client.getProjectAuthorization(projectID).then((loaded) => {
      if (active) setAuthorization(loaded);
    }).catch(() => {
      if (active) setAuthorization(null);
    }).finally(() => {
      if (active) setAuthorizationLoading(false);
    });
    return () => {
      active = false;
    };
  }, [client, projectID]);

  function updateQuery(key: string, value: string) {
    const next = new URLSearchParams(typeof window === "undefined" ? searchParams.toString() : window.location.search);
    if (key === "view" && value === "list") next.delete(key);
    else if (value) next.set(key, value);
    else next.delete(key);
    if (key !== "cursor") next.delete("cursor");
    router.replace(`${pathname}?${next.toString()}`, { scroll: false });
  }

  function openCreate() {
    if (!canCreate) return;
    setCreateError("");
    setCreateOpen(true);
    const next = new URLSearchParams(searchParams.toString());
    next.set("create", "1");
    router.replace(`${pathname}?${next.toString()}`, { scroll: false });
  }

  function openWorkItem(itemID: string) {
    const next = new URLSearchParams(searchParams.toString());
    next.delete("create");
    next.set("item", itemID);
    router.push(`${pathname}?${next.toString()}`, { scroll: false });
  }

  const closeWorkItemHref = (() => {
    const next = new URLSearchParams(searchParams.toString());
    next.delete("item");
    const query = next.toString();
    return `${pathname}${query ? `?${query}` : ""}`;
  })();

  function closeCreate() {
    setCreateOpen(false);
    const next = new URLSearchParams(searchParams.toString());
    next.delete("create");
    router.replace(`${pathname}?${next.toString()}`, { scroll: false });
  }

  async function createWorkItem(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!newTitle.trim() || !canCreate) return;
    setCreateBusy(true);
    setCreateError("");
    try {
      const created = await client.createWorkItem({ project_id: projectID, type: newType, title: newTitle.trim(), description: newDescription }, { projectID, idempotencyKey: crypto.randomUUID() });
      setItems((current) => [...current, created]);
      setNewTitle("");
      setNewDescription("");
      closeCreate();
      openWorkItem(created.id);
    } catch (cause) {
      setCreateError(apiErrorMessage(cause));
    } finally {
      setCreateBusy(false);
    }
  }

  const filteredItems = useMemo(() => items.filter((item) => matchesFilters(item, query, status, type, priority)), [items, priority, query, status, type]);
  const canCreate = authorization?.capabilities.includes("work_item.create") ?? false;
  const canStartAutonomous = authorization?.capabilities.includes("autonomous.start") ?? false;
  const createRoles = rolesForCapabilities(["work_item.create"]).map(label).join(", ");
  const hasFilters = Boolean(query || status || type || priority);

  useEffect(() => {
    const requestRestore = () => {
      setRestoreNonce((value) => value + 1);
    };
    window.addEventListener("forgeflow:restore-work-item", requestRestore);
    window.addEventListener("popstate", requestRestore);
    return () => {
      window.removeEventListener("forgeflow:restore-work-item", requestRestore);
      window.removeEventListener("popstate", requestRestore);
    };
  }, []);

  useEffect(() => {
    const restoreID = window.sessionStorage.getItem("forgeflow:restore-work-item");
    if (!restoreID || busy || document.querySelector('[role="dialog"][aria-modal="true"]')) return;
    const trigger = Array.from(document.querySelectorAll<HTMLElement>("[data-work-item-id]"))
      .find((candidate) => candidate.dataset.workItemId === restoreID);
    if (!trigger) return;
    const timer = window.setTimeout(() => {
      if (window.sessionStorage.getItem("forgeflow:restore-work-item") !== restoreID || document.querySelector('[role="dialog"][aria-modal="true"]')) return;
      const currentTrigger = Array.from(document.querySelectorAll<HTMLElement>("[data-work-item-id]"))
        .find((candidate) => candidate.dataset.workItemId === restoreID);
      if (!currentTrigger) return;
      currentTrigger.focus();
      window.sessionStorage.removeItem("forgeflow:restore-work-item");
    }, 50);
    return () => window.clearTimeout(timer);
  }, [busy, filteredItems, restoreNonce, selectedItemID]);

  const columns = useMemo(() => {
    if (board) return board.columns.map((column) => ({ ...column, items: column.items.filter((item) => matchesFilters(item, query, status, type, priority)) }));
    const grouped = new Map<string, WorkItem[]>();
    for (const item of filteredItems) grouped.set(item.status, [...(grouped.get(item.status) ?? []), item]);
    return [...grouped.entries()].map(([column, columnItems]) => ({ status: column, name: label(column), position: 0, ordering_version: 1, items: columnItems }));
  }, [board, filteredItems, priority, query, status, type]);

  async function moveCard(item: WorkItem, destination: BoardColumn, destinationIndex: number) {
    if (!board || pendingMoveID) return;
    const source = board.columns.find((column) => column.items.some((candidate) => candidate.id === item.id));
    if (!source) return;
    const destinationItems = destination.items.filter((candidate) => candidate.id !== item.id);
    const index = Math.max(0, Math.min(destinationIndex, destinationItems.length));
    const beforeID = destinationItems[index - 1]?.id ?? "";
    const afterID = destinationItems[index]?.id ?? "";
    const transition = source.status === destination.status ? undefined : workflow?.transitions.find((candidate) => candidate.from_status === source.status && candidate.to_status === destination.status);
    if (source.status !== destination.status && !transition) {
      setMoveError(t("backlog.transition-missing", { from: label(source.status), to: label(destination.status) }));
      return;
    }
    const request: MoveWorkItemRequest = {
      target_status: destination.status,
      transition_key: transition?.key,
      before_id: beforeID || undefined,
      after_id: afterID || undefined,
      expected_version: item.version,
      expected_source_ordering_version: source.ordering_version,
      expected_destination_ordering_version: destination.ordering_version,
    };
    const previous = board;
    const optimisticItem = { ...item, status: destination.status };
    const nextColumns = board.columns.map((column) => ({ ...column, items: column.items.filter((candidate) => candidate.id !== item.id) }));
    const nextDestination = nextColumns.find((column) => column.status === destination.status);
    if (!nextDestination) return;
    nextDestination.items.splice(index, 0, optimisticItem);
    setBoard({ ...board, columns: nextColumns });
    setItems(nextColumns.flatMap((column) => column.items));
    setMoveError("");
    setPendingMoveID(item.id);
    try {
      const result = await client.moveWorkItem(item.id, request, projectID);
      const reconciled = nextColumns.map((column) => {
        const itemsWithResult = column.items.map((candidate) => candidate.id === result.item.id ? result.item : candidate);
        if (column.status === source.status) return { ...column, items: itemsWithResult, ordering_version: result.source_ordering_version };
        if (column.status === destination.status) return { ...column, items: itemsWithResult, ordering_version: result.destination_ordering_version };
        return { ...column, items: itemsWithResult };
      });
      setBoard({ ...previous, columns: reconciled });
      setItems(reconciled.flatMap((column) => column.items));
      setAnnouncement(t("backlog.item-moved", { key: result.item.key, status: label(destination.status) }));
    } catch (cause) {
      setBoard(previous);
      setItems(previous.columns.flatMap((column) => column.items));
      setMoveError(apiErrorMessage(cause));
      setAnnouncement(t("backlog.move-restored"));
      setReloadNonce((value) => value + 1);
    } finally {
      setPendingMoveID("");
    }
  }

  function handleDrop(event: DragEvent<HTMLElement>, destination: BoardColumn, index: number) {
    event.preventDefault();
    const item = board?.columns.flatMap((column) => column.items).find((candidate) => candidate.id === draggingID);
    if (item) void moveCard(item, destination, index);
    setDraggingID("");
  }

  function handleKeyboard(event: KeyboardEvent<HTMLAnchorElement>, item: WorkItem, columnIndex: number, itemIndex: number) {
    if (!board) return;
    if (event.key === "Enter") {
      event.preventDefault();
      openWorkItem(item.id);
      return;
    }
    if (event.key === " ") {
      event.preventDefault();
      setKeyboardGrabbedID((current) => current === item.id ? "" : item.id);
      setAnnouncement(keyboardGrabbedID === item.id ? t("backlog.item-dropped", { key: item.key }) : t("backlog.item-grabbed", { key: item.key }));
      return;
    }
    if (keyboardGrabbedID !== item.id) return;
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight" && event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    const destinationColumnIndex = event.key === "ArrowLeft" ? columnIndex - 1 : event.key === "ArrowRight" ? columnIndex + 1 : columnIndex;
    const destination = board.columns[destinationColumnIndex];
    if (!destination) return;
    const targetIndex = event.key === "ArrowUp" ? itemIndex - 1 : event.key === "ArrowDown" ? itemIndex + 1 : destination.items.length;
    if (destination.status === item.status && (targetIndex < 0 || targetIndex >= destination.items.length)) return;
    void moveCard(item, destination, Math.max(0, targetIndex));
  }

  return (
    <section className="app-v2-page" aria-labelledby="backlog-heading">
      <div className="app-v2-page-heading"><div><p className="eyebrow">{t("backlog.eyebrow")}</p><h2 id="backlog-heading">Backlog</h2><p>{t("backlog.description")}</p>{!authorizationLoading && authorization && !canCreate ? <p className="app-v2-page-permission-note">{t("backlog.create-restricted", { roles: createRoles })}</p> : null}</div></div>
      {!authorizationLoading && canCreate && canStartAutonomous ? <AutonomousIntake projectID={projectID} basePath={basePath} canStart={canStartAutonomous} onCreated={() => setReloadNonce((value) => value + 1)} /> : null}
      <div className="app-v2-toolbar" role="search"><label className="app-v2-search"><span aria-hidden="true">⌕</span><input value={query} onChange={(event) => updateQuery("q", event.target.value)} placeholder={t("backlog.search")} aria-label={t("backlog.search")} /></label><ForgeSelect ariaLabel={t("backlog.all-statuses")} value={status} options={[{ value: "", label: t("backlog.all-statuses") }, ...[...new Set(items.map((item) => item.status))].map((item) => ({ value: item, label: label(item) }))]} placeholder={t("backlog.all-statuses")} onChange={(value) => updateQuery("status", value)} /><ForgeSelect ariaLabel={t("backlog.all-types")} value={type} options={[{ value: "", label: t("backlog.all-types") }, ...types.map((item) => ({ value: item, label: label(item) }))]} placeholder={t("backlog.all-types")} onChange={(value) => updateQuery("type", value)} /><ForgeSelect ariaLabel={t("backlog.all-priorities")} value={priority} options={[{ value: "", label: t("backlog.all-priorities") }, ...priorities.map((item) => ({ value: item, label: label(item) }))]} placeholder={t("backlog.all-priorities")} onChange={(value) => updateQuery("priority", value)} /><div className="app-v2-view-toggle" role="group" aria-label={t("backlog.view")}><button className={view === "list" ? "is-active" : ""} type="button" onClick={() => updateQuery("view", "list")}>{t("backlog.list")}</button><button className={view === "board" ? "is-active" : ""} type="button" onClick={() => updateQuery("view", "board")}>{t("backlog.board")}</button></div></div>
      {error ? <div className="app-v2-error-panel" role="alert"><strong>{t("backlog.load-error")}</strong><span>{error}</span><button type="button" onClick={() => setReloadNonce((value) => value + 1)}>{t("app.retry")}</button></div> : null}
      {moveError ? <div className="app-v2-error-panel" role="alert"><strong>{t("backlog.move-error")}</strong><span>{moveError}</span><button type="button" onClick={() => setReloadNonce((value) => value + 1)}>{t("backlog.reload-board")}</button></div> : null}
      <p className="app-v2-sr-only" aria-live="polite">{announcement}</p>
      {busy ? <div className="app-v2-loading" aria-busy="true"><span /><span /><span /></div> : null}
      {!busy && !error && filteredItems.length === 0 ? <div className="app-v2-empty"><strong>{t(hasFilters ? "backlog.no-match" : "backlog.no-items")}</strong><p>{t(hasFilters ? "backlog.create-first" : "backlog.no-items-description")}</p>{canCreate ? <button className="button button-secondary" type="button" onClick={openCreate}>{t("backlog.create")}</button> : null}</div> : null}
      {!busy && !error && filteredItems.length > 0 && view === "list" ? <div className="app-v2-list" role="list">{filteredItems.map((item) => <WorkItemRow key={item.id} item={item} href={`${basePath}/work-items/${item.id}`} onOpen={openWorkItem} />)}</div> : null}
      {!busy && !error && view === "board" && board ? <div className="app-v2-board" aria-label={t("backlog.board")}>{columns.map((column, columnIndex) => <section className="app-v2-column" key={column.status} aria-labelledby={`column-${column.status}`} onDragOver={(event) => event.preventDefault()} onDrop={(event) => handleDrop(event, column, column.items.length)}><div className="app-v2-column-heading"><h3 id={`column-${column.status}`}>{label(column.status)}</h3><span>{column.items.length}</span></div>{column.items.map((item, itemIndex) => <WorkItemCard key={item.id} item={item} href={`${basePath}/work-items/${item.id}`} onOpen={openWorkItem} pending={pendingMoveID === item.id} grabbed={keyboardGrabbedID === item.id} onDragStart={(event) => { event.dataTransfer.effectAllowed = "move"; setDraggingID(item.id); }} onDragEnd={() => setDraggingID("")} onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); handleDrop(event, column, itemIndex); }} onKeyDown={(event) => handleKeyboard(event, item, columnIndex, itemIndex)} />)}</section>)}</div> : null}
      {view === "list" && nextCursor ? <button className="button button-secondary app-v2-load-more" type="button" onClick={() => updateQuery("cursor", nextCursor)}>{t("backlog.load-more")}</button> : null}
      {createOpen && canCreate ? <div className="app-v2-dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) closeCreate(); }}><form className="app-v2-dialog" onSubmit={createWorkItem} role="dialog" aria-modal="true" aria-labelledby="create-heading"><div className="app-v2-dialog-heading"><div><p className="eyebrow">{t("backlog.new-eyebrow")}</p><h2 id="create-heading">{t("backlog.new-title")}</h2></div><button type="button" className="app-v2-icon-button" onClick={closeCreate} aria-label={t("backlog.close")}>×</button></div><label>{t("backlog.title")}<input autoFocus value={newTitle} onChange={(event) => setNewTitle(event.target.value)} maxLength={200} placeholder={t("backlog.title-placeholder")} /></label><div className="app-v2-dialog-field"><span>{t("backlog.type")}</span><ForgeSelect ariaLabel={t("backlog.type")} value={newType} options={types.map((item) => ({ value: item, label: label(item) }))} placeholder={t("backlog.select-type")} onChange={(value) => setNewType(value as WorkItemType)} /></div><label>{t("backlog.context")}<textarea value={newDescription} onChange={(event) => setNewDescription(event.target.value)} rows={4} placeholder={t("backlog.context-placeholder")} /></label>{createError ? <p className="app-v2-form-error" role="alert">{createError}</p> : null}<div className="app-v2-dialog-actions"><button className="button button-secondary" type="button" onClick={closeCreate}>{t("backlog.cancel")}</button><button className="button button-primary" type="submit" disabled={createBusy || !newTitle.trim()}>{createBusy ? t("backlog.creating") : t("backlog.create")}</button></div></form></div> : null}
      {selectedItemID ? <WorkItemModal restoreItemID={selectedItemID} closeHref={closeWorkItemHref}><WorkItemSurface projectID={projectID} itemID={selectedItemID} basePath={basePath} modal /></WorkItemModal> : null}
    </section>
  );
}

function matchesFilters(item: WorkItem, query: string, status: string, type: string, priority: string) {
  const normalizedQuery = query.trim().toLowerCase();
  return (!normalizedQuery || `${item.title} ${item.description}`.toLowerCase().includes(normalizedQuery)) && (!status || item.status === status) && (!type || item.type === type) && (!priority || item.priority === priority);
}

function WorkItemMeta({ item }: { item: WorkItem }) {
  return <div className="app-v2-item-meta"><span className="app-v2-key">{item.key}</span><span className={`app-v2-chip is-${uiTone(item.type)}`}>{label(item.type)}</span><span className={`app-v2-chip is-${uiTone(item.priority)}`}>{label(item.priority)}</span>{item.assignee_id ? <span className="app-v2-assignee is-success">{t("work.assigned")}</span> : null}</div>;
}

function WorkItemRow({ item, href, onOpen }: { item: WorkItem; href: string; onOpen: (itemID: string) => void }) {
  function handleClick(event: MouseEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpen(item.id);
  }

  return <Link className="app-v2-list-row" data-work-item-id={item.id} href={href} onClick={handleClick}><div><WorkItemMeta item={item} /><strong>{item.title}</strong></div><span className={`app-v2-status-pill is-${uiTone(item.status)}`}>{label(item.status)} <span aria-hidden="true">→</span></span></Link>;
}

type CardProps = {
  item: WorkItem;
  href: string;
  onOpen: (itemID: string) => void;
  pending?: boolean;
  grabbed?: boolean;
  onDragStart?: (event: DragEvent<HTMLAnchorElement>) => void;
  onDragEnd?: () => void;
  onDragOver?: (event: DragEvent<HTMLAnchorElement>) => void;
  onDrop?: (event: DragEvent<HTMLAnchorElement>) => void;
  onKeyDown?: (event: KeyboardEvent<HTMLAnchorElement>) => void;
};

function WorkItemCard({ item, href, onOpen, pending = false, grabbed = false, onDragStart, onDragEnd, onDragOver, onDrop, onKeyDown }: CardProps) {
  function handleClick(event: MouseEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpen(item.id);
  }

  return <Link className={`app-v2-card ${pending ? "is-pending" : ""} ${grabbed ? "is-grabbed" : ""}`} data-work-item-id={item.id} href={href} draggable={Boolean(onDragStart)} aria-grabbed={grabbed} onClick={handleClick} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragOver={onDragOver} onDrop={onDrop} onKeyDown={onKeyDown}><WorkItemMeta item={item} /><strong>{item.title}</strong><p>{item.description || t("work.no-context")}</p><span className="app-v2-card-footer">v{item.version} <span aria-hidden="true">↗</span>{pending ? ` · ${t("work.saving")}` : grabbed ? ` · ${t("work.grabbed")}` : ""}</span></Link>;
}
