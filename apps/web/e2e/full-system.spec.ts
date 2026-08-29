import { expect, test, type Page } from "@playwright/test";

const base = "/app/orgs/org-1/workspaces/ws-1/projects/proj-1";

const project = {
  id: "proj-1",
  key: "ASTRO",
  display_name: "Tinh đồ AI",
  organization_id: "org-1",
  workspace_id: "ws-1",
};

const repo = {
  id: "repo-1",
  github_repository_id: 1319397613,
  full_name: "syphune/astro-birth-ai",
  default_branch: "main",
  installation_id: 1,
  installation_account: "syphune",
  linked: true,
};

const authorization = {
  scope: "project",
  organization_id: "org-1",
  workspace_id: "ws-1",
  project_id: "proj-1",
  capabilities: [
    "project.read",
    "project.manage",
    "work_item.read",
    "work_item.create",
    "work_item.edit",
    "work_item.assign",
    "work_item.transition",
    "specification.verify",
    "repository.manage",
    "sprint.manage",
    "agent.execute",
    "agent.approve",
    "autonomous.start",
    "autonomous.retry",
    "autonomous.cancel",
    "branch.manage",
  ],
};

const workflow = {
  name: "Astro delivery",
  statuses: [
    { key: "RAW", display_name: "Mới", category: "TODO", position: 0, is_terminal: false },
    { key: "REFINING", display_name: "Đang làm rõ", category: "TODO", position: 1, is_terminal: false },
    { key: "REVIEW_REQUIRED", display_name: "Chờ duyệt", category: "TODO", position: 2, is_terminal: false },
    { key: "READY", display_name: "Sẵn sàng", category: "TODO", position: 3, is_terminal: false },
    { key: "IN_PROGRESS", display_name: "Đang thực hiện", category: "IN_PROGRESS", position: 4, is_terminal: false },
    { key: "CODE_REVIEW", display_name: "Đang review mã", category: "IN_PROGRESS", position: 5, is_terminal: false },
    { key: "QA", display_name: "Kiểm thử", category: "DONE", position: 6, is_terminal: false },
    { key: "DONE", display_name: "Hoàn tất", category: "DONE", position: 7, is_terminal: true },
  ],
  transitions: [
    { key: "ready_to_progress", from_status: "READY", to_status: "IN_PROGRESS", display_name: "Bắt đầu thực hiện", required_rules: ["require_specification_ready"], required_permissions: ["work_item.transition"] },
    { key: "progress_to_review", from_status: "IN_PROGRESS", to_status: "CODE_REVIEW", display_name: "Gửi review mã", required_rules: ["require_pull_request"], required_permissions: ["work_item.transition"] },
    { key: "review_to_progress", from_status: "CODE_REVIEW", to_status: "IN_PROGRESS", display_name: "Yêu cầu sửa", required_rules: [], required_permissions: ["work_item.transition"] },
    { key: "review_to_qa", from_status: "CODE_REVIEW", to_status: "QA", display_name: "Chuyển QA", required_rules: ["require_ci_success"], required_permissions: ["work_item.transition"] },
    { key: "qa_to_progress", from_status: "QA", to_status: "IN_PROGRESS", display_name: "QA không đạt", required_rules: [], required_permissions: ["work_item.transition"] },
    { key: "qa_to_done", from_status: "QA", to_status: "DONE", display_name: "Hoàn tất", required_rules: [], required_permissions: ["work_item.transition"] },
  ],
};

type FullMockState = {
  calls: Set<string>;
  serverErrors: string[];
  transitionKeys: string[];
  item: Record<string, unknown>;
  items: Record<string, unknown>[];
  specification: Record<string, unknown>;
  branchPlan: Record<string, unknown> | null;
  branchOperations: Record<string, unknown>[];
  comments: Record<string, unknown>[];
  sprints: Record<string, unknown>[];
  fields: Record<string, unknown>[];
  automationRules: Record<string, unknown>[];
  environments: Record<string, unknown>[];
  organizationMembers: Record<string, unknown>[];
  projectMembers: Record<string, unknown>[];
  notifications: Record<string, unknown>[];
  tokens: Record<string, unknown>[];
  agentRuns: Record<string, unknown>[];
  autonomousRuns: Record<string, unknown>[];
  attachments: Record<string, unknown>[];
  workflow: typeof workflow;
  policy: Record<string, unknown>;
  deliveryPolicy: Record<string, unknown>;
  boardSettings: number;
  nextNumber: number;
};

