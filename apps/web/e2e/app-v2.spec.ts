import { expect, test } from "@playwright/test";

const project = {
  id: "project-1",
  key: "APP",
  display_name: "Application",
  organization_id: "org-1",
  workspace_id: "workspace-1",
};

const item = {
  id: "item-1",
  key: "APP-1",
  type: "TASK",
  title: "Document checkout flow",
  description: "Keep the next move visible.",
  status: "RAW",
  priority: "MEDIUM",
  version: 1,
  labels: [],
};

test("new app keeps backlog context in the URL and opens a recoverable drawer", async ({ page }) => {
  let signedIn = true;
  const workItemListURLs: URL[] = [];
  const completedItem = { ...item, id: "item-done", key: "APP-2", title: "Completed task", status: "DONE" };
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    let payload: unknown = {};

    if (path === "/api/v1/auth/logout" && request.method() === "POST") {
      signedIn = false;
      await route.fulfill({ status: 204, body: "" });
      return;
    } else if (path === "/api/v1/me") {
      if (!signedIn) {
        await route.fulfill({ status: 401, json: { code: "UNAUTHORIZED", message: "sign in required" } });
        return;
      }
      payload = { id: "user-1", type: "user", login: "forgeflow-user", display_name: "Forgeflow User", organization_id: "org-1", source: "github" };
    } else if (path === "/api/v1/organizations") {
      payload = { items: [{ id: "org-1", slug: "forgeflow", display_name: "Forgeflow" }] };
    } else if (path === "/api/v1/workspaces") {
      payload = { items: [{ id: "workspace-1", key: "MAIN", display_name: "Main" }] };
    } else if (path === "/api/v1/projects") {
      payload = { items: [project] };
    } else if (path === "/api/v1/projects/project-1/authorization") {
      payload = { scope: "project", organization_id: "org-1", workspace_id: "workspace-1", project_id: "project-1", capabilities: ["project.read", "work_item.read", "work_item.create"] };
    } else if (path === "/api/v1/organizations/org-1/authorization") {
      payload = { scope: "organization", organization_id: "org-1", capabilities: ["organization.read"] };
    } else if (path === "/api/v1/notifications/unread-count") {
      payload = { unread_count: 2 };
    } else if (path === "/api/v1/work-items" && request.method() === "GET") {
      workItemListURLs.push(url);
      payload = url.searchParams.get("status") === "DONE" ? { items: [completedItem], next_cursor: "" } : { items: [item], next_cursor: "" };
    } else if (path === "/api/v1/workflows/current") {
      payload = {
        name: "Default",
        statuses: [{ key: "RAW", display_name: "Raw", category: "TODO", position: 0, is_terminal: false }, { key: "DONE", display_name: "Done", category: "DONE", position: 1, is_terminal: true }],
        transitions: [],
      };
    } else if (path === "/api/v1/boards/current") {
      payload = { project_id: "project-1", done_visibility_days: 0, columns: [{ status: "RAW", name: "Raw", position: 0, ordering_version: 1, items: [item] }, { status: "DONE", name: "Done", position: 1, ordering_version: 1, items: [completedItem] }], truncated: false };
    } else if (path === "/api/v1/work-items/item-1") {
      payload = item;
    } else if (path === "/api/v1/work-items/item-1/comments") {
      payload = { items: [] };
    } else {
      payload = { items: [] };
    }
    await route.fulfill({ status: 200, json: payload });
  });

  const backlogURL = "/app/orgs/org-1/workspaces/workspace-1/projects/project-1/backlog";
  await page.goto(backlogURL);
  await expect(page.getByRole("heading", { name: "Backlog" })).toBeVisible();
  await expect(page.getByText("APP-1")).toBeVisible();
  await expect(page.getByText("@forgeflow-user", { exact: false })).toBeVisible();
  await expect.poll(() => workItemListURLs.some((url) => url.searchParams.get("exclude_terminal") === "true")).toBeTruthy();

  await page.getByRole("combobox", { name: "Công việc đang mở" }).click();
  await page.getByRole("option", { name: "Hoàn tất", exact: true }).click();
  await expect(page).toHaveURL(/\/backlog\?status=DONE$/);
  await expect(page.getByText("APP-2")).toBeVisible();
  await expect(page.getByText("APP-1")).not.toBeVisible();
  expect(workItemListURLs.at(-1)?.searchParams.get("exclude_terminal")).toBeNull();
  await page.goto(backlogURL);
  await expect(page.getByText("APP-1")).toBeVisible();

  await page.getByRole("link", { name: "+ Tạo công việc", exact: true }).click();
  await expect(page).toHaveURL(/\/backlog\?create=1$/);
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "Đóng" }).click();
  await expect(page).toHaveURL(/\/backlog$/);

  await page.getByRole("button", { name: "Bảng" }).click();
  await expect(page).toHaveURL(/\/backlog\?view=board$/);
  await expect(page.getByRole("heading", { name: "Mới" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Hoàn tất" })).toBeVisible();
  await expect(page.getByText("APP-2")).toBeVisible();

  await page.getByRole("button", { name: "Danh sách" }).click();
  await expect(page).toHaveURL(/\/backlog$/);
  await page.getByLabel("Tìm công việc").fill("checkout");
  await expect(page).toHaveURL(/\/backlog\?q=checkout$/);
  const workItemLink = page.getByRole("link", { name: /Document checkout flow/ });
  await workItemLink.focus();
  await workItemLink.press("Enter");
  await expect(page).toHaveURL(/\/backlog\?q=checkout&item=item-1$/);
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "Thêm thao tác" }).click();
  await expect(page.getByRole("menu")).toBeVisible();
  await expect(page.getByRole("menuitem", { name: "Sao chép liên kết" })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: "Lưu trữ" })).toBeVisible();
  await page.getByRole("menuitem", { name: "Sao chép liên kết" }).click();
  await expect(page.locator(".app-v2-action-status")).toHaveText("Đã sao chép liên kết công việc.");
  await expect(page.getByRole("menu")).not.toBeVisible();

  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/backlog\?q=checkout$/);
  await expect(workItemLink).toBeFocused();

  await page.getByRole("link", { name: /Cài đặt tổ chức/ }).click();
  await expect(page).toHaveURL(/\/app\/orgs\/org-1\/settings\/general\?project_id=project-1$/);
  await expect(page.getByRole("link", { name: "Backlog" })).toHaveAttribute("href", `${backlogURL}`);
  await expect(page.getByRole("link", { name: "Kho mã" })).toHaveAttribute("href", "/app/orgs/org-1/workspaces/workspace-1/projects/project-1/repositories");

  await expect(page.getByRole("button", { name: "Đăng xuất" })).toBeVisible();
  await page.getByRole("button", { name: "Đăng xuất" }).click();
  await expect(page.locator(".app-v2-auth-gate")).toContainText("Đăng nhập để mở không gian làm việc");
  await expect(page.getByRole("link", { name: /Đăng nhập với GitHub/ })).toBeVisible();
});

