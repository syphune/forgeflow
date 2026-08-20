import type { components } from "./generated";

export * from "./generated";

export type CreateWorkItemRequest = components["schemas"]["CreateWorkItemRequest"];
export type UpdateWorkItemRequest = components["schemas"]["UpdateWorkItemRequest"];
export type TransitionRequest = components["schemas"]["TransitionRequest"];
export type AssignmentRequest = components["schemas"]["AssignmentRequest"];
export type WorkItem = components["schemas"]["WorkItemResponse"];
export type Comment = components["schemas"]["Comment"];
export type CommentRequest = components["schemas"]["CommentRequest"];
export type ErrorResponse = components["schemas"]["ErrorResponse"];
export type Actor = components["schemas"]["Actor"];
export type AuthorizationContext = components["schemas"]["AuthorizationContext"];
export type Member = components["schemas"]["Member"];
export type Organization = components["schemas"]["Organization"];
export type Workspace = components["schemas"]["Workspace"];
export type Project = components["schemas"]["Project"];
export type WorkflowResponse = components["schemas"]["WorkflowResponse"];
export type WorkflowTransition = components["schemas"]["WorkflowTransition"];
export type SpecificationResponse = components["schemas"]["SpecificationResponse"];
export type Specification = components["schemas"]["Specification"];
export type SpecificationUpdateRequest = components["schemas"]["SpecificationUpdateRequest"];
export type SpecificationReviewRequest = components["schemas"]["SpecificationReviewRequest"];
export type VerificationRequest = components["schemas"]["VerificationRequest"];
export type GitHubRepository = components["schemas"]["GitHubRepository"];
export type GitHubInstallation = components["schemas"]["GitHubInstallation"];
export type RepositoryContext = components["schemas"]["RepositoryContext"];
export type RepositoryCommit = components["schemas"]["RepositoryCommit"];
export type RepositoryTreeEntry = components["schemas"]["RepositoryTreeEntry"];
export type RepositoryFile = components["schemas"]["RepositoryFile"];
export type RepositorySearchMatch = components["schemas"]["RepositorySearchMatch"];
export type RepositorySnapshot = components["schemas"]["RepositorySnapshot"];
export type SnapshotSymbol = components["schemas"]["SnapshotSymbol"];
export type ReproductionStep = components["schemas"]["ReproductionStep"];
export type AcceptanceCriterion = components["schemas"]["AcceptanceCriterion"];
export type Attachment = components["schemas"]["Attachment"];
export type MoveWorkItemRequest = components["schemas"]["MoveWorkItemRequest"];
export type MoveWorkItemResponse = components["schemas"]["MoveWorkItemResponse"];
export type Notification = components["schemas"]["Notification"];
export type AgentRun = components["schemas"]["AgentRun"];
export type AgentRunStatus = components["schemas"]["AgentRunStatus"];
export type AgentRunExecutionInputs = components["schemas"]["AgentRunExecutionInputs"];
export type CreateAgentRunRequest = components["schemas"]["CreateAgentRunRequest"];
export type AgentRunTestResultsRequest = components["schemas"]["AgentRunTestResultsRequest"];
export type AutonomousPolicy = components["schemas"]["AutonomousPolicy"];
export type AutonomousRun = components["schemas"]["AutonomousRun"];
export type AutonomousStartRequest = components["schemas"]["AutonomousStartRequest"];
export type AutonomousRetryRequest = components["schemas"]["AutonomousRetryRequest"];
export type AutonomousFeedbackRequest = components["schemas"]["AutonomousFeedbackRequest"];
export type AutonomousFeedback = components["schemas"]["AutonomousFeedback"];
export type Environment = components["schemas"]["Environment"];
export type CreateEnvironmentRequest = components["schemas"]["CreateEnvironmentRequest"];
export type DeploymentRequest = components["schemas"]["DeploymentRequest"];
export type CreateDeploymentRequest = components["schemas"]["CreateDeploymentRequest"];
export type DeploymentStatusRequest = components["schemas"]["DeploymentStatusRequest"];
export type Sprint = components["schemas"]["Sprint"];
export type CreateSprintRequest = components["schemas"]["CreateSprintRequest"];
export type CustomFieldDefinition = components["schemas"]["CustomFieldDefinition"];
export type CreateCustomFieldRequest = components["schemas"]["CreateCustomFieldRequest"];
export type UpdateCustomFieldRequest = components["schemas"]["UpdateCustomFieldRequest"];
export type AutomationRule = components["schemas"]["AutomationRule"];
export type CreateAutomationRuleRequest = components["schemas"]["CreateAutomationRuleRequest"];
export type PersonalAccessToken = components["schemas"]["PersonalAccessToken"];
export type CreateTokenRequest = components["schemas"]["CreateTokenRequest"];
export type CreateTenantRequest = components["schemas"]["CreateTenantRequest"];
export type RenameRequest = components["schemas"]["RenameRequest"];
export type MemberRoleRequest = components["schemas"]["MemberRoleRequest"];
export type OrganizationMemberRequest = components["schemas"]["OrganizationMemberRequest"];
export type WorkItemType = CreateWorkItemRequest["type"];

