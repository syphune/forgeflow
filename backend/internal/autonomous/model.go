package autonomous

import (
	"strings"
	"time"
)

type Status string

const (
	StatusQueued               Status = "QUEUED"
	StatusIntake               Status = "INTAKE"
	StatusWaitingSpecReview    Status = "WAITING_SPEC_REVIEW"
	StatusReadyForExecution    Status = "READY_FOR_EXECUTION"
	StatusExecuting            Status = "EXECUTING"
	StatusWaitingPRReview      Status = "WAITING_PR_REVIEW"
	StatusWaitingTestFeedback  Status = "WAITING_TEST_FEEDBACK"
	StatusFixing               Status = "FIXING"
	StatusWaitingDeployApprove Status = "WAITING_DEPLOY_APPROVAL"
	StatusDeploying            Status = "DEPLOYING"
	StatusCompleted            Status = "COMPLETED"
	StatusPaused               Status = "PAUSED"
	StatusFailed               Status = "FAILED"
	StatusCancelled            Status = "CANCELLED"
)

type Phase string

const (
	PhaseIntake         Phase = "INTAKE"
	PhaseSpecification  Phase = "SPECIFICATION"
	PhaseImplementation Phase = "IMPLEMENTATION"
	PhaseTesting        Phase = "TESTING"
	PhasePullRequest    Phase = "PULL_REQUEST"
	PhaseDeployment     Phase = "DEPLOYMENT"
)

type Gate string

const (
	GateNone              Gate = ""
	GateSpecification     Gate = "SPECIFICATION_REVIEW"
	GateRunner            Gate = "RUNNER_AVAILABLE"
	GatePullRequest       Gate = "PULL_REQUEST_REVIEW"
	GateTestFeedback      Gate = "TEST_FEEDBACK"
	GateDeployment        Gate = "DEPLOYMENT_APPROVAL"
	GateHumanVerification Gate = "HUMAN_VERIFICATION"
	GateClarification     Gate = "CLARIFICATION"
)

type Policy struct {
	Enabled          bool           `json:"enabled"`
	Providers        []string       `json:"providers,omitempty"`
	Runtime          string         `json:"runtime,omitempty"`
	MaxAttempts      int            `json:"max_attempts,omitempty"`
	TimeoutSeconds   int            `json:"timeout_seconds,omitempty"`
	AutoRetry        bool           `json:"auto_retry"`
	AutoCreatePR     bool           `json:"auto_create_pr"`
	TestScope        string         `json:"test_scope,omitempty"`
	NetworkPolicy    map[string]any `json:"network_policy,omitempty"`
	MCPPermissions   []string       `json:"mcp_permissions,omitempty"`
	ExecutionProfile string         `json:"execution_profile,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{
		Enabled:          true,
		Providers:        []string{"codex", "claude"},
		Runtime:          "server",
		MaxAttempts:      3,
		TimeoutSeconds:   3600,
		AutoRetry:        true,
		AutoCreatePR:     true,
		TestScope:        "unresolved_only",
		MCPPermissions:   []string{"work_item.get_context", "agent_run.get", "agent_run.record_test_results", "agent_run.attach_result"},
		ExecutionProfile: "default",
	}
}

func (p Policy) Normalize() Policy {
	defaults := DefaultPolicy()
	if p.MaxAttempts <= 0 || p.MaxAttempts > 10 {
		p.MaxAttempts = defaults.MaxAttempts
	}
	if p.TimeoutSeconds <= 0 || p.TimeoutSeconds > 24*60*60 {
		p.TimeoutSeconds = defaults.TimeoutSeconds
	}
	p.Runtime = strings.ToLower(strings.TrimSpace(p.Runtime))
	if p.Runtime == "" {
		p.Runtime = defaults.Runtime
	}
	p.TestScope = strings.ToLower(strings.TrimSpace(p.TestScope))
	if p.TestScope == "" {
		p.TestScope = defaults.TestScope
	}
	p.ExecutionProfile = strings.TrimSpace(p.ExecutionProfile)
	if p.ExecutionProfile == "" {
		p.ExecutionProfile = defaults.ExecutionProfile
	}
	if len(p.Providers) == 0 {
		p.Providers = append([]string(nil), defaults.Providers...)
	}
	for i := range p.Providers {
		p.Providers[i] = strings.ToLower(strings.TrimSpace(p.Providers[i]))
	}
	if len(p.MCPPermissions) == 0 {
		p.MCPPermissions = append([]string(nil), defaults.MCPPermissions...)
	}
	return p
}

type Run struct {
	ID                  string     `json:"id"`
	OrganizationID      string     `json:"organization_id"`
	ProjectID           string     `json:"project_id"`
	WorkItemID          string     `json:"work_item_id"`
	RepositoryID        string     `json:"repository_id,omitempty"`
	BaseSHA             string     `json:"base_sha,omitempty"`
	Branch              string     `json:"branch,omitempty"`
	Objective           string     `json:"objective"`
	AgentProvider       string     `json:"agent_provider"`
	AgentName           string     `json:"agent_name"`
	Model               string     `json:"model,omitempty"`
	TargetEnvironment   string     `json:"target_environment,omitempty"`
	Policy              Policy     `json:"policy"`
	Status              Status     `json:"status"`
	Phase               Phase      `json:"phase"`
	Gate                Gate       `json:"gate,omitempty"`
	Attempt             int        `json:"attempt"`
	MaxAttempts         int        `json:"max_attempts"`
	CurrentAgentRunID   string     `json:"current_agent_run_id,omitempty"`
	PullRequestID       string     `json:"pull_request_id,omitempty"`
	CommitSHA           string     `json:"commit_sha,omitempty"`
	UnresolvedPositions []int      `json:"unresolved_positions,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	Version             int64      `json:"version"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}

type StartInput struct {
	ProjectID         string
	WorkItemID        string
	WorkItemType      string
	Title             string
	RepositoryID      string
	BaseSHA           string
	Branch            string
	Objective         string
	AgentProvider     string
	AgentName         string
	Model             string
	TargetEnvironment string
	TestCasePositions []int
	Policy            Policy
}

type RetryInput struct {
	Feedback          string
	TestCasePositions []int
}

type FeedbackInput struct {
	Source            string   `json:"source"`
	Note              string   `json:"note"`
	Severity          string   `json:"severity,omitempty"`
	CommitSHA         string   `json:"commit_sha,omitempty"`
	TestCasePositions []int    `json:"test_case_positions,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
}

type Feedback struct {
	ID                string    `json:"id"`
	RunID             string    `json:"run_id"`
	Source            string    `json:"source"`
	Note              string    `json:"note"`
	Severity          string    `json:"severity,omitempty"`
	CommitSHA         string    `json:"commit_sha,omitempty"`
	TestCasePositions []int     `json:"test_case_positions,omitempty"`
	EvidenceRefs      []string  `json:"evidence_refs,omitempty"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
}

type StateUpdate struct {
	ExpectedVersion     int64
	Status              Status
	Phase               Phase
	Gate                Gate
	CurrentAgentRunID   string
	PullRequestID       string
	CommitSHA           string
	Attempt             int
	UnresolvedPositions []int
	LastError           string
	Finished            bool
}
