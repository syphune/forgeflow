"use client";

import type { AutonomousRun } from "@forgeflow/api-client";
import { statusLabel, translate as t } from "@forgeflow/ui";

const activeStatuses = new Set(["QUEUED", "INTAKE", "READY_FOR_EXECUTION", "EXECUTING", "FIXING", "DEPLOYING"]);

export function isAutonomousRunActive(status: string): boolean {
  return activeStatuses.has(status);
}

export function AutonomousRunProgress({ run }: { run: AutonomousRun }) {
  const running = isAutonomousRunActive(run.status);
  const waitingForReview = run.status === "WAITING_SPEC_REVIEW";
  const completed = run.status === "COMPLETED";
  const failed = run.status === "FAILED" || run.status === "CANCELLED";
  const message = running
    ? t("autonomous.progress-running")
    : waitingForReview
      ? t("autonomous.progress-review")
      : completed
        ? t("autonomous.progress-complete")
        : statusLabel(run.status);
  const detail = running
    ? t("autonomous.progress-updating")
    : t("autonomous.progress-status", { status: statusLabel(run.status) });
  const stateClass = running ? "is-running" : waitingForReview ? "is-review" : completed ? "is-completed" : failed ? "is-failed" : "";

  return <div className={`app-v2-run-progress ${stateClass}`} role="status" aria-live="polite" aria-busy={running}>
    <div className="app-v2-run-progress-heading">
      <span className="app-v2-run-progress-spinner" aria-hidden="true" />
      <strong>{message}</strong>
      <span className="app-v2-run-progress-phase">{statusLabel(run.phase)}</span>
    </div>
    <div className="app-v2-run-progress-track" role="progressbar" aria-label={t("autonomous.progress-label")} aria-valuemin={0} aria-valuemax={100} aria-valuenow={running ? undefined : 100} aria-valuetext={message}><span /></div>
    <small>{detail}</small>
  </div>;
}