export type WorkItemList = { items: WorkItem[]; next_cursor?: string | null };
export type BoardColumn = { status: string; name: string; position: number; ordering_version: number; items: WorkItem[] };
export type BoardResponse = { project_id: string; columns: BoardColumn[]; truncated: boolean };

export type APIClientOptions = {
  baseURL?: string;
  fetch?: typeof globalThis.fetch;
  credentials?: RequestCredentials;
  headers?: HeadersInit;
  organizationID?: string;
  projectID?: string;
  getCSRFToken?: () => string | undefined;
  getRequestID?: () => string | undefined;
};

export type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
  organizationID?: string;
  projectID?: string;
  idempotencyKey?: string;
  expectedVersion?: number;
};

export class APIError extends Error {
  constructor(message: string, readonly status: number, readonly body: ErrorResponse | null) {
    super(message);
    this.name = "APIError";
  }
}

export function createAPIClient(options: string | APIClientOptions = {}): APIClient {
  return new APIClient(typeof options === "string" ? { baseURL: options } : options);
}

export class APIClient {
  private readonly baseURL: string;
  private readonly fetcher: typeof globalThis.fetch;
  private readonly options: APIClientOptions;

  constructor(options: APIClientOptions = {}) {
    this.options = options;
    this.baseURL = (options.baseURL ?? "/api/v1").replace(/\/$/, "");
    this.fetcher = options.fetch ?? globalThis.fetch.bind(globalThis);
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { body, organizationID: requestOrganizationID, projectID: requestProjectID, idempotencyKey, expectedVersion, ...init } = options;
    const organizationID = requestOrganizationID ?? this.options.organizationID;
    const projectID = requestProjectID ?? this.options.projectID;
    const headers = this.requestHeaders(init.headers, organizationID, projectID, body !== undefined);
    if (idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);
    if (expectedVersion !== undefined && body === undefined) headers.set("If-Match", String(expectedVersion));
    const response = await this.fetcher(`${this.baseURL}${path}`, {
      ...init,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: init.credentials ?? this.options.credentials ?? "include",
      headers,
    });
    return decode<T>(response);
  }

  private requestHeaders(initHeaders: HeadersInit | undefined, organizationID?: string, projectID?: string, json = false): Headers {
    const headers = new Headers(initHeaders);
    for (const [name, value] of new Headers(this.options.headers)) headers.set(name, value);
    if (json) headers.set("content-type", "application/json");
    if (organizationID) headers.set("X-Organization-ID", organizationID);
    if (projectID) headers.set("X-Project-ID", projectID);
    const csrfToken = this.options.getCSRFToken?.();
    if (csrfToken) headers.set("X-CSRF-Token", csrfToken);
    const requestID = this.options.getRequestID?.();
    if (requestID) headers.set("X-Request-ID", requestID);
    return headers;
  }

  getWorkItem(id: string, projectID?: string): Promise<WorkItem> {
    return this.request<WorkItem>(`/work-items/${encodeURIComponent(id)}`, { projectID });
  }

