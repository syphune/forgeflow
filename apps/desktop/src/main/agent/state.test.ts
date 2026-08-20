import { describe, expect, it } from "vitest";
import { createDesktopState, reduceDesktopState, type AgentInputSnapshot } from "./state";

const inputs: AgentInputSnapshot = {
  repositoryID: "repo-1",
  baseCommit: "abc123",
  worktree: "/tmp/worktree",
  worktreeDiffHash: "diff-1",
  prompt: "Implement the approved change",
  provider: "codex",
  specificationVersion: 3,
  agentConfiguration: { model: "fast", temperature: 0 },
  toolPermissions: ["read", "write"],
};

describe("desktop AgentRun state machine", () => {
  it("requires reviewed diff and approval before running", () => {
    let state = createDesktopState(inputs);
    state = reduceDesktopState(state, { type: "repository_selected", repositoryID: "repo-1", baseCommit: "abc123" });
    state = reduceDesktopState(state, { type: "worktree_ready", worktree: inputs.worktree, worktreeDiffHash: inputs.worktreeDiffHash });
    state = reduceDesktopState(state, { type: "diff_reviewed" });
    expect(state.phase).toBe("APPROVAL_REQUIRED");
    state = reduceDesktopState(state, { type: "approved", fingerprint: "server-fingerprint" });
    expect(state.phase).toBe("READY_TO_RUN");
    expect(state.approvalFingerprint).toBe("server-fingerprint");
  });

  it("invalidates approval when an execution input changes", () => {
    let state = createDesktopState(inputs);
    state = reduceDesktopState(state, { type: "repository_selected", repositoryID: "repo-1", baseCommit: "abc123" });
    state = reduceDesktopState(state, { type: "worktree_ready", worktree: inputs.worktree, worktreeDiffHash: inputs.worktreeDiffHash });
    state = reduceDesktopState(state, { type: "diff_reviewed" });
    state = reduceDesktopState(state, { type: "approved" });
    state = reduceDesktopState(state, {
      type: "input_changed",
      inputs: { ...inputs, prompt: "A different prompt" },
    });
    expect(state.phase).toBe("APPROVAL_REQUIRED");
    expect(state.approvalFingerprint).toBeUndefined();
  });

  it("moves a lost local process into recoverable state", () => {
    let state = createDesktopState(inputs);
    state = reduceDesktopState(state, { type: "repository_selected", repositoryID: "repo-1", baseCommit: "abc123" });
    state = reduceDesktopState(state, { type: "worktree_ready", worktree: inputs.worktree, worktreeDiffHash: inputs.worktreeDiffHash });
    state = reduceDesktopState(state, { type: "diff_reviewed" });
    state = reduceDesktopState(state, { type: "approved" });
    state = reduceDesktopState(state, { type: "agent_started", status: "PREPARING" });
    state = reduceDesktopState(state, { type: "heartbeat_lost", status: "INTERRUPTED" });
    expect(state.phase).toBe("RECOVERY_REQUIRED");
    state = reduceDesktopState(state, { type: "resumed" });
    expect(state.phase).toBe("READY_TO_RUN");
  });
});
