package impact

import (
	"context"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

const (
	GateReportOnly = "REPORT_ONLY"
	GateRequired   = "REQUIRED"

	ScopeProposed = "PROPOSED"
	ScopeActive   = "ACTIVE"
	ScopeRejected = "REJECTED"

	ProvenanceHumanVerified = "HUMAN_VERIFIED"
	ProvenanceAIInferred    = "AI_INFERRED"
	ProvenanceSystemDerived = "SYSTEM_DERIVED"

	ReportPending  = "PENDING"
	ReportApproved = "APPROVED"
	ReportRejected = "REJECTED"
	ReportStale    = "STALE"

	DecisionApproved = "APPROVED"
	DecisionRejected = "REJECTED"
)

const (
	maxRelatedProjects = 10
	maxPaths           = 500
	maxScopeItems      = 100
	maxPathBytes       = 1024
	maxModuleBytes     = 256
	maxSummaryBytes    = 4000
)

type Subject struct {
	OrganizationID       string
	ProjectID            string
	WorkItemID           string
	RepositoryID         string
	AgentRunID           string
	BaseSHA              string
	Branch               string
	SpecificationVersion int
	ChangedPaths         []string
	DiffHash             string
	GateMode             string
}

type ProjectSummary struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}

type RepositoryProjectScope struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	RepositoryID   string    `json:"repository_id"`
	PathPrefixes   []string  `json:"path_prefixes,omitempty"`
	Modules        []string  `json:"modules,omitempty"`
	ImpactSummary  string    `json:"impact_summary,omitempty"`
	Provenance     string    `json:"provenance"`
	Status         string    `json:"status"`
	Version        int64     `json:"version"`
	CreatedBy      string    `json:"created_by,omitempty"`
	VerifiedBy     string    `json:"verified_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ScopeInput struct {
	ID            string
	ProjectID     string
	RepositoryID  string
	PathPrefixes  []string
	Modules       []string
	ImpactSummary string
}

type RelatedProject struct {
	Project             ProjectSummary `json:"project"`
	PathPrefixes        []string       `json:"path_prefixes,omitempty"`
	Modules             []string       `json:"modules,omitempty"`
	ImpactSummary       string         `json:"impact_summary,omitempty"`
	Provenance          string         `json:"provenance,omitempty"`
	MappingStatus       string         `json:"mapping_status"`
	PotentiallyAffected bool           `json:"potentially_affected"`
	Reason              string         `json:"reason,omitempty"`
}

type Context struct {
	CurrentProject  ProjectSummary   `json:"current_project"`
	RelatedProjects []RelatedProject `json:"related_projects,omitempty"`
	UnknownImpact   bool             `json:"unknown_impact"`
	Truncated       bool             `json:"truncated"`
	ContentTrust    string           `json:"content_trust"`
}

type AffectedProject struct {
	Project          ProjectSummary `json:"project"`
	PathPrefixes     []string       `json:"path_prefixes,omitempty"`
	Modules          []string       `json:"modules,omitempty"`
	Reason           string         `json:"reason"`
	RequiresApproval bool           `json:"requires_approval"`
}

type Approval struct {
	OrganizationID    string    `json:"-"`
	ReportID          string    `json:"report_id"`
	AffectedProjectID string    `json:"affected_project_id"`
	Decision          string    `json:"decision"`
	ApproverID        string    `json:"approver_id"`
	Note              string    `json:"note,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type Report struct {
	ID               string            `json:"id"`
	OrganizationID   string            `json:"organization_id"`
	AgentRunID       string            `json:"agent_run_id"`
	ProjectID        string            `json:"project_id"`
	RepositoryID     string            `json:"repository_id"`
	ReportVersion    int64             `json:"report_version"`
	InputHash        string            `json:"input_hash"`
	BaseSHA          string            `json:"base_sha,omitempty"`
	DiffHash         string            `json:"diff_hash,omitempty"`
	ChangedPaths     []string          `json:"changed_paths,omitempty"`
	AffectedProjects []AffectedProject `json:"affected_projects"`
	UnknownImpact    bool              `json:"unknown_impact"`
	Truncated        bool              `json:"truncated"`
	Status           string            `json:"status"`
	Approvals        []Approval        `json:"approvals,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

type ApprovalInput struct {
	ReportVersion int64
	Decision      string
	Note          string
}

type Resolver interface {
	ProjectsForRepository(context.Context, string, string) ([]ProjectSummary, error)
}

type Store interface {
	SaveScope(context.Context, RepositoryProjectScope) (RepositoryProjectScope, error)
	GetScope(context.Context, string, string) (RepositoryProjectScope, error)
	ListScopes(context.Context, string, string, string) ([]RepositoryProjectScope, error)
	CreateReport(context.Context, Report) (Report, error)
	LatestReport(context.Context, string, string) (Report, error)
	ListApprovals(context.Context, string, string) ([]Approval, error)
	SaveApproval(context.Context, Approval) error
	SetReportStatus(context.Context, string, string, string) error
}

type Checker interface {
	EnsureReport(context.Context, Subject) (Report, error)
	ReadyForPublish(context.Context, Subject) (Report, error)
	GetReport(context.Context, identity.Actor, string, string) (Report, error)
	Approve(context.Context, identity.Actor, string, string, ApprovalInput) (Report, error)
}