  listWorkItems(params: {
    projectID?: string;
    status?: string;
    type?: WorkItemType;
    priority?: string;
    assigneeID?: string;
    sprintID?: string;
    repositoryID?: string;
    query?: string;
    cursor?: string;
    limit?: number;
    includeArchived?: boolean;
    sort?: "updated" | "backlog";
  } = {}): Promise<WorkItemList> {
    const query = new URLSearchParams();
    const values: Record<string, string | number | boolean | undefined> = {
      status: params.status,
      type: params.type,
      priority: params.priority,
      assignee_id: params.assigneeID,
      sprint_id: params.sprintID,
      repository_id: params.repositoryID,
      q: params.query,
      cursor: params.cursor,
      limit: params.limit,
      include_archived: params.includeArchived,
      sort: params.sort,
    };
    for (const [key, value] of Object.entries(values)) {
      if (value !== undefined && value !== "") query.set(key, String(value));
    }
    const suffix = query.toString() ? `?${query}` : "";
    return this.request<WorkItemList>(`/work-items${suffix}`, { projectID: params.projectID });
  }

  getMe(): Promise<Actor> {
    return this.request<Actor>("/me");
  }

  listOrganizations(): Promise<{ items: Organization[] }> {
    return this.request<{ items: Organization[] }>("/organizations");
  }

  listWorkspaces(): Promise<{ items: Workspace[] }> {
    return this.request<{ items: Workspace[] }>("/workspaces");
  }

  listProjects(workspaceID?: string): Promise<{ items: Project[] }> {
    const query = workspaceID ? `?workspace_id=${encodeURIComponent(workspaceID)}` : "";
    return this.request<{ items: Project[] }>(`/projects${query}`);
  }

  getOrganizationAuthorization(id: string): Promise<AuthorizationContext> {
    return this.request<AuthorizationContext>(`/organizations/${encodeURIComponent(id)}/authorization`);
  }

  getWorkspaceAuthorization(id: string): Promise<AuthorizationContext> {
    return this.request<AuthorizationContext>(`/workspaces/${encodeURIComponent(id)}/authorization`);
  }

  getProjectAuthorization(id: string): Promise<AuthorizationContext> {
    return this.request<AuthorizationContext>(`/projects/${encodeURIComponent(id)}/authorization`, { projectID: id });
  }

  listProjectMembers(projectID: string): Promise<{ items: Member[] }> {
    return this.request<{ items: Member[] }>(`/projects/${encodeURIComponent(projectID)}/members`, { projectID });
  }

  getWorkflow(projectID?: string): Promise<WorkflowResponse> {
    return this.request<WorkflowResponse>("/workflows/current", { projectID });
  }

  getBoard(projectID?: string): Promise<BoardResponse> {
    return this.request<BoardResponse>("/boards/current", { projectID });
  }

  getSpecification(id: string, projectID?: string): Promise<SpecificationResponse> {
    return this.request<SpecificationResponse>(`/work-items/${encodeURIComponent(id)}/specification`, { projectID });
  }

  updateSpecification(id: string, input: SpecificationUpdateRequest, projectID?: string): Promise<Specification> {
    return this.request<Specification>(`/work-items/${encodeURIComponent(id)}/specification`, { method: "PATCH", projectID, body: input });
  }

  reviewSpecification(id: string, input: SpecificationReviewRequest, projectID?: string): Promise<Specification> {
    return this.request<Specification>(`/work-items/${encodeURIComponent(id)}/specification/review`, { method: "POST", projectID, body: input });
  }

  verifySpecification(id: string, input: VerificationRequest, projectID?: string): Promise<void> {
    return this.request<void>(`/work-items/${encodeURIComponent(id)}/specification/verifications`, { method: "POST", projectID, body: input });
  }

  listWorkItemAttachments(id: string, projectID?: string): Promise<{ items: Attachment[] }> {
    return this.request<{ items: Attachment[] }>(`/work-items/${encodeURIComponent(id)}/attachments`, { projectID });
  }

  uploadWorkItemAttachment(id: string, file: File, projectID?: string): Promise<Attachment> {
    const form = new FormData();
    form.append("file", file, file.name);
    const organizationID = this.options.organizationID;
    const scopedProjectID = projectID ?? this.options.projectID;
    return this.fetcher(`${this.baseURL}/work-items/${encodeURIComponent(id)}/attachments`, { method: "POST", body: form, credentials: this.options.credentials ?? "include", headers: this.requestHeaders(undefined, organizationID, scopedProjectID) }).then((response) => decode<Attachment>(response));
  }

