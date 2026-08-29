package branchplan

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type BranchType string

const (
	Feature  BranchType = "feature"
	Fix      BranchType = "fix"
	Refactor BranchType = "refactor"
	Chore    BranchType = "chore"
	Hotfix   BranchType = "hotfix"
	Release  BranchType = "release"
)

type BranchRole string

const (
	Development BranchRole = "development"
	Production  BranchRole = "production"
	ReleaseRole BranchRole = "release"
)

type WorkingMode string

const (
	Ephemeral WorkingMode = "ephemeral"
	Managed   WorkingMode = "managed"
)

type PlanState string

const (
	PlanPending PlanState = "PENDING"
	PlanReady   PlanState = "READY"
	PlanFailed  PlanState = "FAILED"
)

type OperationKind string

const (
	OperationBranchCreate OperationKind = "BRANCH_CREATE"
	OperationPullRequest  OperationKind = "PULL_REQUEST"
	OperationSyncPull     OperationKind = "SYNC_PULL_REQUEST"
)

type OperationState string

const (
	OperationPending   OperationState = "PENDING"
	OperationCompleted OperationState = "COMPLETED"
	OperationMerged    OperationState = "MERGED"
	OperationFailed    OperationState = "FAILED"
)

type Rule struct {
	BranchType        BranchType   `json:"branch_type"`
	BaseRole          BranchRole   `json:"base_role"`
	TargetRole        BranchRole   `json:"target_role"`
	SyncRoles         []BranchRole `json:"sync_roles,omitempty"`
	WorkingMode       WorkingMode  `json:"working_mode"`
	AllowReleaseBase  bool         `json:"allow_release_base,omitempty"`
	CreatePullRequest bool         `json:"create_pull_request"`
	RequireSync       bool         `json:"require_sync"`
}

