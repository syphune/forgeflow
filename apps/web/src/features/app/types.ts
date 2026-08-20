export type AppContext = {
  organizationID: string;
  workspaceID: string;
  projectID: string;
};

export type Surface =
  | { kind: "backlog" }
  | { kind: "planning" }
  | { kind: "repositories" }
  | { kind: "settings"; section: string }
  | { kind: "work-item"; id: string };

export function parseSurface(parts: string[] | undefined): Surface {
  const [first, second] = parts ?? [];
  if (first === "work-items" && second) return { kind: "work-item", id: second };
  if (first === "planning") return { kind: "planning" };
  if (first === "repositories") return { kind: "repositories" };
  if (first === "settings") return { kind: "settings", section: second ?? "general" };
  return { kind: "backlog" };
}

export type UITone = "neutral" | "info" | "success" | "warning" | "danger";

export function uiTone(value?: string): UITone {
  const normalized = (value ?? "").toUpperCase();
  if (["DONE", "COMPLETED", "READY", "PASS", "HUMAN_VERIFIED", "ACTIVE", "LINKED", "LOW", "LOWEST"].includes(normalized)) return "success";
  if (["FAILED", "FAIL", "BLOCKED", "CANCELLED", "BUG", "HIGHEST"].includes(normalized)) return "danger";
  if (["IN_PROGRESS", "CODE_REVIEW", "QA", "TESTING", "IMPLEMENTING", "HIGH", "WAITING_SPEC_REVIEW", "WAITING_PR_REVIEW", "WAITING_TEST_FEEDBACK", "PAUSED"].includes(normalized)) return "warning";
  if (["PLANNED", "TODO", "QUEUED", "PREPARING", "MEDIUM", "REVIEW_REQUIRED", "RAW", "TASK", "STORY", "EPIC", "SUB_TASK"].includes(normalized)) return "info";
  return "neutral";
}
