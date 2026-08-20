import { describe, expect, it } from "vitest";
import { assertRemoteExecutionInputs, createAgentRunClient, normalizeBaseURL } from "./agent-run";

describe("desktop AgentRun sync", () => {
  it("allows HTTPS and local HTTP only", () => {
    expect(normalizeBaseURL("https://forgeflow.example/")) .toBe("https://forgeflow.example");
    expect(normalizeBaseURL("http://localhost:18080/")) .toBe("http://localhost:18080");
    expect(() => normalizeBaseURL("http://forgeflow.example")).toThrow(/local/);
  });

  it("sends scoped server updates with the PAT in the authorization header", async () => {
    const calls: Array<{ url: string; init?: RequestInit }> = [];
    const fetcher: typeof fetch = async (input, init) => {
      calls.push({ url: String(input), init });
      return new Response(null, { status: 204 });
    };
    const client = createAgentRunClient(
      {
        apiBaseURL: "https://forgeflow.example",
        token: "ff_pat_secret",
        projectID: "project-1",
        runID: "run-1",
      },
      fetcher,
    );

    await client.start();
    await client.transition("PLANNING");
    await client.attachResult({
      runID: "local-run",
      code: 0,
      signal: null,
      output: "done",
      timedOut: false,
      cancelled: false,
    });

    expect(calls.map((call) => call.url)).toEqual([
      "https://forgeflow.example/api/v1/agent-runs/run-1/start",
      "https://forgeflow.example/api/v1/agent-runs/run-1/transition",
      "https://forgeflow.example/api/v1/agent-runs/run-1/result",
    ]);
    expect(calls[0].init?.headers).toMatchObject({
      Authorization: "Bearer ff_pat_secret",
      "X-Project-ID": "project-1",
    });
    expect(String(calls[2].init?.body)).toContain('"output":"done"');
  });

  it("can use an encrypted desktop session without sending a bearer token", async () => {
    let request: RequestInit | undefined;
    const fetcher: typeof fetch = async (_input, init) => {
      request = init;
      return new Response(null, { status: 204 });
    };
    const client = createAgentRunClient(
      {
        apiBaseURL: "https://forgeflow.example",
        projectID: "project-1",
        runID: "run-1",
        session: { sessionCookie: "session-value", csrfCookie: "csrf-value" },
      },
      fetcher,
    );
    await client.start();
    expect(request?.headers).toMatchObject({
      Cookie: "forgeflow_session=session-value; forgeflow_csrf=csrf-value",
      "X-CSRF-Token": "csrf-value",
    });
    expect(request?.headers).not.toHaveProperty("Authorization");
  });

  it("rejects local execution when an approved prompt or diff changed", () => {
    const snapshot = {
      repositoryID: "repo-1",
      baseCommit: "HEAD",
      worktree: "/managed/run-1",
      worktreeDiffHash: "diff-1",
      prompt: "Implement the approved change",
      provider: "codex",
      specificationVersion: 3,
    } as const;
    expect(() => assertRemoteExecutionInputs(snapshot, {
      run: {
        id: "run-1",
        status: "QUEUED",
        agent_provider: "codex",
        execution_inputs: { prompt: "Different change", worktree_diff_hash: "diff-1" },
      },
    })).toThrow(/prompt changed/);
    expect(() => assertRemoteExecutionInputs(snapshot, {
      run: {
        id: "run-1",
        status: "QUEUED",
        agent_provider: "codex",
        execution_inputs: { prompt: snapshot.prompt, worktree_diff_hash: "diff-2" },
      },
    })).toThrow(/diff changed/);
  });
});