type Policy struct {
	ID                 string    `json:"id"`
	OrganizationID     string    `json:"organization_id"`
	ProjectID          string    `json:"project_id"`
	DevelopmentBranch  string    `json:"development_branch"`
	ProductionBranch   string    `json:"production_branch"`
	ReleasePrefix      string    `json:"release_prefix"`
	WorkBranchTemplate string    `json:"work_branch_template"`
	Rules              []Rule    `json:"rules"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PolicyInput struct {
	DevelopmentBranch  string `json:"development_branch"`
	ProductionBranch   string `json:"production_branch"`
	ReleasePrefix      string `json:"release_prefix"`
	WorkBranchTemplate string `json:"work_branch_template"`
	Rules              []Rule `json:"rules"`
	ExpectedVersion    int64  `json:"expected_version"`
}

type WorkItemInput struct {
	ID           string
	Key          string
	Title        string
	Type         string
	ProjectID    string
	RepositoryID string
	Assignee     string
	AssigneeID   string
	Version      int64
}

type PreviewInput struct {
	BranchType    BranchType `json:"branch_type"`
	ReleaseBranch string     `json:"release_branch,omitempty"`
}

type Plan struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organization_id"`
	ProjectID         string     `json:"project_id"`
	WorkItemID        string     `json:"work_item_id"`
	RepositoryID      string     `json:"repository_id"`
	WorkItemKey       string     `json:"work_item_key"`
	BranchType        BranchType `json:"branch_type"`
	ReleaseBranch     string     `json:"release_branch,omitempty"`
	BaseBranch        string     `json:"base_branch"`
	TargetBranch      string     `json:"target_branch"`
	WorkingBranch     string     `json:"working_branch"`
	SyncBranches      []string   `json:"sync_branches,omitempty"`
	CreatePullRequest bool       `json:"create_pull_request"`
	RequireSync       bool       `json:"require_sync"`
	BaseSHA           string     `json:"base_sha"`
	PolicyVersion     int64      `json:"policy_version"`
	WorkItemVersion   int64      `json:"work_item_version"`
	State             PlanState  `json:"state"`
	Error             string     `json:"error,omitempty"`
	CreatedBy         string     `json:"created_by"`
	Version           int64      `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Operation struct {
	ID                string         `json:"id"`
	PlanID            string         `json:"plan_id"`
	OrganizationID    string         `json:"organization_id"`
	ProjectID         string         `json:"project_id"`
	RepositoryID      string         `json:"repository_id"`
	Kind              OperationKind  `json:"kind"`
	State             OperationState `json:"state"`
	IdempotencyKey    string         `json:"idempotency_key"`
	SourceBranch      string         `json:"source_branch,omitempty"`
	TargetBranch      string         `json:"target_branch,omitempty"`
	PullRequestNumber int64          `json:"pull_request_number,omitempty"`
	PullRequestURL    string         `json:"pull_request_url,omitempty"`
	Error             string         `json:"error,omitempty"`
	Version           int64          `json:"version"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type Repository struct {
	ID             string
	FullName       string
	DefaultBranch  string
	InstallationID int64
}

type PullRequest struct {
	Number int64
	URL    string
	State  string
	Head   string
	Base   string
}

type Provider interface {
	Repository(context.Context, identity.Actor, string, string) (Repository, error)
	Head(context.Context, identity.Actor, Repository, string) (string, error)
	CreateBranch(context.Context, identity.Actor, Repository, string, string) error
	FindPullRequest(context.Context, identity.Actor, string, string, string, string) (PullRequest, bool, error)
	CreateDraftPullRequest(context.Context, identity.Actor, string, string, string, string, string, string) (PullRequest, error)
}

type Store interface {
	GetPolicy(context.Context, string, string) (Policy, error)
	SavePolicy(context.Context, Policy, int64) (Policy, error)
	GetPlan(context.Context, string, string, string) (Plan, error)
	CreatePlan(context.Context, Plan) (Plan, error)
	UpdatePlan(context.Context, string, string, string, int64, PlanState, string) (Plan, error)
	GetOperation(context.Context, string, string, string) (Operation, error)
	CreateOperation(context.Context, Operation) (Operation, error)
	UpdateOperation(context.Context, string, string, string, int64, OperationState, string, int64, string) (Operation, error)
	ListOperations(context.Context, string, string, string) ([]Operation, error)
	MarkPullRequestMerged(context.Context, string, string, int64) error
}

var branchRefInvalid = regexp.MustCompile(`(^/|/$|//|\.\.|@\{|[\x00-\x20~^:?*\\\[\]])`)
var placeholderPattern = regexp.MustCompile(`\{([a-z-]+)\}`)

func DefaultPolicy(organizationID, projectID string) Policy {
	return Policy{
		OrganizationID:     organizationID,
		ProjectID:          projectID,
		DevelopmentBranch:  "develop",
		ProductionBranch:   "main",
		ReleasePrefix:      "release/",
		WorkBranchTemplate: "forgeflow/{assignee}/{type}/{task-key}-{slug}",
		Rules: []Rule{
			{BranchType: Feature, BaseRole: Development, TargetRole: Development, WorkingMode: Ephemeral, CreatePullRequest: true},
			{BranchType: Fix, BaseRole: Development, TargetRole: Development, WorkingMode: Ephemeral, AllowReleaseBase: true, CreatePullRequest: true},
			{BranchType: Refactor, BaseRole: Development, TargetRole: Development, WorkingMode: Ephemeral, CreatePullRequest: true},
			{BranchType: Chore, BaseRole: Development, TargetRole: Development, WorkingMode: Ephemeral, CreatePullRequest: true},
			{BranchType: Hotfix, BaseRole: Production, TargetRole: Production, SyncRoles: []BranchRole{Development}, WorkingMode: Ephemeral, CreatePullRequest: true, RequireSync: true},
			{BranchType: Release, BaseRole: Development, TargetRole: Production, SyncRoles: []BranchRole{Development}, WorkingMode: Managed, CreatePullRequest: true, RequireSync: true},
		},
	}
}

func (p Policy) Normalized() Policy {
	result := p
	defaults := DefaultPolicy(p.OrganizationID, p.ProjectID)
	if strings.TrimSpace(result.DevelopmentBranch) == "" {
		result.DevelopmentBranch = defaults.DevelopmentBranch
	}
	if strings.TrimSpace(result.ProductionBranch) == "" {
		result.ProductionBranch = defaults.ProductionBranch
	}
	if strings.TrimSpace(result.ReleasePrefix) == "" {
		result.ReleasePrefix = defaults.ReleasePrefix
	}
	if strings.TrimSpace(result.WorkBranchTemplate) == "" {
		result.WorkBranchTemplate = defaults.WorkBranchTemplate
	}
	if len(result.Rules) == 0 {
		result.Rules = defaults.Rules
	}
	return result
}

func (p Policy) Validate() error {
	p = p.Normalized()
	if len(p.Rules) == 0 || len(p.Rules) > 20 {
		return fmt.Errorf("rules must contain between 1 and 20 items")
	}
	for field, value := range map[string]string{
		"development_branch": p.DevelopmentBranch,
		"production_branch":  p.ProductionBranch,
	} {
		if err := ValidateBranchRef(value); err != nil {
			return fmt.Errorf("%s is invalid: %w", field, err)
		}
	}
	if p.DevelopmentBranch == p.ProductionBranch {
		return fmt.Errorf("development_branch and production_branch must be different")
	}
	if len([]rune(p.ReleasePrefix)) < 2 || len([]rune(p.ReleasePrefix)) > 80 || strings.ContainsAny(p.ReleasePrefix, "\x00\r\n~^:?*\\[]") || strings.Contains(p.ReleasePrefix, "//") || strings.Contains(p.ReleasePrefix, "..") || !strings.HasSuffix(p.ReleasePrefix, "/") || ValidateBranchRef(strings.TrimSuffix(p.ReleasePrefix, "/")) != nil {
		return fmt.Errorf("release_prefix must be a branch-safe prefix ending in '/'")
	}
	if len([]rune(p.WorkBranchTemplate)) > 200 || strings.TrimSpace(p.WorkBranchTemplate) == "" {
		return fmt.Errorf("work_branch_template is required and must be at most 200 characters")
	}
	if !strings.HasPrefix(p.WorkBranchTemplate, "forgeflow/") {
		return fmt.Errorf("work_branch_template must use the forgeflow/ prefix")
	}
	seen := make(map[BranchType]bool, len(p.Rules))
	for _, rule := range p.Rules {
		if !validBranchType(rule.BranchType) || seen[rule.BranchType] {
			return fmt.Errorf("branch rule type %q is invalid or duplicated", rule.BranchType)
		}
		seen[rule.BranchType] = true
		if !validRole(rule.BaseRole) || !validRole(rule.TargetRole) {
			return fmt.Errorf("branch rule %q has an invalid base or target role", rule.BranchType)
		}
		if rule.WorkingMode != Ephemeral && rule.WorkingMode != Managed {
			return fmt.Errorf("branch rule %q has an invalid working mode", rule.BranchType)
		}
		if rule.WorkingMode == Managed && rule.BranchType != Release {
			return fmt.Errorf("managed working mode is only valid for release branches")
		}
		if rule.WorkingMode == Ephemeral && !rule.CreatePullRequest {
			return fmt.Errorf("ephemeral branch rule %q must create a pull request", rule.BranchType)
		}
		if len(rule.SyncRoles) > 20 {
			return fmt.Errorf("branch rule %q has too many sync roles", rule.BranchType)
		}
		for _, role := range rule.SyncRoles {
			if !validRole(role) {
				return fmt.Errorf("branch rule %q has an invalid sync role", rule.BranchType)
			}
		}
		if rule.RequireSync && len(rule.SyncRoles) == 0 {
			return fmt.Errorf("branch rule %q requires at least one sync role", rule.BranchType)
		}
		switch rule.BranchType {
		case Hotfix:
			if rule.BaseRole != Production || rule.TargetRole != Production || rule.WorkingMode != Ephemeral || !rule.RequireSync || !containsRole(rule.SyncRoles, Development) {
				return fmt.Errorf("hotfix branch rule must be an ephemeral production flow with development sync")
			}
		case Release:
			if rule.BaseRole != Development || rule.TargetRole != Production || rule.WorkingMode != Managed || !rule.CreatePullRequest || !rule.RequireSync || !containsRole(rule.SyncRoles, Development) {
				return fmt.Errorf("release branch rule must be a managed development-to-production flow with development sync")
			}
		default:
			if rule.BaseRole == Production || rule.TargetRole == Production {
				return fmt.Errorf("branch rule %q cannot target production directly", rule.BranchType)
			}
		}
	}
	return validateTemplate(p.WorkBranchTemplate)
}

func BuildPreview(policy Policy, item WorkItemInput, input PreviewInput) (Plan, error) {
	policy = policy.Normalized()
	if err := policy.Validate(); err != nil {
		return Plan{}, err
	}
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Key) == "" || strings.TrimSpace(item.RepositoryID) == "" {
		return Plan{}, fmt.Errorf("work item id, key and repository are required")
	}
	branchType := BranchType(strings.ToLower(strings.TrimSpace(string(input.BranchType))))
	if branchType == "" {
		branchType = deriveBranchType(item.Type)
	}
	rule, ok := findRule(policy.Rules, branchType)
	if !ok {
		return Plan{}, fmt.Errorf("branch type %q is not configured", branchType)
	}
	releaseBranch := strings.TrimSpace(input.ReleaseBranch)
	if releaseBranch != "" {
		if err := ValidateBranchRef(releaseBranch); err != nil {
			return Plan{}, fmt.Errorf("release_branch is invalid: %w", err)
		}
		if !strings.HasPrefix(releaseBranch, policy.ReleasePrefix) {
			return Plan{}, fmt.Errorf("release_branch must use the configured release prefix")
		}
		if branchType != Release && !rule.AllowReleaseBase {
			return Plan{}, fmt.Errorf("release_branch is not allowed for this branch type")
		}
		if rule.AllowReleaseBase && branchType != Release {
			rule.BaseRole = ReleaseRole
			rule.TargetRole = ReleaseRole
			rule.SyncRoles = []BranchRole{Development}
			rule.RequireSync = true
		}
	}
	if branchType == Release && releaseBranch == "" {
		return Plan{}, fmt.Errorf("release_branch is required for release work")
	}
	if (rule.BaseRole == ReleaseRole || rule.TargetRole == ReleaseRole) && releaseBranch == "" {
		return Plan{}, fmt.Errorf("release_branch is required for this branch rule")
	}
	baseBranch, err := resolveRole(policy, rule.BaseRole, releaseBranch)
	if err != nil {
		return Plan{}, err
	}
	targetBranch, err := resolveRole(policy, rule.TargetRole, releaseBranch)
	if err != nil {
		return Plan{}, err
	}
	syncBranches := make([]string, 0, len(rule.SyncRoles))
	for _, role := range rule.SyncRoles {
		branch, roleErr := resolveRole(policy, role, releaseBranch)
		if roleErr != nil {
			return Plan{}, roleErr
		}
		if branch != targetBranch && !contains(syncBranches, branch) {
			syncBranches = append(syncBranches, branch)
		}
	}
	if rule.RequireSync && len(syncBranches) == 0 {
		return Plan{}, fmt.Errorf("this branch rule requires a sync branch different from its target")
	}
	workingBranch := ""
	if rule.WorkingMode == Managed {
		workingBranch = releaseBranch
	} else {
		workingBranch, err = renderTemplate(policy.WorkBranchTemplate, item, branchType)
		if err != nil {
			return Plan{}, err
		}
	}
	return Plan{
		OrganizationID:    itemOrganization(item, policy),
		ProjectID:         item.ProjectID,
		WorkItemID:        item.ID,
		RepositoryID:      item.RepositoryID,
		WorkItemKey:       item.Key,
		BranchType:        branchType,
		ReleaseBranch:     releaseBranch,
		BaseBranch:        baseBranch,
		TargetBranch:      targetBranch,
		WorkingBranch:     workingBranch,
		SyncBranches:      append([]string(nil), syncBranches...),
		CreatePullRequest: rule.CreatePullRequest,
		RequireSync:       rule.RequireSync,
		PolicyVersion:     policy.Version,
		WorkItemVersion:   item.Version,
		State:             PlanPending,
	}, nil
}

