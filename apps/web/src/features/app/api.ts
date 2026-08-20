import { createAPIClient } from "@forgeflow/api-client";

const baseURL = (process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? "").replace(/\/+$/, "");

function csrfToken(): string | undefined {
  if (typeof document === "undefined") return undefined;
  const token = document.cookie
    .split("; ")
    .find((cookie) => cookie.startsWith("forgeflow_csrf="))
    ?.slice("forgeflow_csrf=".length);
  return token ? decodeURIComponent(token) : undefined;
}

export function browserAPI(projectID?: string) {
  const headers = new Headers();
  if (process.env.NEXT_PUBLIC_FORGEFLOW_DEV_AUTH === "true") {
    const organizationID = process.env.NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID;
    const actorID = process.env.NEXT_PUBLIC_FORGEFLOW_ACTOR_ID;
    if (organizationID) headers.set("X-Organization-ID", organizationID);
    if (actorID) headers.set("X-Actor-ID", actorID);
  }
  return createAPIClient({
    baseURL: `${baseURL}/api/v1`,
    projectID,
    credentials: "include",
    headers,
    getCSRFToken: csrfToken,
  });
}

export function apiErrorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return "Forgeflow không thể hoàn tất yêu cầu này.";
}
