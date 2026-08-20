package environment

import (
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/autonomous"
)

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
	ProjectID       string
	Key             string
	Name            string
	Kind            string
	RepositoryID    string
	WorkflowRef     string
	DispatchURL     string
	HealthCheckURL  string
	AutoDeploy      bool
	RequireApproval bool
	SecretRefs      []string
	Metadata        map[string]any
}

type DeploymentStatus string

const (
	DeploymentPending  DeploymentStatus = "PENDING_APPROVAL"
	DeploymentDispatch DeploymentStatus = "DISPATCHED"
	DeploymentRunning  DeploymentStatus = "RUNNING"
	DeploymentSuccess  DeploymentStatus = "SUCCEEDED"
	DeploymentFailed   DeploymentStatus = "FAILED"
	DeploymentCanceled DeploymentStatus = "CANCELLED"
)

type DeploymentRequest struct {
	ID              string           `json:"id"`
	OrganizationID  string           `json:"organization_id"`
	ProjectID       string           `json:"project_id"`
	EnvironmentID   string           `json:"environment_id"`
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
	ProjectID       string
	EnvironmentID   string
	AutonomousRunID string
	CommitSHA       string
}

type StatusInput struct {
	Status     DeploymentStatus
	ExternalID string
	URL        string
	LastError  string
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