function makeState(): FullMockState {
  const item = {
    id: "item-1",
    key: "ASTRO-1",
    type: "BUG",
    title: "Giữ nguyên đại từ trong luận giải",
    description: "Không thay thế bừa Anh thành Người ấy trong phần luận giải.",
    status: "READY",
    priority: "HIGH",
    version: 1,
    labels: ["production"],
    repository_id: "repo-1",
    reporter_id: "user-1",
    assignee_id: null,
    archived_at: null,
  };
  const completedItem = {
    ...item,
    id: "item-done",
    key: "ASTRO-2",
    title: "Đã hoàn tất từ đợt trước",
    status: "DONE",
    completed_at: "2026-08-01T00:00:00Z",
  };
  const specification = {
    id: "spec-1",
    work_item_id: "item-1",
    version: 4,
    summary: "Giữ nguyên đại từ tự nhiên trong prose; chỉ map role có cấu trúc.",
    fields: {
      PROBLEM_STATEMENT: { value: "Anh bị đổi thành Người ấy.", provenance: "HUMAN_PROVIDED", verification_status: "HUMAN_VERIFIED" },
      EXPECTED_BEHAVIOR: { value: "Anh và Chị giữ nguyên trong văn bản thường.", provenance: "HUMAN_PROVIDED", verification_status: "HUMAN_VERIFIED" },
      ACTUAL_BEHAVIOR: { value: "Bộ thay thế broad-match làm hỏng ngữ nghĩa.", provenance: "HUMAN_PROVIDED", verification_status: "HUMAN_VERIFIED" },
      ENVIRONMENT: { value: "Production, desktop và mobile.", provenance: "HUMAN_PROVIDED", verification_status: "HUMAN_VERIFIED" },
    },
    reproduction_steps: [],
    acceptance_criteria: [{ statement: "Prose Anh/Chị không bị rewrite.", verification_status: "HUMAN_VERIFIED" }],
    regression_test_cases: [{ position: 1, scenario: "Đọc luận giải quan hệ có Anh và Chị.", expected_result: "Giữ nguyên đại từ thường.", verification_status: "HUMAN_VERIFIED" }],
    context_refs: [],
    repository_id: "repo-1",
    media_refs: {},
  };
  return {
    calls: new Set(),
    serverErrors: [],
    transitionKeys: [],
    item,
    items: [item, completedItem],
    specification: { specification, readiness: { ready: true, missing: [] } },
    branchPlan: null,
    branchOperations: [],
    comments: [],
    sprints: [
      { id: "sprint-active", project_id: "proj-1", name: "Release hardening", goal: "Ship safely", status: "ACTIVE", starts_at: "2026-08-01T00:00:00Z", ends_at: "2026-08-31T00:00:00Z" },
      { id: "sprint-planned", project_id: "proj-1", name: "Regression sweep", goal: "Cover the release path", status: "PLANNED", starts_at: "2026-09-01T00:00:00Z", ends_at: "2026-09-14T00:00:00Z" },
    ],
    fields: [{ id: "field-1", project_id: "proj-1", key: "RISK", display_name: "Risk level", value_type: "SELECT", options: ["low", "high"], required: false }],
    automationRules: [{ id: "rule-1", project_id: "proj-1", name: "Notify transitions", event_type: "work_item.transitioned", action_type: "notify", enabled: true, config: {} }],
    environments: [{ id: "env-1", project_id: "proj-1", key: "staging", name: "Staging", kind: "staging", auto_deploy: false, require_approval: true }],
    organizationMembers: [
      { id: "user-1", login: "syphune", display_name: "Sy Phu", role_key: "owner" },
      { id: "user-2", login: "qa-user", display_name: "QA User", role_key: "qa" },
    ],
    projectMembers: [
      { id: "user-1", login: "syphune", display_name: "Sy Phu", role_key: "project_manager", project_role: "project_manager" },
      { id: "user-2", login: "qa-user", display_name: "QA User", role_key: "qa", project_role: "qa" },
    ],
    notifications: [
      { id: "notification-1", title: "ASTRO-1 cần review", body: "Specification đã sẵn sàng.", resource_type: "WORK_ITEM", resource_id: "item-1", project_id: "proj-1", created_at: "2026-08-27T08:00:00Z", read_at: null },
      { id: "notification-2", title: "CI đã chạy", body: "CI của pull request đã hoàn tất.", resource_type: "SYSTEM", resource_id: null, project_id: null, created_at: "2026-08-26T08:00:00Z", read_at: null },
    ],
    tokens: [{ id: "token-1", name: "Existing local token", prefix: "ff_existing", scopes: ["project.read"], expires_at: "2026-12-01T00:00:00Z" }],
    agentRuns: [{
      id: "agent-1", project_id: "proj-1", work_item_id: "item-1", repository_id: "repo-1", agent_provider: "local", agent_name: "Astro Fix Agent", status: "COMPLETED", approved: true, attempt: 1, max_attempts: 2, created_at: "2026-08-27T07:00:00Z", execution_inputs: { prompt: "Preserve reader pronouns.", specification_version: 4 }, result: { test_cases: [{ position: 1, status: "PASS", note: "Desktop and mobile passed." }], test_review_note: "All regression cases reviewed." },
    }],
    autonomousRuns: [{ id: "auto-1", project_id: "proj-1", work_item_id: "item-1", repository_id: "repo-1", agent_provider: "codex", agent_name: "Codex", phase: "TESTING", status: "WAITING_TEST_FEEDBACK", gate: "TEST_REVIEW", objective: "Run the Astro regression suite", attempt: 1, max_attempts: 3, unresolved_positions: [1], last_error: "One case needs reviewer confirmation." }],
    attachments: [{ id: "attachment-1", name: "relationship-mobile.png", size_bytes: 1024, content_type: "image/png" }],
    workflow,
    policy: { project_id: "proj-1", enabled: true, runtime: "server", providers: ["codex", "claude"], max_attempts: 3, timeout_seconds: 1800, test_scope: "full_regression", auto_retry: false, auto_create_pr: true, version: 2 },
    deliveryPolicy: {
      project_id: "proj-1",
      version: 1,
      release_mode: "standard",
      required_checks: [],
      review: { min_approvals: 1, forbid_self_approval: true, required_roles: [], path_rules: [] },
      risk_rules: {
        LOW: { min_approvals: 1, required_roles: [], require_qa: false, require_rollback_plan: false, require_production_verification: false },
        NORMAL: { min_approvals: 1, required_roles: [], require_qa: true, require_rollback_plan: false, require_production_verification: false },
        HIGH: { min_approvals: 2, required_roles: [], require_qa: true, require_rollback_plan: true, require_production_verification: false },
        CRITICAL: { min_approvals: 2, required_roles: [], require_qa: true, require_rollback_plan: true, require_production_verification: true },
      },
    },
    boardSettings: 0,
    nextNumber: 2,
  };
}

function bodyOf(request: { postDataJSON?: () => unknown }): Record<string, any> {
  try {
    return (request.postDataJSON?.() as Record<string, any>) ?? {};
  } catch {
    return {};
  }
}

function boardFor(state: FullMockState) {
  return {
    project_id: "proj-1",
    truncated: false,
    done_visibility_days: state.boardSettings,
    columns: state.workflow.statuses.map((status) => ({
      status: status.key,
      name: status.display_name,
      position: status.position,
      ordering_version: 1,
      items: state.items.filter((item) => item.status === status.key && !(state.boardSettings > 0 && status.is_terminal && item.completed_at)),
    })),
  };
}

