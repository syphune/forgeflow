import { describe, expect, it } from "vitest";
import { createGitHubCodeClient } from "./github-code";

describe("Desktop GitHub code client", () => {
  it("requests credentials only for the scoped project repository", async () => {
    let requestURL = "";
    let requestInit: RequestInit | undefined;
    const client = createGitHubCodeClient(
      { apiBaseURL: "https://forgeflow.example/", sessionCookie: "session", csrfCookie: "csrf" },
      "project-1",
      async (input, init) => {
        requestURL = String(input);
        requestInit = init;
        return new Response(JSON.stringify({ repository_id: "repo-1", clone_url: "https://github.com/acme/app.git", username: "x-access-token", token: "secret", expires_at: "2026-08-21T12:00:00Z" }), { status: 200, headers: { "Content-Type": "application/json" } });
      },
    );

    const credential = await client.cloneCredential("repo-1");
    expect(credential.repository_id).toBe("repo-1");
    expect(requestURL).toBe("https://forgeflow.example/api/v1/projects/project-1/repositories/repo-1/clone-credential");
    expect(requestInit?.method).toBe("GET");
    expect(requestInit?.headers).toMatchObject({
      "X-Project-ID": "project-1",
      Cookie: "forgeflow_session=session; forgeflow_csrf=csrf",
      "X-CSRF-Token": "csrf",
    });
  });

  it("surfaces the server authorization requirement", async () => {
    const client = createGitHubCodeClient(
      { apiBaseURL: "https://forgeflow.example", sessionCookie: "session", csrfCookie: "csrf" },
      "project-1",
      async () => new Response(JSON.stringify({ message: "GitHub code authorization is required before cloning" }), { status: 409 }),
    );
    await expect(client.cloneCredential("repo-1")).rejects.toThrow(/authorization is required/);
  });
});
