"use client";

import { ReactNode, useCallback, useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { translate as t } from "@forgeflow/ui";

export function WorkItemModal({ children, restoreItemID, closeHref }: { children: ReactNode; restoreItemID: string; closeHref?: string }) {
  const router = useRouter();
  const closeRef = useRef<HTMLButtonElement>(null);
  const modalRef = useRef<HTMLDivElement>(null);
  const restoreRequested = useRef(false);

  const scheduleRestoreFocus = useCallback(() => {
    restoreRequested.current = true;
    window.sessionStorage.setItem("forgeflow:restore-work-item", restoreItemID);
    window.setTimeout(() => window.dispatchEvent(new Event("forgeflow:restore-work-item")), 0);
  }, [restoreItemID]);

  const close = useCallback(() => {
    scheduleRestoreFocus();
    if (closeHref) router.replace(closeHref, { scroll: false });
    else router.back();
  }, [closeHref, router, scheduleRestoreFocus]);

  useEffect(() => {
    closeRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        close();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (!restoreRequested.current) scheduleRestoreFocus();
    };
  }, [close, scheduleRestoreFocus]);

  return (
    <div className="app-v2-modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) close(); }}>
      <div ref={modalRef} className="app-v2-modal" role="dialog" aria-modal="true" aria-labelledby="work-item-heading" onKeyDown={(event) => {
        if (event.key !== "Tab") return;
        const focusable = Array.from(modalRef.current?.querySelectorAll<HTMLElement>("button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex='-1'])") ?? []);
        if (!focusable.length) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }}>
        <button ref={closeRef} className="app-v2-modal-close" type="button" onClick={close} aria-label={t("work.close")}>×</button>
        {children}
      </div>
    </div>
  );
}
