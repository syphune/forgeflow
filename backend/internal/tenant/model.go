package tenant

import (
	"context"
	"time"
)

type Organization struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Workspace struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Key            string    `json:"key"`
	DisplayName    string    `json:"display_name"`
	CreatedAt      time.Time `json:"created_at"`
}

type Project struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	WorkspaceID    string    `json:"workspace_id"`
	Key            string    `json:"key"`
	DisplayName    string    `json:"display_name"`
	CreatedAt      time.Time `json:"created_at"`
}

type Member struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	RoleKey     string `json:"role_key"`
	ProjectRole bool   `json:"project_role"`
}

type Store interface {
	Organization(context.Context, string) (*Organization, error)
	Workspace(context.Context, string, string) (*Workspace, error)
	Project(context.Context, string, string) (*Project, error)
	ListOrganizations(context.Context, string) ([]Organization, error)
	ListWorkspaces(context.Context, string) ([]Workspace, error)
	CreateWorkspace(context.Context, string, string, string) (Workspace, error)
	UpdateWorkspace(context.Context, string, string, string) (Workspace, error)
	ListProjects(context.Context, string, string) ([]Project, error)
	CreateProject(context.Context, string, string, string, string, string) (Project, error)
	UpdateProject(context.Context, string, string, string) (Project, error)
	ListMembers(context.Context, string, string) ([]Member, error)
	SetProjectMember(context.Context, string, string, string, string) (Member, error)
	RemoveProjectMember(context.Context, string, string, string) error
	ListOrganizationMembers(context.Context, string) ([]Member, error)
	AddOrganizationMember(context.Context, string, string, string) (Member, error)
	SetOrganizationMember(context.Context, string, string, string) (Member, error)
	RemoveOrganizationMember(context.Context, string, string) error
}