  downloadWorkItemAttachment(id: string, attachmentID: string, projectID?: string): Promise<Blob> {
    const organizationID = this.options.organizationID;
    const scopedProjectID = projectID ?? this.options.projectID;
    return this.fetcher(`${this.baseURL}/work-items/${encodeURIComponent(id)}/attachments/${encodeURIComponent(attachmentID)}`, { credentials: this.options.credentials ?? "include", headers: this.requestHeaders(undefined, organizationID, scopedProjectID) }).then(async (response) => {
      if (!response.ok) {
        await decode<void>(response);
      }
      return response.blob();
    });
  }

  deleteWorkItemAttachment(id: string, attachmentID: string, projectID?: string): Promise<void> {
    return this.request<void>(`/work-items/${encodeURIComponent(id)}/attachments/${encodeURIComponent(attachmentID)}`, { method: "DELETE", projectID });
  }

  moveWorkItem(id: string, input: MoveWorkItemRequest, projectID?: string): Promise<MoveWorkItemResponse> {
    return this.request<MoveWorkItemResponse>(`/work-items/${encodeURIComponent(id)}/move`, { method: "POST", projectID, body: input });
  }

  listNotifications(limit = 50): Promise<{ items: Notification[] }> {
    return this.request<{ items: Notification[] }>(`/notifications?limit=${Math.min(Math.max(limit, 1), 100)}`);
  }

  getUnreadNotificationCount(): Promise<{ unread_count: number }> {
    return this.request<{ unread_count: number }>("/notifications/unread-count");
  }

  markNotificationRead(id: string): Promise<void> {
    return this.request<void>(`/notifications/${encodeURIComponent(id)}/read`, { method: "POST" });
  }

  markAllNotificationsRead(): Promise<void> {
    return this.request<void>("/notifications/read-all", { method: "POST" });
  }

  listSprints(projectID: string): Promise<{ items: Sprint[] }> {
    return this.request<{ items: Sprint[] }>("/sprints", { projectID });
  }

  createSprint(input: CreateSprintRequest, projectID: string): Promise<Sprint> {
    return this.request<Sprint>("/sprints", { method: "POST", projectID, body: input });
  }

  updateSprint(id: string, input: Omit<CreateSprintRequest, "project_id">, projectID: string): Promise<Sprint> {
    return this.request<Sprint>(`/sprints/${encodeURIComponent(id)}`, { method: "PATCH", projectID, body: input });
  }

  deleteSprint(id: string, projectID: string): Promise<void> {
    return this.request<void>(`/sprints/${encodeURIComponent(id)}`, { method: "DELETE", projectID });
  }

  startSprint(id: string, projectID: string): Promise<Sprint> {
    return this.request<Sprint>(`/sprints/${encodeURIComponent(id)}/start`, { method: "POST", projectID });
  }

  completeSprint(id: string, projectID: string): Promise<Sprint> {
    return this.request<Sprint>(`/sprints/${encodeURIComponent(id)}/complete`, { method: "POST", projectID });
  }

  listCustomFields(projectID: string): Promise<{ items: CustomFieldDefinition[] }> {
    return this.request<{ items: CustomFieldDefinition[] }>(`/projects/${encodeURIComponent(projectID)}/custom-fields`, { projectID });
  }

  createCustomField(input: CreateCustomFieldRequest, projectID: string): Promise<CustomFieldDefinition> {
    return this.request<CustomFieldDefinition>(`/projects/${encodeURIComponent(projectID)}/custom-fields`, { method: "POST", projectID, body: input });
  }

  updateCustomField(id: string, input: UpdateCustomFieldRequest, projectID: string): Promise<CustomFieldDefinition> {
    return this.request<CustomFieldDefinition>(`/projects/${encodeURIComponent(projectID)}/custom-fields/${encodeURIComponent(id)}`, { method: "PATCH", projectID, body: input });
  }

  deleteCustomField(id: string, projectID: string): Promise<void> {
    return this.request<void>(`/projects/${encodeURIComponent(projectID)}/custom-fields/${encodeURIComponent(id)}`, { method: "DELETE", projectID });
  }

  listAutomationRules(projectID: string): Promise<{ items: AutomationRule[] }> {
    return this.request<{ items: AutomationRule[] }>(`/projects/${encodeURIComponent(projectID)}/automation-rules`, { projectID });
  }

  createAutomationRule(input: CreateAutomationRuleRequest, projectID: string): Promise<AutomationRule> {
    return this.request<AutomationRule>(`/projects/${encodeURIComponent(projectID)}/automation-rules`, { method: "POST", projectID, body: input });
  }