test("keeps a session when the user joins a second organization", async ({ page }) => {
  const scopedOrganizationIDs: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const organizationID = request.headers()["x-organization-id"] ?? "";
    let payload: unknown = {};
    let status = 200;

    if (path === "/api/v1/me") {
      payload = { id: "user-b", type: "user", organization_id: "org-own", source: "github" };
    } else if (path === "/api/v1/organizations") {
      payload = { items: [
        { id: "org-own", slug: "own", display_name: "B's Forgeflow" },
        { id: "org-a", slug: "team-a", display_name: "A's team" },
      ] };
    } else if (["/api/v1/workspaces", "/api/v1/projects", "/api/v1/notifications/unread-count"].includes(path)) {
      if (!organizationID) {
        status = 401;
        payload = { code: "UNAUTHORIZED", message: "organization selection is required" };
      } else {
        scopedOrganizationIDs.push(organizationID);
        payload = path === "/api/v1/projects" && organizationID === "org-a"
          ? { items: [{ id: "project-a", key: "TEAM", display_name: "A's project", organization_id: "org-a", workspace_id: "workspace-a" }] }
          : path === "/api/v1/notifications/unread-count" ? { unread_count: 0 } : { items: [] };
      }
    }
    await route.fulfill({ status, json: payload });
  });

  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "A's project" })).toBeVisible();
  await expect(page.getByText("Đăng nhập để mở không gian làm việc")).not.toBeVisible();
  await page.getByRole("combobox").click();
  await expect(page.getByRole("option", { name: /A's project/ })).toBeVisible();
  expect(scopedOrganizationIDs).toContain("org-own");
  expect(scopedOrganizationIDs).toContain("org-a");
});

test("only members with project management permission can create projects", async ({ page }) => {
  let projects: typeof project[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    let payload: unknown = {};
    let status = 200;

    if (path === "/api/v1/me") payload = { id: "user-owner", type: "user", organization_id: "org-1", source: "github" };
    else if (path === "/api/v1/organizations") payload = { items: [{ id: "org-1", slug: "forgeflow", display_name: "Forgeflow" }] };
    else if (path === "/api/v1/organizations/org-1/authorization") payload = { scope: "organization", organization_id: "org-1", capabilities: ["organization.read", "project.read", "project.manage"] };
    else if (path === "/api/v1/workspaces") payload = { items: [{ id: "workspace-1", key: "MAIN", display_name: "Main" }] };
    else if (path === "/api/v1/projects" && request.method() === "GET") payload = { items: projects };
    else if (path === "/api/v1/projects" && request.method() === "POST") {
      const body = request.postDataJSON() as { workspace_id: string; key: string; display_name: string };
      const created = { id: "project-new", key: body.key, display_name: body.display_name, organization_id: "org-1", workspace_id: body.workspace_id };
      projects = [created];
      payload = created;
      status = 201;
    }
    await route.fulfill({ status, json: payload });
  });

  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "Tạo dự án" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Thêm dự án" })).toBeVisible();
  await page.getByLabel("Mã dự án").fill("OPS");
  await page.getByLabel("Tên dự án").fill("Operations");
  await page.getByRole("button", { name: "Tạo dự án" }).click();
  await expect(page.getByRole("heading", { name: "Operations" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Kết nối repository" })).toHaveAttribute("href", "/app/orgs/org-1/workspaces/workspace-1/projects/project-new/repositories");
});

test("does not show project creation to members without project management permission", async ({ page }) => {
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    let payload: unknown = {};
    if (path === "/api/v1/me") payload = { id: "user-developer", type: "user", organization_id: "org-1", source: "github" };
    else if (path === "/api/v1/organizations") payload = { items: [{ id: "org-1", slug: "forgeflow", display_name: "Forgeflow" }] };
    else if (path === "/api/v1/organizations/org-1/authorization") payload = { scope: "organization", organization_id: "org-1", capabilities: ["organization.read", "project.read"] };
    else if (path === "/api/v1/workspaces") payload = { items: [{ id: "workspace-1", key: "MAIN", display_name: "Main" }] };
    else if (path === "/api/v1/projects") payload = { items: [] };
    await route.fulfill({ status: 200, json: payload });
  });

  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "Tạo dự án" })).not.toBeVisible();
  await expect(page.getByRole("link", { name: "Thêm dự án" })).not.toBeVisible();
});

