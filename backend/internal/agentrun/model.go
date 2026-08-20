package agentrun

import "time"

type Status string

const (
	Interrupted   Status = "INTERRUPTED"
	Queued        Status = "QUEUED"
	Preparing     Status = "PREPARING"
	Planning      Status = "PLANNING"
	Investigating Status = "INVESTIGATING"
	Implementing  Status = "IMPLEMENTING"
	Testing       Status = "TESTING"
	Reviewing     Status = "REVIEWING"
	Completed     Status = "COMPLETED"
	Failed        Status = "FAILED"
	Cancelled     Status = "CANCELLED"
)

const (
	ApprovalFingerprintVersion = 1
	HeartbeatInterval          = 15 * time.Second
	HeartbeatTimeout           = 60 * time.Second
	ProcessingGrace            = 15 * time.Second
	HeartbeatDeadline          = HeartbeatTimeout + ProcessingGrace
)

type ExecutionInputs struct {
	Prompt               string         `json:"prompt,omitempty"`
	WorktreeDiffHash     string         `json:"worktree_diff_hash,omitempty"`
	SpecificationVersion int            `json:"specification_version,omitempty"`
	TestCasePositions    []int          `json:"test_case_positions,omitempty"`
	AgentConfiguration   map[string]any `json:"agent_configuration,omitempty"`
	ToolPermissions      []string       `json:"tool_permissions,omitempty"`
	MCPPermissions       []string       `json:"mcp_permissions,omitempty"`
	SandboxPolicy        map[string]any `json:"sandbox_policy,omitempty"`
	NetworkPolicy        map[string]any `json:"network_policy,omitempty"`
	ExecutionProfile     string         `json:"execution_profile,omitempty"`
}

type Run struct {
	ID                         string          `json:"id"`
	OrganizationID             string          `json:"organization_id"`
	ProjectID                  string          `json:"project_id"`
	WorkItemID                 string          `json:"work_item_id"`
	RepositoryID               string          `json:"repository_id,omitempty"`
	AgentProvider              string          `json:"agent_provider"`
	AgentName                  string          `json:"agent_name"`
	Model                      string          `json:"model"`
	BaseSHA                    string          `json:"base_sha"`
	Branch                     string          `json:"branch"`
	ExecutionInputs            ExecutionInputs `json:"execution_inputs"`
	ExecutionPolicy            map[string]any  `json:"execution_policy,omitempty"`
	ApprovalFingerprintVersion int             `json:"approval_fingerprint_version"`
	ApprovalFingerprint        string          `json:"approval_fingerprint,omitempty"`
	Status                     Status          `json:"status"`
	Approved                   bool            `json:"approved"`
	StartedAt                  *time.Time      `json:"started_at,omitempty"`
	FinishedAt                 *time.Time      `json:"finished_at,omitempty"`
	CommitSHA                  string          `json:"commit_sha,omitempty"`
	PullRequestID              string          `json:"pull_request_id,omitempty"`
	Result                     map[string]any  `json:"result,omitempty"`
	Error                      string          `json:"error,omitempty"`
	Metadata                   map[string]any  `json:"metadata,omitempty"`
	CreatedAt                  time.Time       `json:"created_at"`
	LastHeartbeatAt            *time.Time      `json:"last_heartbeat_at,omitempty"`
	InterruptionReason         string          `json:"interruption_reason,omitempty"`
}

type CreateInput struct {
	ProjectID       string
	WorkItemID      string
	RepositoryID    string
	AgentProvider   string
	AgentName       string
	Model           string
	BaseSHA         string
	Branch          string
	ExecutionInputs ExecutionInputs
	ExecutionPolicy map[string]any
}

type ResultInput struct {
	CommitSHA     string
	PullRequestID string
	Result        map[string]any
	Error         *string
	Metadata      map[string]any
}

type TestCaseStatus string

const (
	TestNotRun  TestCaseStatus = "NOT_RUN"
	TestPassed  TestCaseStatus = "PASS"
	TestFailed  TestCaseStatus = "FAIL"
	TestBlocked TestCaseStatus = "BLOCKED"
)

type TestCaseResultInput struct {
	Position     int            `json:"position"`
	Status       TestCaseStatus `json:"status"`
	Note         string         `json:"note,omitempty"`
	EvidenceRefs []string       `json:"evidence_refs,omitempty"`
}

type TestCaseResult struct {
	Position     int            `json:"position"`
	Status       TestCaseStatus `json:"status"`
	Note         string         `json:"note,omitempty"`
	EvidenceRefs []string       `json:"evidence_refs,omitempty"`
	UpdatedBy    string         `json:"updated_by,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type TestResultsInput struct {
	Cases      []TestCaseResultInput `json:"test_cases"`
	ReviewNote string                `json:"review_note,omitempty"`
}

type TestResultSet struct {
	Cases      []TestCaseResult `json:"test_cases"`
	ReviewNote string           `json:"review_note,omitempty"`
}

type Step struct {
	ID            string         `json:"id"`
	RunID         string         `json:"run_id"`
	Sequence      int            `json:"sequence"`
	Phase         string         `json:"phase"`
	Status        string         `json:"status"`
	Summary       string         `json:"summary"`
	FilesRead     int            `json:"files_read"`
	FilesModified int            `json:"files_modified"`
	Commands      []string       `json:"commands"`
	Tests         []string       `json:"tests"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
}

type Artifact struct {
	ID           string         `json:"id"`
	RunID        string         `json:"run_id"`
	ArtifactType string         `json:"artifact_type"`
	Name         string         `json:"name"`
	ContentHash  string         `json:"content_hash"`
	SizeBytes    int64          `json:"size_bytes"`
	ObjectKey    string         `json:"object_key,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}
