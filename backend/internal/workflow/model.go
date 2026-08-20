package workflow

import (
	"regexp"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
)

const (
	Raw            = "RAW"
	Refining       = "REFINING"
	ReviewRequired = "REVIEW_REQUIRED"
	Ready          = "READY"
	InProgress     = "IN_PROGRESS"
	CodeReview     = "CODE_REVIEW"
	QA             = "QA"
	Done           = "DONE"
	Cancelled      = "CANCELLED"
)

type Status struct {
	Key        string `json:"key"`
	Name       string `json:"display_name"`
	Category   string `json:"category"`
	Position   int    `json:"position"`
	IsTerminal bool   `json:"is_terminal"`
}

type RuleType string

const (
	RequireSpecificationReady RuleType = "require_specification_ready"
	RequireHumanVerification  RuleType = "require_human_verification"
	RequireAssignee           RuleType = "require_assignee"
	RequireRepository         RuleType = "require_repository"
	RequirePullRequest        RuleType = "require_pull_request"
	RequireCISuccess          RuleType = "require_ci_success"
	RequirePermission         RuleType = "require_permission"
)

type Transition struct {
	Key                 string     `json:"key"`
	From                string     `json:"from_status"`
	To                  string     `json:"to_status"`
	Name                string     `json:"display_name"`
	Required            []RuleType `json:"required_rules,omitempty"`
	RequiredPermissions []string   `json:"required_permissions,omitempty"`
}

type Workflow struct {
	ID          string
	Name        string
	Statuses    map[string]Status
	Transitions map[string]Transition
}

// SaveInput is the complete project workflow definition. Keys are stable
// identifiers; changing a display name or rule does not rewrite work-item
// history.
type SaveInput struct {
	Name        string
	Statuses    []Status
	Transitions []Transition
}

var (
	statusKeyPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,31}$`)
	transitionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
	permissionPattern    = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,127}$`)
)

func validateSaveInput(input SaveInput) (SaveInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 {
		return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow name must be 1-120 characters", nil)
	}
	if len(input.Statuses) == 0 || len(input.Statuses) > 50 {
		return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow must contain 1-50 statuses", nil)
	}
	statusKeys := make(map[string]struct{}, len(input.Statuses))
	normalizedStatuses := make([]Status, 0, len(input.Statuses))
	for _, status := range input.Statuses {
		status.Key = strings.ToUpper(strings.TrimSpace(status.Key))
		status.Name = strings.TrimSpace(status.Name)
		status.Category = strings.ToUpper(strings.TrimSpace(status.Category))
		if !statusKeyPattern.MatchString(status.Key) || status.Name == "" || len(status.Name) > 120 {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "status key and display name are invalid", map[string]any{"key": status.Key})
		}
		if _, exists := statusKeys[status.Key]; exists {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow status keys must be unique", map[string]any{"key": status.Key})
		}
		if !validCategory(status.Category) {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow status category is invalid", map[string]any{"key": status.Key})
		}
		statusKeys[status.Key] = struct{}{}
		normalizedStatuses = append(normalizedStatuses, status)
	}
	if _, exists := statusKeys[Raw]; !exists {
		return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow must keep the RAW initial status", nil)
	}
	if len(input.Transitions) > 200 {
		return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow cannot contain more than 200 transitions", nil)
	}
	transitionKeys := make(map[string]struct{}, len(input.Transitions))
	pairs := make(map[string]struct{}, len(input.Transitions))
	normalizedTransitions := make([]Transition, 0, len(input.Transitions))
	for _, transition := range input.Transitions {
		transition.Key = strings.ToLower(strings.TrimSpace(transition.Key))
		transition.From = strings.ToUpper(strings.TrimSpace(transition.From))
		transition.To = strings.ToUpper(strings.TrimSpace(transition.To))
		transition.Name = strings.TrimSpace(transition.Name)
		if !transitionKeyPattern.MatchString(transition.Key) || transition.Name == "" || len(transition.Name) > 120 {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "transition key and display name are invalid", map[string]any{"key": transition.Key})
		}
		if _, exists := transitionKeys[transition.Key]; exists {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow transition keys must be unique", map[string]any{"key": transition.Key})
		}
		if _, exists := statusKeys[transition.From]; !exists {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "transition source status does not exist", map[string]any{"key": transition.Key})
		}
		if _, exists := statusKeys[transition.To]; !exists {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "transition target status does not exist", map[string]any{"key": transition.Key})
		}
		if transition.From == transition.To {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "a transition cannot target the same status", map[string]any{"key": transition.Key})
		}
		pair := transition.From + "\x00" + transition.To
		if _, exists := pairs[pair]; exists {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow transitions must have unique status pairs", map[string]any{"from_status": transition.From, "to_status": transition.To})
		}
		transitionKeys[transition.Key] = struct{}{}
		pairs[pair] = struct{}{}
		for _, rule := range transition.Required {
			if !validRule(rule) {
				return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow transition rule is invalid", map[string]any{"key": transition.Key, "rule": rule})
			}
		}
		transition.Required = normalizeRules(transition.Required)
		for _, permission := range transition.RequiredPermissions {
			if !permissionPattern.MatchString(strings.ToLower(strings.TrimSpace(permission))) {
				return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "workflow permission capability is invalid", map[string]any{"key": transition.Key, "permission": permission})
			}
		}
		transition.RequiredPermissions = normalizePermissions(transition.RequiredPermissions)
		requiresPermission := containsRule(transition.Required, RequirePermission)
		if requiresPermission && len(transition.RequiredPermissions) == 0 {
			return SaveInput{}, apperr.New(apperr.CodeInvalidArgument, 422, "require_permission needs at least one permission", map[string]any{"key": transition.Key})
		}
		if !requiresPermission {
			transition.RequiredPermissions = nil
		}
		if transition.To == Ready {
			transition.Required = appendMissingRule(transition.Required, RequireSpecificationReady)
		}
		normalizedTransitions = append(normalizedTransitions, transition)
	}
	return SaveInput{Name: input.Name, Statuses: normalizedStatuses, Transitions: normalizedTransitions}, nil
}

