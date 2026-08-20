import { app, BrowserWindow, dialog, ipcMain, session, type IpcMainInvokeEvent } from "electron";
import { createHash, randomUUID } from "node:crypto";
import path from "node:path";
import fs from "node:fs/promises";
import { pathToFileURL } from "node:url";

import {
  createManagedWorktree,
  commitWorktree,
  removeManagedWorktree,
  pushWorktree,
  repositoryDiff,
  repositoryStatus,
  resolveManagedWorktree,
} from "./repository/git";
import {
  cancelAgent,
  runAgent,
  type AgentRequest,
  type AgentResult,
} from "./agent/runner";
import { assertRemoteExecutionInputs, createAgentRunClient } from "./server/agent-run";
import { loadDesktopAuth, saveDesktopAuth, clearDesktopAuth } from "./auth/store";
import { normalizeBaseURL } from "./server/agent-run";
import { createDesktopState, reduceDesktopState, type AgentInputSnapshot, type DesktopEvent, type DesktopState } from "./agent/state";

const agentCheckpointDirectory = () => path.join(app.getPath("userData"), "agent-checkpoints");

function checkpointPath(runID: string): string {
  const value = runID.trim();
  if (!value || value.length > 128 || !/^[a-zA-Z0-9_-]+$/.test(value)) {
    throw new Error("AgentRun ID is invalid");
  }
  return path.join(agentCheckpointDirectory(), `${value}.json`);
}

async function saveAgentCheckpoint(state: DesktopState): Promise<void> {
  if (!state.runID) return;
  const file = checkpointPath(state.runID);
  await fs.mkdir(agentCheckpointDirectory(), { recursive: true, mode: 0o700 });
  await fs.writeFile(file, JSON.stringify(state), { encoding: "utf8", mode: 0o600 });
}

async function listAgentCheckpoints(): Promise<Array<Pick<DesktopState, "phase" | "runID" | "serverStatus" | "checkpoint" | "updatedAt">>> {
  let files: string[];
  try {
    files = await fs.readdir(agentCheckpointDirectory());
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return [];
    throw error;
  }
  const result: Array<Pick<DesktopState, "phase" | "runID" | "serverStatus" | "checkpoint" | "updatedAt">> = [];
  for (const file of files.filter((candidate) => candidate.endsWith(".json"))) {
    try {
      const raw = await fs.readFile(path.join(agentCheckpointDirectory(), file), "utf8");
      const state = JSON.parse(raw) as DesktopState;
      if (state && typeof state.phase === "string" && typeof state.updatedAt === "string") {
        result.push({ phase: state.phase, runID: state.runID, serverStatus: state.serverStatus, checkpoint: state.checkpoint, updatedAt: state.updatedAt });
      }
    } catch {
      // A partial checkpoint must not prevent the desktop shell from opening.
    }
  }
  return result.sort((left, right) => right.updatedAt.localeCompare(left.updatedAt));
}

function applyAgentEvent(state: DesktopState, event: DesktopEvent): DesktopState {
  return reduceDesktopState(state, event);
}

function createWindow() {
  const window = new BrowserWindow({
    width: 1280,
    height: 800,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, "preload.js"),
    },
  });

  window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  window.webContents.on("will-navigate", (event, url) => {
    if (!trustedURL(url)) event.preventDefault();
  });

  if (MAIN_WINDOW_VITE_DEV_SERVER_URL) {
    void window.loadURL(MAIN_WINDOW_VITE_DEV_SERVER_URL);
  } else {
    void window.loadFile(
      path.join(__dirname, `../renderer/${MAIN_WINDOW_VITE_NAME}/index.html`),
    );
  }
}

function trustedURL(url: string): boolean {
  if (url.startsWith("file://")) {
    const rendererURL = pathToFileURL(
      path.join(__dirname, `../renderer/${MAIN_WINDOW_VITE_NAME}/index.html`),
    ).href;
    return url === rendererURL;
  }
  if (!MAIN_WINDOW_VITE_DEV_SERVER_URL) return false;
  try {
    return (
      new URL(url).origin === new URL(MAIN_WINDOW_VITE_DEV_SERVER_URL).origin
    );
  } catch {
    return false;
  }
}

function trustedSender(event: IpcMainInvokeEvent): boolean {
  return trustedURL(event.senderFrame?.url ?? "");
}

