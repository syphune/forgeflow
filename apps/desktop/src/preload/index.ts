import { contextBridge, ipcRenderer } from "electron";

type RepositoryStatus = {
  root: string;
  branch: string;
  clean: boolean;
  porcelain: string;
};
type RepositoryDiff = { root: string; files: string[]; patch: string };
type RepositoryCommit = RepositoryStatus & { commitSHA: string };
type AgentRequest = {
  runID?: string;
  provider: "codex" | "claude";
  worktree: string;
  prompt: string;
  approved: boolean;
  repositoryID?: string;
  baseCommit?: string;
  worktreeDiffHash?: string;
  specificationVersion?: number;
  timeoutMs?: number;
  server?: {
    apiBaseURL: string;
    token: string;
    projectID: string;
    runID: string;
  };
};
type AgentResult = {
  runID: string;
  code: number | null;
  signal: NodeJS.Signals | null;
  output: string;
  timedOut: boolean;
  cancelled: boolean;
  serverSyncError?: string;
};

type AgentRecovery = {
  phase: string;
  runID?: string;
  serverStatus?: string;
  checkpoint?: string;
  updatedAt: string;
};

contextBridge.exposeInMainWorld("forgeflow", {
  health: (): Promise<{ ok: boolean }> =>
    ipcRenderer.invoke("forgeflow:health"),
  authStatus: (): Promise<{ signedIn: boolean; apiBaseURL: string }> =>
    ipcRenderer.invoke("forgeflow:auth-status"),
  signIn: (apiBaseURL: string): Promise<{ signedIn: boolean; apiBaseURL: string }> =>
    ipcRenderer.invoke("forgeflow:auth-sign-in", apiBaseURL),
  signOut: (): Promise<{ signedIn: boolean; apiBaseURL: string }> =>
    ipcRenderer.invoke("forgeflow:auth-sign-out"),
  chooseRepository: (): Promise<string> =>
    ipcRenderer.invoke("forgeflow:choose-repository"),
  repositoryStatus: (root: string): Promise<RepositoryStatus> =>
    ipcRenderer.invoke("forgeflow:repository-status", root),
  repositoryDiff: (root: string): Promise<RepositoryDiff> =>
    ipcRenderer.invoke("forgeflow:repository-diff", root),
  createWorktree: (request: {
    sourceRoot: string;
    name: string;
    baseRef?: string;
    branchName?: string;
  }): Promise<RepositoryStatus> =>
    ipcRenderer.invoke("forgeflow:create-worktree", request),
  removeWorktree: (candidate: string): Promise<void> =>
    ipcRenderer.invoke("forgeflow:remove-worktree", candidate),
  commitWorktree: (request: { candidate: string; message: string }): Promise<RepositoryCommit> =>
    ipcRenderer.invoke("forgeflow:commit-worktree", request),
  pushWorktree: (candidate: string): Promise<RepositoryStatus> =>
    ipcRenderer.invoke("forgeflow:push-worktree", candidate),
  runAgent: (request: AgentRequest): Promise<AgentResult> =>
    ipcRenderer.invoke("forgeflow:agent-run", request),
  cancelAgent: (runID: string): Promise<{ cancelled: boolean }> =>
    ipcRenderer.invoke("forgeflow:agent-cancel", runID),
  agentRecoveries: (): Promise<AgentRecovery[]> =>
    ipcRenderer.invoke("forgeflow:agent-recoveries"),
});

declare global {
  interface Window {
    forgeflow: {
      health(): Promise<{ ok: boolean }>;
      authStatus(): Promise<{ signedIn: boolean; apiBaseURL: string }>;
      signIn(apiBaseURL: string): Promise<{ signedIn: boolean; apiBaseURL: string }>;
      signOut(): Promise<{ signedIn: boolean; apiBaseURL: string }>;
      chooseRepository(): Promise<string>;
      repositoryStatus(root: string): Promise<RepositoryStatus>;
      repositoryDiff(root: string): Promise<RepositoryDiff>;
      createWorktree(request: {
        sourceRoot: string;
        name: string;
        baseRef?: string;
        branchName?: string;
      }): Promise<RepositoryStatus>;
      removeWorktree(candidate: string): Promise<void>;
      commitWorktree(request: { candidate: string; message: string }): Promise<RepositoryCommit>;
      pushWorktree(candidate: string): Promise<RepositoryStatus>;
      runAgent(request: AgentRequest): Promise<AgentResult>;
      cancelAgent(runID: string): Promise<{ cancelled: boolean }>;
      agentRecoveries(): Promise<AgentRecovery[]>;
    };
  }
}