func validCategory(category string) bool {
	switch category {
	case "TODO", "IN_PROGRESS", "DONE", "CANCELLED":
		return true
	default:
		return false
	}
}

func normalizeRules(rules []RuleType) []RuleType {
	seen := make(map[RuleType]struct{}, len(rules))
	result := make([]RuleType, 0, len(rules))
	for _, rule := range rules {
		if !validRule(rule) {
			continue
		}
		if _, exists := seen[rule]; exists {
			continue
		}
		seen[rule] = struct{}{}
		result = append(result, rule)
	}
	return result
}

func normalizePermissions(permissions []string) []string {
	seen := make(map[string]struct{}, len(permissions))
	result := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permission = strings.ToLower(strings.TrimSpace(permission))
		if !permissionPattern.MatchString(permission) {
			continue
		}
		if _, exists := seen[permission]; exists {
			continue
		}
		seen[permission] = struct{}{}
		result = append(result, permission)
	}
	return result
}

func containsRule(rules []RuleType, wanted RuleType) bool {
	for _, rule := range rules {
		if rule == wanted {
			return true
		}
	}
	return false
}

func appendMissingRule(rules []RuleType, required RuleType) []RuleType {
	for _, rule := range rules {
		if rule == required {
			return rules
		}
	}
	return append(rules, required)
}

func validRule(rule RuleType) bool {
	switch rule {
	case RequireSpecificationReady, RequireHumanVerification, RequireAssignee, RequireRepository, RequirePullRequest, RequireCISuccess, RequirePermission:
		return true
	default:
		return false
	}
}

func Default() Workflow {
	statuses := []Status{
		{Key: Raw, Name: "Raw", Category: "TODO", Position: 10},
		{Key: Refining, Name: "Refining", Category: "TODO", Position: 20},
		{Key: ReviewRequired, Name: "Review required", Category: "TODO", Position: 30},
		{Key: Ready, Name: "Ready", Category: "TODO", Position: 40},
		{Key: InProgress, Name: "In progress", Category: "IN_PROGRESS", Position: 50},
		{Key: CodeReview, Name: "Code review", Category: "IN_PROGRESS", Position: 60},
		{Key: QA, Name: "QA", Category: "IN_PROGRESS", Position: 70},
		{Key: Done, Name: "Done", Category: "DONE", Position: 80, IsTerminal: true},
		{Key: Cancelled, Name: "Cancelled", Category: "CANCELLED", Position: 90, IsTerminal: true},
	}
	result := Workflow{ID: "default", Name: "Default", Statuses: make(map[string]Status, len(statuses)), Transitions: make(map[string]Transition)}
	for _, status := range statuses {
		result.Statuses[status.Key] = status
	}
	for _, transition := range []Transition{
		{Key: "start_refining", From: Raw, To: Refining, Name: "Start refining"},
		{Key: "request_review", From: Refining, To: ReviewRequired, Name: "Request review"},
		{Key: "mark_ready", From: ReviewRequired, To: Ready, Name: "Mark ready", Required: []RuleType{RequireSpecificationReady}},
		{Key: "start_work", From: Ready, To: InProgress, Name: "Start work", Required: []RuleType{RequireAssignee, RequireRepository}},
		{Key: "submit_code_review", From: InProgress, To: CodeReview, Name: "Submit code review", Required: []RuleType{RequirePullRequest}},
		{Key: "request_changes", From: CodeReview, To: InProgress, Name: "Request changes"},
		{Key: "move_to_qa", From: CodeReview, To: QA, Name: "Move to QA", Required: []RuleType{RequireCISuccess}},
		{Key: "qa_failed", From: QA, To: InProgress, Name: "Return to implementation"},
		{Key: "complete", From: QA, To: Done, Name: "Complete", Required: []RuleType{RequireHumanVerification}},
		{Key: "cancel", From: Raw, To: Cancelled, Name: "Cancel"},
		{Key: "cancel_from_refining", From: Refining, To: Cancelled, Name: "Cancel"},
		{Key: "cancel_from_review", From: ReviewRequired, To: Cancelled, Name: "Cancel"},
		{Key: "cancel_from_ready", From: Ready, To: Cancelled, Name: "Cancel"},
		{Key: "cancel_from_progress", From: InProgress, To: Cancelled, Name: "Cancel"},
	} {
		result.Transitions[transition.Key] = transition
	}
	return result
}
