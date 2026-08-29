package tenant

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type Service struct {
	store       Store
	recorder    MutationRecorder
	transaction TransactionRunner
	now         func() time.Time
}

type AuthorizationContext struct {
	Scope          string   `json:"scope"`
	OrganizationID string   `json:"organization_id"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	ProjectID      string   `json:"project_id,omitempty"`
	Capabilities   []string `json:"capabilities"`
}

type MutationRecorder struct {
	Audit  audit.Writer
	Outbox outbox.Writer
}

type Options struct {
	Recorder    MutationRecorder
	Transaction TransactionRunner
	Now         func() time.Time
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

var roleKeys = map[string]struct{}{
	"owner":           {},
	"admin":           {},
	"project_manager": {},
	"developer":       {},
	"qa":              {},
	"security_reviewer": {},
	"release_approver": {},
	"viewer":          {},
}

var doneVisibilityDays = map[int]struct{}{0: {}, 7: {}, 14: {}, 30: {}, 60: {}}

var organizationSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateRoleKey(value string) error {
	if _, ok := roleKeys[value]; !ok {
		return apperr.New(apperr.CodeInvalidArgument, 422, "role_key is invalid", map[string]any{"allowed": []string{"owner", "admin", "project_manager", "developer", "qa", "security_reviewer", "release_approver", "viewer"}})
	}
	return nil
}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func NewService(store Store, options ...Options) *Service {
	configured := Options{}
	if len(options) > 0 {
		configured = options[0]
	}
	if configured.Now == nil {
		configured.Now = func() time.Time { return time.Now().UTC() }
	}
	if configured.Transaction == nil {
		configured.Transaction = directTransactionRunner{}
	}
	return &Service{store: store, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) Organizations(ctx context.Context, actor identity.Actor) ([]Organization, error) {
	if strings.TrimSpace(actor.ID) == "" {
		return nil, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if strings.TrimSpace(actor.OrganizationID) != "" {
		if err := require(actor, identity.CapabilityOrganizationRead); err != nil {
			return nil, err
		}
	}
	if actor.Source == "pat" {
		organization, err := s.store.Organization(ctx, actor.OrganizationID)
		if err != nil {
			return nil, err
		}
		return []Organization{*organization}, nil
	}
	return s.store.ListOrganizations(ctx, actor.ID)
}

func (s *Service) CreateOrganization(ctx context.Context, actor identity.Actor, slug, displayName string) (Organization, error) {
	if strings.TrimSpace(actor.ID) == "" {
		return Organization{}, apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !organizationSlugPattern.MatchString(slug) {
		return Organization{}, apperr.New(apperr.CodeInvalidArgument, 422, "slug must contain 1-63 lowercase letters, numbers, or hyphens", nil)
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 160 {
		return Organization{}, apperr.New(apperr.CodeInvalidArgument, 422, "display_name is required and must be at most 160 characters", nil)
	}
	var item Organization
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.CreateOrganization(txCtx, slug, displayName, actor.ID)
		if err != nil {
			return err
		}
		recordActor := actor
		recordActor.OrganizationID = item.ID
		return s.record(txCtx, recordActor, "organization.created", "organization", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) Organization(ctx context.Context, actor identity.Actor) (*Organization, error) {
	if err := require(actor, identity.CapabilityOrganizationRead); err != nil {
		return nil, err
	}
	return s.store.Organization(ctx, actor.OrganizationID)
}

func (s *Service) OrganizationAuthorization(ctx context.Context, actor identity.Actor, id string) (AuthorizationContext, error) {
	if err := require(actor, identity.CapabilityOrganizationRead); err != nil {
		return AuthorizationContext{}, err
	}
	organization, err := s.store.Organization(ctx, strings.TrimSpace(id))
	if err != nil {
		return AuthorizationContext{}, err
	}
	if organization.ID != actor.OrganizationID {
		return AuthorizationContext{}, apperr.New(apperr.CodeNotFound, 404, "organization not found", nil)
	}
	return AuthorizationContext{Scope: "organization", OrganizationID: organization.ID, Capabilities: actor.SortedCapabilities()}, nil
}

func (s *Service) WorkspaceAuthorization(ctx context.Context, actor identity.Actor, id string) (AuthorizationContext, error) {
	if err := require(actor, identity.CapabilityWorkspaceRead); err != nil {
		return AuthorizationContext{}, err
	}
	workspace, err := s.store.Workspace(ctx, actor.OrganizationID, strings.TrimSpace(id))
	if err != nil {
		return AuthorizationContext{}, err
	}
	return AuthorizationContext{Scope: "workspace", OrganizationID: workspace.OrganizationID, WorkspaceID: workspace.ID, Capabilities: actor.SortedCapabilities()}, nil
}

func (s *Service) ProjectAuthorization(ctx context.Context, actor identity.Actor, id string) (AuthorizationContext, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return AuthorizationContext{}, err
	}
	project, err := s.store.Project(ctx, actor.OrganizationID, strings.TrimSpace(id))
	if err != nil {
		return AuthorizationContext{}, err
	}
	return AuthorizationContext{Scope: "project", OrganizationID: project.OrganizationID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID, Capabilities: actor.SortedCapabilities()}, nil
}

func (s *Service) Workspaces(ctx context.Context, actor identity.Actor) ([]Workspace, error) {
	if err := require(actor, identity.CapabilityWorkspaceRead); err != nil {
		return nil, err
	}
	return s.store.ListWorkspaces(ctx, actor.OrganizationID)
}

func (s *Service) CreateWorkspace(ctx context.Context, actor identity.Actor, key, displayName string) (Workspace, error) {
	if err := require(actor, identity.CapabilityWorkspaceManage); err != nil {
		return Workspace{}, err
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if err := validateKey(key, "key"); err != nil {
		return Workspace{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		return Workspace{}, apperr.New(apperr.CodeInvalidArgument, 422, "display_name is required", nil)
	}
	var item Workspace
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.CreateWorkspace(txCtx, actor.OrganizationID, key, displayName)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "workspace.created", "workspace", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) UpdateWorkspace(ctx context.Context, actor identity.Actor, id, displayName string) (Workspace, error) {
	if err := require(actor, identity.CapabilityWorkspaceManage); err != nil {
		return Workspace{}, err
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(displayName) == "" {
		return Workspace{}, apperr.New(apperr.CodeInvalidArgument, 422, "workspace id and display_name are required", nil)
	}
	var item Workspace
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.UpdateWorkspace(txCtx, actor.OrganizationID, strings.TrimSpace(id), strings.TrimSpace(displayName))
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "workspace.updated", "workspace", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) Projects(ctx context.Context, actor identity.Actor, workspaceID string) ([]Project, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	return s.store.ListProjects(ctx, actor.OrganizationID, strings.TrimSpace(workspaceID))
}

func (s *Service) ProjectScope(ctx context.Context, actor identity.Actor, projectID string) (string, string, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return "", "", err
	}
	project, err := s.store.Project(ctx, actor.OrganizationID, strings.TrimSpace(projectID))
	if err != nil {
		return "", "", err
	}
	return project.WorkspaceID, project.Key, nil
}

func (s *Service) CreateProject(ctx context.Context, actor identity.Actor, workspaceID, key, displayName string) (Project, error) {
	if err := require(actor, identity.CapabilityProjectManage); err != nil {
		return Project{}, err
	}
	if strings.TrimSpace(workspaceID) == "" {
		return Project{}, apperr.New(apperr.CodeInvalidArgument, 422, "workspace_id is required", nil)
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if err := validateKey(key, "key"); err != nil {
		return Project{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		return Project{}, apperr.New(apperr.CodeInvalidArgument, 422, "display_name is required", nil)
	}
	var item Project
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.CreateProject(txCtx, actor.OrganizationID, workspaceID, key, displayName, actor.ID)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "project.created", "project", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) UpdateProject(ctx context.Context, actor identity.Actor, id, displayName string) (Project, error) {
	if err := require(actor, identity.CapabilityProjectManage); err != nil {
		return Project{}, err
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(displayName) == "" {
		return Project{}, apperr.New(apperr.CodeInvalidArgument, 422, "project id and display_name are required", nil)
	}
	var item Project
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.UpdateProject(txCtx, actor.OrganizationID, strings.TrimSpace(id), strings.TrimSpace(displayName))
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "project.updated", "project", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) GetProjectBoardSettings(ctx context.Context, actor identity.Actor, id string) (ProjectBoardSettings, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return ProjectBoardSettings{}, err
	}
	project, err := s.store.Project(ctx, actor.OrganizationID, strings.TrimSpace(id))
	if err != nil {
		return ProjectBoardSettings{}, err
	}
	return ProjectBoardSettings{ProjectID: project.ID, DoneVisibilityDays: project.DoneVisibilityDays}, nil
}

func (s *Service) ProjectBoardVisibilityDays(ctx context.Context, actor identity.Actor, id string) (int, error) {
	settings, err := s.GetProjectBoardSettings(ctx, actor, id)
	if err != nil {
		return 0, err
	}
	return settings.DoneVisibilityDays, nil
}

func (s *Service) UpdateProjectBoardSettings(ctx context.Context, actor identity.Actor, id string, days int) (ProjectBoardSettings, error) {
	if err := require(actor, identity.CapabilityProjectManage); err != nil {
		return ProjectBoardSettings{}, err
	}
	if strings.TrimSpace(id) == "" {
		return ProjectBoardSettings{}, apperr.New(apperr.CodeInvalidArgument, 422, "project id is required", nil)
	}
	if _, ok := doneVisibilityDays[days]; !ok {
		return ProjectBoardSettings{}, apperr.New(apperr.CodeInvalidArgument, 422, "done_visibility_days must be one of 0, 7, 14, 30, or 60", nil)
	}
	var project Project
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, err := s.store.Project(txCtx, actor.OrganizationID, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		project, err = s.store.UpdateProjectDoneVisibilityDays(txCtx, actor.OrganizationID, strings.TrimSpace(id), days)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "project.board_settings.updated", "project", project.ID, before, project)
	})
	if err != nil {
		return ProjectBoardSettings{}, err
	}
	return ProjectBoardSettings{ProjectID: project.ID, DoneVisibilityDays: project.DoneVisibilityDays}, nil
}

func (s *Service) Members(ctx context.Context, actor identity.Actor, projectID string) ([]Member, error) {
	if err := require(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	return s.store.ListMembers(ctx, actor.OrganizationID, projectID)
}

func (s *Service) SetMember(ctx context.Context, actor identity.Actor, projectID, userID, roleKey string) (Member, error) {
	if err := require(actor, identity.CapabilityProjectManage); err != nil {
		return Member{}, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(roleKey) == "" {
		return Member{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id, user_id, and role_key are required", nil)
	}
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	if err := validateRoleKey(roleKey); err != nil {
		return Member{}, err
	}
	var item Member
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.SetProjectMember(txCtx, actor.OrganizationID, projectID, userID, roleKey)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "project.member.updated", "project_member", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) RemoveMember(ctx context.Context, actor identity.Actor, projectID, userID string) error {
	if err := require(actor, identity.CapabilityProjectManage); err != nil {
		return err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(userID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id and user_id are required", nil)
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.RemoveProjectMember(txCtx, actor.OrganizationID, projectID, userID); err != nil {
			return err
		}
		return s.record(txCtx, actor, "project.member.removed", "project_member", userID, map[string]string{"project_id": projectID, "user_id": userID}, nil)
	})
}

func (s *Service) OrganizationMembers(ctx context.Context, actor identity.Actor) ([]Member, error) {
	if err := require(actor, "organization.read"); err != nil {
		return nil, err
	}
	return s.store.ListOrganizationMembers(ctx, actor.OrganizationID)
}

func (s *Service) AddOrganizationMember(ctx context.Context, actor identity.Actor, login, roleKey string) (Member, error) {
	if err := require(actor, identity.CapabilityOrganizationManage); err != nil {
		return Member{}, err
	}
	login = strings.TrimSpace(login)
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	if login == "" || roleKey == "" {
		return Member{}, apperr.New(apperr.CodeInvalidArgument, 422, "login and role_key are required", nil)
	}
	if err := validateRoleKey(roleKey); err != nil {
		return Member{}, err
	}
	var item Member
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.AddOrganizationMember(txCtx, actor.OrganizationID, login, roleKey)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "organization.member.added", "organization_member", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) SetOrganizationMember(ctx context.Context, actor identity.Actor, userID, roleKey string) (Member, error) {
	if err := require(actor, identity.CapabilityOrganizationManage); err != nil {
		return Member{}, err
	}
	userID = strings.TrimSpace(userID)
	roleKey = strings.ToLower(strings.TrimSpace(roleKey))
	if userID == "" || roleKey == "" {
		return Member{}, apperr.New(apperr.CodeInvalidArgument, 422, "user_id and role_key are required", nil)
	}
	if err := validateRoleKey(roleKey); err != nil {
		return Member{}, err
	}
	var item Member
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.SetOrganizationMember(txCtx, actor.OrganizationID, userID, roleKey)
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "organization.member.updated", "organization_member", item.ID, nil, item)
	})
	return item, err
}

func (s *Service) RemoveOrganizationMember(ctx context.Context, actor identity.Actor, userID string) error {
	if err := require(actor, identity.CapabilityOrganizationManage); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "user_id is required", nil)
	}
	return s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		members, err := s.store.ListOrganizationMembers(txCtx, actor.OrganizationID)
		if err != nil {
			return err
		}
		var before Member
		found := false
		for _, member := range members {
			if member.ID == userID {
				before = member
				found = true
				break
			}
		}
		if !found {
			return apperr.New(apperr.CodeNotFound, 404, "organization member not found", nil)
		}
		if err := s.store.RemoveOrganizationMember(txCtx, actor.OrganizationID, userID); err != nil {
			return err
		}
		return s.record(txCtx, actor, "organization.member.removed", "organization_member", userID, before, nil)
	})
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceType, resourceID string, before, after any) error {
	if s.recorder.Audit != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		id, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: actor.OrganizationID, EventType: action, AggregateType: resourceType, AggregateID: resourceID, IdempotencyKey: action + ":" + resourceID + ":" + id, Payload: map[string]any{"resource_id": resourceID}, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func require(actor identity.Actor, capability string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}
