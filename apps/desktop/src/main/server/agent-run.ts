import type { AgentResult } from "../agent/runner";
import type { AgentInputSnapshot } from "../agent/state";

export type AgentRunSync = {
  apiBaseURL: string;
  token?: string;
  projectID: string;
  runID: string;
  session?: {
    sessionCookie: string;
    csrfCookie: string;
  };
};

export type AgentRunSnapshot = {
  run: {
    id: string;
    status: string;
    agent_provider?: string;
    repository_id?: string;
    base_sha?: string;
    branch?: string;
    approval_fingerprint?: string;
    execution_inputs?: Record<string, unknown>;
  };
};

export function assertRemoteExecutionInputs(snapshot: AgentInputSnapshot, remote: AgentRunSnapshot): void {
  const run = remote.run;
  if (run.agent_provider?.trim() && run.agent_provider.trim() !== snapshot.provider) {
    throw new Error("The approved AgentRun provider does not match the selected desktop provider");
  }
  if (run.repository_id?.trim() && snapshot.repositoryID && run.repository_id.trim() !== snapshot.repositoryID) {
    throw new Error("The approved AgentRun repository does not match the desktop repository");
  }
  if (run.base_sha?.trim() && snapshot.baseCommit && snapshot.baseCommit !== "HEAD" && run.base_sha.trim() !== snapshot.baseCommit) {
    throw new Error("The approved AgentRun base commit does not match the desktop worktree");
  }
  const inputs = run.execution_inputs ?? {};
  if (typeof inputs.prompt === "string" && inputs.prompt.trim() !== snapshot.prompt.trim()) {
    throw new Error("The approved AgentRun prompt changed; review and approve the new inputs");
  }
  if (typeof inputs.worktree_diff_hash === "string" && inputs.worktree_diff_hash.trim() && inputs.worktree_diff_hash.trim() !== snapshot.worktreeDiffHash) {
    throw new Error("The approved AgentRun worktree diff changed; review and approve the new inputs");
  }
  if (typeof inputs.specification_version === "number" && inputs.specification_version > 0 && snapshot.specificationVersion > 0 && inputs.specification_version !== snapshot.specificationVersion) {
    throw new Error("The approved AgentRun specification changed; review and approve the new inputs");
  }
}

type Fetcher = typeof fetch;

function normalizeBaseURL(value: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error("Forgeflow API URL is invalid");
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") {
    throw new Error("Forgeflow API URL must use HTTPS");
  }
  if (
    url.protocol === "http:" &&
    !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)
  ) {
    throw new Error("HTTP is only allowed for a local Forgeflow API");
  }
  url.hash = "";
  url.search = "";
  return url.toString().replace(/\/$/, "");
}

function validateConfig(config: AgentRunSync): AgentRunSync {
  const apiBaseURL = normalizeBaseURL(config.apiBaseURL);
  if (config.token && config.token.length > 512) {
    throw new Error("Forgeflow PAT is too long");
  }
  if (
    !config.token?.trim() &&
    (!config.session?.sessionCookie.trim() || !config.session?.csrfCookie.trim())
  ) {
    throw new Error("Sign in with GitHub or provide a Forgeflow PAT");
  }
  if (!config.projectID.trim() || config.projectID.length > 128) {
    throw new Error("Forgeflow project ID is required and must be bounded");
  }
  if (!config.runID.trim() || config.runID.length > 128) {
    throw new Error("Forgeflow AgentRun ID is required and must be bounded");
  }
  return {
    apiBaseURL,
    token: config.token?.trim() ?? "",
    projectID: config.projectID.trim(),
    runID: config.runID.trim(),
    session: config.session
      ? {
          sessionCookie: config.session.sessionCookie.trim(),
          csrfCookie: config.session.csrfCookie.trim(),
        }
      : undefined,
  };
}

function messageFromPayload(payload: unknown): string | undefined {
  if (!payload || typeof payload !== "object") return undefined;
  const value = (payload as { message?: unknown }).message;
  return typeof value === "string" && value.trim() ? value : undefined;
}

export function createAgentRunClient(
  input: AgentRunSync,
  fetcher: Fetcher = fetch,
) {
  const config = validateConfig(input);

  async function request<T = void>(path: string, init: RequestInit = {}): Promise<T> {
    const response = await fetcher(
      `${config.apiBaseURL}/api/v1/agent-runs/${config.runID}${path}`,
      {
        ...init,
        headers: {
          "X-Project-ID": config.projectID,
          ...(config.token
            ? { Authorization: `Bearer ${config.token}` }
            : {
                Cookie: `forgeflow_session=${config.session?.sessionCookie}; forgeflow_csrf=${config.session?.csrfCookie}`,
                "X-CSRF-Token": config.session?.csrfCookie ?? "",
              }),
          ...(init.body ? { "Content-Type": "application/json" } : {}),
          ...init.headers,
        },
        signal: AbortSignal.timeout(15_000),
      },
    );
    if (response.ok) {
      if (response.status === 204) return undefined as T;
      const payload = await response.json();
      return payload as T;
    }
    let payload: unknown;
    try {
      payload = await response.json();
    } catch {
      payload = undefined;
    }
    throw new Error(
      messageFromPayload(payload) ??
        `Forgeflow AgentRun request failed (${response.status})`,
    );
  }

  return {
    get: () => request<AgentRunSnapshot>("", { method: "GET" }),
    start: () => request("/start", { method: "POST" }),
    resume: () => request("/resume", { method: "POST" }),
    heartbeat: () => request("/heartbeat", { method: "POST" }),
    transition: (status: string) =>
      request("/transition", {
        method: "POST",
        body: JSON.stringify({ status }),
      }),
    cancel: () => request("/cancel", { method: "POST" }),
    attachResult: (result: AgentResult, error?: string) =>
      request("/result", {
        method: "POST",
        body: JSON.stringify({
          result: {
            exit_code: result.code,
            signal: result.signal,
            timed_out: result.timedOut,
            cancelled: result.cancelled,
            output: result.output,
          },
          ...(error ? { error: error.slice(0, 4000) } : {}),
          metadata: { source: "forgeflow-desktop" },
        }),
      }),
  };
}

export { normalizeBaseURL };
