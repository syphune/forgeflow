"use client";

import {
  FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import * as React from "react";
import { readinessLabel, statusLabel, translate as t } from "@forgeflow/ui";
import { optimizeAttachment } from "./attachment-optimization";

type Label = { id: string; name: string; color: string };
type WorkItem = {
  id: string;
  key: string;
  type: string;
  title: string;
  description?: string;
  parent_id?: string;
  status: string;
  priority?: string;
  assignee_id?: string;
  reporter_id?: string;
  repository_id?: string;
  due_at?: string | null;
  estimate_points?: number | null;
  backlog_rank?: number;
  sprint_id?: string;
  archived_at?: string | null;
  archived_by?: string;
  labels?: Label[];
  version: number;
};
type Comment = {
  id: string;
  author_id: string;
  body: string;
  created_at: string;
  updated_at?: string;
  deleted_at?: string | null;
  deleted_by?: string;
};
type WorkItemLink = {
  id: string;
  source_id: string;
  target_id: string;
  relation_type: string;
  created_at: string;
};
type AuditRecord = {
  id: string;
  actor_type: string;
  actor_id: string;
  action: string;
  resource_type: string;
  resource_id: string;
  before?: unknown;
  after?: unknown;
  created_at: string;
};
type ReproductionStep = {
  position: number;
  action: string;
  expected_result: string;
  observed_result: string;
  evidence_refs?: string[];
  verification_status?: string;
  provenance?: string;
};
type AcceptanceCriterion = {
  position: number;
  statement: string;
  verification_status?: string;
  provenance?: string;
};
type RegressionTestCase = {
  position: number;
  scenario: string;
  expected_result: string;
  verification_status?: string;
  provenance?: string;
};
type SpecificationContextRef = {
  repository_id?: string;
  module?: string;
  file?: string;
  symbol?: string;
  commit?: string;
  pull_request?: string;
  rationale?: string;
  provenance?: string;
};
type SpecificationProposal = {
  id: string;
  work_item_id: string;
  field: string;
  value: string;
  provenance: string;
  status: string;
  created_at: string;
};
type SpecificationAnalysis = {
  id: string;
  work_item_id: string;
  root_cause_hypothesis: string;
  blast_radius: string;
  implementation_plan: string;
  test_plan: string;
  evidence_refs: string[];
  confidence: number;
  provenance: string;
  created_by: string;
  created_at: string;
};
type SpecificationFieldVersion = {
  id: string;
  revision: number;
  field: string;
  value: string;
  provenance: string;
  verification_status: string;
  created_at: string;
};
type Specification = {
  id: string;
  work_item_id: string;
  summary: string;
  fields: Record<
    string,
    { value: string; verification_status?: string; provenance?: string }
  >;
  reproduction_steps: ReproductionStep[];
  acceptance_criteria: AcceptanceCriterion[];
  regression_test_cases: RegressionTestCase[];
  context_refs: SpecificationContextRef[];
  repository_id?: string;
};
type Readiness = {
  ready: boolean;
  missing?: string[];
  quality?: {
    completeness: number;
    clarity: number;
    reproducibility: number;
    evidence_quality: number;
    testability: number;
    repository_context: number;
    human_verification_coverage: number;
  };
};
type BoardColumn = {
  status: string;
  name: string;
  position: number;
  items: WorkItem[];
};
type WorkflowStatus = {
  key: string;
  display_name: string;
  category: string;
  position: number;
  is_terminal: boolean;
};
type WorkflowTransition = {
	key: string;
	from_status: string;
	to_status: string;
	display_name: string;
	required_rules?: string[];
	required_permissions?: string[];
};
type Project = {
  id: string;
  key: string;
  display_name: string;
  workspace_id: string;
};
type Organization = {
  id: string;
  slug: string;
  display_name: string;
};
type CustomFieldDefinition = {
  id: string;
  project_id: string;
  key: string;
  display_name: string;
  value_type: "TEXT" | "NUMBER" | "BOOLEAN" | "DATE" | "SELECT";
  options?: string[];
  required: boolean;
};
type CustomFieldValue = CustomFieldDefinition & {
  definition_id: string;
  work_item_id: string;
  value: string;
  updated_at: string;
};
type Workspace = { id: string; key: string; display_name: string };
type Member = {
  id: string;
  login: string;
  display_name: string;
  role_key: string;
  project_role?: boolean;
};
type Sprint = {
  id: string;
  name: string;
  goal: string;
  status: "PLANNED" | "ACTIVE" | "COMPLETED";
  starts_at?: string;
  ends_at?: string;
};
type AgentRun = {
  id: string;
  work_item_id: string;
  repository_id?: string;
  agent_provider: string;
  agent_name: string;
  model?: string;
  base_sha?: string;
  branch?: string;
  status: string;
  approved: boolean;
  started_at?: string;
  finished_at?: string;
  commit_sha?: string;
  pull_request_id?: string;
  result?: Record<string, unknown>;
  error?: string;
  created_at: string;
};
type AgentTestStatus = "NOT_RUN" | "PASS" | "FAIL" | "BLOCKED";
type AgentTestCaseResult = {
  position: number;
  status: AgentTestStatus;
  note?: string;
  evidence_refs?: string[];
  updated_by?: string;
  updated_at?: string;
};

function testResultsForRun(run: AgentRun): AgentTestCaseResult[] {
  if (!Array.isArray(run.result?.test_cases)) return [];
  return run.result.test_cases.flatMap((value) => {
    if (!value || typeof value !== "object") return [];
    const item = value as Partial<AgentTestCaseResult>;
    if (
      typeof item.position !== "number" ||
      !["NOT_RUN", "PASS", "FAIL", "BLOCKED"].includes(item.status ?? "")
    ) {
      return [];
    }
    return [{
      position: item.position,
      status: item.status as AgentTestStatus,
      note: typeof item.note === "string" ? item.note : "",
      evidence_refs: Array.isArray(item.evidence_refs)
        ? item.evidence_refs.filter((ref): ref is string => typeof ref === "string")
        : [],
      updated_by: typeof item.updated_by === "string" ? item.updated_by : "",
      updated_at: typeof item.updated_at === "string" ? item.updated_at : "",
    }];
  });
}
type AgentRunStep = {
  id: string;
  sequence: number;
  phase: string;
  status: string;
  summary: string;
  files_read: number;
  files_modified: number;
};
type AgentRunArtifact = {
  id: string;
  artifact_type: string;
  name: string;
  content_hash: string;
  size_bytes: number;
  created_at: string;
};
type Attachment = {
  id: string;
  project_id: string;
  work_item_id: string;
  name: string;
  content_type: string;
  sha256: string;
  size_bytes: number;
  created_by: string;
  created_at: string;
};
type AccessToken = {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  expires_at: string;
  created_at: string;
};
type NotificationItem = {
  id: string;
  project_id?: string;
  notification_type: string;
  title: string;
  body: string;
  resource_type?: string;
  resource_id?: string;
  read_at?: string;
  created_at: string;
};
type AutomationRule = {
  id: string;
  project_id: string;
  name: string;
  event_type: string;
  action_type: string;
  config?: { title?: string; body?: string };
  enabled: boolean;
  created_at: string;
};
type GitHubRepository = {
  id: string;
  github_repository_id: number;
  full_name: string;
  default_branch: string;
  clone_url: string;
  installation_id: number;
  installation_account: string;
  linked: boolean;
};
type GitHubInstallation = {
  id: string;
  github_installation_id: number;
  account_login: string;
  created_at: string;
};
type RepositoryContext = {
  repository: GitHubRepository;
  branches: {
    id: string;
    name: string;
    head_sha: string;
    updated_at: string;
  }[];
  commits: {
    id: string;
    sha: string;
    message: string;
    author_login: string;
    committed_at?: string;
  }[];
  pull_requests: {
    id: string;
    number: number;
    title: string;
    state: string;
    draft?: boolean;
    head_sha: string;
    head_ref?: string;
    body?: string;
    url: string;
    updated_at: string;
  }[];
  ci_runs: {
    id: string;
    external_id: string;
    status: string;
    conclusion: string;
    sha: string;
    url: string;
    updated_at: string;
  }[];
};
type RepositoryTreeEntry = {
  path: string;
  type: string;
  size: number;
  sha: string;
};
type RepositoryFile = RepositoryTreeEntry & {
  ref: string;
  content: string;
};
type RepositorySearchMatch = {
  path: string;
  sha: string;
  line: number;
  snippet: string;
};
type RepositorySnapshot = {
  id: string;
  commit_sha: string;
  ref_name: string;
  status: string;
  file_count: number;
  symbol_count: number;
  skipped_count: number;
  error_message?: string;
  finished_at?: string;
};
type RepositorySnapshotEdge = {
  from: string;
  to: string;
  kind: string;
  confidence: string;
  provenance: string;
};
type KnowledgeRevision = {
  id: string;
  document_id: string;
  revision_number: number;
  content: string;
  provenance: string;
  created_by: string;
  created_at: string;
};
type KnowledgeDocument = {
  id: string;
  slug: string;
  title: string;
  kind: string;
  current_provenance: string;
  updated_at: string;
  latest_revision?: KnowledgeRevision;
};

function positionalKnowledgeDiff(previous: string, current: string): string {
  // ponytail: positional line diff keeps the UI bounded; add an LCS diff only if revision review needs edit-aware hunks.
  const previousLines = previous.split("\n");
  const currentLines = current.split("\n");
  const lines: string[] = [];
  const count = Math.min(Math.max(previousLines.length, currentLines.length), 2000);
  for (let index = 0; index < count; index += 1) {
    const before = previousLines[index];
    const after = currentLines[index];
    if (before === after) {
      if (after !== undefined) lines.push(`  ${after}`);
      continue;
    }
    if (before !== undefined) lines.push(`- ${before}`);
    if (after !== undefined) lines.push(`+ ${after}`);
  }
  if (previousLines.length > count || currentLines.length > count) lines.push("… diff truncated …");
  return lines.join("\n");
}
type SessionState = "loading" | "ready" | "signed-out" | "unavailable";
type Detail = {
  item: WorkItem;
  comments: Comment[];
  labels: Label[];
  links: WorkItemLink[];
  audit: AuditRecord[];
  specification: Specification | null;
  readiness: Readiness;
  specificationVersions: SpecificationFieldVersion[];
  proposals: SpecificationProposal[];
  analyses: SpecificationAnalysis[];
  attachments: Attachment[];
};

class RequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    readonly details?: unknown,
  ) {
    super(message);
  }
}

const apiBase = (process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? "").replace(
  /\/+$/,
  "",
);
const initialProjectID = process.env.NEXT_PUBLIC_FORGEFLOW_PROJECT_ID ?? "";
const selectedProjectKey = "forgeflow:selected-project";
const selectedOrganizationKey = "forgeflow:selected-organization";
const devHeaders: Record<string, string> = {};
if (process.env.NEXT_PUBLIC_FORGEFLOW_DEV_AUTH === "true") {
  if (process.env.NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID)
    devHeaders["X-Organization-ID"] =
      process.env.NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID;
  if (process.env.NEXT_PUBLIC_FORGEFLOW_ACTOR_ID)
    devHeaders["X-Actor-ID"] = process.env.NEXT_PUBLIC_FORGEFLOW_ACTOR_ID;
}

const priorities = ["HIGHEST", "HIGH", "MEDIUM", "LOW", "LOWEST"];
const types = ["TASK", "STORY", "BUG", "EPIC", "SUB_TASK"];
const bugContextFields = [
  ["ENVIRONMENT", "Environment"],
  ["PRECONDITIONS", "Preconditions"],
  ["FREQUENCY", "Frequency"],
  ["AFFECTED_VERSION", "Affected version"],
  ["SUSPECTED_ROOT_CAUSE", "Suspected root cause"],
  ["SECURITY_IMPACT", "Security impact"],
  ["BUSINESS_IMPACT", "Business impact"],
] as const;
const transitionKeys: Record<string, Record<string, string>> = {
  RAW: { REFINING: "start_refining", CANCELLED: "cancel" },
  REFINING: {
    REVIEW_REQUIRED: "request_review",
    CANCELLED: "cancel_from_refining",
  },
  REVIEW_REQUIRED: { READY: "mark_ready", CANCELLED: "cancel_from_review" },
  READY: { IN_PROGRESS: "start_work", CANCELLED: "cancel_from_ready" },
  IN_PROGRESS: {
    CODE_REVIEW: "submit_code_review",
    CANCELLED: "cancel_from_progress",
  },
  CODE_REVIEW: { IN_PROGRESS: "request_changes", QA: "move_to_qa" },
  QA: { IN_PROGRESS: "qa_failed", DONE: "complete" },
};
const workflowRuleOptions = [
  ["require_specification_ready", "Specification ready"],
  ["require_human_verification", "Human verification"],
  ["require_assignee", "Assignee"],
  ["require_repository", "Repository"],
  ["require_pull_request", "Pull request"],
  ["require_ci_success", "CI success"],
  ["require_permission", "Permission"],
] as const;
const agentRunNextStatus: Record<string, string> = {
  PREPARING: "PLANNING",
  PLANNING: "INVESTIGATING",
  INVESTIGATING: "IMPLEMENTING",
  IMPLEMENTING: "TESTING",
  TESTING: "REVIEWING",
  REVIEWING: "COMPLETED",
};

function apiURL(path: string): string {
  return `${apiBase}${path}`;
}

function csrfToken(): string {
  if (typeof document === "undefined") return "";
  const prefix = "forgeflow_csrf=";
  const value = document.cookie
    .split("; ")
    .find((cookie) => cookie.startsWith(prefix))
    ?.slice(prefix.length);
  return value ? decodeURIComponent(value) : "";
}

function setOrganizationHeader(headers: Headers) {
  if (headers.has("X-Organization-ID")) return;
  try {
    const selectedOrganization = window.localStorage.getItem(
      selectedOrganizationKey,
    );
    if (selectedOrganization) headers.set("X-Organization-ID", selectedOrganization);
  } catch {
    /* convenience only */
  }
}

async function requestJSON<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const token = csrfToken();
  if (token && !headers.has("X-CSRF-Token")) headers.set("X-CSRF-Token", token);
  for (const [name, value] of Object.entries(devHeaders))
    headers.set(name, value);
  if (path !== "/api/v1/organizations") setOrganizationHeader(headers);
  const response = await fetch(apiURL(path), {
    ...init,
    headers,
    credentials: "include",
  });
  const raw = await response.text();
  let payload: unknown;
  if (raw) {
    try {
      payload = JSON.parse(raw);
    } catch {
      payload = undefined;
    }
  }
  if (!response.ok) {
    const message =
      typeof payload === "object" &&
      payload !== null &&
      "message" in payload &&
      typeof payload.message === "string"
        ? payload.message
        : response.status === 401
          ? t("legacy.session-expired")
          : t("legacy.request-failed");
    const code =
      typeof payload === "object" && payload !== null && "code" in payload && typeof payload.code === "string"
        ? payload.code
        : undefined;
    const details =
      typeof payload === "object" && payload !== null && "details" in payload
        ? payload.details
        : undefined;
    const missing =
      typeof details === "object" && details !== null && "missing" in details && Array.isArray(details.missing)
        ? details.missing.filter((item): item is string => typeof item === "string")
        : [];
    throw new RequestError(
      missing.length ? `${message} ${t("legacy.missing", { items: missing.map((item) => readinessLabel(item)).join(", ") })}` : message,
      response.status,
      code,
      details,
    );
  }
  return payload as T;
}

function tone(status: string): string {
  if (status === "READY" || status === "DONE") return "status-good";
  if (["IN_PROGRESS", "CODE_REVIEW", "QA"].includes(status))
    return "status-active";
  if (status === "CANCELLED") return "status-muted";
  return "status-neutral";
}

function priorityLabel(priority?: string): string {
  return statusLabel(priority ?? "MEDIUM");
}

function dateValue(value?: string | null): string {
  return value ? value.slice(0, 10) : "";
}
function toDueAt(value: string): string {
  return value ? new Date(`${value}T23:59:59`).toISOString() : "";
}
function formatDate(value?: string | null): string {
  if (!value) return t("legacy.no-deadline");
  const locale = typeof document !== "undefined" && document.documentElement.lang === "en" ? "en-US" : "vi-VN";
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(new Date(value));
}
function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

