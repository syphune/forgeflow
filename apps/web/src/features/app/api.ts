import { APIError, createAPIClient } from "@forgeflow/api-client";
import { translate as t } from "@forgeflow/ui";

const baseURL = (process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? "").replace(/\/+$/, "");
export const selectedOrganizationStorageKey = "forgeflow:selected-organization";

export function getSelectedOrganizationID(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(selectedOrganizationStorageKey)?.trim() ?? "";
  } catch {
    return "";
  }
}

export function rememberOrganizationID(organizationID: string): void {
  if (typeof window === "undefined") return;
  try {
    if (organizationID) window.localStorage.setItem(selectedOrganizationStorageKey, organizationID);
    else window.localStorage.removeItem(selectedOrganizationStorageKey);
  } catch {
    // Organization context remains usable for the current page.
  }
}

function csrfToken(): string | undefined {
  if (typeof document === "undefined") return undefined;
  const token = document.cookie
    .split("; ")
    .find((cookie) => cookie.startsWith("forgeflow_csrf="))
    ?.slice("forgeflow_csrf=".length);
  return token ? decodeURIComponent(token) : undefined;
}

export function browserAPI(projectID?: string, organizationID?: string | null) {
  const headers = new Headers();
  if (process.env.NEXT_PUBLIC_FORGEFLOW_DEV_AUTH === "true") {
    const devOrganizationID = process.env.NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID;
    const actorID = process.env.NEXT_PUBLIC_FORGEFLOW_ACTOR_ID;
    if (organizationID === null) {
      // Bootstrap requests intentionally start without a tenant context.
    } else if (organizationID?.trim()) {
      headers.set("X-Organization-ID", organizationID.trim());
    } else if (devOrganizationID) {
      headers.set("X-Organization-ID", devOrganizationID);
    }
    if (actorID) headers.set("X-Actor-ID", actorID);
  }
  const scopedOrganizationID = organizationID === null ? "" : organizationID?.trim() || getSelectedOrganizationID();
  if (scopedOrganizationID && !headers.has("X-Organization-ID")) headers.set("X-Organization-ID", scopedOrganizationID);
  return createAPIClient({
    baseURL: `${baseURL}/api/v1`,
    projectID,
    credentials: "include",
    headers,
    getCSRFToken: csrfToken,
  });
}

export function apiErrorMessage(error: unknown): string {
  if (isRepositoryEmptyError(error)) return t("repo.empty-repository");
  if (error instanceof Error) return error.message;
  return "Forgeflow không thể hoàn tất yêu cầu này.";
}

export function isRepositoryEmptyError(error: unknown): boolean {
  return error instanceof APIError && error.body?.code === "REPOSITORY_EMPTY";
}