func ValidateBranchRef(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "HEAD" || value == "@" || strings.HasSuffix(value, ".") || len([]rune(value)) > 255 || branchRefInvalid.MatchString(value) {
		return fmt.Errorf("branch ref is invalid")
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("branch ref is invalid")
		}
	}
	return nil
}

func validateTemplate(value string) error {
	allowed := map[string]bool{"assignee": true, "type": true, "task-key": true, "slug": true}
	for _, match := range placeholderPattern.FindAllStringSubmatch(value, -1) {
		if !allowed[match[1]] {
			return fmt.Errorf("work_branch_template contains unsupported placeholder %q", match[1])
		}
	}
	return nil
}

func renderTemplate(template string, item WorkItemInput, branchType BranchType) (string, error) {
	assignee := slug(item.Assignee)
	if assignee == "" {
		assignee = slug(item.AssigneeID)
	}
	if assignee == "" && strings.Contains(template, "{assignee}") {
		return "", fmt.Errorf("assignee is required to generate a work branch")
	}
	result := strings.NewReplacer(
		"{assignee}", assignee,
		"{type}", slug(string(branchType)),
		"{task-key}", slug(item.Key),
		"{slug}", slug(item.Title),
	).Replace(template)
	if err := ValidateBranchRef(result); err != nil {
		return "", fmt.Errorf("generated working branch is invalid: %w", err)
	}
	return result, nil
}