test("new users create a team organization after GitHub sign-in", async ({ page }) => {
  let organizationID = "";
  let organizations: unknown[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    let payload: unknown = {};
    let status = 200;

    if (path === "/api/v1/me") payload = { id: "user-new", type: "user", organization_id: organizationID, source: "github" };
    else if (path === "/api/v1/organizations" && request.method() === "GET") payload = { items: organizations };
    else if (path === "/api/v1/organizations" && request.method() === "POST") {
      organizationID = "org-new";
      organizations = [{ id: organizationID, slug: "acme", display_name: "Acme" }];
      payload = { id: organizationID, slug: "acme", display_name: "Acme" };
      status = 201;
    } else if (path === "/api/v1/organizations/org-new/authorization") payload = { scope: "organization", organization_id: organizationID, capabilities: ["organization.read", "project.read", "project.manage"] };
    else if (path === "/api/v1/workspaces") payload = { items: [{ id: "workspace-new", key: "MAIN", display_name: "Main" }] };
    else if (path === "/api/v1/projects") payload = { items: [] };
    await route.fulfill({ status, json: payload });
  });

  await page.goto("/app");
  await expect(page.getByRole("heading", { name: "Tạo tổ chức của bạn" })).toBeVisible();
  await page.getByLabel("Tên tổ chức").fill("Acme");
  await page.getByLabel("Slug tổ chức").fill("acme");
  await page.getByRole("button", { name: "Tạo tổ chức" }).click();
  await expect(page.getByRole("heading", { name: "Tạo dự án" })).toBeVisible();
});