async function signInWithGitHub(apiBaseURL: string): Promise<void> {
  const baseURL = normalizeBaseURL(apiBaseURL);
  const loginWindow = new BrowserWindow({
    width: 720,
    height: 760,
    title: "Sign in to Forgeflow",
    webPreferences: { contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  loginWindow.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  await new Promise<void>((resolve, reject) => {
    let settled = false;
    const finish = (error?: Error) => {
      if (settled) return;
      settled = true;
      if (error) reject(error);
      else resolve();
      if (!loginWindow.isDestroyed()) loginWindow.close();
    };
    loginWindow.on("closed", () => {
      if (!settled) finish(new Error("GitHub sign-in was cancelled"));
    });
    loginWindow.webContents.on("did-navigate", async (_event, url) => {
      let parsed: URL;
      try {
        parsed = new URL(url);
      } catch {
        return;
      }
      if (
        parsed.hostname === "github.com" ||
        parsed.pathname.includes("/api/v1/auth/github/")
      ) {
        return;
      }
      try {
        const cookies = await loginWindow.webContents.session.cookies.get({
          name: "forgeflow_session",
        });
        const sessionCookie = cookies.find((cookie) => cookie.value)?.value ?? "";
        if (!sessionCookie) return;
        const response = await fetch(`${baseURL}/api/v1/me`, {
          headers: { Cookie: `forgeflow_session=${sessionCookie}` },
          signal: AbortSignal.timeout(15_000),
        });
        if (!response.ok) return;
        const setCookie = response.headers.get("set-cookie") ?? "";
        const csrfCookie = setCookie.match(/(?:^|,\s*)forgeflow_csrf=([^;]+)/)?.[1] ?? "";
        if (!csrfCookie) return;
        await saveDesktopAuth({ apiBaseURL: baseURL, sessionCookie, csrfCookie });
        finish();
      } catch (error) {
        finish(error instanceof Error ? error : new Error("GitHub sign-in failed"));
      }
    });
    void loginWindow.loadURL(`${baseURL}/api/v1/auth/github/start`).catch((error) =>
      finish(error instanceof Error ? error : new Error("GitHub sign-in failed")),
    );
  });
}

ipcMain.handle("forgeflow:health", (event) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  return { ok: true };
});

ipcMain.handle("forgeflow:agent-recoveries", async (event) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  return listAgentCheckpoints();
});

ipcMain.handle("forgeflow:auth-status", async (event) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  const auth = await loadDesktopAuth();
  return auth ? { signedIn: true, apiBaseURL: auth.apiBaseURL } : { signedIn: false, apiBaseURL: "" };
});

ipcMain.handle("forgeflow:auth-sign-in", async (event, apiBaseURL: unknown) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (typeof apiBaseURL !== "string" || apiBaseURL.length > 2048) throw new Error("Forgeflow API URL is invalid");
  await signInWithGitHub(apiBaseURL);
  const auth = await loadDesktopAuth();
  return { signedIn: Boolean(auth), apiBaseURL: auth?.apiBaseURL ?? "" };
});

ipcMain.handle("forgeflow:auth-sign-out", async (event) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  await clearDesktopAuth();
  await session.defaultSession.clearStorageData({ storages: ["cookies"] });
  return { signedIn: false, apiBaseURL: "" };
});

ipcMain.handle("forgeflow:choose-repository", async (event) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  const result = await dialog.showOpenDialog({
    properties: ["openDirectory", "createDirectory"],
    title: "Choose a local Git repository",
  });
  return result.canceled ? "" : result.filePaths[0] ?? "";
});

ipcMain.handle("forgeflow:repository-status", async (event, root: string) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (typeof root !== "string" || root.length > 4096)
    throw new Error("repository path is invalid");
  return repositoryStatus(root);
});

ipcMain.handle("forgeflow:create-worktree", async (event, request: unknown) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (!request || typeof request !== "object")
    throw new Error("worktree request is invalid");
  const input = request as {
    sourceRoot?: unknown;
    name?: unknown;
    baseRef?: unknown;
    branchName?: unknown;
  };
  if (
    typeof input.sourceRoot !== "string" ||
    typeof input.name !== "string" ||
    (input.baseRef !== undefined && typeof input.baseRef !== "string") ||
    (input.branchName !== undefined && typeof input.branchName !== "string")
  ) {
    throw new Error("worktree request is invalid");
  }
  const managedRoot = path.join(app.getPath("userData"), "worktrees");
  await fs.mkdir(managedRoot, { recursive: true, mode: 0o700 });
  return createManagedWorktree(
    managedRoot,
    input.sourceRoot,
    input.name,
    input.baseRef,
    input.branchName,
  );
});