export function WorkItemWorkspace() {
  const [sessionState, setSessionState] = useState<SessionState>("loading");
  const [currentActorID, setCurrentActorID] = useState("");
  const [bootstrapError, setBootstrapError] = useState("");
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [organizationID, setOrganizationID] = useState("");
  const [organizationSwitching, setOrganizationSwitching] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [projectID, setProjectID] = useState(initialProjectID);
  const [workspaceID, setWorkspaceID] = useState("");
  const [items, setItems] = useState<WorkItem[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [boardColumns, setBoardColumns] = useState<BoardColumn[]>([]);
  const [boardTruncated, setBoardTruncated] = useState(false);
  const [workflowStatuses, setWorkflowStatuses] = useState<WorkflowStatus[]>(
    [],
  );
  const [workflowTransitions, setWorkflowTransitions] = useState<
    WorkflowTransition[]
  >([]);
  const [workflowName, setWorkflowName] = useState("Default");
  const [workflowDraftStatuses, setWorkflowDraftStatuses] = useState<
    WorkflowStatus[]
  >([]);
  const [workflowDraftTransitions, setWorkflowDraftTransitions] = useState<
    WorkflowTransition[]
  >([]);
  const [workflowEditorVisible, setWorkflowEditorVisible] = useState(false);
  const [workflowBusy, setWorkflowBusy] = useState(false);
  const [workflowError, setWorkflowError] = useState("");
  const [view, setView] = useState<"list" | "board">("list");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [createType, setCreateType] = useState("TASK");
  const [createPriority, setCreatePriority] = useState("MEDIUM");
  const [createDueDate, setCreateDueDate] = useState("");
  const [createEstimate, setCreateEstimate] = useState("");
  const [createParentID, setCreateParentID] = useState("");
  const [createAssigneeID, setCreateAssigneeID] = useState("");
  const [createSprintID, setCreateSprintID] = useState("");
  const [createRepositoryID, setCreateRepositoryID] = useState("");
  const [query, setQuery] = useState("");
  const [filterStatus, setFilterStatus] = useState("");
  const [filterType, setFilterType] = useState("");
  const [filterPriority, setFilterPriority] = useState("");
  const [filterAssignee, setFilterAssignee] = useState("");
  const [filterSprint, setFilterSprint] = useState("");
  const [filterRepository, setFilterRepository] = useState("");
  const [includeArchived, setIncludeArchived] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [boardLoading, setBoardLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [setupVisible, setSetupVisible] = useState(false);
  const [setupBusy, setSetupBusy] = useState(false);
  const [setupError, setSetupError] = useState("");
  const [projectKey, setProjectKey] = useState("APP");
  const [projectName, setProjectName] = useState("My project");
  const [projectRenameVisible, setProjectRenameVisible] = useState(false);
  const [projectRenameName, setProjectRenameName] = useState("");
  const [projectRenameBusy, setProjectRenameBusy] = useState(false);
  const [projectRenameError, setProjectRenameError] = useState("");
  const [workspaceManageVisible, setWorkspaceManageVisible] = useState(false);
  const [workspaceCreateKey, setWorkspaceCreateKey] = useState("");
  const [workspaceCreateName, setWorkspaceCreateName] = useState("");
  const [workspaceRenameName, setWorkspaceRenameName] = useState("");
  const [workspaceBusy, setWorkspaceBusy] = useState(false);
  const [workspaceError, setWorkspaceError] = useState("");
  const [repositories, setRepositories] = useState<GitHubRepository[]>([]);
  const [githubInstallations, setGitHubInstallations] = useState<GitHubInstallation[]>([]);
  const [repositoryLoading, setRepositoryLoading] = useState(false);
  const [repositoryError, setRepositoryError] = useState("");
  const [linkingRepositoryID, setLinkingRepositoryID] = useState("");
  const [repositoryContexts, setRepositoryContexts] = useState<
    Record<string, RepositoryContext>
  >({});
  const [selectedRepositoryContextID, setSelectedRepositoryContextID] =
    useState("");
  const [repositoryContextLoading, setRepositoryContextLoading] =
    useState(false);
  const [repositoryContextError, setRepositoryContextError] = useState("");
  const [draftPRTitle, setDraftPRTitle] = useState("");
  const [draftPRBody, setDraftPRBody] = useState("");
  const [draftPRHead, setDraftPRHead] = useState("");
  const [draftPRBase, setDraftPRBase] = useState("");
  const [draftPRBusy, setDraftPRBusy] = useState(false);
  const [repositoryTree, setRepositoryTree] = useState<RepositoryTreeEntry[]>(
    [],
  );
  const [repositoryTreeLoading, setRepositoryTreeLoading] = useState(false);
  const [repositoryTreeVisible, setRepositoryTreeVisible] = useState(false);
  const [repositoryFile, setRepositoryFile] = useState<RepositoryFile | null>(
    null,
  );
  const [repositoryFileLoading, setRepositoryFileLoading] = useState(false);
  const [repositorySearchQuery, setRepositorySearchQuery] = useState("");
  const [repositorySearchResults, setRepositorySearchResults] = useState<
    RepositorySearchMatch[]
  >([]);
  const [repositorySearchBusy, setRepositorySearchBusy] = useState(false);
  const [repositorySnapshots, setRepositorySnapshots] = useState<Record<string, RepositorySnapshot[]>>({});
  const [repositorySnapshotEdges, setRepositorySnapshotEdges] = useState<Record<string, RepositorySnapshotEdge[]>>({});
  const [repositorySnapshotEdgesLoading, setRepositorySnapshotEdgesLoading] = useState("");
  const [repositorySnapshotLoading, setRepositorySnapshotLoading] = useState(false);
  const [repositorySnapshotBusy, setRepositorySnapshotBusy] = useState(false);
  const [repositorySnapshotError, setRepositorySnapshotError] = useState("");
  const [knowledgeDocuments, setKnowledgeDocuments] = useState<Record<string, KnowledgeDocument[]>>({});
  const [knowledgeLoading, setKnowledgeLoading] = useState(false);
  const [knowledgeBusy, setKnowledgeBusy] = useState(false);
  const [knowledgeError, setKnowledgeError] = useState("");
  const [knowledgeTitle, setKnowledgeTitle] = useState("");
  const [knowledgeSlug, setKnowledgeSlug] = useState("");
  const [knowledgeKind, setKnowledgeKind] = useState("CONVENTIONS");
  const [knowledgeContent, setKnowledgeContent] = useState("");
  const [selectedKnowledgeDocument, setSelectedKnowledgeDocument] = useState<KnowledgeDocument | null>(null);
  const [knowledgeRevisions, setKnowledgeRevisions] = useState<KnowledgeRevision[]>([]);
  const [knowledgeCompareRevision, setKnowledgeCompareRevision] = useState<KnowledgeRevision | null>(null);
  const [knowledgeRevisionContent, setKnowledgeRevisionContent] = useState("");
  const [knowledgeRevisionProvenance, setKnowledgeRevisionProvenance] = useState("MANUAL");
  const [knowledgeRevisionBusy, setKnowledgeRevisionBusy] = useState(false);
  const [members, setMembers] = useState<Member[]>([]);
  const [organizationMembers, setOrganizationMembers] = useState<Member[]>([]);
  const [organizationMembersLoading, setOrganizationMembersLoading] = useState(false);
  const [organizationMemberBusy, setOrganizationMemberBusy] = useState("");
  const [membersLoading, setMembersLoading] = useState(false);
  const [membersError, setMembersError] = useState("");
  const [memberRoleBusy, setMemberRoleBusy] = useState("");
  const [memberLogin, setMemberLogin] = useState("");
  const [memberRole, setMemberRole] = useState("developer");
  const [memberAddBusy, setMemberAddBusy] = useState(false);
  const [customFields, setCustomFields] = useState<CustomFieldDefinition[]>([]);
  const [customFieldValues, setCustomFieldValues] = useState<CustomFieldValue[]>([]);
  const [customFieldsLoading, setCustomFieldsLoading] = useState(false);
  const [customFieldsError, setCustomFieldsError] = useState("");
  const [customFieldBusy, setCustomFieldBusy] = useState(false);
  const [customFieldName, setCustomFieldName] = useState("");
  const [customFieldKey, setCustomFieldKey] = useState("");
  const [customFieldType, setCustomFieldType] = useState<CustomFieldDefinition["value_type"]>("TEXT");
  const [customFieldOptions, setCustomFieldOptions] = useState("");
  const [editingCustomFieldID, setEditingCustomFieldID] = useState("");
  const [customFieldEditName, setCustomFieldEditName] = useState("");
  const [customFieldEditOptions, setCustomFieldEditOptions] = useState("");
  const [attachmentLoading, setAttachmentLoading] = useState(false);
  const [attachmentBusy, setAttachmentBusy] = useState(false);
  const [attachmentError, setAttachmentError] = useState("");
  const [sprints, setSprints] = useState<Sprint[]>([]);
  const [sprintsLoading, setSprintsLoading] = useState(false);
  const [sprintError, setSprintError] = useState("");
  const [sprintName, setSprintName] = useState("");
  const [sprintGoal, setSprintGoal] = useState("");
  const [sprintStartsAt, setSprintStartsAt] = useState("");
  const [sprintEndsAt, setSprintEndsAt] = useState("");
  const [editingSprintID, setEditingSprintID] = useState("");
  const [sprintBusy, setSprintBusy] = useState(false);
  const [agentRuns, setAgentRuns] = useState<AgentRun[]>([]);
  const [agentRunsLoading, setAgentRunsLoading] = useState(false);
  const [agentRunError, setAgentRunError] = useState("");
  const [agentRunBusy, setAgentRunBusy] = useState(false);
  const [selectedAgentRunID, setSelectedAgentRunID] = useState("");
  const [agentRunDetails, setAgentRunDetails] = useState<
    Record<string, { steps: AgentRunStep[]; artifacts: AgentRunArtifact[] }>
  >({});
  const [agentRunDetailLoading, setAgentRunDetailLoading] = useState(false);
  const [agentTestResultBusy, setAgentTestResultBusy] = useState(false);
  const [agentTestCaseNotes, setAgentTestCaseNotes] = useState<Record<string, string>>({});
  const [agentTestReviewNote, setAgentTestReviewNote] = useState("");
  const [followUpTestCasePositions, setFollowUpTestCasePositions] = useState<number[] | null>(null);
  const [agentProvider, setAgentProvider] = useState<"codex" | "claude">(
    "codex",
  );
  const [agentPrompt, setAgentPrompt] = useState("");
  const [accessTokens, setAccessTokens] = useState<AccessToken[]>([]);
  const [tokenName, setTokenName] = useState("Codex local MCP");
  const [tokenProfile, setTokenProfile] = useState<"read" | "mcp">("mcp");
  const [tokenExpiry, setTokenExpiry] = useState("90");
  const [tokenBusy, setTokenBusy] = useState(false);
  const [tokenError, setTokenError] = useState("");
  const [newToken, setNewToken] = useState("");
  const [tokenCopied, setTokenCopied] = useState(false);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [notificationsLoading, setNotificationsLoading] = useState(false);
  const [notificationError, setNotificationError] = useState("");
  const [automationRules, setAutomationRules] = useState<AutomationRule[]>([]);
  const [automationLoading, setAutomationLoading] = useState(false);
  const [automationError, setAutomationError] = useState("");
  const [automationBusy, setAutomationBusy] = useState(false);
  const [automationName, setAutomationName] = useState("Notify work changes");
  const [automationEvent, setAutomationEvent] = useState(
    "work_item.transitioned",
  );
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [detail, setDetail] = useState<Detail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [editTitle, setEditTitle] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editParent, setEditParent] = useState("");
  const [editRepository, setEditRepository] = useState("");
  const [editPriority, setEditPriority] = useState("MEDIUM");
  const [editDueDate, setEditDueDate] = useState("");
  const [editEstimate, setEditEstimate] = useState("");
  const [editAssignee, setEditAssignee] = useState("");
  const [editSprint, setEditSprint] = useState("");
  const [savingItem, setSavingItem] = useState(false);
  const [archiveBusy, setArchiveBusy] = useState(false);
  const [commentBody, setCommentBody] = useState("");
  const [commentBusy, setCommentBusy] = useState(false);
  const [editingCommentID, setEditingCommentID] = useState("");
  const [editingCommentBody, setEditingCommentBody] = useState("");
  const [linkTargetID, setLinkTargetID] = useState("");
  const [linkRelation, setLinkRelation] = useState("relates_to");
  const [linkBusy, setLinkBusy] = useState(false);
  const [labelName, setLabelName] = useState("");
  const [labelBusy, setLabelBusy] = useState(false);
  const [specSummary, setSpecSummary] = useState("");
  const [specFields, setSpecFields] = useState<Record<string, string>>({});
  const [specRepositoryID, setSpecRepositoryID] = useState("");
  const [reproductionSteps, setReproductionSteps] = useState<
    ReproductionStep[]
  >([]);
  const [acceptanceCriteria, setAcceptanceCriteria] = useState<
    AcceptanceCriterion[]
  >([]);
  const [regressionTestCases, setRegressionTestCases] = useState<
    RegressionTestCase[]
  >([]);
  const [specContextRefs, setSpecContextRefs] = useState<
    SpecificationContextRef[]
  >([]);
  const [specDirty, setSpecDirty] = useState(false);
  const [savingSpec, setSavingSpec] = useState(false);
  const [analysisHypothesis, setAnalysisHypothesis] = useState("");
  const [analysisBlastRadius, setAnalysisBlastRadius] = useState("");
  const [analysisImplementationPlan, setAnalysisImplementationPlan] =
    useState("");
  const [analysisTestPlan, setAnalysisTestPlan] = useState("");
  const [analysisEvidenceRefs, setAnalysisEvidenceRefs] = useState("");
  const [analysisConfidence, setAnalysisConfidence] = useState("0.5");
  const [analysisBusy, setAnalysisBusy] = useState(false);
  const [draggedItemID, setDraggedItemID] = useState("");
  const [rankBusyID, setRankBusyID] = useState("");
  const requestRef = useRef(0);
  const detailRequestRef = useRef(0);
  const abortRef = useRef<AbortController | null>(null);
  const boardAbortRef = useRef<AbortController | null>(null);
  const repositoryAbortRef = useRef<AbortController | null>(null);
  const selectedProject = projects.find((project) => project.id === projectID);
  const selectedWorkspace = workspaces.find((workspace) => workspace.id === workspaceID);
  const availableTransitionKeys = useMemo(() => {
    if (workflowTransitions.length === 0) return transitionKeys;
    return workflowTransitions.reduce<Record<string, Record<string, string>>>(
      (result, transition) => {
        result[transition.from_status] ??= {};
        result[transition.from_status][transition.to_status] = transition.key;
        return result;
      },
      {},
    );
  }, [workflowTransitions]);

  const loadWorkflow = useCallback(async () => {
    if (sessionState !== "ready" || !projectID.trim()) {
      setWorkflowStatuses([]);
      setWorkflowTransitions([]);
      setWorkflowDraftStatuses([]);
      setWorkflowDraftTransitions([]);
      setWorkflowName("Default");
      return;
    }
    try {
      const payload = await requestJSON<{
        name?: string;
        statuses?: WorkflowStatus[];
        transitions?: WorkflowTransition[];
      }>("/api/v1/workflows/current", {
        headers: { "X-Project-ID": projectID },
      });
      const statuses = payload.statuses ?? [];
      const transitions = payload.transitions ?? [];
      setWorkflowName(payload.name ?? "Default");
      setWorkflowStatuses(statuses);
      setWorkflowTransitions(transitions);
      setWorkflowDraftStatuses(statuses.map((status) => ({ ...status })));
      setWorkflowDraftTransitions(
        transitions.map((transition) => ({
          ...transition,
          required_rules: [...(transition.required_rules ?? [])],
        })),
      );
    } catch {
      // Keep the default map usable if a custom workflow endpoint is unavailable.
      setWorkflowStatuses([]);
      setWorkflowTransitions([]);
      setWorkflowDraftStatuses([]);
      setWorkflowDraftTransitions([]);
    }
  }, [projectID, sessionState]);

  useEffect(() => {
    const timer = window.setTimeout(() => void loadWorkflow(), 0);
    return () => window.clearTimeout(timer);
  }, [loadWorkflow]);

  function updateWorkflowStatus(key: string, patch: Partial<WorkflowStatus>) {
    const nextKey = patch.key ?? key;
    setWorkflowDraftStatuses((current) =>
      current.map((status) =>
        status.key === key ? { ...status, ...patch } : status,
      ),
    );
    if (nextKey !== key) {
      setWorkflowDraftTransitions((current) =>
        current.map((transition) => ({
          ...transition,
          from_status:
            transition.from_status === key ? nextKey : transition.from_status,
          to_status:
            transition.to_status === key ? nextKey : transition.to_status,
        })),
      );
    }
  }

  function removeWorkflowStatus(key: string) {
    if (key === "RAW") return;
    setWorkflowDraftStatuses((current) =>
      current.filter((status) => status.key !== key),
    );
    setWorkflowDraftTransitions((current) =>
      current.filter(
        (transition) =>
          transition.from_status !== key && transition.to_status !== key,
      ),
    );
  }

  function addWorkflowStatus() {
    const existing = new Set(workflowDraftStatuses.map((status) => status.key));
    let index = 1;
    let key = `STAGE_${index}`;
    while (existing.has(key)) {
      index += 1;
      key = `STAGE_${index}`;
    }
    const position =
      Math.max(0, ...workflowDraftStatuses.map((status) => status.position)) +
      10;
    setWorkflowDraftStatuses((current) => [
      ...current,
      {
        key,
        display_name: "New stage",
        category: "TODO",
        position,
        is_terminal: false,
      },
    ]);
  }

  function updateWorkflowTransition(
    key: string,
    patch: Partial<WorkflowTransition>,
  ) {
    setWorkflowDraftTransitions((current) =>
      current.map((transition) =>
        transition.key === key ? { ...transition, ...patch } : transition,
      ),
    );
  }

  function removeWorkflowTransition(key: string) {
    setWorkflowDraftTransitions((current) =>
      current.filter((transition) => transition.key !== key),
    );
  }

  function addWorkflowTransition() {
    if (workflowDraftStatuses.length < 2) return;
    const from = workflowDraftStatuses[0].key;
    const to = workflowDraftStatuses.find((status) => status.key !== from)?.key;
    if (!to) return;
    const existing = new Set(
      workflowDraftTransitions.map((transition) => transition.key),
    );
    let index = 1;
    let key = `move_${from.toLowerCase()}_${to.toLowerCase()}_${index}`;
    while (existing.has(key)) {
      index += 1;
      key = `move_${from.toLowerCase()}_${to.toLowerCase()}_${index}`;
    }
    setWorkflowDraftTransitions((current) => [
      ...current,
      {
        key,
        from_status: from,
        to_status: to,
        display_name: "Move item",
        required_rules: [],
      },
    ]);
  }

  function cancelWorkflowEdit() {
    setWorkflowDraftStatuses(workflowStatuses.map((status) => ({ ...status })));
    setWorkflowDraftTransitions(
      workflowTransitions.map((transition) => ({
        ...transition,
        required_rules: [...(transition.required_rules ?? [])],
      })),
    );
    setWorkflowEditorVisible(false);
    setWorkflowError("");
  }

  async function saveWorkflow(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!projectID || workflowBusy) return;
    setWorkflowBusy(true);
    setWorkflowError("");
    try {
      await requestJSON<{ name: string; statuses: WorkflowStatus[]; transitions: WorkflowTransition[] }>(
        "/api/v1/workflows/current",
        {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({
            name: workflowName,
            statuses: workflowDraftStatuses,
            transitions: workflowDraftTransitions,
          }),
        },
      );
      setWorkflowEditorVisible(false);
      await loadWorkflow();
      if (view === "board") await loadBoard();
      else await load();
    } catch (requestError) {
      setWorkflowError(
        requestError instanceof Error
          ? requestError.message
          : "Could not save the workflow.",
      );
    } finally {
      setWorkflowBusy(false);
    }
  }

  const loadAccessTokens = useCallback(async () => {
    if (sessionState !== "ready") return;
    try {
      const payload = await requestJSON<{ tokens?: AccessToken[] }>(
        "/api/v1/me/tokens",
      );
      setAccessTokens(payload.tokens ?? []);
      setTokenError("");
    } catch (requestError) {
      setTokenError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load access tokens.",
      );
    }
  }, [sessionState]);

  useEffect(() => {
    if (sessionState !== "ready") return;
    const timer = window.setTimeout(() => void loadAccessTokens(), 0);
    return () => window.clearTimeout(timer);
  }, [loadAccessTokens, sessionState]);

  const bootstrap = useCallback(async (signal?: AbortSignal) => {
    setSessionState("loading");
    setBootstrapError("");
    try {
      const organizationPayload = await requestJSON<{
        items?: Organization[];
      }>("/api/v1/organizations", { signal });
      const nextOrganizations = organizationPayload.items ?? [];
      let preferredOrganization = "";
      try {
        preferredOrganization =
          window.localStorage.getItem(selectedOrganizationKey) ?? "";
      } catch {
        /* convenience only */
      }
      const selectedOrganization =
        nextOrganizations.find((organization) => organization.id === preferredOrganization) ??
        nextOrganizations[0];
      if (selectedOrganization) {
        preferredOrganization = selectedOrganization.id;
        try {
          window.localStorage.setItem(
            selectedOrganizationKey,
            selectedOrganization.id,
          );
        } catch {
          /* convenience only */
        }
      }
      setOrganizations(nextOrganizations);
      setOrganizationID(preferredOrganization);
      const me = await requestJSON<{ id?: string }>("/api/v1/me", { signal });
      setCurrentActorID(me.id ?? "");
      const [projectPayload, workspacePayload] = await Promise.all([
        requestJSON<{ items?: Project[] }>("/api/v1/projects", { signal }),
        requestJSON<{ items?: Workspace[] }>("/api/v1/workspaces", { signal }),
      ]);
      const nextProjects = projectPayload.items ?? [];
      const nextWorkspaces = workspacePayload.items ?? [];
      if (signal?.aborted) return;
      const configuredProject =
        initialProjectID &&
        !nextProjects.some((project) => project.id === initialProjectID)
          ? {
              id: initialProjectID,
              key: "CONFIGURED",
              display_name: "Configured project",
              workspace_id: "",
            }
          : undefined;
      const visibleProjects = configuredProject
        ? [...nextProjects, configuredProject]
        : nextProjects;
      setProjects(visibleProjects);
      setWorkspaces(nextWorkspaces);
      let preferred = initialProjectID;
      try {
        preferred =
          preferred || window.localStorage.getItem(selectedProjectKey) || "";
      } catch {
        /* convenience only */
      }
      const selected =
        visibleProjects.find((project) => project.id === preferred) ??
        visibleProjects[0];
      setProjectID(selected?.id ?? "");
      const nextWorkspaceID = selected?.workspace_id || nextWorkspaces[0]?.id || "";
      setWorkspaceID(nextWorkspaceID);
      setWorkspaceRenameName(nextWorkspaces.find((workspace) => workspace.id === nextWorkspaceID)?.display_name ?? "");
      setSessionState("ready");
    } catch (requestError) {
      if (signal?.aborted) return;
      if (requestError instanceof RequestError && requestError.status === 401) {
        setAccessTokens([]);
        setNewToken("");
        setSessionState("signed-out");
        return;
      }
      setSessionState("unavailable");
      setBootstrapError(
        requestError instanceof Error
          ? requestError.message
          : "Forgeflow is temporarily unavailable.",
      );
    }
  }, []);

  async function switchOrganization(nextOrganizationID: string) {
    if (!nextOrganizationID || nextOrganizationID === organizationID) return;
    setOrganizationSwitching(true);
    try {
      window.localStorage.setItem(selectedOrganizationKey, nextOrganizationID);
    } catch {
      /* convenience only */
    }
    setOrganizationID(nextOrganizationID);
    setProjects([]);
    setWorkspaces([]);
    setProjectID("");
    setItems([]);
    setBoardColumns([]);
    setDetail(null);
    try {
      await bootstrap();
    } finally {
      setOrganizationSwitching(false);
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void bootstrap(controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [bootstrap]);

  useEffect(() => {
    if (sessionState !== "ready") return;
    try {
      if (projectID) window.localStorage.setItem(selectedProjectKey, projectID);
      else window.localStorage.removeItem(selectedProjectKey);
    } catch {
      /* convenience only */
    }
  }, [projectID, sessionState]);

  const mergeItem = useCallback((next: WorkItem) => {
    setItems((current) =>
      current.map((item) =>
        item.id === next.id ? { ...item, ...next } : item,
      ),
    );
    setBoardColumns((current) =>
      current.map((column) => ({
        ...column,
        items: column.items.map((item) =>
          item.id === next.id ? { ...item, ...next } : item,
        ),
      })),
    );
    setDetail((current) =>
      current ? { ...current, item: { ...current.item, ...next } } : current,
    );
  }, []);

  const load = useCallback(
    async (search = query) => {
      const requestID = ++requestRef.current;
      abortRef.current?.abort();
      if (sessionState !== "ready" || !projectID.trim()) {
        setItems([]);
        setNextCursor("");
        setLastUpdated(null);
        setLoading(false);
        return;
      }
      const controller = new AbortController();
      abortRef.current = controller;
      setLoading(true);
      setError("");
      try {
        const params = new URLSearchParams({ limit: "50" });
        if (search.trim()) params.set("q", search.trim());
        if (filterStatus) params.set("status", filterStatus);
        if (filterType) params.set("type", filterType);
        if (filterPriority) params.set("priority", filterPriority);
        if (filterAssignee) params.set("assignee_id", filterAssignee);
        if (filterSprint) params.set("sprint_id", filterSprint);
        if (filterRepository) params.set("repository_id", filterRepository);
        params.set("sort", "backlog");
        if (includeArchived) params.set("include_archived", "true");
        const payload = await requestJSON<{ items?: WorkItem[]; next_cursor?: string }>(
          `/api/v1/work-items?${params}`,
          { headers: { "X-Project-ID": projectID }, signal: controller.signal },
        );
        if (requestID !== requestRef.current) return;
        setItems(payload.items ?? []);
        setNextCursor(payload.next_cursor ?? "");
        setLastUpdated(new Date());
      } catch (requestError) {
        if (controller.signal.aborted || requestID !== requestRef.current)
          return;
        setError(
          requestError instanceof Error
            ? requestError.message
            : "Could not load work items.",
        );
      } finally {
        if (requestID === requestRef.current) setLoading(false);
      }
    },
    [
      filterAssignee,
      filterPriority,
      filterRepository,
      filterStatus,
      filterSprint,
      filterType,
      includeArchived,
      projectID,
      query,
      sessionState,
    ],
  );

  useEffect(() => () => abortRef.current?.abort(), []);

  async function loadMoreItems() {
    if (!nextCursor || loadingMore || sessionState !== "ready" || !projectID.trim()) return;
    setLoadingMore(true);
    setError("");
    try {
      const params = new URLSearchParams({ limit: "50", cursor: nextCursor });
      if (query.trim()) params.set("q", query.trim());
      if (filterStatus) params.set("status", filterStatus);
      if (filterType) params.set("type", filterType);
      if (filterPriority) params.set("priority", filterPriority);
      if (filterAssignee) params.set("assignee_id", filterAssignee);
      if (filterSprint) params.set("sprint_id", filterSprint);
      if (filterRepository) params.set("repository_id", filterRepository);
      params.set("sort", "backlog");
      if (includeArchived) params.set("include_archived", "true");
      const payload = await requestJSON<{ items?: WorkItem[]; next_cursor?: string }>(
        `/api/v1/work-items?${params}`,
        { headers: { "X-Project-ID": projectID } },
      );
      setItems((current) => {
        const seen = new Set(current.map((item) => item.id));
        return [...current, ...(payload.items ?? []).filter((item) => !seen.has(item.id))];
      });
      setNextCursor(payload.next_cursor ?? "");
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "Could not load more work items.");
    } finally {
      setLoadingMore(false);
    }
  }
  useEffect(() => {
    if (sessionState !== "ready" || view !== "list") return;
    const timer = window.setTimeout(
      () => {
        void load();
      },
      query.trim() ? 280 : 0,
    );
    return () => window.clearTimeout(timer);
  }, [load, query, sessionState, view]);

  const loadBoard = useCallback(async () => {
    boardAbortRef.current?.abort();
    if (sessionState !== "ready" || !projectID.trim()) {
      setBoardColumns([]);
      setBoardTruncated(false);
      setBoardLoading(false);
      return;
    }
    const controller = new AbortController();
    boardAbortRef.current = controller;
    setBoardLoading(true);
    setError("");
    try {
      const payload = await requestJSON<{ columns?: BoardColumn[]; truncated?: boolean }>(
        "/api/v1/boards/current",
        { headers: { "X-Project-ID": projectID }, signal: controller.signal },
      );
      if (controller.signal.aborted) return;
      setBoardColumns(payload.columns ?? []);
      setBoardTruncated(payload.truncated === true);
      setLastUpdated(new Date());
    } catch (requestError) {
      if (controller.signal.aborted) return;
      setError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load the board.",
      );
    } finally {
      setBoardLoading(false);
    }
  }, [projectID, sessionState]);

  useEffect(() => {
    if (sessionState !== "ready" || view !== "board") return;
    const timer = window.setTimeout(() => {
      void loadBoard();
    }, 0);
    return () => {
      window.clearTimeout(timer);
      boardAbortRef.current?.abort();
    };
  }, [loadBoard, sessionState, view]);

  const loadRepositories = useCallback(async () => {
    repositoryAbortRef.current?.abort();
    if (sessionState !== "ready" || !projectID.trim()) {
      setRepositories([]);
      setGitHubInstallations([]);
      setRepositoryLoading(false);
      setRepositoryError("");
      return;
    }
    const controller = new AbortController();
    repositoryAbortRef.current = controller;
    setRepositoryLoading(true);
    setRepositoryError("");
    try {
      const [payload, installationPayload] = await Promise.all([
        requestJSON<{ items?: GitHubRepository[] }>(
          `/api/v1/integrations/github/repositories?project_id=${encodeURIComponent(projectID)}`,
          { signal: controller.signal },
        ),
        requestJSON<{ items?: GitHubInstallation[] }>(
          "/api/v1/integrations/github/installations",
          { signal: controller.signal },
        ),
      ]);
      if (!controller.signal.aborted) {
        const nextRepositories = payload.items ?? [];
        setRepositories(nextRepositories);
        setGitHubInstallations(installationPayload.items ?? []);
        setRepositoryContexts((current) => {
          const next = { ...current };
          for (const id of Object.keys(next)) {
            if (!nextRepositories.some((repository) => repository.id === id))
              delete next[id];
          }
          return next;
        });
      }
    } catch (requestError) {
      if (controller.signal.aborted) return;
      setRepositoryError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load GitHub repositories.",
      );
    } finally {
      if (!controller.signal.aborted) setRepositoryLoading(false);
    }
  }, [projectID, sessionState]);

  async function loadRepositoryContext(repository: GitHubRepository) {
    if (!projectID || repositoryContextLoading) return;
    if (selectedRepositoryContextID !== repository.id) {
      setRepositoryTree([]);
      setRepositoryTreeVisible(false);
      setRepositoryFile(null);
      setRepositorySearchResults([]);
    }
    setSelectedRepositoryContextID(repository.id);
    setRepositoryContextError("");
    const cached = repositoryContexts[repository.id];
    if (cached) return;
    setRepositoryContextLoading(true);
    try {
      const context = await requestJSON<RepositoryContext>(
        `/api/v1/projects/${projectID}/repositories/${repository.id}/context`,
      );
      setRepositoryContexts((current) => ({
        ...current,
        [repository.id]: context,
      }));
    } catch (requestError) {
      setRepositoryContextError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load repository context.",
      );
    } finally {
      setRepositoryContextLoading(false);
    }
  }

  async function createDraftPullRequest(repositoryID: string) {
    if (
      !projectID ||
      !detail ||
      draftPRBusy ||
      !draftPRTitle.trim() ||
      !draftPRHead.trim()
    )
      return;
    setDraftPRBusy(true);
    setRepositoryContextError("");
    try {
      const created = await requestJSON<RepositoryContext["pull_requests"][number]>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/pull-requests`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
            "Idempotency-Key": `draft-pr-${crypto.randomUUID()}`,
          },
          body: JSON.stringify({
            title: draftPRTitle.trim(),
            body: draftPRBody,
            head: draftPRHead.trim(),
            base: draftPRBase.trim(),
          }),
        },
      );
      setRepositoryContexts((current) => {
        const context = current[repositoryID];
        if (!context) return current;
        return {
          ...current,
          [repositoryID]: {
            ...context,
            pull_requests: [
              created,
              ...context.pull_requests.filter((item) => item.number !== created.number),
            ],
          },
        };
      });
      setDraftPRTitle(`${detail.item.key}: ${detail.item.title}`);
      setDraftPRBody(detail.item.description ?? "");
      setDraftPRHead("");
      setDraftPRBase("");
    } catch (requestError) {
      setRepositoryContextError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create the draft pull request.",
      );
    } finally {
      setDraftPRBusy(false);
    }
  }

  async function loadRepositoryTree(repositoryID: string) {
    if (!projectID || repositoryTreeLoading) return;
    setRepositoryTreeVisible(true);
    setRepositoryTreeLoading(true);
    setRepositoryContextError("");
    try {
      const payload = await requestJSON<{ items?: RepositoryTreeEntry[] }>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/tree`,
      );
      setRepositoryTree(payload.items ?? []);
      setRepositoryFile(null);
    } catch (requestError) {
      setRepositoryContextError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load repository files.",
      );
    } finally {
      setRepositoryTreeLoading(false);
    }
  }

  async function loadRepositoryFile(repositoryID: string, path: string) {
    if (!projectID || repositoryFileLoading) return;
    setRepositoryFileLoading(true);
    setRepositoryContextError("");
    try {
      const file = await requestJSON<RepositoryFile>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/file?path=${encodeURIComponent(path)}`,
      );
      setRepositoryFile(file);
    } catch (requestError) {
      setRepositoryContextError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load this repository file.",
      );
    } finally {
      setRepositoryFileLoading(false);
    }
  }

  async function searchRepositoryCode(repositoryID: string) {
    if (!projectID || !repositorySearchQuery.trim() || repositorySearchBusy)
      return;
    setRepositorySearchBusy(true);
    setRepositoryContextError("");
    try {
      const payload = await requestJSON<{ items?: RepositorySearchMatch[] }>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/search?q=${encodeURIComponent(repositorySearchQuery.trim())}&limit=20`,
      );
      setRepositorySearchResults(payload.items ?? []);
    } catch (requestError) {
      setRepositoryContextError(
        requestError instanceof Error
          ? requestError.message
          : "Could not search this repository.",
      );
    } finally {
      setRepositorySearchBusy(false);
    }
  }

  async function loadRepositorySnapshots(repositoryID: string) {
    if (!projectID || repositorySnapshotLoading) return;
    setRepositorySnapshotLoading(true);
    setRepositorySnapshotError("");
    try {
      const payload = await requestJSON<{ items?: RepositorySnapshot[] }>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/snapshots?limit=10`,
      );
      setRepositorySnapshots((current) => ({ ...current, [repositoryID]: payload.items ?? [] }));
    } catch (requestError) {
      setRepositorySnapshotError(requestError instanceof Error ? requestError.message : "Could not load repository snapshots.");
    } finally {
      setRepositorySnapshotLoading(false);
    }
  }

  async function refreshRepositorySnapshot(repositoryID: string) {
    if (!projectID || repositorySnapshotBusy) return;
    setRepositorySnapshotBusy(true);
    setRepositorySnapshotError("");
    try {
      const snapshot = await requestJSON<RepositorySnapshot>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/snapshots/refresh`,
        { method: "POST" },
      );
      setRepositorySnapshots((current) => ({
        ...current,
        [repositoryID]: [snapshot, ...(current[repositoryID] ?? []).filter((item) => item.id !== snapshot.id)],
      }));
    } catch (requestError) {
      setRepositorySnapshotError(requestError instanceof Error ? requestError.message : "Could not index this repository.");
    } finally {
      setRepositorySnapshotBusy(false);
    }
  }

  async function loadRepositorySnapshotEdges(repositoryID: string, snapshotID: string) {
    if (!projectID || repositorySnapshotEdgesLoading) return;
    setRepositorySnapshotEdgesLoading(snapshotID);
    setRepositorySnapshotError("");
    try {
      const payload = await requestJSON<{ items?: RepositorySnapshotEdge[] }>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/snapshots/${snapshotID}/edges?limit=100`,
      );
      setRepositorySnapshotEdges((current) => ({ ...current, [snapshotID]: payload.items ?? [] }));
    } catch (requestError) {
      setRepositorySnapshotError(requestError instanceof Error ? requestError.message : "Could not load repository import edges.");
    } finally {
      setRepositorySnapshotEdgesLoading("");
    }
  }

  async function loadKnowledge(repositoryID: string) {
    if (!projectID || knowledgeLoading) return;
    setKnowledgeLoading(true);
    setKnowledgeError("");
    try {
      const payload = await requestJSON<{ items?: KnowledgeDocument[] }>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/knowledge?limit=20`,
      );
      setKnowledgeDocuments((current) => ({ ...current, [repositoryID]: payload.items ?? [] }));
    } catch (requestError) {
      setKnowledgeError(requestError instanceof Error ? requestError.message : "Could not load repository knowledge.");
    } finally {
      setKnowledgeLoading(false);
    }
  }

  async function createKnowledge(repositoryID: string) {
    if (!projectID || knowledgeBusy || !knowledgeTitle.trim() || !knowledgeSlug.trim() || !knowledgeContent.trim()) return;
    setKnowledgeBusy(true);
    setKnowledgeError("");
    try {
      const document = await requestJSON<KnowledgeDocument>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/knowledge`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            title: knowledgeTitle.trim(),
            slug: knowledgeSlug.trim().toLowerCase(),
            kind: knowledgeKind,
            content: knowledgeContent,
            provenance: "MANUAL",
          }),
        },
      );
      setKnowledgeDocuments((current) => ({ ...current, [repositoryID]: [document, ...(current[repositoryID] ?? [])] }));
      setKnowledgeTitle("");
      setKnowledgeSlug("");
      setKnowledgeContent("");
    } catch (requestError) {
      setKnowledgeError(requestError instanceof Error ? requestError.message : "Could not save repository knowledge.");
    } finally {
      setKnowledgeBusy(false);
    }
  }

  async function openKnowledgeDocument(repositoryID: string, document: KnowledgeDocument) {
    if (selectedKnowledgeDocument?.id === document.id) {
      setSelectedKnowledgeDocument(null);
      setKnowledgeRevisions([]);
      setKnowledgeCompareRevision(null);
      return;
    }
    setKnowledgeError("");
    try {
      const [detail, revisionPayload] = await Promise.all([
        requestJSON<KnowledgeDocument>(
          `/api/v1/projects/${projectID}/repositories/${repositoryID}/knowledge/${document.id}`,
        ),
        requestJSON<{ items?: KnowledgeRevision[] }>(
          `/api/v1/projects/${projectID}/repositories/${repositoryID}/knowledge/${document.id}/revisions?limit=50`,
        ),
      ]);
      setSelectedKnowledgeDocument(detail);
      setKnowledgeRevisions(revisionPayload.items ?? []);
      setKnowledgeCompareRevision(null);
      setKnowledgeRevisionContent(detail.latest_revision?.content ?? "");
      setKnowledgeRevisionProvenance(detail.current_provenance === "HUMAN_VERIFIED" ? "HUMAN_VERIFIED" : "MANUAL");
    } catch (requestError) {
      setKnowledgeError(requestError instanceof Error ? requestError.message : "Could not open repository memory.");
    }
  }

  async function appendKnowledgeRevision(repositoryID: string) {
    if (!projectID || !selectedKnowledgeDocument || knowledgeRevisionBusy || !knowledgeRevisionContent.trim()) return;
    setKnowledgeRevisionBusy(true);
    setKnowledgeError("");
    try {
      const revision = await requestJSON<KnowledgeDocument["latest_revision"]>(
        `/api/v1/projects/${projectID}/repositories/${repositoryID}/knowledge/${selectedKnowledgeDocument.id}/revisions`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ content: knowledgeRevisionContent, provenance: knowledgeRevisionProvenance }),
        },
      );
      if (!revision) throw new Error("The revision response was empty.");
      const updated: KnowledgeDocument = {
        ...selectedKnowledgeDocument,
        current_provenance: revision.provenance,
        updated_at: revision.created_at ?? selectedKnowledgeDocument.updated_at,
        latest_revision: revision,
      };
      setSelectedKnowledgeDocument(updated);
      setKnowledgeRevisions((current) => [revision, ...current.filter((item) => item.id !== revision.id)]);
      setKnowledgeCompareRevision(null);
      setKnowledgeDocuments((current) => ({
        ...current,
        [repositoryID]: (current[repositoryID] ?? []).map((item) => item.id === updated.id ? updated : item),
      }));
      setKnowledgeRevisionContent("");
    } catch (requestError) {
      setKnowledgeError(requestError instanceof Error ? requestError.message : "Could not append repository memory revision.");
    } finally {
      setKnowledgeRevisionBusy(false);
    }
  }

  const loadMembers = useCallback(async () => {
    if (sessionState !== "ready" || !projectID.trim()) {
      setMembers([]);
      setMembersLoading(false);
      setMembersError("");
      return;
    }
    setMembersLoading(true);
    setMembersError("");
    try {
      const payload = await requestJSON<{ items?: Member[] }>(
        `/api/v1/projects/${projectID}/members`,
      );
      setMembers(payload.items ?? []);
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load project members.",
      );
    } finally {
      setMembersLoading(false);
    }
  }, [projectID, sessionState]);

  const loadOrganizationMembers = useCallback(async () => {
    if (sessionState !== "ready" || !organizationID.trim()) {
      setOrganizationMembers([]);
      setOrganizationMembersLoading(false);
      return;
    }
    setOrganizationMembersLoading(true);
    try {
      const payload = await requestJSON<{ items?: Member[] }>(
        "/api/v1/organizations/current/members",
      );
      setOrganizationMembers(payload.items ?? []);
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load organization members.",
      );
    } finally {
      setOrganizationMembersLoading(false);
    }
  }, [organizationID, sessionState]);

  const loadCustomFields = useCallback(async () => {
    if (sessionState !== "ready" || !projectID.trim()) {
      setCustomFields([]);
      setCustomFieldsLoading(false);
      return;
    }
    setCustomFieldsLoading(true);
    setCustomFieldsError("");
    try {
      const payload = await requestJSON<{ items?: CustomFieldDefinition[] }>(
        `/api/v1/projects/${projectID}/custom-fields`,
      );
      setCustomFields(payload.items ?? []);
    } catch (requestError) {
      setCustomFieldsError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load custom fields.",
      );
    } finally {
      setCustomFieldsLoading(false);
    }
  }, [projectID, sessionState]);

  async function loadCustomFieldValues(workItemID: string) {
    if (!projectID || !workItemID) return;
    try {
      const payload = await requestJSON<{ items?: CustomFieldValue[] }>(
        `/api/v1/work-items/${workItemID}/custom-fields?project_id=${encodeURIComponent(projectID)}`,
      );
      setCustomFieldValues(payload.items ?? []);
    } catch (requestError) {
      setCustomFieldsError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load custom field values.",
      );
    }
  }

  const loadSprints = useCallback(async () => {
    if (sessionState !== "ready" || !projectID.trim()) {
      setSprints([]);
      setSprintsLoading(false);
      setSprintError("");
      return;
    }
    setSprintsLoading(true);
    setSprintError("");
    try {
      const payload = await requestJSON<{ items?: Sprint[] }>(
        "/api/v1/sprints",
        {
          headers: { "X-Project-ID": projectID },
        },
      );
      setSprints(payload.items ?? []);
    } catch (requestError) {
      setSprintError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load sprints.",
      );
    } finally {
      setSprintsLoading(false);
    }
  }, [projectID, sessionState]);

  const loadNotifications = useCallback(async () => {
    if (sessionState !== "ready") {
      setNotifications([]);
      setNotificationsLoading(false);
      return;
    }
    setNotificationsLoading(true);
    setNotificationError("");
    try {
      const payload = await requestJSON<{ items?: NotificationItem[] }>(
        "/api/v1/notifications?limit=30",
      );
      setNotifications(payload.items ?? []);
    } catch (requestError) {
      setNotificationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load notifications.",
      );
    } finally {
      setNotificationsLoading(false);
    }
  }, [sessionState]);

  const loadAutomationRules = useCallback(async () => {
    if (sessionState !== "ready" || !projectID.trim()) {
      setAutomationRules([]);
      setAutomationLoading(false);
      return;
    }
    setAutomationLoading(true);
    setAutomationError("");
    try {
      const payload = await requestJSON<{ items?: AutomationRule[] }>(
        `/api/v1/projects/${projectID}/automation-rules`,
      );
      setAutomationRules(payload.items ?? []);
    } catch (requestError) {
      setAutomationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load automation rules.",
      );
    } finally {
      setAutomationLoading(false);
    }
  }, [projectID, sessionState]);

  const loadAgentRuns = useCallback(
    async (workItemID = detail?.item.id) => {
      if (sessionState !== "ready" || !projectID.trim() || !workItemID) {
        setAgentRuns([]);
        setAgentRunsLoading(false);
        setAgentRunError("");
        return;
      }
      setAgentRunsLoading(true);
      setAgentRunError("");
      try {
        const payload = await requestJSON<{ items?: AgentRun[] }>(
          `/api/v1/agent-runs?work_item_id=${encodeURIComponent(workItemID)}`,
          { headers: { "X-Project-ID": projectID } },
        );
        setAgentRuns(payload.items ?? []);
      } catch (requestError) {
        setAgentRunError(
          requestError instanceof Error
            ? requestError.message
            : "Could not load agent runs.",
        );
      } finally {
        setAgentRunsLoading(false);
      }
    },
    [detail?.item.id, projectID, sessionState],
  );

  useEffect(() => {
    if (sessionState !== "ready") return;
    const timer = window.setTimeout(() => {
      void loadRepositories();
    }, 0);
    return () => {
      window.clearTimeout(timer);
      repositoryAbortRef.current?.abort();
    };
  }, [loadRepositories, sessionState]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadMembers();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadMembers]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadOrganizationMembers();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadOrganizationMembers]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadCustomFields();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadCustomFields]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadSprints();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadSprints]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadNotifications();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadNotifications]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadAutomationRules();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadAutomationRules]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadAgentRuns();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [loadAgentRuns]);
  async function toggleRepository(repository: GitHubRepository) {
    if (!projectID || linkingRepositoryID) return;
    const linked = !repository.linked;
    setLinkingRepositoryID(repository.id);
    setRepositories((current) =>
      current.map((item) =>
        item.id === repository.id ? { ...item, linked } : item,
      ),
    );
    try {
      await requestJSON<void>(
        linked
          ? `/api/v1/projects/${projectID}/repositories`
          : `/api/v1/projects/${projectID}/repositories/${repository.id}`,
        {
          method: linked ? "POST" : "DELETE",
          ...(linked
            ? {
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ repository_id: repository.id }),
              }
            : {}),
        },
      );
      if (!linked && selectedRepositoryContextID === repository.id) {
        setSelectedRepositoryContextID("");
      }
    } catch (requestError) {
      setRepositories((current) =>
        current.map((item) =>
          item.id === repository.id
            ? { ...item, linked: repository.linked }
            : item,
        ),
      );
      setRepositoryError(
        requestError instanceof Error
          ? requestError.message
          : "Could not update the repository link.",
      );
    } finally {
      setLinkingRepositoryID("");
    }
  }

  async function loadAudit(id: string) {
    try {
      const payload = await requestJSON<{ items?: AuditRecord[] }>(
        `/api/v1/work-items/${encodeURIComponent(id)}/activity?limit=50`,
      );
      setDetail((current) =>
        current?.item.id === id
          ? { ...current, audit: payload.items ?? [] }
          : current,
      );
    } catch {
      // Audit visibility is capability-scoped; it must not block work-item detail.
    }
  }

  async function signOut() {
    try {
      await requestJSON<void>("/api/v1/auth/logout", { method: "POST" });
    } catch {
      // Clear local state even when the server session has already expired.
    } finally {
      setSessionState("signed-out");
      setProjects([]);
      setWorkspaces([]);
      setOrganizations([]);
      setOrganizationID("");
      setProjectID("");
      setItems([]);
      setBoardColumns([]);
      setDetail(null);
      setAccessTokens([]);
      setNewToken("");
      try {
        window.localStorage.removeItem(selectedOrganizationKey);
      } catch {
        /* convenience only */
      }
    }
  }

  async function loadDetail(id: string) {
    if (!projectID) return;
    const requestID = ++detailRequestRef.current;
    setDetailLoading(true);
    setAttachmentLoading(true);
    setDetailError("");
    try {
      const headers = { "X-Project-ID": projectID };
      const [
        item,
        comments,
        labels,
        links,
        specificationPayload,
        specificationVersionsPayload,
        proposalPayload,
        analysisPayload,
        attachmentPayload,
      ] = await Promise.all([
        requestJSON<WorkItem>(`/api/v1/work-items/${id}`, { headers }),
        requestJSON<{ items?: Comment[] }>(
          `/api/v1/work-items/${id}/comments`,
          { headers },
        ),
        requestJSON<{ items?: Label[] }>(`/api/v1/work-items/${id}/labels`, {
          headers,
        }),
        requestJSON<{ items?: WorkItemLink[] }>(
          `/api/v1/work-items/${id}/links`,
          { headers },
        ),
        requestJSON<{
          specification: Specification | null;
          readiness: Readiness;
        }>(`/api/v1/work-items/${id}/specification`, { headers }),
        requestJSON<{ items?: SpecificationFieldVersion[] }>(
          `/api/v1/work-items/${id}/specification/versions?limit=100`,
          { headers },
        ),
        requestJSON<{ items?: SpecificationProposal[] }>(
          `/api/v1/work-items/${id}/specification/proposals`,
          { headers },
        ),
        requestJSON<{ items?: SpecificationAnalysis[] }>(
          `/api/v1/work-items/${id}/analyses`,
          { headers },
        ),
        requestJSON<{ items?: Attachment[] }>(
          `/api/v1/work-items/${id}/attachments`,
          { headers },
        ),
      ]);
      if (requestID !== detailRequestRef.current) return;
      const nextDetail: Detail = {
        item,
        comments: comments.items ?? [],
        labels: labels.items ?? [],
        links: links.items ?? [],
        audit: [],
        specification: specificationPayload.specification,
        readiness: specificationPayload.readiness ?? { ready: false },
        specificationVersions: specificationVersionsPayload.items ?? [],
        proposals: proposalPayload.items ?? [],
        analyses: analysisPayload.items ?? [],
        attachments: attachmentPayload.items ?? [],
      };
      setDetail(nextDetail);
      void loadAudit(id);
      setEditTitle(item.title);
      setEditDescription(item.description ?? "");
      setEditParent(item.parent_id ?? "");
      setEditRepository(item.repository_id ?? nextDetail.specification?.repository_id ?? "");
      setEditPriority(item.priority ?? "MEDIUM");
      setEditDueDate(dateValue(item.due_at));
      setEditEstimate(
        item.estimate_points == null ? "" : String(item.estimate_points),
      );
      setEditAssignee(item.assignee_id ?? "");
      setEditSprint(item.sprint_id ?? "");
      setDraftPRTitle(`${item.key}: ${item.title}`);
      setDraftPRBody(item.description ?? "");
      setDraftPRHead("");
      setDraftPRBase("");
      setSpecSummary(nextDetail.specification?.summary ?? "");
      setSpecRepositoryID(nextDetail.specification?.repository_id ?? "");
      setSpecDirty(false);
      setSpecFields(
        Object.fromEntries(
          Object.entries(nextDetail.specification?.fields ?? {}).map(
            ([key, value]) => [key, value.value],
          ),
        ),
      );
      setReproductionSteps(
        nextDetail.specification?.reproduction_steps?.length
          ? nextDetail.specification.reproduction_steps
          : [
              {
                position: 1,
                action: "",
                expected_result: "",
                observed_result: "",
              },
            ],
      );
      setAcceptanceCriteria(
        nextDetail.specification?.acceptance_criteria?.length
          ? nextDetail.specification.acceptance_criteria
          : [{ position: 1, statement: "" }],
      );
      setRegressionTestCases(
        nextDetail.specification?.regression_test_cases ?? [],
      );
      setSpecContextRefs(nextDetail.specification?.context_refs ?? []);
      setCustomFieldValues([]);
      void loadCustomFieldValues(item.id);
    } catch (requestError) {
      if (requestID !== detailRequestRef.current) return;
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load this work item.",
      );
    } finally {
      setAttachmentLoading(false);
      if (requestID === detailRequestRef.current) setDetailLoading(false);
    }
  }

  function openDetail(item: WorkItem) {
    setDetail({
      item,
      comments: [],
      labels: item.labels ?? [],
      links: [],
      audit: [],
      specification: null,
      readiness: { ready: false },
      specificationVersions: [],
      proposals: [],
      analyses: [],
      attachments: [],
    });
    setCustomFieldValues([]);
    void loadDetail(item.id);
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!projectID.trim() || !title.trim() || creating) return;
    setCreating(true);
    setError("");
    try {
      const estimate = createEstimate.trim()
        ? Number(createEstimate)
        : undefined;
      const created = await requestJSON<WorkItem>("/api/v1/work-items", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Project-ID": projectID,
        },
        body: JSON.stringify({
          project_id: projectID,
          workspace_id: selectedProject?.workspace_id,
          project_key: selectedProject?.key,
          type: createType,
          title: title.trim(),
          description: description.trim(),
          priority: createPriority,
          due_at: toDueAt(createDueDate),
          estimate_points: estimate,
          parent_id: createParentID,
          assignee_id: createAssigneeID,
          sprint_id: createSprintID,
          repository_id: createRepositoryID,
        }),
      });
      setTitle("");
      setDescription("");
      setCreateType("TASK");
      setCreatePriority("MEDIUM");
      setCreateDueDate("");
      setCreateEstimate("");
      setCreateParentID("");
      setCreateAssigneeID("");
      setCreateSprintID("");
      setCreateRepositoryID("");
      openDetail(created);
      if (view === "board") await loadBoard();
      else await load();
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create work item.",
      );
    } finally {
      setCreating(false);
    }
  }

  async function saveWorkItem(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || savingItem) return;
    setSavingItem(true);
    setDetailError("");
    try {
      const payload: Record<string, unknown> = {
        title: editTitle.trim(),
        description: editDescription,
        parent_id: editParent || null,
        repository_id: editRepository || null,
        priority: editPriority,
        due_at: editDueDate ? toDueAt(editDueDate) : null,
        estimate_points: editEstimate.trim() ? Number(editEstimate) : null,
        sprint_id: editSprint,
        expected_version: detail.item.version,
      };
      const updated = await requestJSON<WorkItem>(
        `/api/v1/work-items/${detail.item.id}`,
        {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify(payload),
        },
      );
      let finalItem = updated;
      if (editAssignee !== (updated.assignee_id ?? "")) {
        finalItem = await requestJSON<WorkItem>(
          `/api/v1/work-items/${updated.id}/assignments`,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-Project-ID": projectID,
            },
            body: JSON.stringify({
              assignee_id: editAssignee,
              expected_version: updated.version,
            }),
          },
        );
      }
      mergeItem(finalItem);
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not save work item.",
      );
    } finally {
      setSavingItem(false);
    }
  }

  async function archiveOrRestoreWorkItem() {
    if (!detail || archiveBusy) return;
    const archived = Boolean(detail.item.archived_at);
    if (!archived && !window.confirm(`Archive ${detail.item.key}?`)) return;
    setArchiveBusy(true);
    setDetailError("");
    try {
      if (archived) {
        const restored = await requestJSON<WorkItem>(
          `/api/v1/work-items/${detail.item.id}/restore`,
          {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-Project-ID": projectID,
            },
            body: JSON.stringify({ expected_version: detail.item.version }),
          },
        );
        mergeItem(restored);
      } else {
        await requestJSON<void>(
          `/api/v1/work-items/${detail.item.id}?expected_version=${detail.item.version}`,
          { method: "DELETE", headers: { "X-Project-ID": projectID } },
        );
        setDetail(null);
      }
      if (view === "board") await loadBoard();
      else await load();
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : `Could not ${archived ? "restore" : "archive"} work item.`,
      );
    } finally {
      setArchiveBusy(false);
    }
  }

  async function transitionItem(item: WorkItem, targetStatus: string) {
    if (item.status === targetStatus) return;
    const transitionKey = availableTransitionKeys[item.status]?.[targetStatus];
    if (!transitionKey) {
      setError(
        t("legacy.transition-error", { from: statusLabel(item.status), to: statusLabel(targetStatus) }),
      );
      return;
    }
    setError("");
    try {
      const updated = await requestJSON<WorkItem>(
        `/api/v1/work-items/${item.id}/transitions`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({
            transition_key: transitionKey,
            expected_version: item.version,
          }),
        },
      );
      mergeItem(updated);
      if (view === "board") await loadBoard();
      else await load();
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : "The transition was rejected.",
      );
    }
  }

  async function reorderItem(item: WorkItem, direction: "up" | "down") {
    if (!projectID || rankBusyID || item.archived_at) return;
    setRankBusyID(item.id);
    setError("");
    try {
      await requestJSON<WorkItem>(`/api/v1/work-items/${item.id}/rank`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Project-ID": projectID,
        },
        body: JSON.stringify({ direction, expected_version: item.version }),
      });
      await load();
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : "Could not reorder the backlog.",
      );
    } finally {
      setRankBusyID("");
    }
  }

  async function createSprint(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!projectID || !sprintName.trim() || sprintBusy) return;
    setSprintBusy(true);
    setSprintError("");
    try {
      const editing = Boolean(editingSprintID);
      await requestJSON<Sprint>(editing ? `/api/v1/sprints/${editingSprintID}` : "/api/v1/sprints", {
        method: editing ? "PATCH" : "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Project-ID": projectID,
        },
        body: JSON.stringify({
          project_id: projectID,
          name: sprintName.trim(),
          goal: sprintGoal.trim(),
          starts_at: sprintStartsAt,
          ends_at: sprintEndsAt,
        }),
      });
      setSprintName("");
      setSprintGoal("");
      setSprintStartsAt("");
      setSprintEndsAt("");
      setEditingSprintID("");
      await loadSprints();
    } catch (requestError) {
      setSprintError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create the sprint.",
      );
    } finally {
      setSprintBusy(false);
    }
  }

  function startEditingSprint(sprint: Sprint) {
    if (sprint.status !== "PLANNED") return;
    setEditingSprintID(sprint.id);
    setSprintName(sprint.name);
    setSprintGoal(sprint.goal);
    setSprintStartsAt(dateValue(sprint.starts_at));
    setSprintEndsAt(dateValue(sprint.ends_at));
  }

  async function deleteSprint(sprint: Sprint) {
    if (sprint.status !== "PLANNED" || sprintBusy || !window.confirm(`Delete ${sprint.name}?`)) return;
    setSprintBusy(true);
    setSprintError("");
    try {
      await requestJSON<void>(`/api/v1/sprints/${sprint.id}`, {
        method: "DELETE",
        headers: { "X-Project-ID": projectID },
      });
      if (editingSprintID === sprint.id) {
        setEditingSprintID("");
        setSprintName("");
        setSprintGoal("");
        setSprintStartsAt("");
        setSprintEndsAt("");
      }
      await loadSprints();
    } catch (requestError) {
      setSprintError(requestError instanceof Error ? requestError.message : "Could not delete the sprint.");
    } finally {
      setSprintBusy(false);
    }
  }

  async function transitionSprint(sprint: Sprint) {
    if (!projectID || sprintBusy) return;
    setSprintBusy(true);
    setSprintError("");
    try {
      await requestJSON<Sprint>(
        `/api/v1/sprints/${sprint.id}/${sprint.status === "PLANNED" ? "start" : "complete"}`,
        {
          method: "POST",
          headers: { "X-Project-ID": projectID },
        },
      );
      await loadSprints();
    } catch (requestError) {
      setSprintError(
        requestError instanceof Error
          ? requestError.message
          : "Could not update the sprint.",
      );
    } finally {
      setSprintBusy(false);
    }
  }

  async function createAgentRun() {
    if (!detail || !projectID || agentRunBusy) return;
    const prompt = agentPrompt.trim();
    if (!prompt) {
      setAgentRunError("Add the approved task prompt before creating an AgentRun.");
      return;
    }
    setAgentRunBusy(true);
    setAgentRunError("");
    try {
      const run = await requestJSON<AgentRun>("/api/v1/agent-runs", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Project-ID": projectID,
        },
        body: JSON.stringify({
          project_id: projectID,
          work_item_id: detail.item.id,
          repository_id:
            detail.item.repository_id ?? detail.specification?.repository_id ?? "",
          agent_provider: agentProvider,
          agent_name: agentProvider,
          model: "",
          base_sha: "",
          branch: "",
          execution_inputs: {
            prompt,
            ...(followUpTestCasePositions
              ? { test_case_positions: followUpTestCasePositions }
              : {}),
          },
        }),
      });
      setAgentRuns((current) => [
        run,
        ...current.filter((item) => item.id !== run.id),
      ]);
      setFollowUpTestCasePositions(null);
    } catch (requestError) {
      setAgentRunError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create an agent run.",
      );
    } finally {
      setAgentRunBusy(false);
    }
  }

  async function updateAgentRun(
    run: AgentRun,
    action: "approve" | "start" | "cancel" | "transition",
    nextStatus?: string,
  ) {
    if (!projectID || agentRunBusy) return;
    setAgentRunBusy(true);
    setAgentRunError("");
    try {
      const updated = await requestJSON<AgentRun>(
        `/api/v1/agent-runs/${run.id}/${action}`,
        {
          method: "POST",
          headers: {
            "X-Project-ID": projectID,
            ...(action === "transition"
              ? { "Content-Type": "application/json" }
              : {}),
          },
          ...(action === "transition"
            ? { body: JSON.stringify({ status: nextStatus }) }
            : {}),
        },
      );
      setAgentRuns((current) =>
        current.map((item) =>
          item.id === updated.id ? { ...item, ...updated } : item,
        ),
      );
    } catch (requestError) {
      setAgentRunError(
        requestError instanceof Error
          ? requestError.message
          : `Could not ${action} the agent run.`,
      );
    } finally {
      setAgentRunBusy(false);
    }
  }

  async function inspectAgentRun(run: AgentRun) {
    if (!projectID || agentRunDetailLoading) return;
    setSelectedAgentRunID(run.id);
    if (agentRunDetails[run.id]) return;
    setAgentRunDetailLoading(true);
    setAgentRunError("");
    try {
      const payload = await requestJSON<{
        run?: AgentRun;
        steps?: AgentRunStep[];
        artifacts?: AgentRunArtifact[];
      }>(`/api/v1/agent-runs/${run.id}`, {
        headers: { "X-Project-ID": projectID },
      });
      const loadedRun = payload.run ?? run;
      const savedResults = testResultsForRun(loadedRun);
      setAgentRuns((current) =>
        current.map((item) => item.id === loadedRun.id ? { ...item, ...loadedRun } : item),
      );
      setAgentTestCaseNotes((current) => {
        const next = { ...current };
        for (const item of savedResults) next[`${loadedRun.id}:${item.position}`] = item.note ?? "";
        return next;
      });
      setAgentTestReviewNote(
        typeof loadedRun.result?.test_review_note === "string"
          ? loadedRun.result.test_review_note
          : "",
      );
      setAgentRunDetails((current) => ({
        ...current,
        [run.id]: {
          steps: payload.steps ?? [],
          artifacts: payload.artifacts ?? [],
        },
      }));
    } catch (requestError) {
      setAgentRunError(
        requestError instanceof Error
          ? requestError.message
          : "Could not load AgentRun details.",
      );
    } finally {
      setAgentRunDetailLoading(false);
    }
  }

  async function recordAgentTestResults(
    run: AgentRun,
    cases: Array<{ position: number; status: AgentTestStatus; note?: string }>,
    reviewNote = "",
  ) {
    if (!projectID || agentTestResultBusy) return;
    setAgentTestResultBusy(true);
    setAgentRunError("");
    try {
      const updated = await requestJSON<AgentRun>(
        `/api/v1/agent-runs/${run.id}/test-results`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({
            test_cases: cases,
            ...(reviewNote.trim() ? { review_note: reviewNote.trim() } : {}),
          }),
        },
      );
      setAgentRuns((current) =>
        current.map((item) => item.id === updated.id ? { ...item, ...updated } : item),
      );
      if (reviewNote.trim()) setAgentTestReviewNote(reviewNote.trim());
    } catch (requestError) {
      setAgentRunError(
        requestError instanceof Error
          ? requestError.message
          : "Could not save test results.",
      );
    } finally {
      setAgentTestResultBusy(false);
    }
  }

  function prepareFollowUpRun(run: AgentRun) {
    if (!detail) return;
    const saved = new Map(testResultsForRun(run).map((item) => [item.position, item]));
    const pending = detail.specification?.regression_test_cases
      .filter((testCase) => saved.get(testCase.position)?.status !== "PASS") ?? [];
    const positions = pending.map((testCase) => testCase.position);
    setFollowUpTestCasePositions(positions);
    const unresolved = pending.map(
      (testCase) => `- Case ${testCase.position}: ${testCase.scenario} — expected: ${testCase.expected_result}`,
    );
    setAgentPrompt(
      `Continue the existing implementation. Do not rerun or modify already-passed test cases. Fix only the unresolved cases below and run the smallest relevant test scope:\n${unresolved.join("\n") || "- Reproduce and address the remaining issue described in the review notes."}`,
    );
  }

  async function addComment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || !commentBody.trim() || commentBusy) return;
    setCommentBusy(true);
    try {
      const comment = await requestJSON<Comment>(
        `/api/v1/work-items/${detail.item.id}/comments`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({ body: commentBody.trim() }),
        },
      );
      setDetail((current) =>
        current
          ? { ...current, comments: [...current.comments, comment] }
          : current,
      );
      setCommentBody("");
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not add comment.",
      );
    } finally {
      setCommentBusy(false);
    }
  }

  function startEditingComment(comment: Comment) {
    if (comment.deleted_at || comment.author_id !== currentActorID) return;
    setEditingCommentID(comment.id);
    setEditingCommentBody(comment.body);
  }

  function cancelEditingComment() {
    setEditingCommentID("");
    setEditingCommentBody("");
  }

  async function updateComment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || !editingCommentID || !editingCommentBody.trim() || commentBusy) return;
    setCommentBusy(true);
    try {
      const comment = await requestJSON<Comment>(
        `/api/v1/work-items/${detail.item.id}/comments/${editingCommentID}`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json", "X-Project-ID": projectID },
          body: JSON.stringify({ body: editingCommentBody.trim() }),
        },
      );
      setDetail((current) =>
        current
          ? { ...current, comments: current.comments.map((item) => item.id === comment.id ? comment : item) }
          : current,
      );
      cancelEditingComment();
    } catch (requestError) {
      setDetailError(requestError instanceof Error ? requestError.message : "Could not update comment.");
    } finally {
      setCommentBusy(false);
    }
  }

  async function deleteComment(comment: Comment) {
    if (!detail || comment.author_id !== currentActorID || commentBusy || !window.confirm("Delete this comment?")) return;
    setCommentBusy(true);
    try {
      const deleted = await requestJSON<Comment>(
        `/api/v1/work-items/${detail.item.id}/comments/${comment.id}`,
        { method: "DELETE", headers: { "X-Project-ID": projectID } },
      );
      setDetail((current) =>
        current
          ? { ...current, comments: current.comments.map((item) => item.id === deleted.id ? deleted : item) }
          : current,
      );
      if (editingCommentID === comment.id) cancelEditingComment();
    } catch (requestError) {
      setDetailError(requestError instanceof Error ? requestError.message : "Could not delete comment.");
    } finally {
      setCommentBusy(false);
    }
  }

  async function uploadAttachment(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (!detail || !file || attachmentBusy) return;
    setAttachmentBusy(true);
    setAttachmentError("");
    try {
      const prepared = await optimizeAttachment(file);
      const form = new FormData();
      form.append("file", prepared.file, prepared.file.name);
      const headers = new Headers({
        Accept: "application/json",
        "X-Project-ID": projectID,
      });
      setOrganizationHeader(headers);
      for (const [name, value] of Object.entries(devHeaders)) {
        headers.set(name, value);
      }
      const token = csrfToken();
      if (token) headers.set("X-CSRF-Token", token);
      const response = await fetch(
        apiURL(`/api/v1/work-items/${detail.item.id}/attachments`),
        { method: "POST", headers, body: form, credentials: "include" },
      );
      const payload = (await response.json().catch(() => undefined)) as
        | Attachment
        | { message?: string }
        | undefined;
      if (!response.ok || !payload || !("id" in payload)) {
        throw new Error(
          payload && "message" in payload && payload.message
            ? payload.message
            : "Could not upload the attachment.",
        );
      }
      setDetail((current) =>
        current
          ? { ...current, attachments: [payload, ...current.attachments] }
          : current,
      );
    } catch (requestError) {
      setAttachmentError(
        requestError instanceof Error
          ? requestError.message
          : "Could not upload the attachment.",
      );
    } finally {
      setAttachmentBusy(false);
    }
  }

  async function downloadAttachment(item: Attachment) {
    if (!projectID) return;
    setAttachmentError("");
    try {
      const headers = new Headers({
        Accept: item.content_type,
        "X-Project-ID": projectID,
      });
      setOrganizationHeader(headers);
      for (const [name, value] of Object.entries(devHeaders)) {
        headers.set(name, value);
      }
      const response = await fetch(
        apiURL(`/api/v1/work-items/${item.work_item_id}/attachments/${item.id}`),
        { headers, credentials: "include" },
      );
      if (!response.ok) throw new Error("Could not download the attachment.");
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = item.name;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch (requestError) {
      setAttachmentError(
        requestError instanceof Error
          ? requestError.message
          : "Could not download the attachment.",
      );
    }
  }

  async function deleteAttachment(item: Attachment) {
    if (!detail || attachmentBusy) return;
    setAttachmentBusy(true);
    setAttachmentError("");
    try {
      await requestJSON<void>(
        `/api/v1/work-items/${detail.item.id}/attachments/${item.id}`,
        { method: "DELETE", headers: { "X-Project-ID": projectID } },
      );
      setDetail((current) =>
        current
          ? {
              ...current,
              attachments: current.attachments.filter(
                (attachment) => attachment.id !== item.id,
              ),
            }
          : current,
      );
    } catch (requestError) {
      setAttachmentError(
        requestError instanceof Error
          ? requestError.message
          : "Could not delete the attachment.",
      );
    } finally {
      setAttachmentBusy(false);
    }
  }

  async function addLabel(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || !labelName.trim() || labelBusy) return;
    setLabelBusy(true);
    try {
      const label = await requestJSON<Label>(
        `/api/v1/work-items/${detail.item.id}/labels`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({ name: labelName.trim() }),
        },
      );
      setDetail((current) =>
        current
          ? {
              ...current,
              labels: current.labels.some((item) => item.id === label.id)
                ? current.labels
                : [...current.labels, label],
            }
          : current,
      );
      setLabelName("");
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not add label.",
      );
    } finally {
      setLabelBusy(false);
    }
  }

  async function removeLabel(labelID: string) {
    if (!detail) return;
    try {
      await requestJSON<void>(
        `/api/v1/work-items/${detail.item.id}/labels/${labelID}`,
        { method: "DELETE", headers: { "X-Project-ID": projectID } },
      );
      setDetail((current) =>
        current
          ? {
              ...current,
              labels: current.labels.filter((label) => label.id !== labelID),
            }
          : current,
      );
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not remove label.",
      );
    }
  }

  async function addLink(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || !linkTargetID || linkBusy) return;
    setLinkBusy(true);
    try {
      const link = await requestJSON<WorkItemLink>(
        `/api/v1/work-items/${detail.item.id}/links`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({
            target_id: linkTargetID,
            relation_type: linkRelation,
          }),
        },
      );
      setDetail((current) =>
        current
          ? {
              ...current,
              links: [...current.links, link],
            }
          : current,
      );
      setLinkTargetID("");
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not link this work item.",
      );
    } finally {
      setLinkBusy(false);
    }
  }

  async function removeLink(linkID: string) {
    if (!detail || linkBusy) return;
    setLinkBusy(true);
    try {
      await requestJSON<void>(
        `/api/v1/work-items/${detail.item.id}/links/${linkID}`,
        { method: "DELETE", headers: { "X-Project-ID": projectID } },
      );
      setDetail((current) =>
        current
          ? {
              ...current,
              links: current.links.filter((link) => link.id !== linkID),
            }
          : current,
      );
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not remove this relationship.",
      );
    } finally {
      setLinkBusy(false);
    }
  }

  async function saveSpecification(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || savingSpec) return;
    setSavingSpec(true);
    try {
      const fields = Object.fromEntries(
        Object.entries(specFields)
          .filter(([, value]) => value.trim())
          .map(([key, value]) => [key, value]),
      );
      await requestJSON<Specification>(
        `/api/v1/work-items/${detail.item.id}/specification`,
        {
          method: "PATCH",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({
            summary: specSummary,
            fields,
            reproduction_steps: reproductionSteps.map((step, index) => ({
              ...step,
              position: index + 1,
            })),
            acceptance_criteria: acceptanceCriteria.map((criterion, index) => ({
              ...criterion,
              position: index + 1,
            })),
            regression_test_cases: regressionTestCases.map((testCase, index) => ({
              ...testCase,
              position: index + 1,
            })),
            context_refs: specContextRefs,
            repository_id: specRepositoryID,
          }),
        },
      );
      await loadDetail(detail.item.id);
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not save the specification.",
      );
    } finally {
      setSavingSpec(false);
    }
  }

  async function addAnalysis(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || analysisBusy || !analysisHypothesis.trim()) return;
    setAnalysisBusy(true);
    setDetailError("");
    try {
      await requestJSON<SpecificationAnalysis>(
        `/api/v1/work-items/${detail.item.id}/analyses`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({
            root_cause_hypothesis: analysisHypothesis.trim(),
            blast_radius: analysisBlastRadius.trim(),
            implementation_plan: analysisImplementationPlan.trim(),
            test_plan: analysisTestPlan.trim(),
            evidence_refs: analysisEvidenceRefs
              .split("\n")
              .map((reference) => reference.trim())
              .filter(Boolean),
            confidence: Number(analysisConfidence),
          }),
        },
      );
      setAnalysisHypothesis("");
      setAnalysisBlastRadius("");
      setAnalysisImplementationPlan("");
      setAnalysisTestPlan("");
      setAnalysisEvidenceRefs("");
      setAnalysisConfidence("0.5");
      await loadDetail(detail.item.id);
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not save the analysis hypothesis.",
      );
    } finally {
      setAnalysisBusy(false);
    }
  }

  async function verifySpecification(
    kind: string,
    field?: string,
    position?: number,
  ) {
    if (!detail) return;
    if (specDirty) {
      setDetailError("Save the definition before verifying a field.");
      return;
    }
    try {
      await requestJSON<void>(
        `/api/v1/work-items/${detail.item.id}/specification/verifications`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Project-ID": projectID,
          },
          body: JSON.stringify({ kind, field, position }),
        },
      );
      await loadDetail(detail.item.id);
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Only a human reviewer can verify this content.",
      );
    }
  }

  async function acceptProposal(proposal: SpecificationProposal) {
    if (!detail) return;
    try {
      await requestJSON<SpecificationProposal>(
        `/api/v1/work-items/${detail.item.id}/specification/proposals/${proposal.id}/accept`,
        { method: "POST", headers: { "X-Project-ID": projectID } },
      );
      await loadDetail(detail.item.id);
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not accept this proposal.",
      );
    }
  }

  async function rejectProposal(proposal: SpecificationProposal) {
    if (!detail) return;
    try {
      await requestJSON<SpecificationProposal>(
        `/api/v1/work-items/${detail.item.id}/specification/proposals/${proposal.id}/reject`,
        { method: "POST", headers: { "X-Project-ID": projectID } },
      );
      await loadDetail(detail.item.id);
    } catch (requestError) {
      setDetailError(
        requestError instanceof Error
          ? requestError.message
          : "Could not reject this proposal.",
      );
    }
  }

  function updateStep(
    index: number,
    key: keyof ReproductionStep,
    value: string,
  ) {
    setSpecDirty(true);
    setReproductionSteps((current) =>
      current.map((step, stepIndex) =>
        stepIndex === index ? { ...step, [key]: value } : step,
      ),
    );
  }
  function updateAcceptance(index: number, value: string) {
    setSpecDirty(true);
    setAcceptanceCriteria((current) =>
      current.map((criterion, criterionIndex) =>
        criterionIndex === index
          ? { ...criterion, statement: value }
          : criterion,
      ),
    );
  }
  function updateRegression(
    index: number,
    key: keyof RegressionTestCase,
    value: string,
  ) {
    setSpecDirty(true);
    setRegressionTestCases((current) =>
      current.map((testCase, testCaseIndex) =>
        testCaseIndex === index ? { ...testCase, [key]: value } : testCase,
      ),
    );
  }

  function isFieldVerified(field: string): boolean {
    return (
      detail?.specification?.fields[field]?.verification_status ===
      "HUMAN_VERIFIED"
    );
  }

  function memberName(member: Member): string {
    return member.display_name || member.login || member.id;
  }

  async function changeMemberRole(member: Member, roleKey: string) {
    if (!projectID || memberRoleBusy) return;
    setMemberRoleBusy(member.id);
    setMembersError("");
    try {
      const updated = await requestJSON<Member>(
        `/api/v1/projects/${projectID}/members/${member.id}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ user_id: member.id, role_key: roleKey }),
        },
      );
      setMembers((current) =>
        current.map((item) => (item.id === updated.id ? updated : item)),
      );
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not update the member role.",
      );
    } finally {
      setMemberRoleBusy("");
    }
  }

  async function changeOrganizationMemberRole(member: Member, roleKey: string) {
    if (organizationMemberBusy) return;
    setOrganizationMemberBusy(member.id);
    setMembersError("");
    try {
      await requestJSON<Member>(`/api/v1/organizations/current/members/${member.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role_key: roleKey }),
      });
      await Promise.all([loadOrganizationMembers(), loadMembers()]);
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not update the organization role.",
      );
    } finally {
      setOrganizationMemberBusy("");
    }
  }

  async function removeOrganizationMember(member: Member) {
    if (organizationMemberBusy) return;
    if (!window.confirm(`Remove ${memberName(member)} from this organization?`)) return;
    setOrganizationMemberBusy(member.id);
    setMembersError("");
    try {
      await requestJSON<void>(`/api/v1/organizations/current/members/${member.id}`, {
        method: "DELETE",
      });
      await Promise.all([loadOrganizationMembers(), loadMembers()]);
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not remove the organization member.",
      );
    } finally {
      setOrganizationMemberBusy("");
    }
  }

  async function removeMember(member: Member) {
    if (!projectID || memberRoleBusy) return;
    if (!window.confirm(`Remove the project-specific role for ${memberName(member)}?`)) return;
    setMemberRoleBusy(member.id);
    setMembersError("");
    try {
      await requestJSON<void>(`/api/v1/projects/${projectID}/members/${member.id}`, { method: "DELETE" });
      await loadMembers();
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not remove the project member role.",
      );
    } finally {
      setMemberRoleBusy("");
    }
  }

  async function addOrganizationMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!memberLogin.trim() || memberAddBusy) return;
    setMemberAddBusy(true);
    setMembersError("");
    try {
      const member = await requestJSON<Member>("/api/v1/organizations/current/members", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          login: memberLogin.trim(),
          role_key: memberRole,
        }),
      });
      setMemberLogin("");
      await Promise.all([loadOrganizationMembers(), loadMembers()]);
    } catch (requestError) {
      setMembersError(
        requestError instanceof Error
          ? requestError.message
          : "Could not add the organization member.",
      );
    } finally {
      setMemberAddBusy(false);
    }
  }

  async function createCustomField(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!projectID || !customFieldName.trim() || !customFieldKey.trim() || customFieldBusy) return;
    setCustomFieldBusy(true);
    setCustomFieldsError("");
    try {
      await requestJSON<CustomFieldDefinition>(
        `/api/v1/projects/${projectID}/custom-fields`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            key: customFieldKey.trim().toUpperCase(),
            display_name: customFieldName.trim(),
            value_type: customFieldType,
            options:
              customFieldType === "SELECT"
                ? customFieldOptions.split(",").map((option) => option.trim()).filter(Boolean)
                : [],
            required: false,
          }),
        },
      );
      setCustomFieldName("");
      setCustomFieldKey("");
      setCustomFieldOptions("");
      await loadCustomFields();
    } catch (requestError) {
      setCustomFieldsError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create the custom field.",
      );
    } finally {
      setCustomFieldBusy(false);
    }
  }

  async function deleteCustomField(field: CustomFieldDefinition) {
    if (!projectID || customFieldBusy) return;
    setCustomFieldBusy(true);
    setCustomFieldsError("");
    try {
      await requestJSON<void>(
        `/api/v1/projects/${projectID}/custom-fields/${field.id}`,
        { method: "DELETE" },
      );
      setCustomFields((current) => current.filter((item) => item.id !== field.id));
      setCustomFieldValues((current) => current.filter((item) => item.definition_id !== field.id));
    } catch (requestError) {
      setCustomFieldsError(
        requestError instanceof Error
          ? requestError.message
          : "Could not delete the custom field.",
      );
    } finally {
      setCustomFieldBusy(false);
    }
  }

  function startEditingCustomField(field: CustomFieldDefinition) {
    setEditingCustomFieldID(field.id);
    setCustomFieldEditName(field.display_name);
    setCustomFieldEditOptions(field.options?.join(", ") ?? "");
  }

  async function saveCustomFieldEdit(field: CustomFieldDefinition) {
    if (!projectID || customFieldBusy || !customFieldEditName.trim()) return;
    setCustomFieldBusy(true);
    setCustomFieldsError("");
    try {
      await requestJSON<CustomFieldDefinition>(`/api/v1/projects/${projectID}/custom-fields/${field.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          display_name: customFieldEditName.trim(),
          options: field.value_type === "SELECT" ? customFieldEditOptions.split(",").map((option) => option.trim()).filter(Boolean) : undefined,
        }),
      });
      setEditingCustomFieldID("");
      await loadCustomFields();
    } catch (requestError) {
      setCustomFieldsError(requestError instanceof Error ? requestError.message : "Could not update the custom field.");
    } finally {
      setCustomFieldBusy(false);
    }
  }

  async function saveCustomFieldValue(field: CustomFieldDefinition, value: string) {
    if (!detail || customFieldBusy) return;
    setCustomFieldBusy(true);
    setCustomFieldsError("");
    try {
      const updated = await requestJSON<CustomFieldValue>(
        `/api/v1/work-items/${detail.item.id}/custom-fields/${field.id}?project_id=${encodeURIComponent(projectID)}`,
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ value: value === "" ? null : value }),
        },
      );
      setCustomFieldValues((current) => [
        ...current.filter((item) => item.definition_id !== field.id),
        { ...field, ...updated },
      ]);
    } catch (requestError) {
      setCustomFieldsError(
        requestError instanceof Error
          ? requestError.message
          : "Could not save the custom field value.",
      );
    } finally {
      setCustomFieldBusy(false);
    }
  }

  async function createAccessToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!tokenName.trim() || tokenBusy) return;
    setTokenBusy(true);
    setTokenError("");
    setNewToken("");
    const scopes =
      tokenProfile === "read"
        ? ["project.read", "repository.read"]
        : [
            "project.read",
            "work_item.create",
            "work_item.edit",
            "work_item.assign",
            "work_item.transition",
            "comment.create",
            "specification.propose",
            "repository.read",
            "agent.execute",
          ];
    try {
      const token = await requestJSON<AccessToken & { token: string }>(
        "/api/v1/me/tokens",
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            name: tokenName.trim(),
            scopes,
            expires_in_days: Number(tokenExpiry) || 90,
          }),
        },
      );
      setNewToken(token.token);
      setTokenName("Codex local MCP");
      await loadAccessTokens();
    } catch (requestError) {
      setTokenError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create the access token.",
      );
    } finally {
      setTokenBusy(false);
    }
  }

  async function revokeAccessToken(token: AccessToken) {
    if (tokenBusy) return;
    setTokenBusy(true);
    setTokenError("");
    try {
      await requestJSON<void>(`/api/v1/me/tokens/${token.id}`, {
        method: "DELETE",
      });
      setAccessTokens((current) =>
        current.filter((candidate) => candidate.id !== token.id),
      );
    } catch (requestError) {
      setTokenError(
        requestError instanceof Error
          ? requestError.message
          : "Could not revoke the access token.",
      );
    } finally {
      setTokenBusy(false);
    }
  }

  async function copyAccessToken() {
    if (!newToken || !navigator.clipboard) return;
    await navigator.clipboard.writeText(newToken);
    setTokenCopied(true);
    window.setTimeout(() => setTokenCopied(false), 1600);
  }

  async function markNotificationRead(notification: NotificationItem) {
    if (notification.read_at) return;
    try {
      await requestJSON<void>(`/api/v1/notifications/${notification.id}/read`, {
        method: "POST",
      });
      setNotifications((current) =>
        current.map((item) =>
          item.id === notification.id
            ? { ...item, read_at: new Date().toISOString() }
            : item,
        ),
      );
    } catch (requestError) {
      setNotificationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not mark the notification as read.",
      );
    }
  }

  async function markAllNotificationsRead() {
    try {
      await requestJSON<void>("/api/v1/notifications/read-all", {
        method: "POST",
      });
      const now = new Date().toISOString();
      setNotifications((current) =>
        current.map((item) => ({ ...item, read_at: item.read_at ?? now })),
      );
    } catch (requestError) {
      setNotificationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not mark notifications as read.",
      );
    }
  }

  async function createAutomationRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!projectID || !automationName.trim() || automationBusy) return;
    setAutomationBusy(true);
    setAutomationError("");
    try {
      await requestJSON<AutomationRule>(
        `/api/v1/projects/${projectID}/automation-rules`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            name: automationName.trim(),
            event_type: automationEvent,
            action_type: "notify",
            config: {
              title: "{event_type}",
              body: "{event_type} received for {aggregate_type} {aggregate_id}.",
            },
          }),
        },
      );
      setAutomationName("Notify work changes");
      await loadAutomationRules();
    } catch (requestError) {
      setAutomationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create the automation rule.",
      );
    } finally {
      setAutomationBusy(false);
    }
  }

  async function toggleAutomationRule(rule: AutomationRule) {
    if (!projectID || automationBusy) return;
    setAutomationBusy(true);
    setAutomationError("");
    try {
      const updated = await requestJSON<AutomationRule>(
        `/api/v1/projects/${projectID}/automation-rules/${rule.id}`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ enabled: !rule.enabled }),
        },
      );
      setAutomationRules((current) =>
        current.map((item) => (item.id === updated.id ? updated : item)),
      );
    } catch (requestError) {
      setAutomationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not update the automation rule.",
      );
    } finally {
      setAutomationBusy(false);
    }
  }

  async function deleteAutomationRule(rule: AutomationRule) {
    if (!projectID || automationBusy) return;
    setAutomationBusy(true);
    setAutomationError("");
    try {
      await requestJSON<void>(
        `/api/v1/projects/${projectID}/automation-rules/${rule.id}`,
        { method: "DELETE" },
      );
      setAutomationRules((current) =>
        current.filter((item) => item.id !== rule.id),
      );
    } catch (requestError) {
      setAutomationError(
        requestError instanceof Error
          ? requestError.message
          : "Could not delete the automation rule.",
      );
    } finally {
      setAutomationBusy(false);
    }
  }

  async function createProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (setupBusy) return;
    setSetupBusy(true);
    setSetupError("");
    try {
      let selectedWorkspaceID = workspaceID;
      if (!selectedWorkspaceID) {
        const workspace = await requestJSON<Workspace>("/api/v1/workspaces", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ key: "MAIN", display_name: "Main" }),
        });
        selectedWorkspaceID = workspace.id;
        setWorkspaces((current) => [...current, workspace]);
        setWorkspaceID(workspace.id);
        setWorkspaceRenameName(workspace.display_name);
      }
      const project = await requestJSON<Project>("/api/v1/projects", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          workspace_id: selectedWorkspaceID,
          key: projectKey.trim(),
          display_name: projectName.trim(),
        }),
      });
      setProjects((current) => [...current, project]);
      setProjectID(project.id);
      setSetupVisible(false);
      setProjectKey("APP");
      setProjectName("My project");
    } catch (requestError) {
      setSetupError(
        requestError instanceof Error
          ? requestError.message
          : "Could not create the project.",
      );
    } finally {
      setSetupBusy(false);
    }
  }

  async function createWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (workspaceBusy || !workspaceCreateKey.trim() || !workspaceCreateName.trim()) return;
    setWorkspaceBusy(true);
    setWorkspaceError("");
    try {
      const workspace = await requestJSON<Workspace>("/api/v1/workspaces", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          key: workspaceCreateKey.trim().toUpperCase(),
          display_name: workspaceCreateName.trim(),
        }),
      });
      setWorkspaces((current) => [...current, workspace].sort((a, b) => a.key.localeCompare(b.key)));
      setWorkspaceID(workspace.id);
      setWorkspaceRenameName(workspace.display_name);
      setWorkspaceCreateKey("");
      setWorkspaceCreateName("");
    } catch (requestError) {
      setWorkspaceError(requestError instanceof Error ? requestError.message : "Could not create the workspace.");
    } finally {
      setWorkspaceBusy(false);
    }
  }

  async function renameWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedWorkspace || !workspaceRenameName.trim() || workspaceBusy) return;
    setWorkspaceBusy(true);
    setWorkspaceError("");
    try {
      const updated = await requestJSON<Workspace>(`/api/v1/workspaces/${selectedWorkspace.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name: workspaceRenameName.trim() }),
      });
      setWorkspaces((current) => current.map((item) => item.id === updated.id ? updated : item));
      setWorkspaceRenameName(updated.display_name);
    } catch (requestError) {
      setWorkspaceError(requestError instanceof Error ? requestError.message : "Could not rename the workspace.");
    } finally {
      setWorkspaceBusy(false);
    }
  }

  async function renameProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedProject || !projectRenameName.trim() || projectRenameBusy) return;
    setProjectRenameBusy(true);
    setProjectRenameError("");
    try {
      const updated = await requestJSON<Project>(`/api/v1/projects/${selectedProject.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name: projectRenameName.trim() }),
      });
      setProjects((current) => current.map((item) => item.id === updated.id ? updated : item));
      setProjectRenameVisible(false);
    } catch (requestError) {
      setProjectRenameError(requestError instanceof Error ? requestError.message : "Could not rename the project.");
    } finally {
      setProjectRenameBusy(false);
    }
  }

  const countItems =
    view === "board" ? boardColumns.flatMap((column) => column.items) : items;
  const counts = {
    total: countItems.length,
    ready: countItems.filter((item) => item.status === "READY").length,
    active: countItems.filter((item) =>
      ["IN_PROGRESS", "CODE_REVIEW", "QA"].includes(item.status),
    ).length,
  };
  const visibleBoardColumns = useMemo(() => {
    const search = query.trim().toLowerCase();
    if (!search) return boardColumns;
    return boardColumns.map((column) => ({
      ...column,
      items: column.items.filter((item) =>
        `${item.key} ${item.title}`.toLowerCase().includes(search),
      ),
    }));
  }, [boardColumns, query]);
  const signInURL = apiURL("/api/v1/auth/github/start");
  const githubInstallURL = apiURL("/api/v1/integrations/github/install/start");
  const selectedAgentRun = selectedAgentRunID
    ? agentRuns.find((run) => run.id === selectedAgentRunID)
    : undefined;

  return (
    <div
      className="workspace-card"
      aria-busy={
        loading || boardLoading || detailLoading || sessionState === "loading"
      }
    >
      <div className="workspace-header">
        <div>
          <p className="workspace-kicker">{t("legacy.workspace-kicker")}</p>
          <p className="workspace-context">
            {selectedProject
              ? `${selectedProject.key} · ${selectedProject.display_name}`
            : t("legacy.workspace-context")}
          </p>
          {selectedProject && (
            <button
              className="text-button project-rename-trigger"
              type="button"
              onClick={() => {
                setProjectRenameName(selectedProject.display_name);
                setProjectRenameError("");
                setProjectRenameVisible(true);
              }}
            >
              {t("legacy.rename")}
            </button>
          )}
        </div>
        {organizations.length > 1 && (
          <label className="organization-switcher" htmlFor="organization-id">
            <span>{t("legacy.organization")}</span>
            <select
              id="organization-id"
              value={organizationID}
              disabled={organizationSwitching || sessionState !== "ready"}
              onChange={(event) => void switchOrganization(event.target.value)}
            >
              {organizations.map((organization) => (
                <option key={organization.id} value={organization.id}>
                  {organization.display_name}
                </option>
              ))}
            </select>
          </label>
        )}
        <span
          className={`connection-chip connection-${sessionState}`}
          aria-label={t("legacy.session-status")}
        >
          <span className="live-dot" aria-hidden="true" />
          {sessionState === "ready"
            ? t("legacy.signed-in")
            : sessionState === "signed-out"
              ? t("legacy.sign-in-required")
              : sessionState === "unavailable"
                ? t("legacy.reconnecting")
                : t("legacy.checking-session")}
        </span>
        {sessionState === "ready" && (
          <button className="text-button workspace-sign-out" type="button" onClick={() => void signOut()}>
            {t("legacy.sign-out")}
          </button>
        )}
      </div>
      {sessionState === "loading" && (
        <div
          className="workspace-skeleton"
          role="status"
          aria-label={t("legacy.preparing-workspace")}
        >
          <span />
          <span />
          <span />
        </div>
      )}
      {sessionState === "unavailable" && (
        <div className="error-panel" role="alert">
          <span className="error-icon" aria-hidden="true">
            !
          </span>
          <div>
            <strong>{t("legacy.unavailable-title")}</strong>
            <p>{bootstrapError}</p>
          </div>
          <button type="button" onClick={() => void bootstrap()}>
            {t("legacy.try-again")}
          </button>
        </div>
      )}
      {sessionState === "signed-out" && (
        <div className="empty-state auth-state">
          <span className="empty-icon" aria-hidden="true">
            ↗
          </span>
          <strong>{t("legacy.sign-in-workspace")}</strong>
          <p>{t("legacy.private-organization")}</p>
          <a className="button button-primary" href={signInURL}>
            {t("legacy.continue-github")} <span aria-hidden="true">↗</span>
          </a>
        </div>
      )}

      {sessionState === "ready" && (
        <>
          <div className="workspace-toolbar">
            <label className="project-field" htmlFor="project-id">
              <span>{t("legacy.project")}</span>
              <select
                id="project-id"
                value={projectID}
                onChange={(event) => {
                  setDetail(null);
                  setDetailError("");
                  setMembersError("");
                  const nextProjectID = event.target.value;
                  setProjectID(nextProjectID);
                  const nextProject = projects.find((item) => item.id === nextProjectID);
                  if (nextProject?.workspace_id) {
                    setWorkspaceID(nextProject.workspace_id);
                    setWorkspaceRenameName(workspaces.find((workspace) => workspace.id === nextProject.workspace_id)?.display_name ?? "");
                  }
                }}
                disabled={projects.length === 0}
                aria-describedby="project-help"
              >
                <option value="">
                  {projects.length === 0
                    ? t("legacy.no-projects")
                    : t("legacy.choose-project")}
                </option>
                {projects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.key} · {project.display_name}
                  </option>
                ))}
              </select>
            </label>
            <div className="workspace-actions">
              {projects.length > 0 && (
                <>
                  <div
                    className="view-toggle"
                    role="radiogroup"
                    aria-label={t("legacy.workspace-view")}
                  >
                    <button
                      type="button"
                      role="radio"
                      aria-checked={view === "list"}
                      className={view === "list" ? "is-selected" : ""}
                      onClick={() => setView("list")}
                      disabled={!projectID.trim()}
                    >
                      {t("legacy.list")}
                    </button>
                    <button
                      type="button"
                      role="radio"
                      aria-checked={view === "board"}
                      className={view === "board" ? "is-selected" : ""}
                      onClick={() => setView("board")}
                      disabled={!projectID.trim()}
                    >
                      {t("legacy.board")}
                    </button>
                  </div>
                  <button
                    className="icon-button"
                    type="button"
                    onClick={() =>
                      void (view === "board" ? loadBoard() : load())
                    }
                    disabled={loading || boardLoading || !projectID.trim()}
                    aria-label={
                      view === "board" ? t("legacy.refresh-board") : t("legacy.refresh-items")
                    }
                    title={
                      view === "board" ? t("legacy.refresh-board") : t("legacy.refresh-items")
                    }
                  >
                    <span
                      className={
                        loading || boardLoading
                          ? "refresh-icon is-spinning"
                          : "refresh-icon"
                      }
                      aria-hidden="true"
                    >
                      ↻
                    </span>
                  </button>
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => {
                      setSetupError("");
                      setSetupVisible((visible) => !visible);
                    }}
                    aria-expanded={setupVisible}
                  >
                    {t("legacy.new-project")}
                  </button>
                </>
              )}
            </div>
          </div>
          <p id="project-help" className="field-help">
            {selectedProject
              ? t("legacy.scoped-project")
              : projects.length === 0
                ? t("legacy.create-unlock")
                : t("legacy.choose-load")}
          </p>
          <div className="workspace-context-bar">
            <label htmlFor="workspace-context">
              {t("legacy.workspace")}
              <select
                id="workspace-context"
                value={workspaceID}
                onChange={(event) => {
                  setWorkspaceID(event.target.value);
                  setWorkspaceRenameName(workspaces.find((workspace) => workspace.id === event.target.value)?.display_name ?? "");
                }}
                disabled={workspaces.length === 0}
              >
                <option value="">
                  {workspaces.length === 0 ? t("legacy.no-workspaces") : t("legacy.choose-workspace")}
                </option>
                {workspaces.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.key} · {workspace.display_name}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="text-button"
              type="button"
              onClick={() => {
                setWorkspaceError("");
                setWorkspaceManageVisible((visible) => !visible);
              }}
              aria-expanded={workspaceManageVisible}
            >
              {workspaceManageVisible ? t("legacy.close-settings") : t("legacy.manage-workspaces")}
            </button>
          </div>
          {workspaceManageVisible && (
            <div className="workspace-manage-panel">
              <form className="workspace-manage-form" onSubmit={createWorkspace}>
                <label>
                  {t("legacy.new-workspace-key")}
                  <input
                    value={workspaceCreateKey}
                    onChange={(event) => setWorkspaceCreateKey(event.target.value.toUpperCase())}
                    placeholder="PRODUCT"
                    maxLength={32}
                  />
                </label>
                <label>
                  {t("legacy.new-workspace-name")}
                  <input
                    value={workspaceCreateName}
                    onChange={(event) => setWorkspaceCreateName(event.target.value)}
                    placeholder={t("legacy.product-team")}
                    maxLength={120}
                  />
                </label>
                <button className="button button-secondary" type="submit" disabled={workspaceBusy || !workspaceCreateKey.trim() || !workspaceCreateName.trim()}>
                  {workspaceBusy ? t("work.saving") : t("legacy.create-workspace")}
                </button>
              </form>
              {selectedWorkspace && (
                <form className="workspace-manage-form" onSubmit={renameWorkspace}>
                  <label>
                    {t("legacy.rename")} {selectedWorkspace.key}
                    <input
                      value={workspaceRenameName}
                      onChange={(event) => setWorkspaceRenameName(event.target.value)}
                      maxLength={120}
                    />
                  </label>
                  <span className="field-help workspace-manage-note">{t("legacy.workspace-note")}</span>
                  <button className="button button-secondary" type="submit" disabled={workspaceBusy || !workspaceRenameName.trim()}>
                    {workspaceBusy ? t("work.saving") : t("legacy.save-name")}
                  </button>
                </form>
              )}
              {workspaceError && <p className="setup-error" role="alert">{workspaceError}</p>}
            </div>
          )}
          {projectRenameVisible && selectedProject && (
            <form className="inline-form project-rename-form" onSubmit={renameProject}>
              <input
                aria-label="Project name"
                value={projectRenameName}
                maxLength={160}
                onChange={(event) => setProjectRenameName(event.target.value)}
              />
              <button className="button button-secondary" type="submit" disabled={projectRenameBusy || !projectRenameName.trim()}>
                {projectRenameBusy ? t("work.saving") : t("legacy.save-name")}
              </button>
              <button className="text-button" type="button" onClick={() => setProjectRenameVisible(false)} disabled={projectRenameBusy}>
                {t("backlog.cancel")}
              </button>
            </form>
          )}
          {projectRenameError && <div className="setup-error" role="alert">{projectRenameError}</div>}
          {projectID && (
            <nav className="workspace-shortcuts" aria-label={t("legacy.project-sections")}>
              <a href="#work-items">{t("legacy.work")}</a>
              <button type="button" onClick={() => setView("board")}>{t("legacy.board")}</button>
              <a href="#code-context">{t("legacy.code")}</a>
              <a href="#people">{t("legacy.people")}</a>
              <a href="#workflow">{t("legacy.workflow")}</a>
              <a href="#planning">{t("legacy.planning")}</a>
              <a href="#automation">{t("legacy.automation")}</a>
            </nav>
          )}
          {setupVisible && (
            <form className="setup-panel" onSubmit={createProject}>
              <div>
                <p className="workspace-kicker">Start with a clear home</p>
                <strong>Create a project</strong>
              </div>
              <div className="setup-fields">
                <label htmlFor="project-key">
                  Key
                  <input
                    id="project-key"
                    value={projectKey}
                    onChange={(event) =>
                      setProjectKey(event.target.value.toUpperCase())
                    }
                    maxLength={32}
                    placeholder="APP"
                    required
                  />
                </label>
                <label htmlFor="project-name">
                  Name
                  <input
                    id="project-name"
                    value={projectName}
                    onChange={(event) => setProjectName(event.target.value)}
                    maxLength={120}
                    placeholder="My project"
                    required
                  />
                </label>
                {workspaces.length > 0 && (
                  <label htmlFor="workspace-id">
                    Workspace
                    <select
                      id="workspace-id"
                      value={workspaceID}
                      onChange={(event) => {
                        setWorkspaceID(event.target.value);
                        setWorkspaceRenameName(workspaces.find((workspace) => workspace.id === event.target.value)?.display_name ?? "");
                      }}
                    >
                      {workspaces.map((workspace) => (
                        <option key={workspace.id} value={workspace.id}>
                          {workspace.display_name}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
              </div>
              {setupError && (
                <p className="setup-error" role="alert">
                  {setupError}
                </p>
              )}
              <div className="form-actions">
                <button
                  className="button button-primary"
                  type="submit"
                  disabled={
                    setupBusy || !projectKey.trim() || !projectName.trim()
                  }
                >
                  {setupBusy ? (
                    <>
                      <span className="button-spinner" aria-hidden="true" />{" "}
                      Creating…
                    </>
                  ) : (
                    "Create project"
                  )}
                </button>
                <button
                  className="text-button"
                  type="button"
                  onClick={() => setSetupVisible(false)}
                >
                  Cancel
                </button>
              </div>
            </form>
          )}
          {!projectID && !setupVisible && (
            <div className="empty-state project-empty">
              <span className="empty-icon" aria-hidden="true">
                ✦
              </span>
              <div className="empty-state-copy">
                <p className="workspace-kicker">Start here</p>
                <strong>Create your first project</strong>
                <p>
                  Projects are the home for your work items and connected
                  repositories.
                </p>
              </div>
              <ol className="setup-steps" aria-label="Getting started">
                <li>
                  <span className="setup-step-number">1</span>
                  <span>
                    <strong>Create a project</strong>
                    <small>Give the work a clear home.</small>
                  </span>
                </li>
                <li>
                  <span className="setup-step-number">2</span>
                  <span>
                    <strong>Connect GitHub</strong>
                    <small>Choose repositories for this project.</small>
                  </span>
                </li>
                <li>
                  <span className="setup-step-number">3</span>
                  <span>
                    <strong>Add your first task</strong>
                    <small>Make the next move visible.</small>
                  </span>
                </li>
              </ol>
              <button
                className="button button-primary"
                type="button"
                onClick={() => setSetupVisible(true)}
              >
                Create project <span aria-hidden="true">→</span>
              </button>
            </div>
          )}
          {projectID && (
            <>
              <section
                id="code-context"
                className="repository-panel"
                aria-labelledby="repository-heading"
              >
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Code context</p>
                    <strong id="repository-heading">
                      Choose a GitHub repository
                    </strong>
                    <p>
                      Link the codebase that gives this project its branches,
                      pull requests, and engineering context.
                    </p>
                    {githubInstallations.length > 0 && (
                      <span className="field-help">
                        Connected to GitHub as {githubInstallations.map((installation) => installation.account_login).join(", ")}.
                      </span>
                    )}
                  </div>
                  <a
                    className="button button-secondary"
                    href={githubInstallURL}
                  >
                    Connect GitHub <span aria-hidden="true">↗</span>
                  </a>
                </div>
                {repositoryLoading && (
                  <div className="repository-empty" role="status">
                    Checking available repositories…
                  </div>
                )}
                {repositoryError && (
                  <div className="error-panel" role="alert">
                    <span className="error-icon" aria-hidden="true">
                      !
                    </span>
                    <div>
                      <strong>GitHub repositories are unavailable</strong>
                      <p>{repositoryError}</p>
                    </div>
                    <button
                      type="button"
                      onClick={() => void loadRepositories()}
                    >
                      Try again
                    </button>
                  </div>
                )}
                {!repositoryLoading &&
                  !repositoryError &&
                  repositories.length === 0 && (
                    <div className="repository-empty">
                      <strong>
                        {githubInstallations.length === 0
                          ? "GitHub is not connected yet"
                          : "No repositories available yet"}
                      </strong>
                      <span>
                        {githubInstallations.length === 0
                          ? "Install the Forgeflow GitHub App, choose an account, then return here."
                          : "The App is connected, but no repository is selected for it. Update the GitHub App installation, select repositories, then refresh this list."}
                      </span>
                      <button
                        className="text-button"
                        type="button"
                        onClick={() => void loadRepositories()}
                      >
                        Refresh repositories
                      </button>
                    </div>
                  )}
                {!repositoryLoading &&
                  !repositoryError &&
                  repositories.length > 0 && (
                    <div className="repository-list">
                      {repositories.map((repository) => (
                        <div
                          className="repository-option-wrap"
                          key={repository.id}
                        >
                          <label className="repository-option">
                            <input
                              type="checkbox"
                              checked={repository.linked}
                              disabled={linkingRepositoryID !== ""}
                              onChange={() => void toggleRepository(repository)}
                            />
                            <span className="repository-option-copy">
                              <strong>{repository.full_name}</strong>
                              <small>
                                {repository.installation_account}{" "}
                                <span aria-hidden="true">·</span>{" "}
                                {repository.default_branch || "default branch"}
                              </small>
                            </span>
                            <span className="repository-option-state">
                              {repository.linked ? "Linked" : "Link"}
                            </span>
                          </label>
                          {repository.linked && (
                            <button
                              className="text-button repository-context-button"
                              type="button"
                              disabled={repositoryContextLoading}
                              onClick={() =>
                                void loadRepositoryContext(repository)
                              }
                            >
                              {selectedRepositoryContextID === repository.id &&
                              repositoryContextLoading
                                ? "Loading…"
                                : "View context"}
                            </button>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                {repositoryContextError && (
                  <p className="setup-error" role="alert">
                    {repositoryContextError}
                  </p>
                )}
                {selectedRepositoryContextID &&
                  repositoryContexts[selectedRepositoryContextID] && (
                    <div className="repository-context-card">
                      <div className="repository-context-heading">
                        <div>
                          <strong>
                            {
                              repositoryContexts[selectedRepositoryContextID]
                                .repository.full_name
                            }
                          </strong>
                          <small>
                            GitHub engineering context from webhook sync
                          </small>
                        </div>
                        <button
                          className="text-button"
                          type="button"
                          onClick={() => setSelectedRepositoryContextID("")}
                        >
                          Close
                        </button>
                      </div>
                      <div className="repository-context-stats">
                        <span>
                          <strong>
                            {
                              repositoryContexts[selectedRepositoryContextID]
                                .branches.length
                            }
                          </strong>{" "}
                          branches
                        </span>
                        <span>
                          <strong>
                            {
                              repositoryContexts[selectedRepositoryContextID]
                                .commits.length
                            }
                          </strong>{" "}
                          commits
                        </span>
                        <span>
                          <strong>
                            {
                              repositoryContexts[selectedRepositoryContextID]
                                .pull_requests.length
                            }
                          </strong>{" "}
                          pull requests
                        </span>
                        <span>
                          <strong>
                            {
                              repositoryContexts[selectedRepositoryContextID]
                                .ci_runs.length
                            }
                          </strong>{" "}
                          CI runs
                        </span>
                      </div>
                      <div className="repository-context-columns">
                        <div>
                          <strong>Recent pull requests</strong>
                          {repositoryContexts[
                            selectedRepositoryContextID
                          ].pull_requests
                            .slice(0, 5)
                            .map((pullRequest) => (
                              <a
                                className="repository-context-row"
                                href={pullRequest.url || undefined}
                                target="_blank"
                                rel="noreferrer"
                                key={pullRequest.id}
                              >
                                <span>#{pullRequest.number}</span>
                                <span>
                                  {pullRequest.title || "Untitled PR"}
                                </span>
                                <small>{pullRequest.state}</small>
                              </a>
                            ))}
                          {repositoryContexts[selectedRepositoryContextID]
                            .pull_requests.length === 0 && (
                            <span className="field-help">
                              No PR webhook data yet.
                            </span>
                          )}
                        </div>
                        <div>
                          <strong>Branches</strong>
                          {repositoryContexts[
                            selectedRepositoryContextID
                          ].branches
                            .slice(0, 5)
                            .map((branch) => (
                              <div
                                className="repository-context-row"
                                key={branch.id}
                              >
                                <span>{branch.name}</span>
                                <small>{branch.head_sha.slice(0, 8)}</small>
                              </div>
                            ))}
                          {repositoryContexts[selectedRepositoryContextID]
                            .branches.length === 0 && (
                            <span className="field-help">
                              No push webhook data yet.
                            </span>
                          )}
                        </div>
                        <div>
                          <strong>Recent commits</strong>
                          {repositoryContexts[
                            selectedRepositoryContextID
                          ].commits
                            .slice(0, 5)
                            .map((commit) => (
                              <div
                                className="repository-context-row"
                                key={commit.id}
                              >
                                <span>{commit.sha.slice(0, 8)}</span>
                                <span>
                                  {commit.message || "Untitled commit"}
                                </span>
                                <small>
                                  {commit.author_login || "unknown"}
                                </small>
                              </div>
                            ))}
                          {repositoryContexts[selectedRepositoryContextID]
                            .commits.length === 0 && (
                            <span className="field-help">
                              No push webhook commit data yet.
                            </span>
                          )}
                        </div>
                      </div>
                      {detail && (
                        <form
                          className="repository-index-card"
                          onSubmit={(event) => {
                            event.preventDefault();
                            void createDraftPullRequest(selectedRepositoryContextID);
                          }}
                        >
                          <div className="repository-explorer-heading">
                            <div>
                              <strong>Create a draft pull request</strong>
                              <small>
                                Opens a reviewable PR for {detail.item.key}; Forgeflow never merges it automatically.
                              </small>
                            </div>
                          </div>
                          <div className="knowledge-create-form">
                            <label>
                              Title
                              <input
                                value={draftPRTitle}
                                maxLength={256}
                                onChange={(event) => setDraftPRTitle(event.target.value)}
                              />
                            </label>
                            <label>
                              Head branch
                              <input
                                value={draftPRHead}
                                maxLength={255}
                                placeholder="forgeflow/hrm-1"
                                onChange={(event) => setDraftPRHead(event.target.value)}
                              />
                            </label>
                            <label>
                              Base branch (optional)
                              <input
                                value={draftPRBase}
                                maxLength={255}
                                placeholder={repositoryContexts[selectedRepositoryContextID].repository.default_branch || "main"}
                                onChange={(event) => setDraftPRBase(event.target.value)}
                              />
                            </label>
                            <label>
                              Description
                              <textarea
                                value={draftPRBody}
                                maxLength={131072}
                                rows={3}
                                onChange={(event) => setDraftPRBody(event.target.value)}
                              />
                            </label>
                          </div>
                          <button
                            className="button button-primary"
                            type="submit"
                            disabled={draftPRBusy || !draftPRTitle.trim() || !draftPRHead.trim()}
                          >
                            {draftPRBusy ? "Creating draft PR…" : "Create draft PR"}
                          </button>
                        </form>
                      )}
                      <div className="repository-index-card">
                        <div className="repository-explorer-heading">
                          <div>
                            <strong>Fixed-commit repository index</strong>
                            <small>Bounded code context for search, symbols, MCP, and agents.</small>
                          </div>
                          <div className="inline-actions">
                            <button
                              className="text-button"
                              type="button"
                              onClick={() => void loadRepositorySnapshots(selectedRepositoryContextID)}
                            >
                              {repositorySnapshotLoading ? "Loading…" : "History"}
                            </button>
                            <button
                              className="button button-secondary"
                              type="button"
                              disabled={repositorySnapshotBusy}
                              onClick={() => void refreshRepositorySnapshot(selectedRepositoryContextID)}
                            >
                              {repositorySnapshotBusy ? "Indexing…" : "Index latest commit"}
                            </button>
                          </div>
                        </div>
                        {repositorySnapshotError && <p className="setup-error" role="alert">{repositorySnapshotError}</p>}
                        {(repositorySnapshots[selectedRepositoryContextID] ?? []).length > 0 ? (
                          <div className="snapshot-list">
                            {(repositorySnapshots[selectedRepositoryContextID] ?? []).map((snapshot) => (
                              <div className="snapshot-row" key={snapshot.id}>
                                <span className={`snapshot-status ${snapshot.status === "READY" ? "snapshot-ready" : "snapshot-failed"}`}>{snapshot.status}</span>
                                <span className="snapshot-copy"><strong>{snapshot.commit_sha.slice(0, 12)}</strong><small>{snapshot.file_count} files · {snapshot.symbol_count} symbols · {snapshot.skipped_count} skipped</small></span>
                                <small>{snapshot.ref_name || "fixed ref"}</small>
                                <button
                                  className="text-button"
                                  type="button"
                                  onClick={() => {
                                    if (repositorySnapshotEdges[snapshot.id]) {
                                      setRepositorySnapshotEdges((current) => {
                                        const next = { ...current };
                                        delete next[snapshot.id];
                                        return next;
                                      });
                                    } else {
                                      void loadRepositorySnapshotEdges(selectedRepositoryContextID, snapshot.id);
                                    }
                                  }}
                                  disabled={repositorySnapshotEdgesLoading !== ""}
                                >
                                  {repositorySnapshotEdgesLoading === snapshot.id ? "Loading…" : repositorySnapshotEdges[snapshot.id] ? "Hide imports" : "Imports"}
                                </button>
                                {repositorySnapshotEdges[snapshot.id] && (
                                  <div className="snapshot-edge-list">
                                    {repositorySnapshotEdges[snapshot.id].length === 0 ? (
                                      <small>No extracted imports in this snapshot.</small>
                                    ) : (
                                      repositorySnapshotEdges[snapshot.id].slice(0, 30).map((edge) => (
                                        <span key={`${edge.from}:${edge.to}:${edge.kind}`}>
                                          {edge.from} → {edge.to}
                                        </span>
                                      ))
                                    )}
                                  </div>
                                )}
                              </div>
                            ))}
                          </div>
                        ) : (
                          <p className="field-help">No persisted snapshot yet. Index the latest default-branch commit to make repository context stable and reviewable.</p>
                        )}
                      </div>
                      <div className="knowledge-panel">
                        <div className="repository-explorer-heading">
                          <div>
                            <strong>Repository memory</strong>
                            <small>Manual, extracted, or AI-proposed notes with immutable revisions.</small>
                          </div>
                          <button className="text-button" type="button" onClick={() => void loadKnowledge(selectedRepositoryContextID)}>
                            {knowledgeLoading ? "Loading…" : "Refresh memory"}
                          </button>
                        </div>
                        {knowledgeError && <p className="setup-error" role="alert">{knowledgeError}</p>}
                        {(knowledgeDocuments[selectedRepositoryContextID] ?? []).map((document) => (
                          <div className={`knowledge-row${selectedKnowledgeDocument?.id === document.id ? " is-selected" : ""}`} key={document.id}>
                            <span className="snapshot-status">{document.kind}</span>
                            <span className="snapshot-copy"><strong>{document.title}</strong><small>{document.slug} · {document.current_provenance}</small></span>
                            <button className="text-button" type="button" aria-expanded={selectedKnowledgeDocument?.id === document.id} onClick={() => void openKnowledgeDocument(selectedRepositoryContextID, document)}>
                              {selectedKnowledgeDocument?.id === document.id ? "Close" : "Review"}
                            </button>
                          </div>
                        ))}
                        {selectedKnowledgeDocument && (
                          <div className="knowledge-revision-editor">
                            <div className="repository-explorer-heading">
                              <div>
                                <strong>{selectedKnowledgeDocument.title}</strong>
                                <small>Revision {selectedKnowledgeDocument.latest_revision?.revision_number ?? 0} · append-only history</small>
                              </div>
                              <span className="snapshot-status">{selectedKnowledgeDocument.current_provenance}</span>
                            </div>
                            {knowledgeRevisions.length > 0 && (
                              <div className="knowledge-history" aria-label="Knowledge revision history">
                                <div className="repository-explorer-heading">
                                  <div>
                                    <strong>Revision history</strong>
                                    <small>Review an older revision against the current one.</small>
                                  </div>
                                  <small>{knowledgeRevisions.length} revision{knowledgeRevisions.length === 1 ? "" : "s"}</small>
                                </div>
                                <div className="knowledge-history-list">
                                  {knowledgeRevisions.map((revision) => (
                                    <button
                                      className={`knowledge-history-item${knowledgeCompareRevision?.id === revision.id ? " is-selected" : ""}`}
                                      type="button"
                                      aria-pressed={knowledgeCompareRevision?.id === revision.id}
                                      key={revision.id}
                                      onClick={() => setKnowledgeCompareRevision(revision.id === selectedKnowledgeDocument.latest_revision?.id ? null : revision)}
                                    >
                                      <span>Revision {revision.revision_number}</span>
                                      <small>{revision.provenance} · {new Date(revision.created_at).toLocaleString()}</small>
                                    </button>
                                  ))}
                                </div>
                                {knowledgeCompareRevision && selectedKnowledgeDocument.latest_revision && knowledgeCompareRevision.id !== selectedKnowledgeDocument.latest_revision.id && (
                                  <pre className="knowledge-diff" aria-label="Knowledge revision diff">{positionalKnowledgeDiff(knowledgeCompareRevision.content, selectedKnowledgeDocument.latest_revision.content)}</pre>
                                )}
                              </div>
                            )}
                            <textarea aria-label="Knowledge revision content" value={knowledgeRevisionContent} onChange={(event) => setKnowledgeRevisionContent(event.target.value)} placeholder="Write the next revision…" maxLength={512 * 1024} />
                            <div className="knowledge-revision-actions">
                              <label>Provenance<select value={knowledgeRevisionProvenance} onChange={(event) => setKnowledgeRevisionProvenance(event.target.value)}><option value="MANUAL">Manual</option><option value="EXTRACTED">Extracted</option><option value="AI_PROPOSED">AI proposed</option><option value="HUMAN_VERIFIED">Human verified</option></select></label>
                              <button className="button button-secondary" type="button" disabled={knowledgeRevisionBusy || !knowledgeRevisionContent.trim()} onClick={() => void appendKnowledgeRevision(selectedRepositoryContextID)}>{knowledgeRevisionBusy ? "Appending…" : "Append revision"}</button>
                            </div>
                            <small className="field-help">Verified knowledge can only receive another verified revision. AI-proposed content remains explicitly unverified.</small>
                          </div>
                        )}
                        <form className="knowledge-create-form" onSubmit={(event) => { event.preventDefault(); void createKnowledge(selectedRepositoryContextID); }}>
                          <input aria-label="Knowledge title" value={knowledgeTitle} onChange={(event) => setKnowledgeTitle(event.target.value)} placeholder="Document title" maxLength={160} />
                          <input aria-label="Knowledge slug" value={knowledgeSlug} onChange={(event) => setKnowledgeSlug(event.target.value)} placeholder="conventions" maxLength={96} />
                          <select aria-label="Knowledge kind" value={knowledgeKind} onChange={(event) => setKnowledgeKind(event.target.value)}><option value="ARCHITECTURE">Architecture</option><option value="CONVENTIONS">Conventions</option><option value="TESTING">Testing</option><option value="DOMAIN_RULES">Domain rules</option><option value="KNOWN_ISSUES">Known issues</option><option value="MODULE">Module</option></select>
                          <textarea aria-label="Knowledge content" value={knowledgeContent} onChange={(event) => setKnowledgeContent(event.target.value)} placeholder="A bounded rule or convention the team should preserve…" maxLength={512 * 1024} />
                          <button className="button button-secondary" type="submit" disabled={knowledgeBusy || !knowledgeTitle.trim() || !knowledgeSlug.trim() || !knowledgeContent.trim()}>{knowledgeBusy ? "Saving…" : "Save memory"}</button>
                        </form>
                      </div>
                      <div className="repository-explorer">
                        <div className="repository-explorer-heading">
                          <div>
                            <strong>Repository explorer</strong>
                            <small>
                              Read-only, bounded files from the default branch
                            </small>
                          </div>
                          <button
                            className="text-button"
                            type="button"
                            onClick={() =>
                              void loadRepositoryTree(
                                selectedRepositoryContextID,
                              )
                            }
                          >
                            {repositoryTreeLoading
                              ? "Loading…"
                              : repositoryTreeVisible
                                ? "Reload files"
                                : "Browse files"}
                          </button>
                        </div>
                        {repositoryTreeVisible && (
                          <>
                            <form
                              className="repository-search-form"
                              onSubmit={(event) => {
                                event.preventDefault();
                                void searchRepositoryCode(
                                  selectedRepositoryContextID,
                                );
                              }}
                            >
                              <input
                                aria-label="Search repository code"
                                value={repositorySearchQuery}
                                onChange={(event) =>
                                  setRepositorySearchQuery(event.target.value)
                                }
                                placeholder="Search code or file path"
                              />
                              <button
                                className="button button-secondary"
                                type="submit"
                                disabled={
                                  repositorySearchBusy ||
                                  !repositorySearchQuery.trim()
                                }
                              >
                                {repositorySearchBusy ? "Searching…" : "Search"}
                              </button>
                            </form>
                            {repositorySearchResults.length > 0 && (
                              <div className="repository-search-results">
                                {repositorySearchResults.map((match, index) => (
                                  <button
                                    className="repository-file-row"
                                    type="button"
                                    key={`${match.path}-${match.line}-${index}`}
                                    onClick={() =>
                                      void loadRepositoryFile(
                                        selectedRepositoryContextID,
                                        match.path,
                                      )
                                    }
                                  >
                                    <span>{match.path}</span>
                                    <small>
                                      line {match.line} · {match.snippet}
                                    </small>
                                  </button>
                                ))}
                              </div>
                            )}
                            <div className="repository-explorer-grid">
                              <div className="repository-file-list">
                                {repositoryTree
                                  .filter((entry) => entry.type === "blob")
                                  .slice(0, 80)
                                  .map((entry) => (
                                    <button
                                      className="repository-file-row"
                                      type="button"
                                      key={entry.path}
                                      onClick={() =>
                                        void loadRepositoryFile(
                                          selectedRepositoryContextID,
                                          entry.path,
                                        )
                                      }
                                    >
                                      <span>{entry.path}</span>
                                      <small>
                                        {entry.size.toLocaleString()} bytes
                                      </small>
                                    </button>
                                  ))}
                              </div>
                              <div className="repository-file-preview">
                                {repositoryFileLoading && (
                                  <span className="field-help">
                                    Loading file…
                                  </span>
                                )}
                                {!repositoryFileLoading && repositoryFile && (
                                  <>
                                    <strong>{repositoryFile.path}</strong>
                                    <pre>{repositoryFile.content}</pre>
                                  </>
                                )}
                                {!repositoryFileLoading && !repositoryFile && (
                                  <span className="field-help">
                                    Select a file to inspect it.
                                  </span>
                                )}
                              </div>
                            </div>
                          </>
                        )}
                      </div>
                    </div>
                  )}
              </section>
              <section
                id="people"
                className="members-panel"
                aria-labelledby="members-heading"
              >
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">People</p>
                    <strong id="members-heading">People and access</strong>
                    <p>
                      Organization roles control access everywhere. A project
                      override is optional and only affects this project.
                    </p>
                  </div>
                  <span className="member-count">
                    {organizationMembers.length} organization {organizationMembers.length === 1 ? "member" : "members"}
                  </span>
                </div>
                <form
                  className="member-add-form"
                  onSubmit={addOrganizationMember}
                >
                  <label htmlFor="member-login">
                    Add by GitHub login
                    <input
                      id="member-login"
                      value={memberLogin}
                      onChange={(event) => setMemberLogin(event.target.value)}
                      placeholder="octocat"
                      maxLength={120}
                    />
                  </label>
                  <label htmlFor="member-role">
                    Organization role
                    <select
                      id="member-role"
                      value={memberRole}
                      onChange={(event) => setMemberRole(event.target.value)}
                    >
                      <option value="developer">Developer</option>
                      <option value="project_manager">Project Manager</option>
                      <option value="qa">QA</option>
                      <option value="viewer">Viewer</option>
                      <option value="admin">Admin</option>
                    </select>
                  </label>
                  <button
                    className="button button-secondary"
                    type="submit"
                    disabled={memberAddBusy || !memberLogin.trim()}
                  >
                    {memberAddBusy ? "Adding…" : "Add member"}
                  </button>
                </form>
                {membersLoading && (
                  <div className="repository-empty" role="status">
                    Loading project members…
                  </div>
                )}
                {membersError && (
                  <div className="setup-error" role="alert">
                    {membersError}
                  </div>
                )}
                {!membersLoading && !membersError && members.length === 0 && (
                  <div className="repository-empty">
                    <strong>No members available</strong>
                    <span>
                      Invite people through the organization membership flow
                      before assigning work.
                    </span>
                  </div>
                )}
                {!membersLoading && members.length > 0 && (
                  <div className="member-list">
                    {members.map((member) => (
                      <div className="member-row" key={member.id}>
                        <span className="member-avatar" aria-hidden="true">
                          {memberName(member).slice(0, 1).toUpperCase()}
                        </span>
                        <span className="member-copy">
                          <strong>{memberName(member)}</strong>
                          <small>@{member.login}</small>
                        </span>
                        <span className="member-actions">
                          <span className="member-role-group">
                            <small>Organization</small>
                            <select
                              aria-label={`Organization role for ${memberName(member)}`}
                              value={organizationMembers.find((item) => item.id === member.id)?.role_key ?? member.role_key}
                              disabled={organizationMemberBusy !== "" || organizationMembersLoading}
                              onChange={(event) =>
                                void changeOrganizationMemberRole(member, event.target.value)
                              }
                            >
                              <option value="owner">Owner</option>
                              <option value="admin">Admin</option>
                              <option value="project_manager">Project Manager</option>
                              <option value="developer">Developer</option>
                              <option value="qa">QA</option>
                              <option value="viewer">Viewer</option>
                            </select>
                          </span>
                          <span className="member-role-group">
                            <small>Project access</small>
                            <select
                              aria-label={`Project role for ${memberName(member)}`}
                              value={member.project_role ? member.role_key : ""}
                              disabled={memberRoleBusy !== ""}
                              onChange={(event) => {
                                if (event.target.value) void changeMemberRole(member, event.target.value);
                              }}
                            >
                              <option value="">
                                Inherited ({organizationMembers.find((item) => item.id === member.id)?.role_key ?? member.role_key})
                              </option>
                              <option value="owner">Owner</option>
                              <option value="admin">Admin</option>
                              <option value="project_manager">Project Manager</option>
                              <option value="developer">Developer</option>
                              <option value="qa">QA</option>
                              <option value="viewer">Viewer</option>
                            </select>
                          </span>
                          {member.project_role && (
                            <button
                              className="text-button danger-text"
                              type="button"
                              disabled={memberRoleBusy !== ""}
                              onClick={() => void removeMember(member)}
                            >
                              Remove override
                            </button>
                          )}
                          <button
                            className="text-button danger-text"
                            type="button"
                            disabled={organizationMemberBusy !== ""}
                            onClick={() => void removeOrganizationMember(member)}
                          >
                            Remove organization access
                          </button>
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </section>
              <section
                id="workflow"
                className="workflow-panel"
                aria-labelledby="workflow-heading"
              >
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Flow control</p>
                    <strong id="workflow-heading">Workflow</strong>
                    <p>
                      Define the statuses and guarded transitions that move
                      work through this project.
                    </p>
                  </div>
                  <div className="inline-actions">
                    <span className="member-count">
                      {workflowStatuses.length} statuses · {workflowTransitions.length} transitions
                    </span>
                    <button
                      className="button button-secondary"
                      type="button"
                      onClick={() => {
                        setWorkflowError("");
                        setWorkflowEditorVisible((visible) => !visible);
                      }}
                      aria-expanded={workflowEditorVisible}
                    >
                      {workflowEditorVisible ? "Close editor" : "Edit workflow"}
                    </button>
                  </div>
                </div>
                <p className="field-help">
                  {workflowName}. READY always enforces the specification quality gate on the server.
                </p>
                {workflowError && (
                  <div className="setup-error" role="alert">
                    {workflowError}
                  </div>
                )}
                {workflowEditorVisible && (
                  <form className="workflow-editor" onSubmit={saveWorkflow}>
                    <label htmlFor="workflow-name">
                      Workflow name
                      <input
                        id="workflow-name"
                        value={workflowName}
                        maxLength={120}
                        onChange={(event) => setWorkflowName(event.target.value)}
                        required
                      />
                    </label>
                    <div className="workflow-editor-grid">
                      <div className="workflow-editor-card">
                        <div className="workflow-editor-card-header">
                          <div>
                            <strong>Status columns</strong>
                            <small>RAW is the protected initial status.</small>
                          </div>
                          <button
                            className="text-button"
                            type="button"
                            onClick={addWorkflowStatus}
                            disabled={workflowBusy || workflowDraftStatuses.length >= 50}
                          >
                            Add status
                          </button>
                        </div>
                        <div className="workflow-status-list">
                          {workflowDraftStatuses.map((status) => (
                            <div className="workflow-status-row" key={status.key}>
                              <input
                                aria-label={`Key for ${status.display_name}`}
                                value={status.key}
                                maxLength={32}
                                onChange={(event) =>
                                  updateWorkflowStatus(status.key, {
                                    key: event.target.value.toUpperCase(),
                                  })
                                }
                              />
                              <input
                                aria-label={`Name for ${status.display_name}`}
                                value={status.display_name}
                                maxLength={120}
                                onChange={(event) =>
                                  updateWorkflowStatus(status.key, {
                                    display_name: event.target.value,
                                  })
                                }
                              />
                              <select
                                aria-label={`Category for ${status.display_name}`}
                                value={status.category}
                                onChange={(event) =>
                                  updateWorkflowStatus(status.key, {
                                    category: event.target.value,
                                  })
                                }
                              >
                                <option value="TODO">To do</option>
                                <option value="IN_PROGRESS">In progress</option>
                                <option value="DONE">Done</option>
                                <option value="CANCELLED">Cancelled</option>
                              </select>
                              <input
                                aria-label={`Position for ${status.display_name}`}
                                type="number"
                                value={status.position}
                                onChange={(event) =>
                                  updateWorkflowStatus(status.key, {
                                    position: Number(event.target.value),
                                  })
                                }
                              />
                              <label className="workflow-check">
                                <input
                                  type="checkbox"
                                  checked={status.is_terminal}
                                  onChange={(event) =>
                                    updateWorkflowStatus(status.key, {
                                      is_terminal: event.target.checked,
                                    })
                                  }
                                />
                                Terminal
                              </label>
                              <button
                                className="text-button danger-text"
                                type="button"
                                onClick={() => removeWorkflowStatus(status.key)}
                                disabled={workflowBusy || status.key === "RAW"}
                                aria-label={`Remove ${status.display_name}`}
                              >
                                Remove
                              </button>
                            </div>
                          ))}
                        </div>
                      </div>
                      <div className="workflow-editor-card">
                        <div className="workflow-editor-card-header">
                          <div>
                            <strong>Transitions</strong>
                            <small>Rules are checked atomically before a status change.</small>
                          </div>
                          <button
                            className="text-button"
                            type="button"
                            onClick={addWorkflowTransition}
                            disabled={workflowBusy || workflowDraftStatuses.length < 2 || workflowDraftTransitions.length >= 200}
                          >
                            Add transition
                          </button>
                        </div>
                        <div className="workflow-transition-list">
                          {workflowDraftTransitions.map((transition) => (
                            <div className="workflow-transition-row" key={transition.key}>
                              <div className="workflow-transition-fields">
                                <input
                                  aria-label={`Key for ${transition.display_name}`}
                                  value={transition.key}
                                  maxLength={64}
                                  onChange={(event) =>
                                    updateWorkflowTransition(transition.key, {
                                      key: event.target.value.toLowerCase(),
                                    })
                                  }
                                />
                                <input
                                  aria-label={`Name for ${transition.key}`}
                                  value={transition.display_name}
                                  maxLength={120}
                                  onChange={(event) =>
                                    updateWorkflowTransition(transition.key, {
                                      display_name: event.target.value,
                                    })
                                  }
                                />
                                <select
                                  aria-label={`From status for ${transition.key}`}
                                  value={transition.from_status}
                                  onChange={(event) =>
                                    updateWorkflowTransition(transition.key, {
                                      from_status: event.target.value,
                                    })
                                  }
                                >
                                  {workflowDraftStatuses.map((status) => (
                                    <option key={status.key} value={status.key}>
                                      From {status.display_name}
                                    </option>
                                  ))}
                                </select>
                                <select
                                  aria-label={`To status for ${transition.key}`}
                                  value={transition.to_status}
                                  onChange={(event) =>
                                    updateWorkflowTransition(transition.key, {
                                      to_status: event.target.value,
                                    })
                                  }
                                >
                                  {workflowDraftStatuses.map((status) => (
                                    <option key={status.key} value={status.key}>
                                      To {status.display_name}
                                    </option>
                                  ))}
                                </select>
                              </div>
                              <div className="workflow-rules">
                                {workflowRuleOptions.map(([rule, label]) => (
                                  <label className="workflow-check" key={rule}>
                                    <input
                                      type="checkbox"
                                      checked={(transition.required_rules ?? []).includes(rule)}
                                      onChange={(event) => {
                                        const rules = new Set(transition.required_rules ?? []);
                                        if (event.target.checked) rules.add(rule);
                                        else rules.delete(rule);
                                        updateWorkflowTransition(transition.key, {
                                          required_rules: [...rules],
                                        });
                                      }}
                                    />
                                    {label}
                                  </label>
                                ))}
                              </div>
                              {(transition.required_rules ?? []).includes("require_permission") && (
                                <label className="workflow-permission-input">
                                  Required capabilities
                                  <input
                                    aria-label={`Required capabilities for ${transition.key}`}
                                    value={(transition.required_permissions ?? []).join(", ")}
                                    placeholder="work_item.transition, agent.approve"
                                    onChange={(event) =>
                                      updateWorkflowTransition(transition.key, {
                                        required_permissions: event.target.value
                                          .split(",")
                                          .map((permission) => permission.trim().toLowerCase())
                                          .filter(Boolean),
                                      })
                                    }
                                  />
                                  <small>Comma-separated capability keys checked on the server.</small>
                                </label>
                              )}
                              <button
                                className="text-button danger-text"
                                type="button"
                                onClick={() => removeWorkflowTransition(transition.key)}
                                disabled={workflowBusy}
                              >
                                Remove transition
                              </button>
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                    <div className="workflow-editor-actions">
                      <button
                        className="button button-primary"
                        type="submit"
                        disabled={workflowBusy || !workflowName.trim()}
                      >
                        {workflowBusy ? "Saving workflow…" : "Save workflow"}
                      </button>
                      <button
                        className="text-button"
                        type="button"
                        onClick={cancelWorkflowEdit}
                        disabled={workflowBusy}
                      >
                        Cancel
                      </button>
                    </div>
                  </form>
                )}
              </section>
              <section className="custom-field-panel" aria-labelledby="custom-fields-heading">
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Project fields</p>
                    <strong id="custom-fields-heading">Custom fields</strong>
                    <p>Keep project-specific facts typed and searchable without changing the core work-item model.</p>
                  </div>
                  <span className="member-count">{customFields.length} fields</span>
                </div>
                {customFieldsError && <div className="setup-error" role="alert">{customFieldsError}</div>}
                <form className="custom-field-create-form" onSubmit={createCustomField}>
                  <label htmlFor="custom-field-key">Key<input id="custom-field-key" value={customFieldKey} maxLength={32} onChange={(event) => setCustomFieldKey(event.target.value.toUpperCase())} placeholder="RISK" /></label>
                  <label htmlFor="custom-field-name">Name<input id="custom-field-name" value={customFieldName} maxLength={120} onChange={(event) => setCustomFieldName(event.target.value)} placeholder="Risk level" /></label>
                  <label htmlFor="custom-field-type">Type<select id="custom-field-type" value={customFieldType} onChange={(event) => setCustomFieldType(event.target.value as CustomFieldDefinition["value_type"])}><option value="TEXT">Text</option><option value="NUMBER">Number</option><option value="BOOLEAN">Boolean</option><option value="DATE">Date</option><option value="SELECT">Select</option></select></label>
                  {customFieldType === "SELECT" && <label htmlFor="custom-field-options">Options<input id="custom-field-options" value={customFieldOptions} onChange={(event) => setCustomFieldOptions(event.target.value)} placeholder="low, medium, high" /></label>}
                  <button className="button button-secondary" type="submit" disabled={customFieldBusy || !customFieldKey.trim() || !customFieldName.trim()}>{customFieldBusy ? "Saving…" : "Add field"}</button>
                </form>
                {customFieldsLoading ? <div className="repository-empty" role="status">Loading custom fields…</div> : customFields.length > 0 ? <div className="custom-field-list">{customFields.map((field) => editingCustomFieldID === field.id ? <div className="custom-field-row" key={field.id}><div className="custom-field-edit"><input aria-label={`Name for ${field.key}`} value={customFieldEditName} onChange={(event) => setCustomFieldEditName(event.target.value)} /><small>{field.key} · {field.value_type.toLowerCase()}</small>{field.value_type === "SELECT" && <input aria-label={`Options for ${field.key}`} value={customFieldEditOptions} onChange={(event) => setCustomFieldEditOptions(event.target.value)} placeholder="low, medium, high" />}</div><span className="inline-actions"><button className="text-button" type="button" disabled={customFieldBusy || !customFieldEditName.trim()} onClick={() => void saveCustomFieldEdit(field)}>Save</button><button className="text-button" type="button" disabled={customFieldBusy} onClick={() => setEditingCustomFieldID("")}>Cancel</button></span></div> : <div className="custom-field-row" key={field.id}><span><strong>{field.display_name}</strong><small>{field.key} · {field.value_type.toLowerCase()}</small></span><span className="inline-actions"><button className="text-button" type="button" disabled={customFieldBusy} onClick={() => startEditingCustomField(field)}>Edit</button><button className="text-button danger-text" type="button" disabled={customFieldBusy} onClick={() => void deleteCustomField(field)}>Delete</button></span></div>)}</div> : <div className="repository-empty"><span>No custom fields yet.</span></div>}
              </section>
              <section
                id="planning"
                className="sprint-panel"
                aria-labelledby="sprint-heading"
              >
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Planning</p>
                    <strong id="sprint-heading">Sprints</strong>
                    <p>
                      Keep a short horizon visible without duplicating workflow
                      status.
                    </p>
                  </div>
                  <span className="member-count">
                    {
                      sprints.filter((sprint) => sprint.status === "ACTIVE")
                        .length
                    }{" "}
                    active
                  </span>
                </div>
                {sprintError && (
                  <div className="setup-error" role="alert">
                    {sprintError}
                  </div>
                )}
                {sprintsLoading ? (
                  <div className="repository-empty" role="status">
                    Loading sprints…
                  </div>
                ) : (
                  <div className="sprint-list">
                    {sprints.map((sprint) => (
                      <div className="sprint-row" key={sprint.id}>
                        <span
                          className={`sprint-status sprint-${sprint.status.toLowerCase()}`}
                        >
                          {sprint.status}
                        </span>
                        <span className="sprint-copy">
                          <strong>{sprint.name}</strong>
                          <small>
                            {sprint.goal || "No sprint goal yet"}
                            {(sprint.starts_at || sprint.ends_at) && (
                              <>
                                <span aria-hidden="true"> · </span>
                                {dateValue(sprint.starts_at)}
                                {sprint.ends_at
                                  ? ` → ${dateValue(sprint.ends_at)}`
                                  : ""}
                              </>
                            )}
                          </small>
                        </span>
                        {sprint.status !== "COMPLETED" && (
                          <span className="inline-actions">
                            {sprint.status === "PLANNED" && (
                              <>
                                <button className="text-button" type="button" disabled={sprintBusy} onClick={() => startEditingSprint(sprint)}>Edit</button>
                                <button className="text-button danger-text" type="button" disabled={sprintBusy} onClick={() => void deleteSprint(sprint)}>Delete</button>
                              </>
                            )}
                            <button
                              className="text-button"
                              type="button"
                              disabled={sprintBusy}
                              onClick={() => void transitionSprint(sprint)}
                            >
                              {sprint.status === "PLANNED" ? "Start" : "Complete"}
                            </button>
                          </span>
                        )}
                      </div>
                    ))}
                    {sprints.length === 0 && (
                      <span className="field-help">
                        No sprint yet. Create one when the backlog is ready.
                      </span>
                    )}
                  </div>
                )}
                <form className="sprint-create-form" onSubmit={createSprint}>
                  <input
                    aria-label="Sprint name"
                    value={sprintName}
                    onChange={(event) => setSprintName(event.target.value)}
                    placeholder="Sprint name"
                  />
                  <input
                    aria-label="Sprint goal"
                    value={sprintGoal}
                    onChange={(event) => setSprintGoal(event.target.value)}
                    placeholder="Goal (optional)"
                  />
                  <input
                    aria-label="Sprint start date"
                    type="date"
                    value={sprintStartsAt}
                    onChange={(event) => setSprintStartsAt(event.target.value)}
                  />
                  <input
                    aria-label="Sprint end date"
                    type="date"
                    value={sprintEndsAt}
                    onChange={(event) => setSprintEndsAt(event.target.value)}
                  />
                  <button
                    className="button button-secondary"
                    type="submit"
                    disabled={sprintBusy || !sprintName.trim()}
                  >
                    {sprintBusy ? "Saving…" : editingSprintID ? "Save sprint" : "Create sprint"}
                  </button>
                  {editingSprintID && (
                    <button
                      className="text-button"
                      type="button"
                      onClick={() => {
                        setEditingSprintID("");
                        setSprintName("");
                        setSprintGoal("");
                        setSprintStartsAt("");
                        setSprintEndsAt("");
                      }}
                    >
                      Cancel edit
                    </button>
                  )}
                </form>
              </section>
              <section className="token-panel" aria-labelledby="token-heading">
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Developer access</p>
                    <strong id="token-heading">MCP access tokens</strong>
                    <p>
                      Create a scoped token for Codex, Claude, or another MCP
                      client. The secret is shown once.
                    </p>
                  </div>
                  <span className="member-count">
                    {accessTokens.length} active
                  </span>
                </div>
                {tokenError && (
                  <div className="setup-error" role="alert">
                    {tokenError}
                  </div>
                )}
                {newToken && (
                  <div className="token-created" role="status">
                    <strong>Copy this token now</strong>
                    <code>{newToken}</code>
                    <button
                      className="button button-secondary"
                      type="button"
                      onClick={() => void copyAccessToken()}
                    >
                      {tokenCopied ? "Copied" : "Copy token"}
                    </button>
                  </div>
                )}
                <form
                  className="token-create-form"
                  onSubmit={createAccessToken}
                >
                  <label htmlFor="token-name">
                    Name
                    <input
                      id="token-name"
                      value={tokenName}
                      maxLength={100}
                      onChange={(event) => setTokenName(event.target.value)}
                    />
                  </label>
                  <label htmlFor="token-profile">
                    Access
                    <select
                      id="token-profile"
                      value={tokenProfile}
                      onChange={(event) =>
                        setTokenProfile(event.target.value as "read" | "mcp")
                      }
                    >
                      <option value="mcp">MCP work-item access</option>
                      <option value="read">Read-only context</option>
                    </select>
                  </label>
                  <label htmlFor="token-expiry">
                    Expires in days
                    <input
                      id="token-expiry"
                      type="number"
                      min="1"
                      max="365"
                      value={tokenExpiry}
                      onChange={(event) => setTokenExpiry(event.target.value)}
                    />
                  </label>
                  <button
                    className="button button-secondary"
                    type="submit"
                    disabled={tokenBusy || !tokenName.trim()}
                  >
                    {tokenBusy ? "Saving…" : "Create token"}
                  </button>
                </form>
                {accessTokens.length > 0 && (
                  <div className="token-list">
                    {accessTokens.map((token) => (
                      <div className="token-row" key={token.id}>
                        <span>
                          <strong>{token.name}</strong>
                          <small>
                            {token.prefix} · expires{" "}
                            {formatDate(token.expires_at)}
                          </small>
                        </span>
                        <button
                          className="text-button danger-text"
                          type="button"
                          disabled={tokenBusy}
                          onClick={() => void revokeAccessToken(token)}
                        >
                          Revoke
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </section>
              <section
                className="notification-panel"
                aria-labelledby="notification-heading"
              >
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Keep in the loop</p>
                    <strong id="notification-heading">Notifications</strong>
                    <p>
                      Project automation and engineering events appear here.
                    </p>
                  </div>
                  <button
                    className="text-button"
                    type="button"
                    disabled={notifications.every((item) => item.read_at)}
                    onClick={() => void markAllNotificationsRead()}
                  >
                    Mark all read
                  </button>
                </div>
                {notificationError && (
                  <div className="setup-error" role="alert">
                    {notificationError}
                  </div>
                )}
                {notificationsLoading ? (
                  <div className="repository-empty" role="status">
                    Loading notifications…
                  </div>
                ) : notifications.length === 0 ? (
                  <div className="repository-empty">
                    <strong>You’re all caught up</strong>
                    <span>Unread project activity will land here.</span>
                  </div>
                ) : (
                  <div className="notification-list">
                    {notifications.map((notification) => (
                      <button
                        className={`notification-row${notification.read_at ? " is-read" : ""}`}
                        type="button"
                        key={notification.id}
                        onClick={() => void markNotificationRead(notification)}
                      >
                        <span className="notification-dot" aria-hidden="true" />
                        <span>
                          <strong>{notification.title}</strong>
                          <small>{notification.body}</small>
                          <small>{formatDate(notification.created_at)}</small>
                        </span>
                      </button>
                    ))}
                  </div>
                )}
              </section>
              <section
                id="automation"
                className="automation-panel"
                aria-labelledby="automation-heading"
              >
                <div className="repository-panel-header">
                  <div className="repository-panel-copy">
                    <p className="workspace-kicker">Small, explicit rules</p>
                    <strong id="automation-heading">Automation</strong>
                    <p>Notify project members when a selected event happens.</p>
                  </div>
                  <span className="member-count">
                    {automationRules.length} rules
                  </span>
                </div>
                {automationError && (
                  <div className="setup-error" role="alert">
                    {automationError}
                  </div>
                )}
                <form
                  className="automation-create-form"
                  onSubmit={createAutomationRule}
                >
                  <label htmlFor="automation-name">
                    Rule name
                    <input
                      id="automation-name"
                      value={automationName}
                      maxLength={120}
                      onChange={(event) =>
                        setAutomationName(event.target.value)
                      }
                    />
                  </label>
                  <label htmlFor="automation-event">
                    When
                    <select
                      id="automation-event"
                      value={automationEvent}
                      onChange={(event) =>
                        setAutomationEvent(event.target.value)
                      }
                    >
                      <option value="work_item.created">
                        Work item is created
                      </option>
                      <option value="work_item.updated">
                        Work item is updated
                      </option>
                      <option value="work_item.assigned">
                        Work item is assigned
                      </option>
                      <option value="work_item.transitioned">
                        Work item changes status
                      </option>
                      <option value="work_item.comment.created">
                        Comment is added
                      </option>
                      <option value="repository.linked">
                        Repository is linked
                      </option>
                      <option value="github.pull_request.updated">
                        Pull request changes
                      </option>
                      <option value="github.ci.updated">
                        CI result changes
                      </option>
                      <option value="github.push">
                        Repository receives a push
                      </option>
                    </select>
                  </label>
                  <button
                    className="button button-secondary"
                    type="submit"
                    disabled={automationBusy || !automationName.trim()}
                  >
                    {automationBusy ? "Saving…" : "Add rule"}
                  </button>
                </form>
                {automationLoading ? (
                  <div className="repository-empty" role="status">
                    Loading automation rules…
                  </div>
                ) : automationRules.length > 0 ? (
                  <div className="automation-list">
                    {automationRules.map((rule) => (
                      <div className="automation-row" key={rule.id}>
                        <span>
                          <strong>{rule.name}</strong>
                          <small>
                            {rule.event_type} · notify project members
                          </small>
                        </span>
                        <span className="automation-actions">
                          <button
                            className="text-button"
                            type="button"
                            disabled={automationBusy}
                            onClick={() => void toggleAutomationRule(rule)}
                          >
                            {rule.enabled ? "Pause" : "Enable"}
                          </button>
                          <button
                            className="text-button danger-text"
                            type="button"
                            disabled={automationBusy}
                            onClick={() => void deleteAutomationRule(rule)}
                          >
                            Delete
                          </button>
                        </span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="repository-empty">
                    <span>
                      No rules yet. Add one when the team needs a nudge.
                    </span>
                  </div>
                )}
              </section>
              <div className="workspace-stats" aria-label={t("legacy.work-summary")}>
                <div>
                  <strong>{counts.total}</strong>
                  <span>{t("legacy.visible-items")}</span>
                </div>
                <div>
                  <strong>{counts.ready}</strong>
                  <span>{t("legacy.ready-to-move")}</span>
                </div>
                <div>
                  <strong>{counts.active}</strong>
                  <span>{t("legacy.in-motion")}</span>
                </div>
                <span className="updated-at">
                  {lastUpdated
                    ? t("legacy.updated", { time: lastUpdated.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) })
                    : t("legacy.preparing-view")}
                </span>
              </div>
              <form id="work-items" className="create-form" onSubmit={create}>
                <div className="create-main-field">
                  <label htmlFor="work-item-title">
                    <span>{t("legacy.new-work-item")}</span>
                    <input
                      id="work-item-title"
                      value={title}
                      onChange={(event) => setTitle(event.target.value)}
                      placeholder={t("legacy.move-forward")}
                    />
                  </label>
                  <label htmlFor="work-item-description">
                    <span>
                      {t("legacy.context")} <small>({t("legacy.optional")})</small>
                    </span>
                    <textarea
                      id="work-item-description"
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                      placeholder={t("legacy.context-placeholder")}
                      rows={2}
                    />
                  </label>
                </div>
                <div className="create-fields">
                  <label htmlFor="create-type">
                    <span>{t("backlog.type")}</span>
                    <select
                      id="create-type"
                      value={createType}
                      onChange={(event) => setCreateType(event.target.value)}
                    >
                      {types.map((type) => (
                        <option key={type}>{type}</option>
                      ))}
                    </select>
                  </label>
                  <label htmlFor="create-priority">
                    <span>{t("work.priority")}</span>
                    <select
                      id="create-priority"
                      value={createPriority}
                      onChange={(event) =>
                        setCreatePriority(event.target.value)
                      }
                    >
                      {priorities.map((priority) => (
                        <option key={priority} value={priority}>
                          {priorityLabel(priority)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label htmlFor="create-due-date">
                    <span>{t("legacy.deadline")}</span>
                    <input
                      id="create-due-date"
                      type="date"
                      value={createDueDate}
                      onChange={(event) => setCreateDueDate(event.target.value)}
                    />
                  </label>
                  <label htmlFor="create-estimate">
                    <span>{t("legacy.points")}</span>
                    <input
                      id="create-estimate"
                      type="number"
                      min="0"
                      max="100"
                      value={createEstimate}
                      onChange={(event) =>
                        setCreateEstimate(event.target.value)
                      }
                      placeholder="—"
                    />
                  </label>
                  <label htmlFor="create-parent">
                    <span>{t("legacy.parent")}</span>
                    <select
                      id="create-parent"
                      value={createParentID}
                      onChange={(event) =>
                        setCreateParentID(event.target.value)
                      }
                    >
                      <option value="">{t("legacy.no-parent")}</option>
                      {items.map((item) => (
                        <option value={item.id} key={item.id}>
                          {item.key} · {item.title}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label htmlFor="create-assignee">
                    <span>{t("legacy.assignee")}</span>
                    <select
                      id="create-assignee"
                      value={createAssigneeID}
                      onChange={(event) => setCreateAssigneeID(event.target.value)}
                    >
                      <option value="">{t("legacy.unassigned")}</option>
                      {members.map((member) => (
                        <option value={member.id} key={member.id}>
                          {memberName(member)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label htmlFor="create-sprint">
                    <span>{t("legacy.sprint")}</span>
                    <select
                      id="create-sprint"
                      value={createSprintID}
                      onChange={(event) => setCreateSprintID(event.target.value)}
                    >
                      <option value="">{t("legacy.backlog")}</option>
                      {sprints.map((sprint) => (
                        <option value={sprint.id} key={sprint.id}>
                          {sprint.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label htmlFor="create-repository">
                    <span>{t("legacy.repository")}</span>
                    <select
                      id="create-repository"
                      value={createRepositoryID}
                      onChange={(event) =>
                        setCreateRepositoryID(event.target.value)
                      }
                    >
                      <option value="">{t("legacy.no-repository")}</option>
                      {repositories
                        .filter((repository) => repository.linked)
                        .map((repository) => (
                          <option value={repository.id} key={repository.id}>
                            {repository.full_name}
                          </option>
                        ))}
                    </select>
                  </label>
                  <button
                    className="button button-primary create-button"
                    type="submit"
                    disabled={!title.trim() || creating}
                  >
                    {creating ? (
                      <>
                        <span className="button-spinner" aria-hidden="true" />{" "}
                        {t("legacy.creating")}
                      </>
                    ) : (
                      <>
                        {t("legacy.create")} <span aria-hidden="true">↗</span>
                      </>
                    )}
                  </button>
                </div>
              </form>
              <div className="list-toolbar">
                <label className="search-field" htmlFor="work-item-search">
                  <span className="search-icon" aria-hidden="true">
                    ⌕
                  </span>
                  <input
                    id="work-item-search"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder={t("legacy.search-description")}
                  />
                </label>
                {query && (
                  <button
                    className="clear-search"
                    type="button"
                    onClick={() => setQuery("")}
                    aria-label={t("legacy.clear")}
                  >
                    {t("legacy.clear")}
                  </button>
                )}
                <div className="filter-row" aria-label={t("legacy.work-filters")}>
                  <label>
                    <span>{t("work.status")}</span>
                    <select
                      value={filterStatus}
                      onChange={(event) => setFilterStatus(event.target.value)}
                    >
                      <option value="">{t("legacy.all-statuses")}</option>
                      {(workflowStatuses.length > 0
                        ? workflowStatuses.map((status) => status.key)
                        : Object.keys(availableTransitionKeys)
                      ).map((status) => (
                        <option value={status} key={status}>
                          {statusLabel(status)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>{t("backlog.type")}</span>
                    <select
                      value={filterType}
                      onChange={(event) => setFilterType(event.target.value)}
                    >
                      <option value="">{t("legacy.all-types")}</option>
                      {types.map((type) => (
                        <option value={type} key={type}>
                          {type}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>{t("work.priority")}</span>
                    <select
                      value={filterPriority}
                      onChange={(event) =>
                        setFilterPriority(event.target.value)
                      }
                    >
                      <option value="">{t("legacy.all-priorities")}</option>
                      {priorities.map((priority) => (
                        <option value={priority} key={priority}>
                          {priorityLabel(priority)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>{t("legacy.assignee")}</span>
                    <select
                      value={filterAssignee}
                      onChange={(event) =>
                        setFilterAssignee(event.target.value)
                      }
                    >
                      <option value="">{t("legacy.anyone")}</option>
                      {members.map((member) => (
                        <option value={member.id} key={member.id}>
                          {memberName(member)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>{t("legacy.sprint")}</span>
                    <select
                      value={filterSprint}
                      onChange={(event) => setFilterSprint(event.target.value)}
                    >
                      <option value="">{t("legacy.all-sprints")}</option>
                      {sprints.map((sprint) => (
                        <option value={sprint.id} key={sprint.id}>
                          {sprint.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    <span>{t("legacy.repository")}</span>
                    <select
                      value={filterRepository}
                      onChange={(event) =>
                        setFilterRepository(event.target.value)
                      }
                    >
                      <option value="">{t("legacy.all-repositories")}</option>
                      {repositories
                        .filter((repository) => repository.linked)
                        .map((repository) => (
                          <option value={repository.id} key={repository.id}>
                            {repository.full_name}
                          </option>
                        ))}
                    </select>
                  </label>
                  <label className="filter-checkbox">
                    <input
                      type="checkbox"
                      checked={includeArchived}
                      onChange={(event) => setIncludeArchived(event.target.checked)}
                    />
                    <span>{t("legacy.show-archived")}</span>
                  </label>
                  {(filterStatus ||
                    filterType ||
                    filterPriority ||
                    filterAssignee ||
                    filterSprint ||
                    filterRepository ||
                    includeArchived) && (
                    <button
                      className="text-button filter-clear"
                      type="button"
                      onClick={() => {
                        setFilterStatus("");
                        setFilterType("");
                        setFilterPriority("");
                        setFilterAssignee("");
                        setFilterSprint("");
                        setFilterRepository("");
                        setIncludeArchived(false);
                      }}
                    >
                      {t("legacy.clear-filters")}
                    </button>
                  )}
                </div>
              </div>
              {error && (
                <div className="error-panel" role="alert">
                  <span className="error-icon" aria-hidden="true">
                    !
                  </span>
                  <div>
                    <strong>{t("legacy.refresh-failed")}</strong>
                    <p>{error}</p>
                  </div>
                  <button
                    type="button"
                    onClick={() =>
                      void (view === "board" ? loadBoard() : load())
                    }
                  >
                    {t("legacy.try-again")}
                  </button>
                </div>
              )}
              {view === "list" && loading && (
                <div
                  className="workspace-skeleton"
                  role="status"
                  aria-label={t("legacy.loading-work-items")}
                >
                  <span />
                  <span />
                  <span />
                </div>
              )}
              {view === "list" && !loading && !error && items.length === 0 && (
                <div className="empty-state">
                  <span className="empty-icon" aria-hidden="true">
                    ⌁
                  </span>
                  <strong>
                    {query ? t("legacy.no-match") : t("legacy.clean-slate")}
                  </strong>
                  <p>
                    {query
                      ? t("legacy.different-search")
                      : t("legacy.first-work-item")}
                  </p>
                </div>
              )}
              {view === "list" && !loading && !error && items.length > 0 && (
                <div className="work-items" aria-live="polite">
                  {items.map((item) => (
                    <div className="work-item-row" key={item.id}>
                      <button
                        className={`work-item${item.archived_at ? " archived" : ""}`}
                        type="button"
                        onClick={() => openDetail(item)}
                      >
                        <span className="work-item-key">{item.key}</span>
                        <span className="work-item-copy">
                          <strong>{item.title}</strong>
                          <small>
                            {item.type} <span aria-hidden="true">·</span>{" "}
                            {priorityLabel(item.priority)}{" "}
                            <span aria-hidden="true">·</span>{" "}
                            {formatDate(item.due_at)}
                          </small>
                        </span>
                        <span className="work-item-meta">
                          {item.labels?.slice(0, 2).map((label) => (
                            <i
                              key={label.id}
                              className="label-chip"
                              style={
                                {
                                  "--label-color": label.color,
                                } as React.CSSProperties
                              }
                            >
                              {label.name}
                            </i>
                          ))}
                          <span className={`status-pill ${tone(item.status)}`}>
                            <i aria-hidden="true" />
                            {statusLabel(item.status)}
                          </span>
                          {item.archived_at && (
                            <span className="status-pill status-muted">{t("legacy.archived")}</span>
                          )}
                        </span>
                      </button>
                      {!item.archived_at && (
                        <div className="rank-actions" aria-label={t("legacy.reorder", { key: item.key })}>
                          <button
                            className="icon-button"
                            type="button"
                            aria-label={t("legacy.move-up", { key: item.key })}
                            title={t("legacy.move-up", { key: item.key })}
                            disabled={rankBusyID !== ""}
                            onClick={() => void reorderItem(item, "up")}
                          >
                            ↑
                          </button>
                          <button
                            className="icon-button"
                            type="button"
                            aria-label={t("legacy.move-down", { key: item.key })}
                            title={t("legacy.move-down", { key: item.key })}
                            disabled={rankBusyID !== ""}
                            onClick={() => void reorderItem(item, "down")}
                          >
                            ↓
                          </button>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
              {view === "list" && !loading && !error && nextCursor && (
                <div className="load-more-row">
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => void loadMoreItems()}
                    disabled={loadingMore}
                  >
                    {loadingMore ? t("legacy.loading-more") : t("legacy.load-more")}
                  </button>
                </div>
              )}
              {view === "board" && boardLoading && (
                <div
                  className="workspace-skeleton"
                  role="status"
                  aria-label={t("legacy.loading-board")}
                >
                  <span />
                  <span />
                  <span />
                </div>
              )}
              {view === "board" && !boardLoading && !error && (
                <>
                  {boardTruncated && (
                    <div className="field-help" role="status">
                      {t("legacy.board-truncated")}
                    </div>
                  )}
                  <div className="board-grid" aria-live="polite">
                  {visibleBoardColumns.map((column) => (
                    <section
                      className="board-column"
                      key={column.status}
                      aria-labelledby={`board-${column.status}`}
                      onDragOver={(event) => event.preventDefault()}
                      onDrop={(event) => {
                        event.preventDefault();
                        const item = boardColumns
                          .flatMap((boardColumn) => boardColumn.items)
                          .find((candidate) => candidate.id === draggedItemID);
                        if (item) void transitionItem(item, column.status);
                        setDraggedItemID("");
                      }}
                    >
                      <div className="board-column-header">
                        <div>
                          <strong id={`board-${column.status}`}>
                            {column.name}
                          </strong>
                          <small>
                            {column.items.length}{" "}
                            {column.items.length === 1 ? t("legacy.item") : t("legacy.items")}
                          </small>
                        </div>
                        <span className={`status-pill ${tone(column.status)}`}>
                          <i aria-hidden="true" />
                          {statusLabel(column.status)}
                        </span>
                      </div>
                      <div className="board-items">
                        {column.items.map((item) => (
                          <button
                            className="board-item"
                            key={item.id}
                            type="button"
                            draggable
                            aria-label={`${item.key}: ${item.title}. Use left and right arrow keys to move between workflow columns.`}
                            onDragStart={() => setDraggedItemID(item.id)}
                            onDragEnd={() => setDraggedItemID("")}
                            onKeyDown={(event) => {
                              if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
                              const columnIndex = visibleBoardColumns.findIndex((candidate) => candidate.status === column.status);
                              const targetColumn = visibleBoardColumns[columnIndex + (event.key === "ArrowLeft" ? -1 : 1)];
                              if (!targetColumn) return;
                              event.preventDefault();
                              void transitionItem(item, targetColumn.status);
                            }}
                            onClick={() => openDetail(item)}
                          >
                            <span className="work-item-key">{item.key}</span>
                            <strong>{item.title}</strong>
                            <small>
                              {item.type} <span aria-hidden="true">·</span>{" "}
                              {priorityLabel(item.priority)}{" "}
                              <span aria-hidden="true">·</span>{" "}
                              {formatDate(item.due_at)}
                            </small>
                            <span className="board-item-actions">
                              <span
                                className={`status-pill ${tone(item.status)}`}
                              >
                                {statusLabel(item.status)}
                              </span>
                              <span className="drag-hint">{t("legacy.drag-hint")}</span>
                            </span>
                          </button>
                        ))}
                        {column.items.length === 0 && (
                          <p className="board-empty">{t("legacy.drop-here")}</p>
                        )}
                      </div>
                    </section>
                  ))}
                  </div>
                </>
              )}
            </>
          )}
        </>
      )}

      {detail && (
        <aside className="work-item-detail" aria-label="Work item details">
          <div className="detail-header">
            <div>
              <span className="work-item-key">{detail.item.key}</span>
              <span className={`status-pill ${tone(detail.item.status)}`}>
                <i aria-hidden="true" />
                {statusLabel(detail.item.status)}
              </span>
            </div>
            <div className="detail-header-actions">
              <button
                type="button"
                className={`text-button${detail.item.archived_at ? "" : " danger-text"}`}
                onClick={() => void archiveOrRestoreWorkItem()}
                disabled={archiveBusy}
              >
                {archiveBusy
                  ? detail.item.archived_at
                    ? "Restoring…"
                    : "Archiving…"
                  : detail.item.archived_at
                    ? "Restore"
                    : "Archive"}
              </button>
              <button
                type="button"
                className="detail-close"
                onClick={() => setDetail(null)}
                aria-label="Close work item details"
              >
                ×
              </button>
            </div>
          </div>
          {detailLoading && (
            <div className="detail-loading" role="status">
              Loading the full work item…
            </div>
          )}
          {detailError && (
            <div className="setup-error" role="alert">
              {detailError}
            </div>
          )}
          <form className="detail-form" onSubmit={saveWorkItem}>
            <label htmlFor="detail-title">
              Title
              <input
                id="detail-title"
                value={editTitle}
                onChange={(event) => setEditTitle(event.target.value)}
              />
            </label>
            <div className="detail-grid">
              <label htmlFor="detail-priority">
                Priority
                <select
                  id="detail-priority"
                  value={editPriority}
                  onChange={(event) => setEditPriority(event.target.value)}
                >
                  {priorities.map((priority) => (
                    <option key={priority} value={priority}>
                      {priorityLabel(priority)}
                    </option>
                  ))}
                </select>
              </label>
              <label htmlFor="detail-parent">
                Parent work item
                <select
                  id="detail-parent"
                  value={editParent}
                  onChange={(event) => setEditParent(event.target.value)}
                >
                  <option value="">No parent</option>
                  {items
                    .filter((item) => item.id !== detail.item.id)
                    .map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.key} · {item.title}
                      </option>
                    ))}
                </select>
              </label>
              <label htmlFor="detail-repository">
                Repository
                <select
                  id="detail-repository"
                  value={editRepository}
                  onChange={(event) => setEditRepository(event.target.value)}
                >
                  <option value="">No repository</option>
                  {repositories
                    .filter((repository) => repository.linked)
                    .map((repository) => (
                      <option key={repository.id} value={repository.id}>
                        {repository.full_name}
                      </option>
                    ))}
                </select>
              </label>
              <label htmlFor="detail-due-date">
                Deadline
                <input
                  id="detail-due-date"
                  type="date"
                  value={editDueDate}
                  onChange={(event) => setEditDueDate(event.target.value)}
                />
              </label>
              <label htmlFor="detail-estimate">
                Points
                <input
                  id="detail-estimate"
                  type="number"
                  min="0"
                  max="100"
                  value={editEstimate}
                  onChange={(event) => setEditEstimate(event.target.value)}
                  placeholder="—"
                />
              </label>
              <label htmlFor="detail-assignee">
                Assignee
                <select
                  id="detail-assignee"
                  value={editAssignee}
                  onChange={(event) => setEditAssignee(event.target.value)}
                >
                  <option value="">Unassigned</option>
                  {members.map((member) => (
                    <option key={member.id} value={member.id}>
                      {memberName(member)}
                    </option>
                  ))}
                </select>
              </label>
              <label htmlFor="detail-sprint">
                Sprint
                <select
                  id="detail-sprint"
                  value={editSprint}
                  onChange={(event) => setEditSprint(event.target.value)}
                >
                  <option value="">Backlog</option>
                  {sprints.map((sprint) => (
                    <option key={sprint.id} value={sprint.id}>
                      {sprint.name} · {sprint.status.toLowerCase()}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label htmlFor="detail-description">
              Description
              <textarea
                id="detail-description"
                value={editDescription}
                onChange={(event) => setEditDescription(event.target.value)}
                rows={4}
                placeholder="Describe the outcome or context"
              />
            </label>
            <button
              className="button button-primary"
              type="submit"
              disabled={savingItem}
            >
              {savingItem ? "Saving…" : "Save changes"}
            </button>
          </form>
          <section className="detail-section attachment-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Evidence</p>
                <strong>Attachments</strong>
              </div>
              <label className="button button-secondary attachment-upload">
                {attachmentBusy ? "Uploading…" : "Add file"}
                <input
                  type="file"
                  onChange={(event) => void uploadAttachment(event)}
                  disabled={attachmentBusy}
                  hidden
                />
              </label>
            </div>
            <p className="field-help">Files are private to this project and limited to 10 MiB each.</p>
            {attachmentError && <p className="setup-error" role="alert">{attachmentError}</p>}
            {attachmentLoading ? (
              <div className="repository-empty" role="status">Loading attachments…</div>
            ) : detail.attachments.length === 0 ? (
              <span className="field-help">Screenshots, logs, and other evidence can live here.</span>
            ) : (
              <div className="attachment-list">
                {detail.attachments.map((attachment) => (
                  <div className="attachment-row" key={attachment.id}>
                    <button className="text-button attachment-name" type="button" onClick={() => void downloadAttachment(attachment)}>
                      {attachment.name}
                    </button>
                    <small>{formatBytes(attachment.size_bytes)} · {formatDate(attachment.created_at)}</small>
                    <button className="text-button danger-text" type="button" disabled={attachmentBusy} onClick={() => void deleteAttachment(attachment)}>Remove</button>
                  </div>
                ))}
              </div>
            )}
          </section>
          {customFields.length > 0 && (
            <section className="detail-section custom-value-section">
              <div className="detail-section-heading">
                <div>
                  <p className="workspace-kicker">Project fields</p>
                  <strong>Custom values</strong>
                </div>
              </div>
              <div className="custom-value-grid">
                {customFields.map((field) => {
                  const value = customFieldValues.find(
                    (item) => item.definition_id === field.id,
                  )?.value ?? "";
                  if (field.value_type === "SELECT") {
                    return (
                      <label key={`${field.id}-${value}`} htmlFor={`custom-value-${field.id}`}>
                        {field.display_name}
                        <select
                          id={`custom-value-${field.id}`}
                          defaultValue={value}
                          disabled={customFieldBusy}
                          onChange={(event) =>
                            void saveCustomFieldValue(field, event.target.value)
                          }
                        >
                          <option value="">Not set</option>
                          {(field.options ?? []).map((option) => (
                            <option key={option} value={option}>
                              {option}
                            </option>
                          ))}
                        </select>
                      </label>
                    );
                  }
                  if (field.value_type === "BOOLEAN") {
                    return (
                      <label key={`${field.id}-${value}`} htmlFor={`custom-value-${field.id}`}>
                        {field.display_name}
                        <select
                          id={`custom-value-${field.id}`}
                          defaultValue={value}
                          disabled={customFieldBusy}
                          onChange={(event) =>
                            void saveCustomFieldValue(field, event.target.value)
                          }
                        >
                          <option value="">Not set</option>
                          <option value="true">True</option>
                          <option value="false">False</option>
                        </select>
                      </label>
                    );
                  }
                  return (
                    <label key={`${field.id}-${value}`} htmlFor={`custom-value-${field.id}`}>
                      {field.display_name}
                      <input
                        id={`custom-value-${field.id}`}
                        type={
                          field.value_type === "NUMBER"
                            ? "number"
                            : field.value_type === "DATE"
                              ? "date"
                              : "text"
                        }
                        defaultValue={value}
                        disabled={customFieldBusy}
                        onBlur={(event) =>
                          void saveCustomFieldValue(field, event.currentTarget.value)
                        }
                      />
                    </label>
                  );
                })}
              </div>
              <p className="field-help">Text, number, and date fields save when you leave the input.</p>
            </section>
          )}
          <section className="detail-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Workflow</p>
                <strong>Move this work forward</strong>
              </div>
            </div>
            <div className="transition-actions">
              {Object.entries(availableTransitionKeys[detail.item.status] ?? {}).map(
                ([target, key]) => (
                  <button
                    key={key}
                    type="button"
                    className="button button-secondary"
                    onClick={() => void transitionItem(detail.item, target)}
                  >
                    {statusLabel(target)}
                  </button>
                ),
              )}
              {Object.keys(availableTransitionKeys[detail.item.status] ?? {}).length ===
                0 && (
                <span className="field-help">
                  No further transition is available from this status.
                </span>
              )}
            </div>
          </section>
          <section className="detail-section agent-run-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Agent run</p>
                <strong>Plan and execute with approval</strong>
              </div>
              <span className="field-help">
                {agentRuns.length} {agentRuns.length === 1 ? "run" : "runs"}
              </span>
            </div>
            <p className="readiness-help">
              Code-changing runs require a Ready work item and explicit human
              approval before they can start.
            </p>
            {agentRunError && (
              <p className="setup-error" role="alert">
                {agentRunError}
              </p>
            )}
            {agentRunsLoading ? (
              <div className="repository-empty" role="status">
                Loading agent runs…
              </div>
            ) : (
              <div className="agent-run-list">
                {agentRuns.map((run) => (
                  <article className="agent-run-row" key={run.id}>
                    <div>
                      <strong>{run.agent_name}</strong>
                      <small>
                        {run.agent_provider} · {run.status.toLowerCase()} ·{" "}
                        {formatDate(run.created_at)}
                      </small>
                      {(run.commit_sha || run.pull_request_id || run.error) && (
                        <small>
                          {run.commit_sha ? `commit ${run.commit_sha.slice(0, 8)}` : ""}
                          {run.pull_request_id ? " · PR linked" : ""}
                          {run.error ? " · failed evidence" : ""}
                        </small>
                      )}
                    </div>
                    <div className="agent-run-actions">
                      <span className={`status-pill ${tone(run.status)}`}>
                        <i aria-hidden="true" />
                        {run.status}
                      </span>
                      <button
                        className="text-button"
                        type="button"
                        disabled={agentRunDetailLoading}
                        onClick={() => void inspectAgentRun(run)}
                      >
                        Inspect
                      </button>
                      {run.status === "QUEUED" && !run.approved && (
                        <button
                          className="text-button"
                          type="button"
                          disabled={agentRunBusy}
                          onClick={() => void updateAgentRun(run, "approve")}
                        >
                          Approve
                        </button>
                      )}
                      {run.status === "QUEUED" && run.approved && (
                        <button
                          className="text-button"
                          type="button"
                          disabled={agentRunBusy}
                          onClick={() => void updateAgentRun(run, "start")}
                        >
                          Start
                        </button>
                      )}
                      {agentRunNextStatus[run.status] && (
                        <button
                          className="text-button"
                          type="button"
                          disabled={agentRunBusy}
                          onClick={() =>
                            void updateAgentRun(
                              run,
                              "transition",
                              agentRunNextStatus[run.status],
                            )
                          }
                        >
                          Advance to {agentRunNextStatus[run.status]}
                        </button>
                      )}
                      {!["COMPLETED", "FAILED", "CANCELLED"].includes(
                        run.status,
                      ) && (
                        <button
                          className="text-button danger-text"
                          type="button"
                          disabled={agentRunBusy}
                          onClick={() => void updateAgentRun(run, "cancel")}
                        >
                          Cancel
                        </button>
                      )}
                    </div>
                  </article>
                ))}
                {agentRuns.length === 0 && (
                  <p className="field-help">No agent run has been created.</p>
                )}
              </div>
            )}
            {selectedAgentRunID && agentRunDetails[selectedAgentRunID] && (
              <div className="agent-run-detail">
                <div className="spec-subheading">
                  <strong>Run evidence</strong>
                  <button
                    className="text-button"
                    type="button"
                    onClick={() => setSelectedAgentRunID("")}
                  >
                    Close
                  </button>
                </div>
                {selectedAgentRun && detail?.specification?.regression_test_cases?.length ? (
                  <div className="agent-test-checklist" aria-label="Test case results">
                    <div className="spec-subheading">
                      <strong>Test checklist</strong>
                      <span className="field-help">Passed cases stay checked for the next run.</span>
                    </div>
                    {detail.specification.regression_test_cases.map((testCase) => {
                      const result = testResultsForRun(selectedAgentRun).find((item) => item.position === testCase.position);
                      const status = result?.status ?? "NOT_RUN";
                      const draftKey = `${selectedAgentRun.id}:${testCase.position}`;
                      const note = agentTestCaseNotes[draftKey] ?? result?.note ?? "";
                      return (
                        <div className="agent-test-case" key={`${selectedAgentRun.id}-${testCase.position}`}>
                          <label className="agent-test-case-heading">
                            <input
                              type="checkbox"
                              checked={status === "PASS"}
                              disabled={agentTestResultBusy}
                              onChange={(event) =>
                                void recordAgentTestResults(selectedAgentRun, [{
                                  position: testCase.position,
                                  status: event.target.checked ? "PASS" : "NOT_RUN",
                                  note,
                                }])
                              }
                            />
                            <span>Case {testCase.position}: {testCase.scenario}</span>
                            <strong>{status}</strong>
                          </label>
                          <small>Expected: {testCase.expected_result}</small>
                          <textarea
                            value={note}
                            onChange={(event) => setAgentTestCaseNotes((current) => ({ ...current, [draftKey]: event.target.value }))}
                            placeholder="What passed, failed, or blocked? Add evidence or a reproduction note."
                            rows={2}
                            maxLength={4000}
                          />
                          <div className="comment-actions">
                            <button
                              className="text-button danger-text"
                              type="button"
                              disabled={agentTestResultBusy || !note.trim()}
                              onClick={() => void recordAgentTestResults(selectedAgentRun, [{ position: testCase.position, status: "FAIL", note }])}
                            >
                              Mark failed
                            </button>
                            <button
                              className="text-button"
                              type="button"
                              disabled={agentTestResultBusy || !note.trim()}
                              onClick={() => void recordAgentTestResults(selectedAgentRun, [{ position: testCase.position, status: "BLOCKED", note }])}
                            >
                              Mark blocked
                            </button>
                          </div>
                          {result?.evidence_refs?.length ? <small>Evidence: {result.evidence_refs.join(" · ")}</small> : null}
                        </div>
                      );
                    })}
                    <label className="dialog-field">
                      Review note
                      <textarea
                        value={agentTestReviewNote}
                        onChange={(event) => setAgentTestReviewNote(event.target.value)}
                        placeholder="Overall review note or reason for requesting changes"
                        rows={2}
                        maxLength={4000}
                      />
                    </label>
                    <div className="comment-actions">
                      <button
                        className="button button-secondary"
                        type="button"
                        disabled={agentTestResultBusy || !agentTestReviewNote.trim()}
                        onClick={() => void recordAgentTestResults(selectedAgentRun, [], agentTestReviewNote)}
                      >
                        Save review note
                      </button>
                      {detail.item.status === "IN_PROGRESS" && (
                        <button className="button button-secondary" type="button" onClick={() => prepareFollowUpRun(selectedAgentRun)}>
                          Prepare follow-up for unresolved cases
                        </button>
                      )}
                    </div>
                  </div>
                ) : null}
                {agentRunDetails[selectedAgentRunID].steps.map((step) => (
                  <div className="agent-run-step" key={step.id}>
                    <strong>
                      #{step.sequence} {step.phase}
                    </strong>
                    <small>
                      {step.status} · {step.files_read} read · {step.files_modified} modified
                    </small>
                    <span>{step.summary || "No summary"}</span>
                  </div>
                ))}
                {agentRunDetails[selectedAgentRunID].artifacts.map(
                  (artifact) => (
                    <div className="agent-run-artifact" key={artifact.id}>
                      <strong>{artifact.name}</strong>
                      <small>
                        {artifact.artifact_type} · {artifact.size_bytes} bytes · {formatDate(artifact.created_at)}
                      </small>
                    </div>
                  ),
                )}
                {agentRunDetails[selectedAgentRunID].steps.length === 0 &&
                  agentRunDetails[selectedAgentRunID].artifacts.length === 0 && (
                    <span className="field-help">No evidence attached yet.</span>
                  )}
              </div>
            )}
            <div className="agent-run-create">
              <label htmlFor="agent-provider">
                Provider
                <select
                  id="agent-provider"
                  value={agentProvider}
                  onChange={(event) =>
                    setAgentProvider(event.target.value as "codex" | "claude")
                  }
                >
                  <option value="codex">Codex Desktop</option>
                  <option value="claude">Claude Desktop</option>
                </select>
              </label>
              <label htmlFor="agent-prompt">
                Approved task prompt
                <textarea
                  id="agent-prompt"
                  value={agentPrompt}
                  onChange={(event) => setAgentPrompt(event.target.value)}
                  maxLength={131072}
                  rows={4}
                  placeholder="Describe the exact change the agent is approved to make."
                />
              </label>
              <button
                className="button button-secondary"
                type="button"
                disabled={
                  agentRunBusy ||
                  !["READY", "IN_PROGRESS"].includes(detail.item.status) ||
                  !(detail.item.repository_id ?? detail.specification?.repository_id)
                }
                onClick={() => void createAgentRun()}
              >
                {!['READY', 'IN_PROGRESS'].includes(detail.item.status)
                  ? "Move to Ready or In progress first"
                  : !(detail.item.repository_id ?? detail.specification?.repository_id)
                    ? "Link a repository first"
                    : followUpTestCasePositions
                      ? `Create follow-up (${followUpTestCasePositions.length} cases)`
                      : "Create approved run"}
              </button>
            </div>
          </section>
          <section className="detail-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Labels</p>
                <strong>Make it easy to scan</strong>
              </div>
            </div>
            <div className="detail-labels">
              {detail.labels.map((label) => (
                <button
                  type="button"
                  key={label.id}
                  className="label-chip"
                  style={
                    { "--label-color": label.color } as React.CSSProperties
                  }
                  onClick={() => void removeLabel(label.id)}
                  title="Remove label"
                >
                  {label.name} ×
                </button>
              ))}
              {detail.labels.length === 0 && (
                <span className="field-help">No labels yet.</span>
              )}
            </div>
            <form className="inline-form" onSubmit={addLabel}>
              <input
                aria-label="Label name"
                value={labelName}
                onChange={(event) => setLabelName(event.target.value)}
                placeholder="Add a label"
              />
              <button
                className="button button-secondary"
                type="submit"
                disabled={labelBusy || !labelName.trim()}
              >
                Add
              </button>
            </form>
          </section>
          <section className="detail-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Discussion</p>
                <strong>
                  {detail.comments.length}{" "}
                  {detail.comments.length === 1 ? "comment" : "comments"}
                </strong>
              </div>
            </div>
            <div className="comments-list">
              {detail.comments.map((comment) => (
                <article className="comment" key={comment.id}>
                  <small>
                    {comment.author_id} · {formatDate(comment.created_at)}
                    {comment.updated_at && comment.updated_at !== comment.created_at ? " · edited" : ""}
                  </small>
                  {editingCommentID === comment.id ? (
                    <form className="comment-edit-form" onSubmit={updateComment}>
                      <textarea
                        value={editingCommentBody}
                        onChange={(event) => setEditingCommentBody(event.target.value)}
                        rows={3}
                        maxLength={20000}
                        autoFocus
                      />
                      <div className="comment-actions">
                        <button className="button button-secondary" type="submit" disabled={commentBusy || !editingCommentBody.trim()}>Save</button>
                        <button className="button button-ghost" type="button" onClick={cancelEditingComment} disabled={commentBusy}>Cancel</button>
                      </div>
                    </form>
                  ) : (
                    <p className={comment.deleted_at ? "comment-deleted" : undefined}>{comment.body}</p>
                  )}
                  {!comment.deleted_at && comment.author_id === currentActorID && editingCommentID !== comment.id && (
                    <div className="comment-actions">
                      <button className="button button-ghost" type="button" onClick={() => startEditingComment(comment)}>Edit</button>
                      <button className="button button-ghost danger-text" type="button" onClick={() => void deleteComment(comment)} disabled={commentBusy}>Delete</button>
                    </div>
                  )}
                </article>
              ))}
              {detail.comments.length === 0 && (
                <span className="field-help">
                  Start a focused discussion about the work.
                </span>
              )}
            </div>
            <form className="comment-form" onSubmit={addComment}>
              <textarea
                value={commentBody}
                onChange={(event) => setCommentBody(event.target.value)}
                placeholder="Leave a useful note…"
                rows={3}
              />
              <button
                className="button button-secondary"
                type="submit"
                disabled={commentBusy || !commentBody.trim()}
              >
                Comment
              </button>
            </form>
          </section>
          <section className="detail-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Relationships</p>
                <strong>{detail.links.length} linked work items</strong>
              </div>
            </div>
            <div className="relationship-list">
              {detail.links.map((link) => {
                const target = items.find((item) => item.id === link.target_id);
                return (
                  <div className="relationship-row" key={link.id}>
                    <span>{link.relation_type.replaceAll("_", " ")}</span>
                    <strong>{target?.key ?? link.target_id}</strong>
                    <small>{target?.title ?? "Linked work item"}</small>
                    <button
                      className="text-button danger-text"
                      type="button"
                      disabled={linkBusy}
                      onClick={() => void removeLink(link.id)}
                    >
                      Remove
                    </button>
                  </div>
                );
              })}
              {detail.links.length === 0 && (
                <span className="field-help">
                  Link blockers, related work, or duplicates to keep context
                  close.
                </span>
              )}
            </div>
            <form className="inline-form relationship-form" onSubmit={addLink}>
              <select
                aria-label="Link target"
                value={linkTargetID}
                onChange={(event) => setLinkTargetID(event.target.value)}
              >
                <option value="">Choose work item</option>
                {items
                  .filter((item) => item.id !== detail.item.id)
                  .map((item) => (
                    <option value={item.id} key={item.id}>
                      {item.key} · {item.title}
                    </option>
                  ))}
              </select>
              <select
                aria-label="Relationship type"
                value={linkRelation}
                onChange={(event) => setLinkRelation(event.target.value)}
              >
                <option value="relates_to">Relates to</option>
                <option value="blocks">Blocks</option>
                <option value="blocked_by">Blocked by</option>
                <option value="duplicates">Duplicates</option>
              </select>
              <button
                className="button button-secondary"
                type="submit"
                disabled={linkBusy || !linkTargetID}
              >
                {linkBusy ? "Linking…" : "Add link"}
              </button>
            </form>
          </section>
          <section className="detail-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Activity</p>
                <strong>Audit trail</strong>
              </div>
              <span className="field-help">Append-only</span>
            </div>
            <div className="activity-list">
              {detail.audit.map((record) => (
                <div className="activity-row" key={record.id}>
                  <span className="activity-dot" aria-hidden="true" />
                  <div>
                    <strong>{record.action.replaceAll(".", " ")}</strong>
                    <small>
                      {record.actor_type} · {record.actor_id} ·{" "}
                      {formatDate(record.created_at)}
                    </small>
                  </div>
                </div>
              ))}
              {detail.audit.length === 0 && (
                <span className="field-help">
                  Activity will appear here after the first mutation.
                </span>
              )}
            </div>
          </section>
          <section className="detail-section specification-section">
            <div className="detail-section-heading">
              <div>
                <p className="workspace-kicker">Definition</p>
                <strong>
                  {detail.item.type === "BUG"
                    ? "Bug specification"
                    : "Definition of done"}
                </strong>
              </div>
              <span
                className={`readiness-chip ${detail.readiness.ready ? "is-ready" : ""}`}
              >
                {detail.readiness.ready
                  ? "Ready"
                  : `${detail.readiness.missing?.length ?? 0} gaps`}
              </span>
            </div>
            {!detail.readiness.ready && detail.readiness.missing?.length ? (
              <p className="readiness-help">
                Complete and human-verify the required fields before marking
                this item Ready.
              </p>
            ) : null}
            {detail.readiness.quality && (
              <div className="quality-dimensions" aria-label="Specification quality guidance">
                {([
                  ["Completeness", detail.readiness.quality.completeness],
                  ["Clarity", detail.readiness.quality.clarity],
                  ["Reproducibility", detail.readiness.quality.reproducibility],
                  ["Evidence", detail.readiness.quality.evidence_quality],
                  ["Testability", detail.readiness.quality.testability],
                  ["Repository context", detail.readiness.quality.repository_context],
                  ["Human verification", detail.readiness.quality.human_verification_coverage],
                ] as const).map(([label, value]) => (
                  <div className="quality-dimension" key={label}>
                    <div><span>{label}</span><strong>{Math.round(value * 100)}%</strong></div>
                    <span className="quality-track"><span style={{ width: `${Math.round(value * 100)}%` }} /></span>
                  </div>
                ))}
                <small>Guidance only — server readiness rules remain authoritative.</small>
              </div>
            )}
            {specDirty && (
              <p className="readiness-help" role="status">
                Save the definition before marking fields as human verified.
              </p>
            )}
            {detail.specificationVersions.length > 0 && (
              <details className="specification-history">
                <summary>Specification history ({detail.specificationVersions.length} entries)</summary>
                <div className="specification-history-list">
                  {detail.specificationVersions.map((version) => (
                    <div className="specification-history-row" key={version.id}>
                      <span>r{version.revision} · {version.field}</span>
                      <small>{version.provenance} · {formatDate(version.created_at)}</small>
                      <p>{version.value || "(empty)"}</p>
                    </div>
                  ))}
                </div>
              </details>
            )}
            {detail.proposals.length > 0 && (
              <div className="proposal-review" aria-label="AI proposals">
                <div className="spec-subheading">
                  <strong>AI proposals</strong>
                  <span className="field-help">
                    Accepting keeps the content unverified.
                  </span>
                </div>
                {detail.proposals.map((proposal) => (
                  <article className="proposal-row" key={proposal.id}>
                    <div>
                      <strong>{proposal.field}</strong>
                      <p>{proposal.value}</p>
                      <small>
                        {proposal.provenance} · {proposal.status.toLowerCase()}
                      </small>
                    </div>
                    {proposal.status === "PENDING" && (
                      <span className="proposal-actions">
                        <button
                          className="text-button"
                          type="button"
                          onClick={() => void acceptProposal(proposal)}
                        >
                          Accept unverified
                        </button>
                        <button
                          className="text-button danger-text"
                          type="button"
                          onClick={() => void rejectProposal(proposal)}
                        >
                          Reject
                        </button>
                      </span>
                    )}
                  </article>
                ))}
              </div>
            )}
            {detail.analyses.length > 0 && (
              <div className="analysis-review" aria-label="AI analyses">
                <div className="spec-subheading">
                  <strong>AI analysis hypotheses</strong>
                  <span className="field-help">Untrusted · evidence required</span>
                </div>
                {detail.analyses.map((analysis) => (
                  <article className="analysis-row" key={analysis.id}>
                    <strong>{analysis.root_cause_hypothesis}</strong>
                    <p>{analysis.implementation_plan}</p>
                    <small>
                      confidence {Math.round(analysis.confidence * 100)}% · {formatDate(analysis.created_at)}
                    </small>
                    <small>Test plan: {analysis.test_plan}</small>
                    {analysis.evidence_refs.length > 0 && (
                      <small>Evidence: {analysis.evidence_refs.join(" · ")}</small>
                    )}
                  </article>
                ))}
              </div>
            )}
            <form className="analysis-form" onSubmit={addAnalysis}>
              <div className="spec-subheading">
                <strong>Record an analysis hypothesis</strong>
                <span className="field-help">Never treated as fact</span>
              </div>
              <textarea
                aria-label="Root cause hypothesis"
                value={analysisHypothesis}
                onChange={(event) => setAnalysisHypothesis(event.target.value)}
                placeholder="What might be causing this?"
                rows={2}
              />
              <textarea
                aria-label="Blast radius"
                value={analysisBlastRadius}
                onChange={(event) => setAnalysisBlastRadius(event.target.value)}
                placeholder="Likely blast radius (optional)"
                rows={2}
              />
              <textarea
                aria-label="Implementation plan"
                value={analysisImplementationPlan}
                onChange={(event) => setAnalysisImplementationPlan(event.target.value)}
                placeholder="Implementation plan"
                rows={2}
              />
              <textarea
                aria-label="Analysis test plan"
                value={analysisTestPlan}
                onChange={(event) => setAnalysisTestPlan(event.target.value)}
                placeholder="How will we test the hypothesis?"
                rows={2}
              />
              <textarea
                aria-label="Analysis evidence references"
                value={analysisEvidenceRefs}
                onChange={(event) => setAnalysisEvidenceRefs(event.target.value)}
                placeholder="Evidence references, one per line (file, commit, PR, test)"
                rows={2}
                maxLength={12000}
              />
              <label>
                Confidence (0–1)
                <input
                  type="number"
                  min="0"
                  max="1"
                  step="0.05"
                  value={analysisConfidence}
                  onChange={(event) => setAnalysisConfidence(event.target.value)}
                />
              </label>
              <button
                className="button button-secondary"
                type="submit"
                disabled={analysisBusy || !analysisHypothesis.trim() || !analysisImplementationPlan.trim() || !analysisTestPlan.trim()}
              >
                {analysisBusy ? "Saving…" : "Save hypothesis"}
              </button>
            </form>
            <form className="spec-form" onSubmit={saveSpecification}>
              <label htmlFor="spec-summary">
                Summary
                <input
                  id="spec-summary"
                  value={specSummary}
                  onChange={(event) => {
                    setSpecDirty(true);
                    setSpecSummary(event.target.value);
                  }}
                  placeholder="What is this work about?"
                />
              </label>
              <div className="spec-field">
                <div className="spec-field-heading">
                  <label htmlFor="spec-goal">
                    {detail.item.type === "BUG"
                      ? "Problem statement"
                      : "Goal / problem statement"}
                  </label>
                  {detail.item.type === "BUG" &&
                    (isFieldVerified("PROBLEM_STATEMENT") ? (
                      <span className="verified-mark">Human verified</span>
                    ) : (
                      <button
                        className="text-button"
                        type="button"
                        disabled={
                          specDirty || !specFields.PROBLEM_STATEMENT?.trim()
                        }
                        title={
                          specDirty
                            ? "Save the definition before verifying"
                            : "Verify this field"
                        }
                        onClick={() =>
                          void verifySpecification("field", "PROBLEM_STATEMENT")
                        }
                      >
                        Verify
                      </button>
                    ))}
                </div>
                <textarea
                  id="spec-goal"
                  value={
                    specFields[
                      detail.item.type === "BUG" ? "PROBLEM_STATEMENT" : "GOAL"
                    ] ?? ""
                  }
                  onChange={(event) => (
                    setSpecDirty(true),
                    setSpecFields((current) => ({
                      ...current,
                      [detail.item.type === "BUG"
                        ? "PROBLEM_STATEMENT"
                        : "GOAL"]: event.target.value,
                    }))
                  )}
                  rows={3}
                />
              </div>
              {detail.item.type === "BUG" && (
                <>
                  <div className="spec-field">
                    <div className="spec-field-heading">
                      <label htmlFor="spec-expected">Expected behavior</label>
                      {isFieldVerified("EXPECTED_BEHAVIOR") ? (
                        <span className="verified-mark">Human verified</span>
                      ) : (
                        <button
                          className="text-button"
                          type="button"
                          disabled={
                            specDirty || !specFields.EXPECTED_BEHAVIOR?.trim()
                          }
                          title={
                            specDirty
                              ? "Save the definition before verifying"
                              : "Verify this field"
                          }
                          onClick={() =>
                            void verifySpecification(
                              "field",
                              "EXPECTED_BEHAVIOR",
                            )
                          }
                        >
                          Verify
                        </button>
                      )}
                    </div>
                    <textarea
                      id="spec-expected"
                      value={specFields.EXPECTED_BEHAVIOR ?? ""}
                      onChange={(event) => (
                        setSpecDirty(true),
                        setSpecFields((current) => ({
                          ...current,
                          EXPECTED_BEHAVIOR: event.target.value,
                        }))
                      )}
                      rows={3}
                    />
                  </div>
                  <div className="spec-field">
                    <div className="spec-field-heading">
                      <label htmlFor="spec-actual">Actual behavior</label>
                      {isFieldVerified("ACTUAL_BEHAVIOR") ? (
                        <span className="verified-mark">Human verified</span>
                      ) : (
                        <button
                          className="text-button"
                          type="button"
                          disabled={
                            specDirty || !specFields.ACTUAL_BEHAVIOR?.trim()
                          }
                          title={
                            specDirty
                              ? "Save the definition before verifying"
                              : "Verify this field"
                          }
                          onClick={() =>
                            void verifySpecification("field", "ACTUAL_BEHAVIOR")
                          }
                        >
                          Verify
                        </button>
                      )}
                    </div>
                    <textarea
                      id="spec-actual"
                      value={specFields.ACTUAL_BEHAVIOR ?? ""}
                      onChange={(event) => (
                        setSpecDirty(true),
                        setSpecFields((current) => ({
                          ...current,
                          ACTUAL_BEHAVIOR: event.target.value,
                        }))
                      )}
                      rows={3}
                    />
                  </div>
                  <div className="bug-context-fields">
                    {bugContextFields.map(([key, label]) => (
                      <div className="spec-field" key={key}>
                        <div className="spec-field-heading">
                          <label htmlFor={`spec-${key.toLowerCase()}`}>
                            {label}
                          </label>
                          {isFieldVerified(key) ? (
                            <span className="verified-mark">
                              Human verified
                            </span>
                          ) : (
                            <button
                              className="text-button"
                              type="button"
                              disabled={specDirty || !specFields[key]?.trim()}
                              title={
                                specFields[key]?.trim()
                                  ? "Verify this field"
                                  : "Save content before verifying"
                              }
                              onClick={() =>
                                void verifySpecification("field", key)
                              }
                            >
                              Verify
                            </button>
                          )}
                        </div>
                        <textarea
                          id={`spec-${key.toLowerCase()}`}
                          value={specFields[key] ?? ""}
                          onChange={(event) => (
                            setSpecDirty(true),
                            setSpecFields((current) => ({
                              ...current,
                              [key]: event.target.value,
                            }))
                          )}
                          rows={2}
                        />
                      </div>
                    ))}
                  </div>
                </>
              )}
              {detail.item.type !== "BUG" && (
                <div className="spec-field">
                  <div className="spec-field-heading">
                    <label htmlFor="spec-no-code">
                      No-code-change rationale{" "}
                      <small>(optional when a repository is linked)</small>
                    </label>
                  </div>
                  <textarea
                    id="spec-no-code"
                    value={specFields.NO_CODE_CHANGE_RATIONALE ?? ""}
                    onChange={(event) => (
                      setSpecDirty(true),
                      setSpecFields((current) => ({
                        ...current,
                        NO_CODE_CHANGE_RATIONALE: event.target.value,
                      }))
                    )}
                    rows={2}
                    placeholder="Explain why this work does not change code."
                  />
                </div>
              )}
              <div className="spec-field">
                <div className="spec-field-heading">
                  <label htmlFor="spec-repository">Repository context</label>
                  <span className="field-help">
                    Required before Ready unless no-code rationale is provided.
                  </span>
                </div>
                <select
                  id="spec-repository"
                  value={specRepositoryID}
                  onChange={(event) => {
                    setSpecDirty(true);
                    setSpecRepositoryID(event.target.value);
                  }}
                >
                  <option value="">Choose a linked repository</option>
                  {repositories
                    .filter((repository) => repository.linked)
                    .map((repository) => (
                      <option key={repository.id} value={repository.id}>
                        {repository.full_name}
                      </option>
                    ))}
                </select>
              </div>
              <div className="spec-subheading">
                <strong>Reproduction steps</strong>
                <button
                  className="text-button"
                  type="button"
                  onClick={() => (
                    setSpecDirty(true),
                    setReproductionSteps((current) => [
                      ...current,
                      {
                        position: current.length + 1,
                        action: "",
                        expected_result: "",
                        observed_result: "",
                      },
                    ])
                  )}
                >
                  + Add step
                </button>
              </div>
              {reproductionSteps.map((step, index) => (
                <div className="repro-step" key={`${step.position}-${index}`}>
                  <div className="repro-step-top">
                    <span>Step {index + 1}</span>
                    <span className="inline-actions">
                      {step.verification_status === "HUMAN_VERIFIED" ? (
                        <span className="verified-mark">Human verified</span>
                      ) : (
                        detail.item.type === "BUG" && (
                          <button
                            className="text-button"
                            type="button"
                            disabled={
                              specDirty ||
                              !step.action.trim() ||
                              !step.expected_result.trim() ||
                              !step.observed_result.trim()
                            }
                            title={
                              specDirty
                                ? "Save the definition before verifying"
                                : "Complete all step fields before verifying"
                            }
                            onClick={() =>
                              void verifySpecification(
                                "reproduction_step",
                                undefined,
                                index + 1,
                              )
                            }
                          >
                            Verify step
                          </button>
                        )
                      )}
                      <button
                        className="text-button danger-text"
                        type="button"
                        onClick={() => {
                          setSpecDirty(true);
                          setReproductionSteps((current) => current.filter((_, itemIndex) => itemIndex !== index));
                        }}
                      >
                        Remove
                      </button>
                    </span>
                  </div>
                  <input
                    aria-label={`Step ${index + 1} action`}
                    value={step.action}
                    onChange={(event) =>
                      updateStep(index, "action", event.target.value)
                    }
                    placeholder="Action"
                  />
                  <input
                    aria-label={`Step ${index + 1} expected result`}
                    value={step.expected_result}
                    onChange={(event) =>
                      updateStep(index, "expected_result", event.target.value)
                    }
                    placeholder="Expected result"
                  />
                  <input
                    aria-label={`Step ${index + 1} observed result`}
                    value={step.observed_result}
                    onChange={(event) =>
                      updateStep(index, "observed_result", event.target.value)
                    }
                    placeholder="Observed result"
                  />
                  <textarea
                    aria-label={`Step ${index + 1} evidence references`}
                    value={(step.evidence_refs ?? []).join("\n")}
                    onChange={(event) => {
                      setSpecDirty(true);
                      setReproductionSteps((current) =>
                        current.map((item, itemIndex) =>
                          itemIndex === index
                            ? {
                                ...item,
                                evidence_refs: event.target.value
                                  .split("\n")
                                  .map((reference) => reference.trim())
                                  .filter(Boolean),
                              }
                            : item,
                        ),
                      );
                    }}
                    rows={2}
                    placeholder="Evidence references (one file, URL, or attachment ID per line)"
                  />
                </div>
              ))}
              <div className="spec-subheading">
                <strong>Acceptance criteria</strong>
                <button
                  className="text-button"
                  type="button"
                  onClick={() => (
                    setSpecDirty(true),
                    setAcceptanceCriteria((current) => [
                      ...current,
                      { position: current.length + 1, statement: "" },
                    ])
                  )}
                >
                  + Add criterion
                </button>
              </div>
              {acceptanceCriteria.map((criterion, index) => (
                <div
                  className="acceptance-row"
                  key={`${criterion.position}-${index}`}
                >
                  <input
                    aria-label={`Acceptance criterion ${index + 1}`}
                    value={criterion.statement}
                    onChange={(event) =>
                      updateAcceptance(index, event.target.value)
                    }
                    placeholder="The work is complete when…"
                  />
                  {criterion.verification_status === "HUMAN_VERIFIED" ? (
                    <span className="inline-actions">
                      <span className="verified-mark">Verified</span>
                      <button
                        className="text-button danger-text"
                        type="button"
                        onClick={() => {
                          setSpecDirty(true);
                          setAcceptanceCriteria((current) => current.filter((_, itemIndex) => itemIndex !== index));
                        }}
                      >
                        Remove
                      </button>
                    </span>
                  ) : (
                    <span className="inline-actions">
                      <button
                        className="text-button"
                        type="button"
                        disabled={specDirty || !criterion.statement.trim()}
                        title={
                          specDirty
                            ? "Save the definition before verifying"
                            : "Enter a criterion before verifying"
                        }
                        onClick={() =>
                          void verifySpecification(
                            "acceptance_criterion",
                            undefined,
                            index + 1,
                          )
                        }
                      >
                        Verify
                      </button>
                      <button
                        className="text-button danger-text"
                        type="button"
                        onClick={() => {
                          setSpecDirty(true);
                          setAcceptanceCriteria((current) => current.filter((_, itemIndex) => itemIndex !== index));
                        }}
                      >
                        Remove
                      </button>
                    </span>
                  )}
                </div>
              ))}
              <div className="spec-subheading">
                <strong>Regression test cases</strong>
                <button
                  className="text-button"
                  type="button"
                  onClick={() => (
                    setSpecDirty(true),
                    setRegressionTestCases((current) => [
                      ...current,
                      { position: current.length + 1, scenario: "", expected_result: "" },
                    ])
                  )}
                >
                  + Add test case
                </button>
              </div>
              {regressionTestCases.map((testCase, index) => (
                <div className="regression-row" key={`${testCase.position}-${index}`}>
                  <input
                    aria-label={`Regression scenario ${index + 1}`}
                    value={testCase.scenario}
                    onChange={(event) => updateRegression(index, "scenario", event.target.value)}
                    placeholder="Scenario or regression risk"
                  />
                  <input
                    aria-label={`Regression expected result ${index + 1}`}
                    value={testCase.expected_result}
                    onChange={(event) => updateRegression(index, "expected_result", event.target.value)}
                    placeholder="Expected result"
                  />
                  {testCase.verification_status === "HUMAN_VERIFIED" ? (
                    <span className="inline-actions">
                      <span className="verified-mark">Verified</span>
                      <button
                        className="text-button danger-text"
                        type="button"
                        onClick={() => {
                          setSpecDirty(true);
                          setRegressionTestCases((current) => current.filter((_, itemIndex) => itemIndex !== index));
                        }}
                      >
                        Remove
                      </button>
                    </span>
                  ) : (
                    <span className="inline-actions">
                      <button
                        className="text-button"
                        type="button"
                        disabled={specDirty || !testCase.scenario.trim() || !testCase.expected_result.trim()}
                        onClick={() => void verifySpecification("regression_case", undefined, index + 1)}
                      >
                        Verify
                      </button>
                      <button
                        className="text-button danger-text"
                        type="button"
                        onClick={() => {
                          setSpecDirty(true);
                          setRegressionTestCases((current) => current.filter((_, itemIndex) => itemIndex !== index));
                        }}
                      >
                        Remove
                      </button>
                    </span>
                  )}
                </div>
              ))}
              <div className="spec-subheading">
                <strong>Engineering context references</strong>
                <button
                  className="text-button"
                  type="button"
                  onClick={() => (
                    setSpecDirty(true),
                    setSpecContextRefs((current) => [
                      ...current,
                      { repository_id: specRepositoryID, file: "", symbol: "", rationale: "" },
                    ])
                  )}
                >
                  + Add reference
                </button>
              </div>
              {specContextRefs.map((ref, index) => (
                <div className="context-ref-row" key={`${index}-${ref.file ?? "context"}`}>
                  <input
                    aria-label={`Context file ${index + 1}`}
                    value={ref.file ?? ""}
                    onChange={(event) => {
                      setSpecDirty(true);
                      setSpecContextRefs((current) =>
                        current.map((item, itemIndex) => itemIndex === index ? { ...item, file: event.target.value } : item),
                      );
                    }}
                    placeholder="src/module/file.go"
                  />
                  <input
                    aria-label={`Context symbol ${index + 1}`}
                    value={ref.symbol ?? ""}
                    onChange={(event) => {
                      setSpecDirty(true);
                      setSpecContextRefs((current) =>
                        current.map((item, itemIndex) => itemIndex === index ? { ...item, symbol: event.target.value } : item),
                      );
                    }}
                    placeholder="Symbol"
                  />
                  <input
                    aria-label={`Context rationale ${index + 1}`}
                    value={ref.rationale ?? ""}
                    onChange={(event) => {
                      setSpecDirty(true);
                      setSpecContextRefs((current) =>
                        current.map((item, itemIndex) => itemIndex === index ? { ...item, rationale: event.target.value } : item),
                      );
                    }}
                    placeholder="Why this context matters"
                    />
                  <button
                    className="text-button danger-text"
                    type="button"
                    onClick={() => {
                      setSpecDirty(true);
                      setSpecContextRefs((current) => current.filter((_, itemIndex) => itemIndex !== index));
                    }}
                  >
                    Remove
                  </button>
                </div>
              ))}
              <button
                className="button button-primary"
                type="submit"
                disabled={savingSpec}
              >
                {savingSpec ? "Saving definition…" : "Save definition"}
              </button>
            </form>
          </section>
        </aside>
      )}
    </div>
  );
}