test("project repository links feed the work-item definition editor", async ({ page }) => {
  let linked = false;
  let reviewed = false;
  const editableItem = { ...item, status: "REFINING" as const };
  let specification = {
    id: "spec-1",
    work_item_id: "item-1",
    version: 1,
    summary: "",
    fields: {} as Record<string, { value: string; provenance: string; verification_status: string }>,
    reproduction_steps: [],
    acceptance_criteria: [] as Array<{ statement: string; verification_status?: string }>,
    context_refs: [],
    repository_id: "",
  };
  const repository = {
    id: "repo-1",
    github_repository_id: 101,
    full_name: "forgeflow/web",
    default_branch: "main",
    installation_id: 1,
    installation_account: "forgeflow",
    linked: false,
  };
  const projectRepository = () => ({ ...repository, linked });
  const readiness = () => {
    const complete = Boolean(specification.fields.GOAL?.value && specification.acceptance_criteria.length && specification.acceptance_criteria.every((criterion) => criterion.verification_status === "HUMAN_VERIFIED") && specification.repository_id);
    return { ready: complete && reviewed, missing: complete && !reviewed ? ["HUMAN_REVIEW"] : complete ? [] : ["REPOSITORY_OR_NO_CODE_CHANGE_RATIONALE"] };
  };

  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    let payload: unknown = {};
    let statusCode = 200;
    if (path === "/api/v1/me") payload = { id: "user-1", type: "user", organization_id: "org-1", source: "github" };
    else if (path === "/api/v1/organizations") payload = { items: [{ id: "org-1", slug: "forgeflow", display_name: "Forgeflow" }] };
    else if (path === "/api/v1/workspaces") payload = { items: [{ id: "workspace-1", key: "MAIN", display_name: "Main" }] };
    else if (path === "/api/v1/projects") payload = { items: [project] };
    else if (path === "/api/v1/projects/project-1/authorization") payload = { scope: "project", organization_id: "org-1", workspace_id: "workspace-1", project_id: "project-1", capabilities: ["project.read", "work_item.read", "work_item.edit", "repository.manage"] };
    else if (path === "/api/v1/notifications/unread-count") payload = { unread_count: 0 };
    else if (path === "/api/v1/integrations/github/installations") payload = { items: [{ id: "installation-1", github_installation_id: 1, account_login: "forgeflow", created_at: "2026-01-01T00:00:00Z" }] };
    else if (path === "/api/v1/integrations/github/repositories") payload = { items: [projectRepository()] };
    else if (path === "/api/v1/projects/project-1/repositories" && request.method() === "GET") payload = { items: linked ? [projectRepository()] : [] };
    else if (path === "/api/v1/projects/project-1/repositories" && request.method() === "POST") { linked = true; statusCode = 204; }
    else if (path === "/api/v1/projects/project-1/repositories/repo-1/context") payload = { repository: projectRepository(), branches: [{ name: "main" }], commits: [{ sha: "abc123", message: "Add backlog page", author_login: "forgeflow" }], pull_requests: [{ number: 7, title: "Improve backlog loading", url: "https://github.com/forgeflow/web/pull/7", state: "OPEN" }], ci_runs: [] };
    else if (path === "/api/v1/projects/project-1/repositories/repo-1/tree") payload = { items: [{ path: "apps/web/src/features/backlog/backlog-page.tsx", type: "blob", size: 100, sha: "file-sha" }, { path: "apps/web/src/features/repositories/repository-page.tsx", type: "blob", size: 100, sha: "file-sha-2" }] };
    else if (path === "/api/v1/projects/project-1/repositories/repo-1/snapshots") payload = { items: [{ id: "snapshot-1", project_id: "project-1", repository_id: "repo-1", commit_sha: "abc123", ref_name: "main", status: "READY" }] };
    else if (path === "/api/v1/projects/project-1/repositories/repo-1/snapshots/snapshot-1/symbols") payload = { items: [{ path: "apps/web/src/features/backlog/backlog-page.tsx", name: "BacklogPage", qualified_name: "BacklogPage", kind: "function", start_line: 1, end_line: 20, confidence: "HIGH", provenance: "EXTRACTED" }] };
    else if (path === "/api/v1/work-items/item-1") payload = editableItem;
    else if (path === "/api/v1/work-items/item-1/comments") payload = { items: [] };
    else if (path === "/api/v1/workflows/current") payload = { name: "Default", statuses: [{ key: "RAW", display_name: "Raw", category: "TODO", position: 0, is_terminal: false }, { key: "REFINING", display_name: "Refining", category: "TODO", position: 1, is_terminal: false }], transitions: [] };
    else if (path === "/api/v1/work-items/item-1/specification" && request.method() === "GET") payload = { specification, readiness: readiness() };
    else if (path === "/api/v1/work-items/item-1/specification" && request.method() === "PATCH") {
      const body = request.postDataJSON() as { summary?: string; fields?: Record<string, string>; acceptance_criteria?: Array<{ statement?: string }>; repository_id?: string };
      specification = { ...specification, version: specification.version + 1, summary: body.summary ?? "", fields: Object.fromEntries(Object.entries(body.fields ?? {}).map(([key, value]) => [key, { value, provenance: "HUMAN_PROVIDED", verification_status: "UNVERIFIED" }])), acceptance_criteria: (body.acceptance_criteria ?? []).map((criterion) => ({ statement: criterion.statement ?? "", verification_status: "UNVERIFIED" })), context_refs: (request.postDataJSON() as { context_refs?: Array<{ module?: string; file?: string; symbol?: string; rationale?: string }> }).context_refs ?? [], repository_id: body.repository_id ?? "" };
      payload = specification;
    } else if (path === "/api/v1/work-items/item-1/specification/verifications" && request.method() === "POST") {
      const body = request.postDataJSON() as { kind?: string; field?: string; position?: number };
      if (body.kind === "acceptance_criterion" && body.position) specification.acceptance_criteria[body.position - 1].verification_status = "HUMAN_VERIFIED";
      if (body.kind === "field" && body.field && specification.fields[body.field]) specification.fields[body.field].verification_status = "HUMAN_VERIFIED";
      statusCode = 204;
    } else if (path === "/api/v1/work-items/item-1/specification/review" && request.method() === "POST") { reviewed = true; payload = specification; }
    else payload = { items: [] };
    await route.fulfill({ status: statusCode, json: statusCode === 204 ? undefined : payload });
  });

  const base = "/app/orgs/org-1/workspaces/workspace-1/projects/project-1";
  await page.goto(`${base}/repositories`);
  await expect(page.getByRole("heading", { name: "Ngữ cảnh repository" })).toBeVisible();
  await page.getByRole("button", { name: "Liên kết với dự án" }).click();
  await expect(page.getByText("forgeflow/web đã liên kết với dự án này.")).toBeVisible();
  await page.getByRole("button", { name: "Xem ngữ cảnh" }).click();
  await expect(page.getByText("Branch")).toBeVisible();

  await page.goto(`${base}/work-items/item-1`);
  await page.getByRole("button", { name: "Đặc tả" }).click();
  await expect(page.getByRole("heading", { name: "Trình chỉnh sửa đặc tả" })).toBeVisible();
  await page.getByLabel("Mục tiêu").fill("Keep project context visible while loading.");
  await page.getByRole("combobox", { name: "Ngữ cảnh mã nguồn" }).click();
  await page.getByRole("option", { name: "forgeflow/web", exact: true }).click();
  await page.getByRole("button", { name: "Thêm tiêu chí" }).click();
  await page.getByLabel("Tiêu chí 1").fill("The user sees a loading state before the backlog is usable.");
  await page.getByRole("button", { name: "Thêm thành phần" }).click();
  await expect(page.getByRole("button", { name: "Tải lại ngữ cảnh repository" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Tệp" })).toBeVisible();
  await page.getByRole("combobox", { name: "Mô-đun" }).click();
  await page.getByRole("option", { name: "apps/web", exact: true }).click();
  await page.getByRole("combobox", { name: "Tệp" }).click();
  await page.getByRole("option", { name: "apps/web/src/features/backlog/backlog-page.tsx", exact: true }).click();
  await page.getByRole("combobox", { name: "Ký hiệu" }).click();
  await page.getByRole("option", { name: /BacklogPage · function/ }).click();
  await page.getByRole("combobox", { name: "Commit" }).click();
  await page.getByRole("option", { name: /abc123 · Add backlog page/ }).click();
  await page.getByRole("combobox", { name: "Pull request" }).click();
  await page.getByRole("option", { name: /#7 Improve backlog loading/ }).click();
  await expect(page.getByRole("link", { name: "Mở tệp ↗" })).toHaveAttribute("href", /github\.com\/forgeflow\/web\/blob\/abc123/);
  await expect(page.getByRole("link", { name: "Mở commit ↗" })).toHaveAttribute("href", /github\.com\/forgeflow\/web\/commit\/abc123/);
  await expect(page.getByRole("link", { name: "Mở pull request ↗" })).toHaveAttribute("href", "https://github.com/forgeflow/web/pull/7");
  await page.getByRole("button", { name: "Lưu đặc tả" }).click();
  await expect(page.getByText(/Đã lưu đặc tả phiên bản 2/)).toBeVisible();
  await page.getByRole("button", { name: "Đánh dấu đã xác minh" }).last().click();
  await expect(page.getByText("Đã lưu xác minh thủ công.")).toBeVisible();
  await expect(page.getByRole("button", { name: /Review đặc tả/ })).not.toBeVisible();
  await page.getByRole("button", { name: "Lần chạy" }).click();
  await expect(page.getByRole("heading", { name: "Workflow autonomous" })).toBeVisible();
  await expect(page.getByText("Hãy liên kết repository với dự án và chọn nó trong phần Đặc tả trước khi tạo AgentRun.", { exact: true })).not.toBeVisible();
});