  toggleAutomationRule(id: string, enabled: boolean, projectID: string): Promise<AutomationRule> {
    return this.request<AutomationRule>(`/projects/${encodeURIComponent(projectID)}/automation-rules/${encodeURIComponent(id)}`, { method: "PATCH", projectID, body: { enabled } });
  }

  deleteAutomationRule(id: string, projectID: string): Promise<void> {
    return this.request<void>(`/projects/${encodeURIComponent(projectID)}/automation-rules/${encodeURIComponent(id)}`, { method: "DELETE", projectID });
  }

  listPersonalAccessTokens(): Promise<{ items: PersonalAccessToken[] }> {
    return this.request<{ items: PersonalAccessToken[] }>("/me/tokens");
  }

  createPersonalAccessToken(input: CreateTokenRequest): Promise<PersonalAccessToken> {
    return this.request<PersonalAccessToken>("/me/tokens", { method: "POST", body: input });
  }

  revokePersonalAccessToken(id: string): Promise<void> {
    return this.request<void>(`/me/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  listOrganizationMembers(): Promise<{ items: Member[] }> {
    return this.request<{ items: Member[] }>("/organizations/current/members");
  }

  addOrganizationMember(input: OrganizationMemberRequest): Promise<Member> {
    return this.request<Member>("/organizations/current/members", { method: "POST", body: input });
  }

  setOrganizationMemberRole(id: string, input: MemberRoleRequest): Promise<Member> {
    return this.request<Member>(`/organizations/current/members/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  }

  removeOrganizationMember(id: string): Promise<void> {
    return this.request<void>(`/organizations/current/members/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  updateWorkspace(id: string, input: RenameRequest): Promise<Workspace> {
    return this.request<Workspace>(`/workspaces/${encodeURIComponent(id)}`, { method: "PATCH", body: input });
  }

  updateProject(id: string, input: RenameRequest): Promise<Project> {
    return this.request<Project>(`/projects/${encodeURIComponent(id)}`, { method: "PATCH", body: input });
  }

  listAgentRuns(projectID: string, workItemID?: string): Promise<{ items: AgentRun[] }> {
    const query = workItemID ? `?work_item_id=${encodeURIComponent(workItemID)}` : "";
    return this.request<{ items: AgentRun[] }>(`/agent-runs${query}`, { projectID });
  }

  createAgentRun(input: CreateAgentRunRequest, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>("/agent-runs", { method: "POST", projectID, body: input });
  }

  recordAgentRunTestResults(id: string, input: AgentRunTestResultsRequest, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/test-results`, { method: "POST", projectID, body: input });
  }

  getAgentRun(id: string, projectID?: string): Promise<{ run: AgentRun; steps: components["schemas"]["AgentRunStep"][]; artifacts: components["schemas"]["AgentRunArtifact"][] }> {
    return this.request(`/agent-runs/${encodeURIComponent(id)}`, { projectID });
  }

  approveAgentRun(id: string, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/approve`, { method: "POST", projectID });
  }

  startAgentRun(id: string, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/start`, { method: "POST", projectID });
  }

