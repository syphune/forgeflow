import { normalizeBaseURL } from "./agent-run";

export type GitHubCodeCredential = {
  repository_id: string;
  clone_url: string;
  username: string;
  token: string;
  expires_at: string;
};

export type GitHubCodeSession = {
  apiBaseURL: string;
  sessionCookie: string;
  csrfCookie: string;
};

type Fetcher = typeof fetch;

function messageFromPayload(payload: unknown): string | undefined {
  if (!payload || typeof payload !== "object") return undefined;
  const value = (payload as { message?: unknown }).message;
  return typeof value === "string" && value.trim() ? value : undefined;
}

export function createGitHubCodeClient(
  session: GitHubCodeSession,
  projectID: string,
  fetcher: Fetcher = fetch,
) {
  const apiBaseURL = normalizeBaseURL(session.apiBaseURL);
  if (!projectID.trim() || projectID.length > 128) {
    throw new Error("Forgeflow project ID is invalid");
  }

  return {
    cloneCredential: async (repositoryID: string): Promise<GitHubCodeCredential> => {
      if (!repositoryID.trim() || repositoryID.length > 128) {
        throw new Error("Forgeflow repository ID is invalid");
      }
      const response = await fetcher(
        `${apiBaseURL}/api/v1/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/clone-credential`,
        {
          method: "GET",
          headers: {
            "X-Project-ID": projectID,
            Cookie: `forgeflow_session=${session.sessionCookie}; forgeflow_csrf=${session.csrfCookie}`,
            "X-CSRF-Token": session.csrfCookie,
          },
          signal: AbortSignal.timeout(15_000),
        },
      );
      if (response.ok) return (await response.json()) as GitHubCodeCredential;
      let payload: unknown;
      try {
        payload = await response.json();
      } catch {
        payload = undefined;
      }
      throw new Error(
        messageFromPayload(payload) ??
          `Forgeflow GitHub code request failed (${response.status})`,
      );
    },
  };
}
