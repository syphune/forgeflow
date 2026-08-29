package environment

import (
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
)

type ReleaseMode string

const (
	ReleaseModeStandard ReleaseMode = "standard"
	ReleaseModeGated    ReleaseMode = "release_gated"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"
	RiskNormal   RiskLevel = "NORMAL"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
)

type PathReviewRule struct {
	Pattern       string   `json:"pattern"`
	MinApprovals  int      `json:"min_approvals,omitempty"`
	RequiredRoles []string `json:"required_roles,omitempty"`
}

type ReviewPolicy struct {
	MinApprovals       int              `json:"min_approvals"`
	ForbidSelfApproval bool             `json:"forbid_self_approval"`
	RequiredRoles      []string         `json:"required_roles,omitempty"`
	PathRules          []PathReviewRule `json:"path_rules,omitempty"`
}

type RiskRule struct {
	MinApprovals                  int      `json:"min_approvals"`
	RequiredRoles                 []string `json:"required_roles,omitempty"`
	RequireQA                     bool     `json:"require_qa"`
	RequireRollbackPlan           bool     `json:"require_rollback_plan"`
	RequireProductionVerification bool     `json:"require_production_verification"`
}

type DeliveryPolicy struct {
	ID                      string                 `json:"id,omitempty"`
	OrganizationID          string                 `json:"organization_id,omitempty"`
	ProjectID               string                 `json:"project_id,omitempty"`
	Version                 int64                  `json:"version"`
	ReleaseMode             ReleaseMode            `json:"release_mode"`
	ProductionEnvironmentID string                 `json:"production_environment_id,omitempty"`
	RequiredChecks          []string               `json:"required_checks,omitempty"`
	Review                  ReviewPolicy           `json:"review"`
	RiskRules               map[RiskLevel]RiskRule `json:"risk_rules"`
	UpdatedAt               time.Time              `json:"updated_at,omitempty"`
}

type Environment struct {
	ID              string         `json:"id"`
	OrganizationID  string         `json:"organization_id"`
	ProjectID       string         `json:"project_id"`
	Key             string         `json:"key"`
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	RepositoryID    string         `json:"repository_id,omitempty"`
	WorkflowRef     string         `json:"workflow_ref,omitempty"`
	DispatchURL     string         `json:"dispatch_url,omitempty"`
	HealthCheckURL  string         `json:"health_check_url,omitempty"`
	AutoDeploy      bool           `json:"auto_deploy"`
	RequireApproval bool           `json:"require_approval"`
	SecretRefs      []string       `json:"secret_refs,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type CreateInput struct {
	ProjectID       string         `json:"project_id"`
	Key             string         `json:"key"`
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	RepositoryID    string         `json:"repository_id"`
	WorkflowRef     string         `json:"workflow_ref"`
	DispatchURL     string         `json:"dispatch_url"`
	HealthCheckURL  string         `json:"health_check_url"`
	AutoDeploy      bool           `json:"auto_deploy"`
	RequireApproval bool           `json:"require_approval"`
	SecretRefs      []string       `json:"secret_refs"`
	Metadata        map[string]any `json:"metadata"`
}

type DeploymentStatus string

const (
	DeploymentPending  DeploymentStatus = "PENDING_APPROVAL"
	DeploymentDispatch DeploymentStatus = "DISPATCHED"
	DeploymentRunning  DeploymentStatus = "RUNNING"
	DeploymentSuccess  DeploymentStatus = "SUCCEEDED"
	DeploymentFailed   DeploymentStatus = "FAILED"
	DeploymentCanceled DeploymentStatus = "CANCELLED"
	DeploymentVerified DeploymentStatus = "VERIFIED"
)

type DeploymentRequest struct {
	ID              string           `json:"id"`
	OrganizationID  string           `json:"organization_id"`
	ProjectID       string           `json:"project_id"`
	EnvironmentID   string           `json:"environment_id"`
	WorkItemID      string           `json:"work_item_id,omitempty"`
	AutonomousRunID string           `json:"autonomous_run_id,omitempty"`
	CommitSHA       string           `json:"commit_sha"`
	Status          DeploymentStatus `json:"status"`
	ExternalID      string           `json:"external_id,omitempty"`
	URL             string           `json:"url,omitempty"`
	ApprovedBy      string           `json:"approved_by,omitempty"`
	ApprovedAt      *time.Time       `json:"approved_at,omitempty"`
	LastError       string           `json:"last_error,omitempty"`
	Version         int64            `json:"version"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type DeploymentInput struct {
	ProjectID       string `json:"project_id"`
	EnvironmentID   string `json:"environment_id"`
	WorkItemID      string `json:"work_item_id"`
	AutonomousRunID string `json:"autonomous_run_id"`
	CommitSHA       string `json:"commit_sha"`
}

type StatusInput struct {
	Status     DeploymentStatus `json:"status"`
	ExternalID string           `json:"external_id"`
	URL        string           `json:"url"`
	LastError  string           `json:"last_error"`
}

func validDeploymentUpdate(current, next DeploymentStatus) bool {
	switch next {
	case DeploymentRunning:
		return current == DeploymentDispatch
	case DeploymentSuccess, DeploymentFailed, DeploymentCanceled:
		return current == DeploymentDispatch || current == DeploymentRunning
	default:
		return false
	}
}

func normalizeKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "staging"
	}
	return value
}

func validKind(value string) bool {
	value = normalizeKind(value)
	return value == "preview" || value == "development" || value == "staging" || value == "production"
}

func PolicyDefaults() autonomous.Policy { return autonomous.DefaultPolicy() }

func DefaultDeliveryPolicy() DeliveryPolicy {
	return DeliveryPolicy{
		ReleaseMode: ReleaseModeStandard,
		Review:      ReviewPolicy{MinApprovals: 1, ForbidSelfApproval: true},
		RiskRules: map[RiskLevel]RiskRule{
			RiskLow:      {MinApprovals: 1},
			RiskNormal:   {MinApprovals: 1, RequireQA: true},
			RiskHigh:     {MinApprovals: 2, RequireQA: true, RequireRollbackPlan: true},
			RiskCritical: {MinApprovals: 2, RequireQA: true, RequireRollbackPlan: true, RequireProductionVerification: true},
		},
	}
}

func (p DeliveryPolicy) Normalize() DeliveryPolicy {
	defaults := DefaultDeliveryPolicy()
	if p.ReleaseMode != ReleaseModeStandard && p.ReleaseMode != ReleaseModeGated {
		p.ReleaseMode = defaults.ReleaseMode
	}
	if p.Review.MinApprovals < 0 || p.Review.MinApprovals > 20 {
		p.Review.MinApprovals = defaults.Review.MinApprovals
	}
	if !p.Review.ForbidSelfApproval {
		// Self approval is a safety default; it can only be enabled by an
		// explicit policy value in a future versioned contract.
		p.Review.ForbidSelfApproval = true
	}
	if p.RiskRules == nil {
		p.RiskRules = defaults.RiskRules
	}
	for _, risk := range []RiskLevel{RiskLow, RiskNormal, RiskHigh, RiskCritical} {
		if _, ok := p.RiskRules[risk]; !ok {
			p.RiskRules[risk] = defaults.RiskRules[risk]
		}
	}
	return p
}

func (p DeliveryPolicy) RiskRule(level RiskLevel) RiskRule {
	p = p.Normalize()
	rule, ok := p.RiskRules[RiskLevel(strings.ToUpper(strings.TrimSpace(string(level))))]
	if !ok {
		return p.RiskRules[RiskNormal]
	}
	return rule
}