ipcMain.handle("forgeflow:commit-worktree", async (event, request: unknown) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (!request || typeof request !== "object") throw new Error("commit request is invalid");
  const input = request as { candidate?: unknown; message?: unknown };
  if (typeof input.candidate !== "string" || input.candidate.length > 4096 || typeof input.message !== "string") {
    throw new Error("commit request is invalid");
  }
  const managedRoot = path.join(app.getPath("userData"), "worktrees");
  return commitWorktree(await resolveManagedWorktree(managedRoot, input.candidate), input.message);
});

ipcMain.handle("forgeflow:push-worktree", async (event, candidate: unknown) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (typeof candidate !== "string" || candidate.length > 4096) throw new Error("worktree path is invalid");
  const managedRoot = path.join(app.getPath("userData"), "worktrees");
  return pushWorktree(await resolveManagedWorktree(managedRoot, candidate));
});

ipcMain.handle("forgeflow:repository-diff", async (event, candidate: unknown) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (typeof candidate !== "string" || candidate.length > 4096) throw new Error("worktree path is invalid");
  const managedRoot = path.join(app.getPath("userData"), "worktrees");
  return repositoryDiff(await resolveManagedWorktree(managedRoot, candidate));
});

ipcMain.handle(
  "forgeflow:remove-worktree",
  async (event, candidate: unknown) => {
    if (!trustedSender(event)) throw new Error("untrusted IPC sender");
    if (typeof candidate !== "string" || candidate.length > 4096)
      throw new Error("worktree path is invalid");
    const managedRoot = path.join(app.getPath("userData"), "worktrees");
    await removeManagedWorktree(managedRoot, candidate);
  },
);

