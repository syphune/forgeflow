export type DesktopPhase =
  | "REPOSITORY"
  | "WORKTREE"
  | "DIFF_REVIEW"
  | "APPROVAL_REQUIRED"
  | "READY_TO_RUN"
  | "RUNNING"
  | "RESULT_REVIEW"
  | "COMMIT_READY"
  | "PUSH_READY"
  | "COMPLETED"
  | "FAILED"
  | "RECOVERY_REQUIRED";

export type AgentInputSnapshot = {
  repositoryID: string;
  baseCommit: string;
  worktree: string;
  worktreeDiffHash: string;
  prompt: string;
  provider: string;
  specificationVersion: number;
  agentConfiguration?: Record<string, unknown>;
  toolPermissions?: string[];
  mcpPermissions?: string[];
  sandboxPolicy?: Record<string, unknown>;
  networkPolicy?: Record<string, unknown>;
  executionProfile?: string;
};

export type DesktopState = {
  phase: DesktopPhase;
  inputs: AgentInputSnapshot;
  runID?: string;
  approvalFingerprint?: string;
  approvedInputs?: AgentInputSnapshot;
  serverStatus?: string;
  checkpoint?: string;
  updatedAt: string;
};

export type DesktopEvent =
  | { type: "repository_selected"; repositoryID: string; baseCommit: string }
  | { type: "worktree_ready"; worktree: string; worktreeDiffHash: string }
  | { type: "diff_reviewed" }
  | { type: "approved"; fingerprint?: string }
  | { type: "input_changed"; inputs: AgentInputSnapshot }
  | { type: "agent_run_created"; runID: string; status: string }
  | { type: "agent_started"; status?: string }
  | { type: "heartbeat_lost"; status?: string }
  | { type: "reconciled"; status: string }
  | { type: "resumed" }
  | { type: "result"; success: boolean }
  | { type: "commit_ready" }
  | { type: "push_ready" }
  | { type: "completed" }
  | { type: "checkpoint"; name: string };

export function createDesktopState(inputs: AgentInputSnapshot): DesktopState {
  return {
    phase: "REPOSITORY",
    inputs,
    updatedAt: new Date().toISOString(),
  };
}

export function reduceDesktopState(
  current: DesktopState,
  event: DesktopEvent,
): DesktopState {
  const updatedAt = new Date().toISOString();
  switch (event.type) {
    case "repository_selected":
	  if (current.phase !== "REPOSITORY" && current.phase !== "WORKTREE") return current;
      return {
        ...current,
        phase: "WORKTREE",
        inputs: {
          ...current.inputs,
          repositoryID: event.repositoryID,
          baseCommit: event.baseCommit,
        },
        updatedAt,
      };
    case "worktree_ready":
	  if (current.phase !== "WORKTREE" && current.phase !== "DIFF_REVIEW") return current;
      return {
        ...current,
        phase: "DIFF_REVIEW",
        inputs: {
          ...current.inputs,
          worktree: event.worktree,
          worktreeDiffHash: event.worktreeDiffHash,
        },
        updatedAt,
      };
    case "diff_reviewed":
	  if (current.phase !== "DIFF_REVIEW") return current;
      return { ...current, phase: "APPROVAL_REQUIRED", updatedAt };
    case "approved":
      if (current.phase !== "APPROVAL_REQUIRED" && current.phase !== "READY_TO_RUN") {
        return current;
      }
      return {
        ...current,
        phase: "READY_TO_RUN",
        approvalFingerprint: event.fingerprint,
        approvedInputs: clone(current.inputs),
        updatedAt,
      };
    case "input_changed":
      if (sameInputs(current.inputs, event.inputs)) return current;
      return {
        ...current,
        inputs: event.inputs,
        phase: current.phase === "RUNNING" ? "RECOVERY_REQUIRED" : "APPROVAL_REQUIRED",
        approvalFingerprint: undefined,
        approvedInputs: undefined,
        updatedAt,
      };
    case "agent_run_created":
      return { ...current, runID: event.runID, serverStatus: event.status, updatedAt };
    case "agent_started":
	  if (current.phase !== "READY_TO_RUN") return current;
      return { ...current, phase: "RUNNING", serverStatus: event.status ?? "RUNNING", updatedAt };
    case "heartbeat_lost":
	  if (current.phase !== "RUNNING") return current;
      return {
        ...current,
        phase: "RECOVERY_REQUIRED",
        serverStatus: event.status ?? current.serverStatus,
        updatedAt,
      };
    case "reconciled":
      return {
        ...current,
        phase: event.status === "INTERRUPTED" ? "RECOVERY_REQUIRED" : current.phase,
        serverStatus: event.status,
        updatedAt,
      };
    case "resumed":
      if (current.phase !== "RECOVERY_REQUIRED" || !current.approvedInputs || !sameInputs(current.inputs, current.approvedInputs)) {
        return current;
      }
      return { ...current, phase: "READY_TO_RUN", updatedAt };
    case "result":
	  if (current.phase !== "RUNNING") return current;
      return { ...current, phase: event.success ? "RESULT_REVIEW" : "FAILED", updatedAt };
    case "commit_ready":
	  if (current.phase !== "RESULT_REVIEW") return current;
      return { ...current, phase: "COMMIT_READY", updatedAt };
    case "push_ready":
	  if (current.phase !== "COMMIT_READY") return current;
      return { ...current, phase: "PUSH_READY", updatedAt };
    case "completed":
	  if (current.phase !== "PUSH_READY") return current;
      return { ...current, phase: "COMPLETED", updatedAt };
    case "checkpoint":
      return { ...current, checkpoint: event.name, updatedAt };
  }
}

export function sameInputs(left: AgentInputSnapshot, right: AgentInputSnapshot): boolean {
  return stableSerialize(left) === stableSerialize(right);
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function stableSerialize(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableSerialize).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.entries(value as Record<string, unknown>)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => `${JSON.stringify(key)}:${stableSerialize(item)}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}