func deriveBranchType(itemType string) BranchType {
	switch strings.ToUpper(strings.TrimSpace(itemType)) {
	case "BUG":
		return Fix
	case "EPIC", "STORY", "TASK", "SUB_TASK":
		return Feature
	default:
		return Feature
	}
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func findRule(rules []Rule, branchType BranchType) (Rule, bool) {
	for _, rule := range rules {
		if rule.BranchType == branchType {
			return rule, true
		}
	}
	return Rule{}, false
}

func resolveRole(policy Policy, role BranchRole, releaseBranch string) (string, error) {
	switch role {
	case Development:
		return policy.DevelopmentBranch, nil
	case Production:
		return policy.ProductionBranch, nil
	case ReleaseRole:
		if strings.TrimSpace(releaseBranch) == "" {
			return "", fmt.Errorf("release branch is required")
		}
		return releaseBranch, nil
	default:
		return "", fmt.Errorf("unsupported branch role %q", role)
	}
}

func itemOrganization(item WorkItemInput, policy Policy) string {
	return policy.OrganizationID
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validBranchType(value BranchType) bool {
	switch value {
	case Feature, Fix, Refactor, Chore, Hotfix, Release:
		return true
	default:
		return false
	}
}

func validRole(value BranchRole) bool {
	return value == Development || value == Production || value == ReleaseRole
}

func containsRole(values []BranchRole, wanted BranchRole) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func SortedRules(rules []Rule) []Rule {
	result := append([]Rule(nil), rules...)
	sort.Slice(result, func(i, j int) bool { return result[i].BranchType < result[j].BranchType })
	return result
}