ipcMain.handle("forgeflow:agent-run", async (event, request: AgentRequest) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (
    !request ||
    typeof request !== "object" ||
    !["codex", "claude"].includes(request.provider) ||
    typeof request.worktree !== "string" ||
    typeof request.prompt !== "string" ||
    typeof request.approved !== "boolean"
  ) {
    throw new Error("agent request is invalid");
  }
  if (request.server !== undefined) {
    if (
      !request.server ||
      typeof request.server !== "object" ||
      typeof request.server.apiBaseURL !== "string" ||
      typeof request.server.token !== "string" ||
      typeof request.server.projectID !== "string" ||
      typeof request.server.runID !== "string"
    ) {
      throw new Error("Forgeflow AgentRun sync request is invalid");
    }
  }
  const managedRoot = path.join(app.getPath("userData"), "worktrees");
  await fs.mkdir(managedRoot, { recursive: true, mode: 0o700 });
  request.runID ??= randomUUID();
  const safeWorktree = await resolveManagedWorktree(managedRoot, request.worktree);
  const diff = await repositoryDiff(safeWorktree);
  const snapshot: AgentInputSnapshot = {
    repositoryID: request.repositoryID?.trim() ?? "",
    baseCommit: request.baseCommit?.trim() ?? "",
    worktree: safeWorktree,
    worktreeDiffHash:
      request.worktreeDiffHash?.trim() ||
      createHash("sha256").update(diff.patch).digest("hex"),
    prompt: request.prompt,
    provider: request.provider,
    specificationVersion: request.specificationVersion ?? 0,
  };
  let state = createDesktopState(snapshot);
  const checkpoint = async (event: DesktopEvent) => {
    state = applyAgentEvent(state, event);
    await saveAgentCheckpoint(state);
  };
  await checkpoint({
    type: "repository_selected",
    repositoryID: snapshot.repositoryID,
    baseCommit: snapshot.baseCommit,
  });
  await checkpoint({
    type: "worktree_ready",
    worktree: snapshot.worktree,
    worktreeDiffHash: snapshot.worktreeDiffHash,
  });
  await checkpoint({ type: "diff_reviewed" });
  if (!request.approved) {
    throw new Error("human approval is required before agent execution");
  }
  await checkpoint({ type: "approved" });

  if (!request.server) {
    await checkpoint({ type: "agent_run_created", runID: request.runID, status: "LOCAL" });
    await checkpoint({ type: "agent_started", status: "RUNNING" });
    try {
      const localResult = await runAgent(request, managedRoot);
      await checkpoint({ type: "result", success: localResult.code === 0 && !localResult.cancelled && !localResult.timedOut });
      return localResult;
    } catch (error) {
      await checkpoint({ type: "result", success: false });
      throw error;
    }
  }

  const storedAuth = request.server.token.trim() ? null : await loadDesktopAuth();
  const normalizedServerURL = normalizeBaseURL(request.server.apiBaseURL);
  const server = createAgentRunClient(
    storedAuth && storedAuth.apiBaseURL === normalizedServerURL
      ? { ...request.server, session: storedAuth }
      : request.server,
  );
  const remote = await server.get();
  assertRemoteExecutionInputs(snapshot, remote);
  await checkpoint({ type: "agent_run_created", runID: request.server.runID, status: remote.run.status });
  await checkpoint({ type: "reconciled", status: remote.run.status });
  let started = false;
  let heartbeatTimer: ReturnType<typeof setInterval> | undefined;
  let heartbeatBusy = false;
  let heartbeatError: Error | undefined;
  try {
    if (remote.run.status === "INTERRUPTED") {
      state = applyAgentEvent(state, { type: "resumed" });
      await saveAgentCheckpoint(state);
      await server.resume();
    } else if (remote.run.status === "QUEUED") {
      await server.start();
    } else if (!["PREPARING", "PLANNING", "INVESTIGATING", "IMPLEMENTING", "TESTING", "REVIEWING"].includes(remote.run.status)) {
      throw new Error(`Forgeflow AgentRun cannot start from ${remote.run.status}`);
    } else {
      throw new Error("Forgeflow AgentRun is active but the local process is not attached; wait for recovery or resume it after interruption");
    }
    started = true;
    await checkpoint({ type: "agent_started", status: "PREPARING" });
    for (const status of ["PLANNING", "INVESTIGATING", "IMPLEMENTING"]) {
      await server.transition(status);
      await checkpoint({ type: "checkpoint", name: status });
    }
    heartbeatTimer = setInterval(() => {
      if (heartbeatBusy) return;
      heartbeatBusy = true;
      void server.heartbeat().catch((error: unknown) => {
        heartbeatError = error instanceof Error ? error : new Error("AgentRun heartbeat failed");
        state = applyAgentEvent(state, { type: "heartbeat_lost", status: "INTERRUPTED" });
        void saveAgentCheckpoint(state);
      }).finally(() => {
        heartbeatBusy = false;
      });
    }, 15_000);
    heartbeatTimer.unref?.();
  } catch (error) {
    if (heartbeatTimer) clearInterval(heartbeatTimer);
    if (started) {
      try {
        await server.cancel();
      } catch {
        // The original error is more actionable and does not expose the PAT.
      }
    }
    throw error;
  }

  let localResult: AgentResult;
  try {
    localResult = await runAgent(request, managedRoot);
  } catch (error) {
    if (heartbeatTimer) clearInterval(heartbeatTimer);
    await checkpoint({ type: "result", success: false });
    const failure: AgentResult = {
      runID: request.runID ?? "",
      code: null,
      signal: null,
      output: "",
      timedOut: false,
      cancelled: false,
    };
    try {
      await server.attachResult(failure, error instanceof Error ? error.message : "local agent execution failed");
      await server.cancel();
    } catch (syncError) {
      throw new Error(`${error instanceof Error ? error.message : "local agent execution failed"}; server sync failed: ${syncError instanceof Error ? syncError.message : "unknown error"}`);
    }
    throw error;
  }

  try {
    if (heartbeatTimer) clearInterval(heartbeatTimer);
    await checkpoint({ type: "result", success: !localResult.cancelled && !localResult.timedOut && localResult.code === 0 });
    await server.transition("TESTING");
    await checkpoint({ type: "checkpoint", name: "TESTING" });
    const failed = localResult.cancelled || localResult.timedOut || localResult.code !== 0;
    await server.attachResult(
      localResult,
      failed && !localResult.cancelled
        ? `Local agent exited with ${localResult.code ?? localResult.signal ?? "unknown"}`
        : undefined,
    );
    if (localResult.cancelled) await server.cancel();
    else await server.transition(failed ? "FAILED" : "REVIEWING");
    await checkpoint({ type: "checkpoint", name: failed ? "FAILED" : "REVIEWING" });
  } catch (error) {
    return {
      ...localResult,
      serverSyncError: heartbeatError
        ? `${heartbeatError.message}; ${error instanceof Error ? error.message : "server sync failed"}`
        : error instanceof Error
          ? error.message
          : "server sync failed",
    };
  }
  if (heartbeatTimer) clearInterval(heartbeatTimer);
  return localResult;
});

ipcMain.handle("forgeflow:agent-cancel", (event, runID: unknown) => {
  if (!trustedSender(event)) throw new Error("untrusted IPC sender");
  if (typeof runID !== "string" || runID.length > 128) throw new Error("agent run id is invalid");
  return { cancelled: cancelAgent(runID) };
});

void app.whenReady().then(() => {
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
