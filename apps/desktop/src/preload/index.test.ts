import { describe, expect, it, vi } from "vitest";

const electronMocks = vi.hoisted(() => ({
  exposeInMainWorld: vi.fn(),
  invoke: vi.fn(async (...args: unknown[]) => args),
}));

vi.mock("electron", () => ({
  contextBridge: { exposeInMainWorld: electronMocks.exposeInMainWorld },
  ipcRenderer: { invoke: electronMocks.invoke },
}));

import "./index";

describe("desktop preload bridge", () => {
  it("exposes and forwards every IPC capability", async () => {
    expect(electronMocks.exposeInMainWorld).toHaveBeenCalledTimes(1);
    expect(electronMocks.exposeInMainWorld.mock.calls[0][0]).toBe("forgeflow");
    const bridge = electronMocks.exposeInMainWorld.mock.calls[0][1] as Record<string, (...args: unknown[]) => Promise<unknown>>;
    const calls: Array<[string, string, unknown[]]> = [
      ["health", "forgeflow:health", []],
      ["authStatus", "forgeflow:auth-status", []],
      ["signIn", "forgeflow:auth-sign-in", ["http://localhost:18080"]],
      ["signOut", "forgeflow:auth-sign-out", []],
      ["authorizeGitHubCode", "forgeflow:github-code-authorize", [{ apiBaseURL: "http://localhost:18080", projectID: "project-1" }]],
      ["chooseRepository", "forgeflow:choose-repository", []],
      ["repositoryStatus", "forgeflow:repository-status", ["/tmp/repository"]],
      ["findRepositories", "forgeflow:find-repositories", ["/tmp/repositories"]],
      ["cloneRepository", "forgeflow:clone-repository", [{ root: "/tmp/repositories", name: "astro", apiBaseURL: "http://localhost:18080", projectID: "project-1", repositoryID: "repository-1" }]],
      ["repositoryDiff", "forgeflow:repository-diff", ["/tmp/worktree"]],
      ["createWorktree", "forgeflow:create-worktree", [{ sourceRoot: "/tmp/repository", name: "astro-1" }]],
      ["removeWorktree", "forgeflow:remove-worktree", ["/tmp/worktree"]],
      ["commitWorktree", "forgeflow:commit-worktree", [{ candidate: "/tmp/worktree", message: "fix: preserve reader language" }]],
      ["pushWorktree", "forgeflow:push-worktree", [{ candidate: "/tmp/worktree", apiBaseURL: "http://localhost:18080", projectID: "project-1", repositoryID: "repository-1" }]],
      ["runAgent", "forgeflow:agent-run", [{ provider: "codex", worktree: "/tmp/worktree", prompt: "run tests", approved: true }]],
      ["cancelAgent", "forgeflow:agent-cancel", ["run-1"]],
      ["agentRecoveries", "forgeflow:agent-recoveries", []],
    ];

    expect(Object.keys(bridge)).toEqual(calls.map(([name]) => name));
    for (const [name, channel, args] of calls) {
      electronMocks.invoke.mockClear();
      await bridge[name](...args);
      expect(electronMocks.invoke).toHaveBeenCalledWith(channel, ...args);
    }
  });
});