async function installFullMock(page: Page) {
  const state = makeState();
  page.on("response", (response) => {
    if (response.status() >= 500) {
      state.serverErrors.push(`${response.status()} ${response.request().method()} ${response.url()}`);
    }
  });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    state.calls.add(`${method} ${path}`);
    let status = 200;
    let payload: unknown = { items: [] };
    const body = bodyOf(request);
    const dynamicItemMatch = path.match(/^\/api\/v1\/work-items\/([^/]+)/);
    const dynamicItemID = dynamicItemMatch?.[1];
    const currentItem = state.items.find((entry) => entry.id === dynamicItemID) ?? state.item;

    if (path === "/api/v1/me") payload = { id: "user-1", type: "user", login: "syphune", display_name: "Sy Phu", organization_id: "org-1", source: "github" };
    else if (path === "/api/v1/organizations" && method === "GET") payload = { items: [{ id: "org-1", slug: "astro", display_name: "Tinh đồ AI" }] };
    else if (path === "/api/v1/organizations/org-1/authorization" || path === "/api/v1/organizations/current/authorization") payload = { scope: "organization", organization_id: "org-1", capabilities: ["organization.read", "organization.manage", "project.read", "project.manage"] };
    else if (path === "/api/v1/organizations/current" && method === "GET") payload = { id: "org-1", slug: "astro", display_name: "Tinh đồ AI" };
    else if (path === "/api/v1/organizations/current/members" && method === "GET") payload = { items: state.organizationMembers };
    else if (path === "/api/v1/organizations/current/members" && method === "POST") {
      const member = { id: "user-3", login: body.login, display_name: body.login, role_key: body.role_key ?? "developer" };
      state.organizationMembers.push(member);
      payload = member;
      status = 201;
    } else if (path.match(/^\/api\/v1\/organizations\/current\/members\/[^/]+$/) && method === "PUT") {
      const id = path.split("/").pop();
      const member = state.organizationMembers.find((entry) => entry.id === id) ?? { id, login: id, display_name: id };
      Object.assign(member, { role_key: body.role_key });
      payload = member;
    } else if (path.match(/^\/api\/v1\/organizations\/current\/members\/[^/]+$/) && method === "DELETE") {
      state.organizationMembers = state.organizationMembers.filter((entry) => entry.id !== path.split("/").pop());
      status = 204;
    } else if (path === "/api/v1/workspaces" && method === "GET") payload = { items: [{ id: "ws-1", key: "MAIN", display_name: "Engineering" }] };
    else if (path === "/api/v1/workspaces/ws-1/authorization") payload = { scope: "workspace", organization_id: "org-1", workspace_id: "ws-1", capabilities: ["workspace.read", "workspace.manage"] };
    else if (path === "/api/v1/workspaces/ws-1" && method === "PATCH") payload = { id: "ws-1", key: "MAIN", display_name: body.display_name };
    else if (path === "/api/v1/projects" && method === "GET") payload = { items: [project] };
    else if (path === "/api/v1/projects" && method === "POST") {
      payload = { ...project, id: "proj-new", key: body.key, display_name: body.display_name, workspace_id: body.workspace_id };
      status = 201;
    } else if (path === "/api/v1/projects/proj-1/authorization") payload = authorization;
    else if (path === "/api/v1/projects/proj-1/board-settings" && method === "GET") payload = { project_id: "proj-1", done_visibility_days: state.boardSettings };
    else if (path === "/api/v1/projects/proj-1/board-settings" && method === "PUT") { state.boardSettings = Number(body.done_visibility_days); payload = { project_id: "proj-1", done_visibility_days: state.boardSettings }; }
    else if (path === "/api/v1/projects/proj-1/members" && method === "GET") payload = { items: state.projectMembers };
    else if (path.match(/^\/api\/v1\/projects\/proj-1\/members\/[^/]+$/) && method === "PUT") {
      const id = path.split("/").pop();
      const member = state.projectMembers.find((entry) => entry.id === id) ?? { id, login: id, display_name: id };
      Object.assign(member, { role_key: body.role_key, project_role: body.role_key });
      payload = member;
    } else if (path.match(/^\/api\/v1\/projects\/proj-1\/members\/[^/]+$/) && method === "DELETE") {
      state.projectMembers = state.projectMembers.filter((entry) => entry.id !== path.split("/").pop());
      status = 204;
    } else if (path === "/api/v1/projects/proj-1/branch-policy" && method === "GET") payload = {
      project_id: "proj-1", version: 2, development_branch: "develop", production_branch: "main", release_prefix: "release/", work_branch_template: "forgeflow/{type}/{task-key}-{slug}",
      rules: ["feature", "fix", "refactor", "chore", "hotfix", "release"].map((branch_type) => ({ branch_type, base_role: "development", target_role: "development", sync_roles: [], working_mode: "ephemeral", allow_release_base: false, create_pull_request: true, require_sync: false })),
    };
    else if (path === "/api/v1/projects/proj-1/branch-policy" && method === "PUT") payload = { ...body, project_id: "proj-1", version: 3 };
    else if (path === "/api/v1/projects/proj-1/delivery-policy" && method === "GET") payload = state.deliveryPolicy;
    else if (path === "/api/v1/projects/proj-1/delivery-policy" && method === "PUT") { state.deliveryPolicy = { ...body, project_id: "proj-1", version: Number(state.deliveryPolicy.version) + 1 }; payload = state.deliveryPolicy; }
    else if (path === "/api/v1/projects/proj-1/ai-policy" && method === "GET") payload = state.policy;
    else if (path === "/api/v1/projects/proj-1/ai-policy" && method === "PUT") { state.policy = { ...body, version: Number(state.policy.version) + 1 }; payload = state.policy; }
    else if (path === "/api/v1/projects/proj-1/environments" && method === "GET") payload = { items: state.environments };
    else if (path === "/api/v1/projects/proj-1/environments" && method === "POST") { const environment = { ...body, id: "env-2", project_id: "proj-1" }; state.environments.push(environment); payload = environment; status = 201; }
    else if (path === "/api/v1/projects/proj-1/custom-fields" && method === "GET") payload = { items: state.fields };
    else if (path === "/api/v1/projects/proj-1/custom-fields" && method === "POST") { const field = { ...body, id: "field-2", project_id: "proj-1" }; state.fields.push(field); payload = field; status = 201; }
    else if (path.match(/^\/api\/v1\/projects\/proj-1\/custom-fields\/[^/]+$/) && method === "PATCH") { const field = state.fields.find((entry) => entry.id === path.split("/").pop()) ?? {}; Object.assign(field, body); payload = field; }
    else if (path.match(/^\/api\/v1\/projects\/proj-1\/custom-fields\/[^/]+$/) && method === "DELETE") { state.fields = state.fields.filter((entry) => entry.id !== path.split("/").pop()); status = 204; }
    else if (path === "/api/v1/projects/proj-1/automation-rules" && method === "GET") payload = { items: state.automationRules };
    else if (path === "/api/v1/projects/proj-1/automation-rules" && method === "POST") { const rule = { ...body, id: "rule-2", project_id: "proj-1", enabled: true }; state.automationRules.push(rule); payload = rule; status = 201; }
    else if (path.match(/^\/api\/v1\/projects\/proj-1\/automation-rules\/[^/]+$/) && method === "PATCH") { const rule = state.automationRules.find((entry) => entry.id === path.split("/").pop()) ?? {}; Object.assign(rule, body); payload = rule; }
    else if (path.match(/^\/api\/v1\/projects\/proj-1\/automation-rules\/[^/]+$/) && method === "DELETE") { state.automationRules = state.automationRules.filter((entry) => entry.id !== path.split("/").pop()); status = 204; }
    else if (path === "/api/v1/workflows/current" && method === "GET") payload = state.workflow;
    else if (path === "/api/v1/workflows/current" && method === "PUT") { state.workflow = { ...body, statuses: body.statuses ?? state.workflow.statuses, transitions: body.transitions ?? state.workflow.transitions }; payload = state.workflow; }
    else if (path === "/api/v1/workflows/current/templates/release-gated" && method === "POST") payload = state.workflow;
    else if (path === "/api/v1/notifications/unread-count") payload = { unread_count: state.notifications.filter((entry) => !entry.read_at).length };
    else if (path === "/api/v1/notifications" && method === "GET") payload = { items: state.notifications };
    else if (path.match(/^\/api\/v1\/notifications\/[^/]+\/read$/) && method === "POST") { const notification = state.notifications.find((entry) => entry.id === path.split("/")[4]); if (notification) notification.read_at = "2026-08-27T10:00:00Z"; status = 204; }
    else if (path === "/api/v1/notifications/read-all" && method === "POST") { state.notifications.forEach((entry) => { entry.read_at = entry.read_at ?? "2026-08-27T10:00:00Z"; }); status = 204; }
    else if (path === "/api/v1/work-items" && method === "GET") payload = { items: state.items, next_cursor: "" };
    else if (path === "/api/v1/work-items" && method === "POST") {
      const id = `item-${state.nextNumber++}`;
      const created = { ...state.item, ...body, id, key: `ASTRO-${state.nextNumber - 1}`, version: 1, status: "RAW", repository_id: null };
      state.items.push(created);
      payload = created;
      status = 201;
    } else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}` && method === "GET") payload = currentItem;
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/comments` && method === "GET") payload = { items: state.comments };
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/comments` && method === "POST") { const comment = { id: `comment-${state.comments.length + 1}`, body: body.body, created_at: "2026-08-27T10:00:00Z" }; state.comments.push(comment); payload = comment; status = 201; }
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/specification` && method === "GET") payload = state.specification;
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/specification` && method === "PATCH") { const current = state.specification.specification as Record<string, any>; state.specification = { ...state.specification, specification: { ...current, ...body, version: Number(current.version) + 1 } }; payload = state.specification.specification; }
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/specification/verifications` && method === "POST") status = 204;
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/specification/review` && method === "POST") { state.specification = { ...state.specification, readiness: { ready: true, missing: [] } }; payload = state.specification.specification; }
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/transitions` && method === "POST") {
      const targetByTransition: Record<string, string> = { ready_to_progress: "IN_PROGRESS", progress_to_review: "CODE_REVIEW", review_to_progress: "IN_PROGRESS", review_to_qa: "QA", qa_to_progress: "IN_PROGRESS", qa_to_done: "DONE" };
      state.transitionKeys.push(body.transition_key);
      const updated = { ...currentItem, status: targetByTransition[body.transition_key] ?? currentItem.status, version: Number(currentItem.version) + 1 };
      state.item = updated;
      state.items = state.items.map((entry) => entry.id === dynamicItemID ? updated : entry);
      payload = updated;
    }
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/assignments` && method === "POST") { const updated = { ...currentItem, assignee_id: body.assignee_id, version: Number(currentItem.version) + 1 }; state.item = updated; state.items = state.items.map((entry) => entry.id === dynamicItemID ? updated : entry); payload = updated; }
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/move` && method === "POST") { const updated = { ...currentItem, status: body.target_status, version: Number(currentItem.version) + 1 }; state.item = updated; state.items = state.items.map((entry) => entry.id === dynamicItemID ? updated : entry); payload = { item: updated, source_ordering_version: 2, destination_ordering_version: 2 }; }
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/attachments` && method === "GET") payload = { items: state.attachments };
    else if (dynamicItemID && path === `/api/v1/work-items/${dynamicItemID}/attachments` && method === "POST") { const attachment = { id: `attachment-${state.attachments.length + 1}`, name: "uploaded-evidence.png", size_bytes: 1024, content_type: "image/png" }; state.attachments.push(attachment); payload = attachment; status = 201; }
    else if (dynamicItemID && path.match(new RegExp(`/api/v1/work-items/${dynamicItemID}/attachments/[^/]+$`)) && method === "DELETE") { state.attachments = state.attachments.filter((entry) => entry.id !== path.split("/").pop()); status = 204; }
    else if (path === "/api/v1/boards/current") payload = boardFor(state);
    else if (path === "/api/v1/integrations/github/installations") payload = { items: [{ id: "installation-1", github_installation_id: 1, account_login: "syphune", created_at: "2026-01-01T00:00:00Z" }] };
    else if (path === "/api/v1/integrations/github/repositories") payload = { items: [repo] };
    else if (path === "/api/v1/projects/proj-1/repositories" && method === "GET") payload = { items: [repo] };
    else if (path === "/api/v1/projects/proj-1/repositories" && method === "POST") status = 204;
    else if (path.match(/^\/api\/v1\/projects\/proj-1\/repositories\/repo-1$/) && method === "DELETE") status = 204;
    else if (path === "/api/v1/projects/proj-1/repositories/repo-1/context") payload = { repository: repo, branches: [{ name: "main" }, { name: "develop" }], commits: [{ sha: "abc123", message: "Fix pronoun replacement", author_login: "syphune" }], pull_requests: [{ number: 1, title: "Preserve reader pronouns", url: "https://github.com/syphune/astro-birth-ai/pull/1", state: "OPEN" }], ci_runs: [{ id: "ci-1", name: "verify", status: "completed", conclusion: "success" }] };
    else if (path === "/api/v1/projects/proj-1/repositories/repo-1/tree") payload = { items: [{ path: "apps/web/components/report-shell.tsx", type: "blob", size: 100, sha: "file-1" }, { path: "backend/internal/report/friendly.go", type: "blob", size: 100, sha: "file-2" }] };
    else if (path === "/api/v1/projects/proj-1/repositories/repo-1/snapshots") payload = { items: [{ id: "snapshot-1", project_id: "proj-1", repository_id: "repo-1", commit_sha: "abc123", ref_name: "main", status: "READY" }] };
    else if (path === "/api/v1/projects/proj-1/repositories/repo-1/snapshots/snapshot-1/symbols") payload = { items: [{ path: "apps/web/components/report-shell.tsx", name: "relationshipReaderText", qualified_name: "relationshipReaderText", kind: "function", start_line: 42, end_line: 62, confidence: "HIGH", provenance: "EXTRACTED" }] };
    else if (path === "/api/v1/sprints" && method === "GET") payload = { items: state.sprints };
    else if (path === "/api/v1/sprints" && method === "POST") { const sprint = { ...body, id: "sprint-new", project_id: "proj-1", status: "PLANNED" }; state.sprints.unshift(sprint); payload = sprint; status = 201; }
    else if (path.match(/^\/api\/v1\/sprints\/[^/]+$/) && method === "PATCH") { const sprint = state.sprints.find((entry) => entry.id === path.split("/").pop()) ?? {}; Object.assign(sprint, body); payload = sprint; }
    else if (path.match(/^\/api\/v1\/sprints\/[^/]+$/) && method === "DELETE") { state.sprints = state.sprints.filter((entry) => entry.id !== path.split("/").pop()); status = 204; }
    else if (path.match(/^\/api\/v1\/sprints\/[^/]+\/(start|complete)$/) && method === "POST") { const sprint = state.sprints.find((entry) => entry.id === path.split("/")[4]) ?? {}; sprint.status = path.endsWith("/start") ? "ACTIVE" : "COMPLETED"; payload = sprint; }
    else if (path === "/api/v1/me/tokens" && method === "GET") payload = { items: state.tokens };
    else if (path === "/api/v1/me/tokens" && method === "POST") { const token = { id: `token-${state.tokens.length + 1}`, name: body.name, prefix: "ff_new", token: "ff_secret_once", scopes: body.scopes, expires_at: "2026-12-01T00:00:00Z" }; state.tokens.unshift(token); payload = token; status = 201; }
    else if (path.match(/^\/api\/v1\/me\/tokens\/[^/]+$/) && method === "DELETE") { state.tokens = state.tokens.filter((entry) => entry.id !== path.split("/").pop()); status = 204; }
    else if (path === "/api/v1/agent-runs" && method === "GET") payload = { items: state.agentRuns };
    else if (path === "/api/v1/agent-runs" && method === "POST") { const run = { id: `agent-${state.agentRuns.length + 1}`, ...body, status: "QUEUED", approved: false, attempt: 1, max_attempts: 2, created_at: "2026-08-27T10:00:00Z" }; state.agentRuns.unshift(run); payload = run; status = 201; }
    else if (path.match(/^\/api\/v1\/agent-runs\/[^/]+\/approve$/) && method === "POST") { const run = state.agentRuns.find((entry) => entry.id === path.split("/")[4]) ?? {}; Object.assign(run, { approved: true, status: "QUEUED" }); payload = run; }
    else if (path.match(/^\/api\/v1\/agent-runs\/[^/]+\/(start|resume|cancel)$/) && method === "POST") { const run = state.agentRuns.find((entry) => entry.id === path.split("/")[4]) ?? {}; Object.assign(run, { status: path.endsWith("/cancel") ? "CANCELLED" : path.endsWith("/resume") ? "COMPLETED" : "INTERRUPTED" }); payload = run; }
    else if (path.match(/^\/api\/v1\/agent-runs\/[^/]+\/test-results$/) && method === "POST") { const run = state.agentRuns.find((entry) => entry.id === path.split("/")[4]) ?? {}; run.result = { ...(run.result as Record<string, unknown> | undefined), ...(body.review_note ? { test_review_note: body.review_note } : {}), ...(body.test_cases ? { test_cases: body.test_cases } : {}) }; payload = run; }
    else if (path === "/api/v1/autonomous-runs" && method === "GET") payload = { items: state.autonomousRuns };
    else if (path === "/api/v1/autonomous-runs" && method === "POST") { const run = { id: `auto-${state.autonomousRuns.length + 1}`, ...body, status: "QUEUED", phase: "INTAKE", attempt: 1, max_attempts: 3 }; state.autonomousRuns.unshift(run); payload = run; status = 201; }
    else if (path.match(/^\/api\/v1\/autonomous-runs\/[^/]+$/) && method === "GET") payload = { run: state.autonomousRuns.find((entry) => entry.id === path.split("/")[4]), feedback: [{ id: "feedback-1", source: "qa", note: "Xác nhận case mobile trước khi retry." }] };
    else if (path.match(/^\/api\/v1\/autonomous-runs\/[^/]+\/(resume|cancel|retry)$/) && method === "POST") { const run = state.autonomousRuns.find((entry) => entry.id === path.split("/")[4]) ?? {}; Object.assign(run, { status: path.endsWith("/cancel") ? "CANCELLED" : path.endsWith("/retry") ? "EXECUTING" : "EXECUTING" }); payload = run; }
    else if (path.match(/^\/api\/v1\/autonomous-runs\/[^/]+\/test-results$/) && method === "POST") payload = state.autonomousRuns.find((entry) => entry.id === path.split("/")[4]) ?? {};
    else if (path.match(/^\/api\/v1\/autonomous-runs\/[^/]+\/feedback$/) && method === "POST") { payload = { id: "feedback-2", ...body, source: "human" }; status = 201; }
    else if (path.match(/^\/api\/v1\/work-items\/[^/]+\/branch-plan$/) && method === "GET") {
      if (!state.branchPlan) status = 404;
      else payload = state.branchPlan;
    } else if (path.match(/^\/api\/v1\/work-items\/[^/]+\/branch-plan\/preview$/) && method === "POST") payload = { ...body, state: "PREVIEW", base_branch: "develop", target_branch: "develop", working_branch: "forgeflow/fix/astro-1", base_sha: "c0e5164", sync_branches: [], create_pull_request: true };
    else if (path.match(/^\/api\/v1\/work-items\/[^/]+\/branch-plan\/prepare$/) && method === "POST") { state.branchPlan = { id: "plan-1", ...body, state: "READY", base_branch: "develop", target_branch: "develop", working_branch: "forgeflow/fix/astro-1", base_sha: "c0e5164", sync_branches: ["main"], create_pull_request: true }; payload = state.branchPlan; }
    else if (path.match(/^\/api\/v1\/work-items\/[^/]+\/branch-plan\/operations$/) && method === "GET") payload = { items: state.branchOperations };
    else if (path.match(/^\/api\/v1\/work-items\/[^/]+\/branch-plan\/pull-request$/) && method === "POST") { state.branchOperations = [{ id: "op-1", kind: "PULL_REQUEST", state: "OPEN", source_branch: "forgeflow/fix/astro-1", target_branch: "develop", pull_request_number: 1, pull_request_url: "https://github.com/syphune/astro-birth-ai/pull/1" }]; payload = state.branchOperations[0]; status = 201; }
    else if (path.match(/^\/api\/v1\/work-items\/[^/]+\/branch-plan\/sync$/) && method === "POST") payload = { items: state.branchOperations };
    else if (path === "/api/v1/auth/logout" && method === "POST") status = 204;

    if (status === 204) await route.fulfill({ status, body: "" });
    else await route.fulfill({ status, json: payload });
  });
  return state;
}

const routeMatrix: Array<[string, string]> = [
  ["/app", "#app-home-heading"],
  [`${base}/backlog`, "#backlog-heading"],
  [`${base}/planning`, "#planning-heading"],
  [`${base}/repositories`, "#repositories-heading"],
  [`${base}/work-items/item-1`, "#work-item-heading"],
  [`${base}/settings/workflow`, "#settings-heading"],
  [`${base}/settings/board`, "#settings-heading"],
  [`${base}/settings/branching`, "#settings-heading"],
  [`${base}/settings/autonomous`, "#settings-heading"],
  [`${base}/settings/people`, "#settings-heading"],
  [`${base}/settings/fields`, "#settings-heading"],
  [`${base}/settings/automation`, "#settings-heading"],
  ["/app/orgs/org-1/settings/general?project_id=proj-1", "#tenant-settings-heading"],
  ["/app/orgs/org-1/settings/members?project_id=proj-1", "#tenant-settings-heading"],
  ["/app/orgs/org-1/workspaces/ws-1/settings/general?project_id=proj-1", "#tenant-settings-heading"],
  ["/app/orgs/org-1/workspaces/ws-1/settings/members?project_id=proj-1", "#tenant-settings-heading"],
  ["/app/inbox", "#inbox-heading"],
  ["/app/account/developer", "#developer-heading"],
];

async function expectRoute(page: Page, path: string, selector: string) {
  await page.goto(path);
  await expect(page.locator(selector)).toBeVisible();
}

async function expectUsableViewport(page: Page) {
  const audit = await page.evaluate(() => {
    const viewportWidth = document.documentElement.clientWidth;
    const documentWidth = Math.max(document.documentElement.scrollWidth, document.body?.scrollWidth ?? 0);
    const visibleInteractive = Array.from(document.querySelectorAll<HTMLElement>("button, a, input, select, textarea, [role='button']"))
      .filter((element) => {
        const style = getComputedStyle(element);
        const rect = element.getBoundingClientRect();
        return style.display !== "none" && style.visibility !== "hidden" && Number(style.opacity) !== 0 && rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.left < viewportWidth;
      });
    const clippedInteractive = visibleInteractive
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.left < -1 || rect.right > viewportWidth + 1;
      })
      .slice(0, 10)
      .map((element) => `${element.tagName.toLowerCase()}: ${element.textContent?.trim() || element.getAttribute("aria-label") || element.getAttribute("placeholder") || "unnamed"}`);
    return { horizontalOverflow: documentWidth - viewportWidth, clippedInteractive };
  });
  expect(audit.horizontalOverflow, "page has horizontal overflow").toBeLessThanOrEqual(1);
  expect(audit.clippedInteractive, "visible controls are clipped by the viewport").toEqual([]);
}

async function expectNoServerErrors(page: Page, state: FullMockState) {
  await page.waitForTimeout(0);
  expect(state.serverErrors, "browser observed a server error").toEqual([]);
}

test("covers the complete app-v2 route matrix", async ({ page }) => {
  const state = await installFullMock(page);
  let visited = 0;
  for (const [path, selector] of routeMatrix) {
    await expectRoute(page, path, selector);
    await expectUsableViewport(page);
    visited += 1;
  }
  expect(visited).toBe(routeMatrix.length);
  expect(state.calls.has("GET /api/v1/projects/proj-1/authorization")).toBeTruthy();
  expect(state.calls.has("GET /api/v1/projects/proj-1/board-settings")).toBeTruthy();
  expect(state.calls.has("GET /api/v1/projects/proj-1/branch-policy")).toBeTruthy();
  expect(state.calls.has("GET /api/v1/projects/proj-1/automation-rules")).toBeTruthy();
  expect(state.calls.has("GET /api/v1/me/tokens")).toBeTruthy();
  await expectNoServerErrors(page, state);
});

test("keeps the complete route matrix usable on a mobile viewport", async ({ page }) => {
  const state = await installFullMock(page);
  await page.setViewportSize({ width: 390, height: 844 });
  for (const [path, selector] of routeMatrix) {
    await expectRoute(page, path, selector);
    await expectUsableViewport(page);
  }
  await page.goto(`${base}/work-items/item-1`);
  await expect(page.locator("#work-item-heading")).toBeVisible();
  await page.screenshot({ path: "docs/case-studies/images/forgeflow-mobile-usability.png", fullPage: true });
  await expectNoServerErrors(page, state);
});

test("covers work intake, evidence, workflow, branch plan and agent gates", async ({ page }) => {
  const state = await installFullMock(page);
  await page.goto(`${base}/backlog`);
  await expect(page.getByText("ASTRO-1")).toBeVisible();

  await page.getByRole("link", { name: "+ Tạo công việc", exact: true }).click();
  await page.getByLabel("Tiêu đề").fill("Regression task from intake");
  await page.getByRole("dialog").getByRole("textbox", { name: "Ngữ cảnh" }).fill("Verify the production report behavior.");
  await page.getByRole("button", { name: "Tạo công việc", exact: true }).click();
  await expect(page).toHaveURL(/backlog\?item=item-2$/);
  await expect(page.getByRole("heading", { name: "Regression task from intake" })).toBeVisible();

  await page.goto(`${base}/backlog?view=board`);
  const card = page.locator('[data-work-item-id="item-1"]');
  await expect(card).toBeVisible();
  await card.focus();
  await card.press("Space");
  await expect(card).toHaveAttribute("aria-grabbed", "true");
  await card.press("ArrowRight");
  await expect.poll(() => state.calls.has("POST /api/v1/work-items/item-1/move")).toBeTruthy();

  await page.goto(`${base}/work-items/item-1`);
  await expect(page.getByRole("heading", { name: "Giữ nguyên đại từ trong luận giải" })).toBeVisible();
  const moveThroughWorkflow = async (name: string) => {
    await page.getByRole("combobox", { name: /Chọn chuyển đổi/ }).click();
    await page.getByRole("option", { name, exact: true }).click();
    await page.getByRole("button", { name: "Chuyển công việc", exact: true }).click();
  };
  await moveThroughWorkflow("Gửi review mã");
  await expect(page.locator(".app-v2-detail-aside strong").first()).toHaveText("Đang review mã");
  await moveThroughWorkflow("Yêu cầu sửa");
  await moveThroughWorkflow("Gửi review mã");
  await moveThroughWorkflow("Chuyển QA");
  await expect(page.locator(".app-v2-detail-aside strong").first()).toHaveText("Kiểm thử");
  await moveThroughWorkflow("QA không đạt");
  await moveThroughWorkflow("Gửi review mã");
  await moveThroughWorkflow("Chuyển QA");
  await moveThroughWorkflow("Hoàn tất");
  await expect(page.locator(".app-v2-detail-aside strong").first()).toHaveText("Hoàn tất");
  const assignee = page.getByLabel("Người đang xử lý");
  await assignee.selectOption("user-2");
  await page.getByRole("button", { name: "Lưu người xử lý" }).click();
  await expect(page.getByText("Đã cập nhật người xử lý.")).toBeVisible();

  await page.getByRole("button", { name: "Hoạt động" }).click();
  await page.getByLabel("Bình luận").fill("QA đã xác nhận nội dung Anh/Chị không bị đổi.");
  await page.getByRole("button", { name: "Bình luận", exact: true }).click();
  await expect(page.getByText("QA đã xác nhận nội dung Anh/Chị không bị đổi.")).toBeVisible();

  await page.getByRole("button", { name: "Đặc tả" }).click();
  await expect(page.getByRole("heading", { name: "Trình chỉnh sửa đặc tả" })).toBeVisible();
  await expect(page.locator("p.app-v2-prose").filter({ hasText: "Giữ nguyên đại từ tự nhiên trong prose; chỉ map role có cấu trúc." })).toBeVisible();
  await page.getByRole("button", { name: "Lần chạy" }).click();
  await expect(page.getByRole("heading", { name: "Workflow autonomous" })).toBeVisible();

  await page.getByRole("button", { name: "Review kiểm thử" }).click();
  await expect(page.getByRole("heading", { name: "Checklist kiểm thử" })).toBeVisible();
  await page.getByLabel("Ghi chú review").fill("Reviewer checked desktop and mobile evidence.");
  await page.getByRole("button", { name: "Lưu ghi chú review" }).click();
  await expect(page.getByText("Đã lưu ghi chú review.")).toBeVisible();

  await page.getByRole("button", { name: "Feedback" }).click();
  await expect(page.getByText("Xác nhận case mobile trước khi retry.")).toBeVisible();
  await page.getByLabel("Lần tiếp theo cần sửa gì?").fill("Re-run the mobile viewport assertion.");
  await page.getByRole("button", { name: "Retry phần chưa đạt" }).click();
  await expect(page.getByText("Đã retry workflow với phạm vi chưa đạt.")).toBeVisible();

  await page.getByRole("button", { name: "Kế hoạch branch" }).click();
  await page.getByRole("button", { name: "Xem trước kế hoạch" }).click();
  await expect(page.getByText("forgeflow/fix/astro-1")).toBeVisible();
  await page.getByRole("button", { name: "Chuẩn bị branch" }).click();
  await expect(page.getByText("Đã chuẩn bị kế hoạch branch.")).toBeVisible();
  await page.getByRole("button", { name: "Tạo PR nháp" }).click();
  await expect(page.getByText("Đã chuẩn bị thao tác pull request.")).toBeVisible();
  await page.getByRole("button", { name: "Chuẩn bị PR đồng bộ" }).click();
  await expect(page.getByText("Đã chuẩn bị các pull request đồng bộ.")).toBeVisible();

  expect(state.calls.has("POST /api/v1/work-items/item-1/assignments")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/work-items/item-1/comments")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/agent-runs/agent-1/test-results")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/autonomous-runs/auto-1/retry")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/work-items/item-1/branch-plan/prepare")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/work-items/item-1/branch-plan/pull-request")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/work-items/item-1/branch-plan/sync")).toBeTruthy();
  expect(state.transitionKeys).toEqual([
    "progress_to_review",
    "review_to_progress",
    "progress_to_review",
    "review_to_qa",
    "qa_to_progress",
    "progress_to_review",
    "review_to_qa",
    "qa_to_done",
  ]);

  await page.screenshot({ path: "docs/case-studies/images/forgeflow-full-e2e-branch-plan.png", fullPage: true });
  await expectNoServerErrors(page, state);
});

test("covers user-controlled AgentRun and autonomous lifecycle", async ({ page }) => {
  const state = await installFullMock(page);
  await page.goto(`${base}/work-items/item-1`);
  await page.getByRole("button", { name: "Lần chạy" }).click();

  const autonomous = page.locator('section[aria-labelledby="autonomous-workflow-heading"]');
  await autonomous.getByRole("button", { name: "Bắt đầu autonomous", exact: true }).first().click();
  await autonomous.getByLabel("Mục tiêu của manager hoặc leader").fill("Run the Astro regression workflow from the user-defined acceptance cases.");
  await autonomous.locator("form").getByRole("button", { name: "Bắt đầu autonomous", exact: true }).click();
  await expect(page.getByText("Đã bắt đầu workflow autonomous.")).toBeVisible();
  const createdAutonomous = autonomous.locator(".app-v2-run-card").filter({ hasText: "Run the Astro regression workflow" }).first();
  await createdAutonomous.getByRole("button", { name: "Hủy", exact: true }).click();
  await expect(page.getByText("Đã hủy workflow autonomous.")).toBeVisible();

  const agent = page.locator('section[aria-labelledby="agent-runs-heading"]');
  await agent.getByRole("button", { name: "Tạo AgentRun", exact: true }).click();
  await agent.getByLabel("Tên agent").fill("Astro user agent");
  await agent.getByLabel("Prompt tác vụ đã duyệt").fill("Run the verified relationship regression cases.");
  await agent.locator("form").getByRole("button", { name: "Tạo AgentRun", exact: true }).click();
  await expect(page.getByText("Đã tạo AgentRun. Run vẫn cần được duyệt rõ ràng.")).toBeVisible();
  const createdAgent = agent.locator(".app-v2-run-card").filter({ hasText: "Astro user agent" }).first();
  await createdAgent.getByRole("button", { name: "Duyệt", exact: true }).click();
  await expect(page.getByText("Đã duyệt AgentRun.")).toBeVisible();
  await createdAgent.getByRole("button", { name: "Bắt đầu", exact: true }).click();
  await expect(page.getByText("Đã bắt đầu AgentRun.")).toBeVisible();
  await createdAgent.getByRole("button", { name: "Tiếp tục", exact: true }).click();
  await expect(page.getByText("Đã tiếp tục AgentRun.")).toBeVisible();

  expect(state.calls.has("POST /api/v1/autonomous-runs")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/autonomous-runs/auto-2/cancel")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/agent-runs")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/agent-runs/agent-2/approve")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/agent-runs/agent-2/start")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/agent-runs/agent-2/resume")).toBeTruthy();

  await page.screenshot({ path: "docs/case-studies/images/forgeflow-user-agent-lifecycle.png", fullPage: true });
  await expectNoServerErrors(page, state);
});

test("covers planning, repository, settings, inbox and developer mutations", async ({ page }) => {
  const state = await installFullMock(page);
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);

  await page.goto(`${base}/planning`);
  await page.getByRole("button", { name: "Tạo sprint" }).click();
  await page.getByLabel("Tên sprint").fill("Release smoke");
  await page.getByLabel("Mục tiêu").fill("Verify the release path");
  await page.getByRole("button", { name: "Lưu sprint" }).click();
  await expect(page.getByText("Đã tạo sprint.")).toBeVisible();
  await page.getByRole("button", { name: "Sửa" }).first().click();
  await page.getByLabel("Tên sprint").fill("Release smoke updated");
  await page.getByRole("button", { name: "Lưu sprint" }).click();
  await expect(page.getByText("Đã cập nhật sprint.")).toBeVisible();
  const sprintCard = page.locator(".app-v2-sprint-card").filter({ hasText: "Release smoke updated" }).first();
  await sprintCard.getByRole("button", { name: "Bắt đầu sprint", exact: true }).click();
  await expect(page.getByText("Release smoke updated đang hoạt động.")).toBeVisible();
  await sprintCard.getByRole("button", { name: "Hoàn tất sprint", exact: true }).click();
  await expect(page.getByText("Release smoke updated đã hoàn tất.")).toBeVisible();

  await page.goto(`${base}/repositories`);
  await page.getByRole("button", { name: "Xem ngữ cảnh" }).click();
  await expect(page.locator(".app-v2-repository-context").getByText("syphune/astro-birth-ai", { exact: true })).toBeVisible();
  await expect(page.getByText("Pull request")).toBeVisible();

  await page.goto(`${base}/settings/workflow`);
  await page.getByRole("button", { name: "Sửa workflow" }).click();
  await page.getByRole("button", { name: "Thêm trạng thái" }).click();
  await page.getByRole("button", { name: "Lưu workflow" }).click();
  await expect(page.getByText("Đã lưu workflow.")).toBeVisible();

  await page.goto(`${base}/settings/board`);
  await page.getByLabel("Ẩn công việc hoàn tất sau").selectOption("14");
  await page.getByRole("button", { name: "Lưu cài đặt Board", exact: true }).click();
  await expect(page.getByText("Đã lưu cài đặt Board.")).toBeVisible();
  expect(state.boardSettings).toBe(14);
  expect(state.calls.has("PUT /api/v1/projects/proj-1/board-settings")).toBeTruthy();
  await page.goto(`${base}/backlog?view=board`);
  await expect(page.getByText("Công việc hoàn tất quá 14 ngày sẽ được ẩn khỏi Board này.")).toBeVisible();
  await expect(page.getByText("ASTRO-2")).not.toBeVisible();
  const board = page.locator(".app-v2-board");
  await expect(board).toHaveCSS("grid-auto-flow", "column");
  await board.screenshot({ path: "docs/case-studies/images/forgeflow-board-retention.png" });

  await page.goto(`${base}/settings/branching`);
  await page.getByLabel("Branch development").fill("develop");
  await page.getByRole("button", { name: "Lưu branch policy" }).click();
  await expect(page.getByText("Đã lưu branch policy.")).toBeVisible();

  await page.goto(`${base}/settings/autonomous`);
  await page.getByRole("button", { name: "Lưu policy AI" }).click();
  await expect(page.getByText("Đã lưu policy AI autonomous.")).toBeVisible();
  await page.getByLabel("Key môi trường").fill("production");
  await page.getByLabel("Tên hiển thị").fill("Production");
  await page.getByRole("button", { name: "Thêm môi trường" }).click();
  await expect(page.locator(".app-v2-rule-row strong").filter({ hasText: "Production" })).toBeVisible();

  await page.goto(`${base}/settings/people`);
  await page.getByLabel("Vai trò QA User").selectOption("developer");
  await expect(page.getByText("Đã cập nhật role dự án.")).toBeVisible();
  page.once("dialog", (dialog) => void dialog.accept());
  await page.locator(".app-v2-member-row").filter({ hasText: "QA User" }).locator("button").click();
  await expect(page.getByText("Đã gỡ role dự án.")).toBeVisible();

  await page.goto(`${base}/settings/fields`);
  await page.getByLabel("Key trường").fill("SEVERITY");
  await page.getByLabel("Tên hiển thị").fill("Severity");
  await page.getByRole("button", { name: "Thêm trường" }).click();
  await expect(page.getByText("Đã tạo trường tùy chỉnh.")).toBeVisible();
  await page.getByRole("button", { name: "Sửa" }).last().click();
  await page.locator(".app-v2-field-edit input").first().fill("Severity updated");
  await page.getByRole("button", { name: "Lưu" }).click();
  await expect(page.getByText("Đã cập nhật trường tùy chỉnh.")).toBeVisible();

  await page.goto(`${base}/settings/automation`);
  await page.getByLabel("Tên rule").fill("Notify QA");
  await page.getByRole("button", { name: "Thêm rule" }).click();
  await expect(page.getByText("Đã tạo automation rule.")).toBeVisible();
  await page.getByRole("button", { name: "Đang bật" }).last().click();
  await expect(page.getByText("Đã cập nhật automation rule.")).toBeVisible();

  await page.goto("/app/orgs/org-1/settings/members?project_id=proj-1");
  await page.getByLabel("GitHub login").fill("new-contributor");
  await page.getByRole("button", { name: "Thêm thành viên" }).click();
  await expect(page.getByText("Đã thêm thành viên tổ chức.")).toBeVisible();

  await page.goto("/app/orgs/org-1/workspaces/ws-1/settings/general?project_id=proj-1");
  await page.getByLabel("Tên không gian").fill("Astro Engineering");
  await page.getByRole("button", { name: "Lưu" }).click();
  await expect(page.getByText("Đã lưu tên không gian.")).toBeVisible();

  await page.goto("/app/inbox");
  await page.getByRole("button", { name: "Đánh dấu đã đọc" }).first().click();
  await expect(page.getByText("Đã đọc", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Đánh dấu tất cả đã đọc" }).click();
  await expect(page.getByText("Đã đánh dấu tất cả đã đọc.")).toBeVisible();

  await page.goto("/app/account/developer");
  await page.getByLabel("Tên token").fill("E2E local token");
  await page.getByRole("button", { name: "Tạo token" }).click();
  await expect(page.getByText("Đã tạo token. Hãy sao chép secret ngay.")).toBeVisible();
  await page.getByRole("button", { name: "Tạo kết nối" }).click();
  await expect(page.getByText("Đã tạo kết nối. Hãy sao chép secret và cấu hình.")).toBeVisible();
  await page.getByRole("button", { name: "Sao chép cấu hình" }).click();
  await expect(page.getByText("Đã sao chép cấu hình MCP.")).toBeVisible();
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "Thu hồi" }).first().click();
  await expect(page.getByText("Đã thu hồi token.")).toBeVisible();

  expect(state.calls.has("POST /api/v1/sprints")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/projects/proj-1/environments")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/projects/proj-1/custom-fields")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/projects/proj-1/automation-rules")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/organizations/current/members")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/notifications/read-all")).toBeTruthy();
  expect(state.calls.has("POST /api/v1/me/tokens")).toBeTruthy();

  await page.screenshot({ path: "docs/case-studies/images/forgeflow-full-e2e-developer.png", fullPage: true });
  await expectNoServerErrors(page, state);
});
