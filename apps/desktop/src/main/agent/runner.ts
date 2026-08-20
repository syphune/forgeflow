import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { basename } from "node:path";
import { resolveManagedWorktree } from "../repository/git";

const MAX_OUTPUT = 512 * 1024;
const SAFE_ENV_KEYS = new Set(["PATH", "HOME", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL"]);

export type Provider = "codex" | "claude";

export type AgentRequest = {
	runID?: string;
	provider: Provider;
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

export type AgentResult = {
	runID: string;
	code: number | null;
	signal: NodeJS.Signals | null;
	output: string;
	timedOut: boolean;
	cancelled: boolean;
  serverSyncError?: string;
};

const activeRuns = new Map<string, { child: ReturnType<typeof spawn>; cancelled: boolean }>();

export function cancelAgent(runID: string): boolean {
  const active = activeRuns.get(runID.trim());
  if (!active) return false;
  active.cancelled = true;
  return active.child.kill("SIGTERM");
}

export function allowlistedExecutable(provider: Provider, configured: string): string {
  const executable = configured.trim() || provider;
  const name = basename(executable).toLowerCase();
  if (name !== provider && name !== `${provider}.exe`) {
    throw new Error(`${provider} executable is not allowlisted`);
  }
  return executable;
}

const providerCommands: Record<Provider, { executable: string; args: (prompt: string) => string[] }> = {
  codex: {
    executable: process.env.FORGEFLOW_CODEX_BIN || "codex",
    args: (prompt) => ["exec", "--full-auto", prompt],
  },
  claude: {
    executable: process.env.FORGEFLOW_CLAUDE_BIN || "claude",
    args: (prompt) => ["-p", prompt],
  },
};

export async function runAgent(request: AgentRequest, managedRoot: string): Promise<AgentResult> {
  if (!request || typeof request !== "object" || !["codex", "claude"].includes(request.provider) || typeof request.worktree !== "string" || typeof request.prompt !== "string" || typeof request.approved !== "boolean") throw new Error("agent request is invalid");
  if (!request.approved) throw new Error("human approval is required before agent execution");
  if (!request.worktree || !request.prompt.trim() || request.worktree.length > 4096 || request.prompt.length > 128 * 1024) throw new Error("worktree and prompt are required and bounded");
  const provider = providerCommands[request.provider];
  if (!provider) throw new Error("agent provider is not allowlisted");
  const worktree = await resolveManagedWorktree(managedRoot, request.worktree);
  const timeoutMs = Math.min(Math.max(request.timeoutMs ?? 15 * 60_000, 1_000), 60 * 60_000);
  const runID = request.runID?.trim() || randomUUID();
  const environment = Object.fromEntries(Object.entries(process.env).filter(([key, value]) => SAFE_ENV_KEYS.has(key) && value !== undefined));
  return new Promise((resolve, reject) => {
    const child = spawn(allowlistedExecutable(request.provider, provider.executable), provider.args(request.prompt), {
      cwd: worktree,
      env: environment,
      shell: false,
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let output = "";
    let timedOut = false;
    let settled = false;
    activeRuns.set(runID, { child, cancelled: false });
    const append = (chunk: Buffer) => {
      if (output.length >= MAX_OUTPUT) return;
      output += chunk.toString("utf8").slice(0, MAX_OUTPUT - output.length);
    };
    child.stdout.on("data", append);
    child.stderr.on("data", append);
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGTERM");
      setTimeout(() => child.kill("SIGKILL"), 2_000).unref();
    }, timeoutMs);
    const cleanup = () => activeRuns.delete(runID);
    child.once("error", (error) => {
      clearTimeout(timer);
      cleanup();
      if (!settled) { settled = true; reject(error); }
    });
    child.once("close", (code, signal) => {
      clearTimeout(timer);
      const wasCancelled = activeRuns.get(runID)?.cancelled ?? false;
      cleanup();
      if (settled) return;
      settled = true;
      resolve({ runID, code, signal, output: redact(output), timedOut, cancelled: wasCancelled });
    });
  });
}

export function redact(value: string): string {
  return value
    .replace(/(authorization\s*[:=]\s*bearer\s+)[^\s]+/gi, "$1[REDACTED]")
    .replace(/(token|secret|password|api[_-]?key)(\s*[:=]\s*)[^\s]+/gi, "$1$2[REDACTED]");
}