  resumeAgentRun(id: string, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/resume`, { method: "POST", projectID });
  }

  heartbeatAgentRun(id: string, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/heartbeat`, { method: "POST", projectID });
  }

  transitionAgentRun(id: string, status: AgentRunStatus, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/transition`, { method: "POST", projectID, body: { status } });
  }

  cancelAgentRun(id: string, projectID?: string): Promise<AgentRun> {
    return this.request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/cancel`, { method: "POST", projectID });
  }

  listAutonomousRuns(projectID: string, workItemID?: string): Promise<{ items: AutonomousRun[] }> {
    const query = workItemID ? `?work_item_id=${encodeURIComponent(workItemID)}` : "";
    return this.request<{ items: AutonomousRun[] }>(`/autonomous-runs${query}`, { projectID });
  }

  startAutonomousRun(input: AutonomousStartRequest, projectID?: string): Promise<AutonomousRun> {
    return this.request<AutonomousRun>("/autonomous-runs", { method: "POST", projectID, body: input });
  }

  getAutonomousRun(id: string, projectID?: string): Promise<{ run: AutonomousRun; feedback: AutonomousFeedback[] }> {
    return this.request(`/autonomous-runs/${encodeURIComponent(id)}`, { projectID });
  }

  resumeAutonomousRun(id: string, projectID?: string): Promise<AutonomousRun> {
    return this.request<AutonomousRun>(`/autonomous-runs/${encodeURIComponent(id)}/resume`, { method: "POST", projectID });
  }

  retryAutonomousRun(id: string, input: AutonomousRetryRequest = {}, projectID?: string): Promise<AutonomousRun> {
    return this.request<AutonomousRun>(`/autonomous-runs/${encodeURIComponent(id)}/retry`, { method: "POST", projectID, body: input });
  }

  cancelAutonomousRun(id: string, projectID?: string): Promise<AutonomousRun> {
    return this.request<AutonomousRun>(`/autonomous-runs/${encodeURIComponent(id)}/cancel`, { method: "POST", projectID });
  }

  addAutonomousFeedback(id: string, input: AutonomousFeedbackRequest, projectID?: string): Promise<AutonomousFeedback> {
    return this.request<AutonomousFeedback>(`/autonomous-runs/${encodeURIComponent(id)}/feedback`, { method: "POST", projectID, body: input });
  }

  recordAutonomousTestResults(id: string, input: AgentRunTestResultsRequest, projectID?: string): Promise<AutonomousRun> {
    return this.request<AutonomousRun>(`/autonomous-runs/${encodeURIComponent(id)}/test-results`, { method: "POST", projectID, body: input });
  }

  getProjectAIPolicy(projectID: string): Promise<AutonomousPolicy> {
    return this.request<AutonomousPolicy>(`/projects/${encodeURIComponent(projectID)}/ai-policy`, { projectID });
  }

  updateProjectAIPolicy(projectID: string, input: AutonomousPolicy): Promise<AutonomousPolicy> {
    return this.request<AutonomousPolicy>(`/projects/${encodeURIComponent(projectID)}/ai-policy`, { method: "PUT", projectID, body: input });
  }

  listProjectEnvironments(projectID: string): Promise<{ items: Environment[] }> {
    return this.request<{ items: Environment[] }>(`/projects/${encodeURIComponent(projectID)}/environments`, { projectID });
  }

  createProjectEnvironment(projectID: string, input: CreateEnvironmentRequest): Promise<Environment> {
    return this.request<Environment>(`/projects/${encodeURIComponent(projectID)}/environments`, { method: "POST", projectID, body: input });
  }

  requestDeployment(input: CreateDeploymentRequest, projectID?: string): Promise<DeploymentRequest> {
    return this.request<DeploymentRequest>("/deployments", { method: "POST", projectID, body: input });
  }

  getDeployment(id: string, projectID?: string): Promise<DeploymentRequest> {
    return this.request<DeploymentRequest>(`/deployments/${encodeURIComponent(id)}`, { projectID });
  }

  approveDeployment(id: string, projectID?: string): Promise<DeploymentRequest> {
    return this.request<DeploymentRequest>(`/deployments/${encodeURIComponent(id)}/approve`, { method: "POST", projectID });
  }

  updateDeploymentStatus(id: string, input: DeploymentStatusRequest, projectID?: string): Promise<DeploymentRequest> {
    return this.request<DeploymentRequest>(`/deployments/${encodeURIComponent(id)}/status`, { method: "POST", projectID, body: input });
  }

  createWorkItem(input: CreateWorkItemRequest, options: Pick<RequestOptions, "projectID" | "idempotencyKey"> = {}): Promise<WorkItem> {
    return this.request<WorkItem>("/work-items", { method: "POST", ...options, body: input });
  }

  updateWorkItem(id: string, input: UpdateWorkItemRequest, projectID?: string): Promise<WorkItem> {
    return this.request<WorkItem>(`/work-items/${encodeURIComponent(id)}`, { method: "PATCH", projectID, body: input });
  }

  transitionWorkItem(id: string, input: TransitionRequest, projectID?: string): Promise<WorkItem> {
    return this.request<WorkItem>(`/work-items/${encodeURIComponent(id)}/transitions`, { method: "POST", projectID, body: input });
  }

  assignWorkItem(id: string, input: AssignmentRequest, projectID?: string): Promise<WorkItem> {
    return this.request<WorkItem>(`/work-items/${encodeURIComponent(id)}/assignments`, { method: "POST", projectID, body: input });
  }

  listComments(id: string, projectID?: string): Promise<{ items: Comment[] }> {
    return this.request<{ items: Comment[] }>(`/work-items/${encodeURIComponent(id)}/comments`, { projectID });
  }

  createComment(id: string, input: CommentRequest, projectID?: string): Promise<Comment> {
    return this.request<Comment>(`/work-items/${encodeURIComponent(id)}/comments`, { method: "POST", projectID, body: input });
  }

  updateComment(id: string, commentID: string, input: CommentRequest, projectID?: string): Promise<Comment> {
    return this.request<Comment>(`/work-items/${encodeURIComponent(id)}/comments/${encodeURIComponent(commentID)}`, { method: "PATCH", projectID, body: input });
  }

  deleteComment(id: string, commentID: string, projectID?: string): Promise<Comment> {
    return this.request<Comment>(`/work-items/${encodeURIComponent(id)}/comments/${encodeURIComponent(commentID)}`, { method: "DELETE", projectID });
  }

  listGitHubRepositories(projectID: string): Promise<{ items: GitHubRepository[] }> {
    return this.request<{ items: GitHubRepository[] }>(`/integrations/github/repositories?project_id=${encodeURIComponent(projectID)}`, { projectID });
  }

  listGitHubInstallations(): Promise<{ items: GitHubInstallation[] }> {
    return this.request<{ items: GitHubInstallation[] }>("/integrations/github/installations");
  }

  listProjectRepositories(projectID: string): Promise<{ items: GitHubRepository[] }> {
    return this.request<{ items: GitHubRepository[] }>(`/projects/${encodeURIComponent(projectID)}/repositories`, { projectID });
  }

  linkProjectRepository(projectID: string, repositoryID: string): Promise<void> {
    return this.request<void>(`/projects/${encodeURIComponent(projectID)}/repositories`, { method: "POST", projectID, body: { repository_id: repositoryID } });
  }

  unlinkProjectRepository(projectID: string, repositoryID: string): Promise<void> {
    return this.request<void>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}`, { method: "DELETE", projectID });
  }

  getRepositoryContext(projectID: string, repositoryID: string): Promise<RepositoryContext> {
    return this.request<RepositoryContext>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/context`, { projectID });
  }

  listRepositoryTree(projectID: string, repositoryID: string): Promise<{ items: RepositoryTreeEntry[] }> {
    return this.request<{ items: RepositoryTreeEntry[] }>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/tree`, { projectID });
  }

  getRepositoryFile(projectID: string, repositoryID: string, path: string): Promise<RepositoryFile> {
    return this.request<RepositoryFile>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/file?path=${encodeURIComponent(path)}`, { projectID });
  }

  listRepositorySnapshots(projectID: string, repositoryID: string, limit = 10): Promise<{ items: RepositorySnapshot[] }> {
    return this.request<{ items: RepositorySnapshot[] }>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/snapshots?limit=${Math.min(Math.max(limit, 1), 50)}`, { projectID });
  }

  refreshRepositorySnapshot(projectID: string, repositoryID: string): Promise<RepositorySnapshot> {
    return this.request<RepositorySnapshot>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/snapshots/refresh`, { method: "POST", projectID });
  }

  listRepositorySnapshotSymbols(projectID: string, repositoryID: string, snapshotID: string, options: { name?: string; limit?: number } = {}): Promise<{ items: SnapshotSymbol[] }> {
    const query = new URLSearchParams();
    if (options.name?.trim()) query.set("name", options.name.trim());
    query.set("limit", String(Math.min(Math.max(options.limit ?? 100, 1), 100)));
    return this.request<{ items: SnapshotSymbol[] }>(`/projects/${encodeURIComponent(projectID)}/repositories/${encodeURIComponent(repositoryID)}/snapshots/${encodeURIComponent(snapshotID)}/symbols?${query.toString()}`, { projectID });
  }
}

async function decode<T>(response: Response): Promise<T> {
  const text = await response.text();
  if (response.status === 204) return undefined as T;
  let body: T | ErrorResponse | null = null;
  if (text) {
    try {
      body = JSON.parse(text) as T | ErrorResponse;
    } catch {
      body = null;
    }
  }
  if (!response.ok) {
    const error = body && typeof body === "object" && "message" in body ? (body as ErrorResponse) : null;
    throw new APIError(error?.message ?? `Forgeflow API request failed (${response.status})`, response.status, error);
  }
  return body as T;
}
