package impact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/google/uuid"
)

type Service struct {
	store       Store
	resolver    Resolver
	recorder    MutationRecorder
	transaction TransactionRunner
	approvalMu  sync.Mutex
	now         func() time.Time
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

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func NewService(store Store, resolver Resolver, options ...Options) *Service {
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
	return &Service{store: store, resolver: resolver, recorder: configured.Recorder, transaction: configured.Transaction, now: configured.Now}
}

func (s *Service) EnsureReport(ctx context.Context, subject Subject) (Report, error) {
	subject, err := normalizeSubject(subject)
	if err != nil {
		return Report{}, err
	}
	var result Report
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		report, buildErr := s.buildReport(txCtx, subject)
		if buildErr != nil {
			return buildErr
		}
		existing, getErr := s.store.LatestReport(txCtx, subject.OrganizationID, subject.AgentRunID)
		if getErr == nil && existing.InputHash == report.InputHash {
			result, getErr = s.withApprovals(txCtx, existing)
			return getErr
		}
		if getErr != nil && apperr.From(getErr).Code != apperr.CodeNotFound {
			return getErr
		}
		if getErr == nil {
			if statusErr := s.store.SetReportStatus(txCtx, subject.OrganizationID, existing.ID, ReportStale); statusErr != nil {
				return statusErr
			}
		}
		var createErr error
		result, createErr = s.store.CreateReport(txCtx, report)
		return createErr
	})
	return result, err
}

func (s *Service) ReadyForPublish(ctx context.Context, subject Subject) (Report, error) {
	var err error
	subject, err = normalizeSubject(subject)
	if err != nil {
		return Report{}, err
	}
	report, err := s.EnsureReport(ctx, subject)
	if err != nil {
		return Report{}, err
	}
	if subject.GateMode == GateReportOnly {
		return report, nil
	}
	if len(subject.ChangedPaths) == 0 {
		return report, nil
	}
	if strings.TrimSpace(subject.DiffHash) == "" || report.UnknownImpact || report.Truncated || report.Status != ReportApproved {
		return report, apperr.New(apperr.CodeImpactReviewRequired, 409, "impact approval is required before publishing AgentRun changes", map[string]any{
			"report_id": report.ID, "report_version": report.ReportVersion, "status": report.Status, "unknown_impact": report.UnknownImpact, "truncated": report.Truncated,
		})
	}
	return report, nil
}

func (s *Service) GetReport(ctx context.Context, actor identity.Actor, projectID, agentRunID string) (Report, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.TrimSpace(agentRunID) == "" {
		return Report{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and agent_run_id are required", nil)
	}
	if err := requireProject(actor, projectID, identity.CapabilityProjectRead); err != nil {
		return Report{}, err
	}
	report, err := s.store.LatestReport(ctx, actor.OrganizationID, strings.TrimSpace(agentRunID))
	if err != nil {
		return Report{}, err
	}
	if report.ProjectID != projectID && !reportAffectsProject(report, projectID) {
		return Report{}, apperr.New(apperr.CodeNotFound, 404, "impact report not found", nil)
	}
	return s.withApprovals(ctx, report)
}

func (s *Service) Approve(ctx context.Context, actor identity.Actor, affectedProjectID, agentRunID string, input ApprovalInput) (Report, error) {
	if actor.Type != "human" {
		return Report{}, apperr.New(apperr.CodeAICannotVerify, 403, "AI actors cannot approve repository impact", nil)
	}
	affectedProjectID = strings.TrimSpace(affectedProjectID)
	if affectedProjectID == "" || strings.TrimSpace(agentRunID) == "" {
		return Report{}, apperr.New(apperr.CodeInvalidArgument, 422, "affected_project_id and agent_run_id are required", nil)
	}
	if err := requireProject(actor, affectedProjectID, identity.CapabilityAgentApprove); err != nil {
		return Report{}, err
	}
	decision := strings.ToUpper(strings.TrimSpace(input.Decision))
	if decision != DecisionApproved && decision != DecisionRejected {
		return Report{}, apperr.New(apperr.CodeInvalidArgument, 422, "decision must be APPROVED or REJECTED", nil)
	}
	if len(input.Note) > 4000 {
		return Report{}, apperr.New(apperr.CodeInvalidArgument, 422, "approval note is too long", nil)
	}
	if input.ReportVersion <= 0 {
		return Report{}, apperr.New(apperr.CodeInvalidArgument, 422, "report_version is required", nil)
	}
	// ponytail: one process-wide lock; approvals are rare, use keyed locks if throughput matters.
	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()
	var result Report
	err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		report, getErr := s.store.LatestReport(txCtx, actor.OrganizationID, strings.TrimSpace(agentRunID))
		if getErr != nil {
			return getErr
		}
		result = report
		if input.ReportVersion != report.ReportVersion {
			return apperr.New(apperr.CodeStaleSpecification, 409, "impact approval is stale because the report changed", map[string]any{"report_version": report.ReportVersion})
		}
		if report.Status == ReportStale {
			return apperr.New(apperr.CodeConflict, 409, "impact report is stale; refresh the report before approving", nil)
		}
		if report.Status == ReportApproved || report.Status == ReportRejected {
			return apperr.New(apperr.CodeConflict, 409, "impact report has a final decision; create a new report before changing it", map[string]any{"status": report.Status})
		}
		affected := false
		for _, item := range report.AffectedProjects {
			if item.Project.ID == affectedProjectID {
				affected = true
				break
			}
		}
		if !affected {
			return apperr.New(apperr.CodeForbidden, 403, "project is not an affected project for this impact report", nil)
		}
		approval := Approval{OrganizationID: actor.OrganizationID, ReportID: report.ID, AffectedProjectID: affectedProjectID, Decision: decision, ApproverID: actor.ID, Note: strings.TrimSpace(input.Note)}
		if saveErr := s.store.SaveApproval(txCtx, approval); saveErr != nil {
			return saveErr
		}
		approvals, listErr := s.store.ListApprovals(txCtx, actor.OrganizationID, report.ID)
		if listErr != nil {
			return listErr
		}
		status := ReportPending
		if decision == DecisionRejected {
			status = ReportRejected
		}
		approved := make(map[string]string, len(approvals))
		for _, item := range approvals {
			approved[item.AffectedProjectID] = item.Decision
			if item.Decision == DecisionRejected {
				status = ReportRejected
			}
		}
		if status != ReportRejected && len(approved) == len(report.AffectedProjects) {
			status = ReportApproved
		}
		if statusErr := s.store.SetReportStatus(txCtx, actor.OrganizationID, report.ID, status); statusErr != nil {
			return statusErr
		}
		result.Status = status
		result.Approvals = approvals
		if status == ReportApproved || status == ReportRejected {
			if emitErr := s.emit(txCtx, actor.OrganizationID, result, status); emitErr != nil {
				return emitErr
			}
		}
		return nil
	})
	return result, err
}

func (s *Service) SaveScope(ctx context.Context, actor identity.Actor, input ScopeInput) (RepositoryProjectScope, error) {
	projectID, repositoryID, paths, modules, summary, err := normalizeScopeInput(input)
	if err != nil {
		return RepositoryProjectScope{}, err
	}
	if err := requireProject(actor, projectID, identity.CapabilityRepositoryManage); err != nil {
		return RepositoryProjectScope{}, err
	}
	scope := RepositoryProjectScope{ID: strings.TrimSpace(input.ID), OrganizationID: actor.OrganizationID, ProjectID: projectID, RepositoryID: repositoryID, PathPrefixes: paths, Modules: modules, ImpactSummary: summary, Provenance: ProvenanceHumanVerified, Status: ScopeActive, CreatedBy: actor.ID, VerifiedBy: actor.ID}
	var result RepositoryProjectScope
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if scope.ID != "" {
			existing, getErr := s.store.GetScope(txCtx, actor.OrganizationID, scope.ID)
			if getErr != nil {
				return getErr
			}
			if existing.ProjectID != projectID || existing.RepositoryID != repositoryID {
				return apperr.New(apperr.CodeForbidden, 403, "repository project scope belongs to another project or repository", nil)
			}
		}
		var saveErr error
		result, saveErr = s.store.SaveScope(txCtx, scope)
		if saveErr != nil {
			return saveErr
		}
		return s.record(txCtx, actor, "scope.save", result.ID, nil, result)
	})
	return result, err
}

func (s *Service) ListScopes(ctx context.Context, actor identity.Actor, projectID, repositoryID string) ([]RepositoryProjectScope, error) {
	projectID, repositoryID = strings.TrimSpace(projectID), strings.TrimSpace(repositoryID)
	if projectID == "" || repositoryID == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	if err := requireProject(actor, projectID, identity.CapabilityRepositoryRead); err != nil {
		return nil, err
	}
	return s.store.ListScopes(ctx, actor.OrganizationID, projectID, repositoryID)
}

func (s *Service) InferScope(ctx context.Context, actor identity.Actor, input ScopeInput) (RepositoryProjectScope, error) {
	projectID, repositoryID, paths, modules, summary, err := normalizeScopeInput(input)
	if err != nil {
		return RepositoryProjectScope{}, err
	}
	if err := requireProject(actor, projectID, identity.CapabilityRepositoryManage); err != nil {
		return RepositoryProjectScope{}, err
	}
	if len(paths) == 0 && len(modules) == 0 {
		return RepositoryProjectScope{}, apperr.New(apperr.CodeInvalidArgument, 422, "at least one path_prefix or module is required for inference", nil)
	}
	scope := RepositoryProjectScope{OrganizationID: actor.OrganizationID, ProjectID: projectID, RepositoryID: repositoryID, PathPrefixes: paths, Modules: modules, ImpactSummary: summary, Provenance: ProvenanceAIInferred, Status: ScopeProposed, CreatedBy: actor.ID}
	var result RepositoryProjectScope
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var saveErr error
		result, saveErr = s.store.SaveScope(txCtx, scope)
		if saveErr != nil {
			return saveErr
		}
		return s.record(txCtx, actor, "scope.infer", result.ID, nil, result)
	})
	return result, err
}

func (s *Service) Context(ctx context.Context, organizationID, projectID, repositoryID string) (Context, error) {
	projectID = strings.TrimSpace(projectID)
	repositoryID = strings.TrimSpace(repositoryID)
	if projectID == "" || repositoryID == "" {
		return Context{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	result := Context{CurrentProject: ProjectSummary{ID: projectID}, ContentTrust: "UNTRUSTED_CONTENT"}
	if s.resolver == nil {
		result.UnknownImpact = true
		return result, nil
	}
	projects, err := s.resolver.ProjectsForRepository(ctx, organizationID, repositoryID)
	if err != nil {
		return Context{}, err
	}
	currentFound := false
	for _, project := range projects {
		if project.ID == projectID {
			result.CurrentProject = project
			currentFound = true
		}
	}
	if !currentFound {
		result.UnknownImpact = true
	}
	for _, project := range projects {
		if project.ID == projectID {
			continue
		}
		if len(result.RelatedProjects) >= maxRelatedProjects {
			result.Truncated = true
			result.UnknownImpact = true
			continue
		}
		scopes, scopeErr := s.store.ListScopes(ctx, organizationID, project.ID, repositoryID)
		if scopeErr != nil {
			return Context{}, scopeErr
		}
		active := activeScopes(scopes)
		related := RelatedProject{Project: project, MappingStatus: "UNKNOWN", PotentiallyAffected: true, Reason: "mapping_missing"}
		if len(active) > 0 {
			related.PathPrefixes, related.Modules, related.ImpactSummary, related.Provenance = scopeSummary(active)
			if hasUnboundedScope(active) {
				related.Reason = "mapping_unbounded"
				result.UnknownImpact = true
			} else {
				related.MappingStatus = "MAPPED"
				related.PotentiallyAffected = false
				related.Reason = "advisory_path_mapping"
			}
		}
		result.RelatedProjects = append(result.RelatedProjects, related)
	}
	if len(result.RelatedProjects) > 0 {
		result.UnknownImpact = result.UnknownImpact || anyUnknown(result.RelatedProjects)
	}
	return result, nil
}

func (s *Service) ContextForActor(ctx context.Context, actor identity.Actor, projectID, repositoryID string) (Context, error) {
	result, err := s.Context(ctx, actor.OrganizationID, projectID, repositoryID)
	if err != nil {
		return Context{}, err
	}
	if actor.Has("*") {
		return result, nil
	}
	if actor.Source == "runner-mcp" {
		// The signed runner token is already pinned to the current project by
		// the MCP middleware. Related entries are bounded summaries only; no
		// project-scoped content is authorized by this context.
		return result, nil
	}
	if actor.ProjectCapabilities == nil {
		result.RelatedProjects = nil
		result.UnknownImpact = true
		return result, nil
	}
	filtered := result.RelatedProjects[:0]
	hidden := len(result.RelatedProjects)
	for _, related := range result.RelatedProjects {
		capabilities, ok := actor.ProjectCapabilities[related.Project.ID]
		if ok && (capabilities[identity.CapabilityProjectRead] || capabilities["*"]) {
			filtered = append(filtered, related)
		}
	}
	result.RelatedProjects = filtered
	if len(filtered) != hidden {
		result.UnknownImpact = true
	}
	return result, nil
}

func (s *Service) buildReport(ctx context.Context, subject Subject) (Report, error) {
	projects := []ProjectSummary{{ID: subject.ProjectID}}
	unknown := s.resolver == nil
	if s.resolver != nil {
		resolved, err := s.resolver.ProjectsForRepository(ctx, subject.OrganizationID, subject.RepositoryID)
		if err != nil {
			return Report{}, err
		}
		projects = resolved
	}
	currentFound := false
	for _, project := range projects {
		if project.ID == subject.ProjectID {
			currentFound = true
			break
		}
	}
	if !currentFound {
		projects = append(projects, ProjectSummary{ID: subject.ProjectID})
		unknown = true
	}
	sort.SliceStable(projects, func(i, j int) bool {
		if projects[i].ID == subject.ProjectID {
			return true
		}
		if projects[j].ID == subject.ProjectID {
			return false
		}
		return projects[i].ID < projects[j].ID
	})
	report := Report{OrganizationID: subject.OrganizationID, AgentRunID: subject.AgentRunID, ProjectID: subject.ProjectID, RepositoryID: subject.RepositoryID, BaseSHA: subject.BaseSHA, DiffHash: subject.DiffHash, ChangedPaths: append([]string(nil), subject.ChangedPaths...), Status: ReportPending}
	report.AffectedProjects = append(report.AffectedProjects, AffectedProject{Project: projects[0], Reason: "current_project", RequiresApproval: true})
	snapshot := make([]any, 0, len(projects))
	if len(projects)-1 > maxRelatedProjects {
		report.Truncated = true
		unknown = true
	}
	for index, project := range projects {
		scopes, err := s.store.ListScopes(ctx, subject.OrganizationID, project.ID, subject.RepositoryID)
		if err != nil {
			return Report{}, err
		}
		active := activeScopes(scopes)
		snapshot = append(snapshot, scopeSnapshot{ProjectID: project.ID, Scopes: active})
		if index == 0 {
			continue
		}
		if index > maxRelatedProjects {
			continue
		}
		reason := "mapping_missing"
		matches := false
		if len(subject.ChangedPaths) == 0 {
			unknown = true
			matches = true
			reason = "changed_paths_unknown"
		} else if len(active) == 0 {
			unknown = true
			matches = true
		} else if hasUnboundedScope(active) {
			unknown = true
			matches = true
			reason = "mapping_unbounded"
		} else {
			matches = scopesMatch(active, subject.ChangedPaths)
			if matches {
				reason = "path_scope_match"
			}
		}
		if matches {
			paths, modules, _, _ := scopeSummary(active)
			report.AffectedProjects = append(report.AffectedProjects, AffectedProject{Project: project, PathPrefixes: paths, Modules: modules, Reason: reason, RequiresApproval: true})
		}
	}
	report.UnknownImpact = unknown
	payload := struct {
		Subject  Subject           `json:"subject"`
		Mapping  []any             `json:"mapping"`
		Affected []AffectedProject `json:"affected"`
	}{Subject: subject, Mapping: snapshot, Affected: report.AffectedProjects}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Report{}, err
	}
	digest := sha256.Sum256(encoded)
	report.InputHash = hex.EncodeToString(digest[:])
	return report, nil
}

type scopeSnapshot struct {
	ProjectID string                   `json:"project_id"`
	Scopes    []RepositoryProjectScope `json:"scopes"`
}

func (s *Service) withApprovals(ctx context.Context, report Report) (Report, error) {
	approvals, err := s.store.ListApprovals(ctx, report.OrganizationID, report.ID)
	if err != nil {
		return Report{}, err
	}
	report.Approvals = approvals
	return report, nil
}

func (s *Service) emit(ctx context.Context, organizationID string, report Report, status string) error {
	if s.recorder.Outbox == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	eventType := "agent_run.impact_approved"
	if status == ReportRejected {
		eventType = "agent_run.impact_rejected"
	}
	return s.recorder.Outbox.Append(ctx, outbox.Event{ID: id, OrganizationID: organizationID, EventType: eventType, AggregateType: "agent_run", AggregateID: report.AgentRunID, IdempotencyKey: eventType + ":" + report.ID, Payload: map[string]any{"project_id": report.ProjectID, "agent_run_id": report.AgentRunID, "report_id": report.ID, "report_version": report.ReportVersion, "status": status}, OccurredAt: s.now().UTC()})
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit == nil {
		return nil
	}
	id, err := ids.New()
	if err != nil {
		return err
	}
	return s.recorder.Audit.Record(ctx, audit.Record{ID: id, ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: "impact." + action, ResourceType: "repository_project_scope", ResourceID: resourceID, Before: before, After: after, RequestID: httpapi.RequestID(ctx), CorrelationID: httpapi.CorrelationID(ctx), CreatedAt: s.now().UTC()})
}

func normalizeSubject(subject Subject) (Subject, error) {
	subject.OrganizationID = strings.TrimSpace(subject.OrganizationID)
	subject.ProjectID = strings.TrimSpace(subject.ProjectID)
	subject.WorkItemID = strings.TrimSpace(subject.WorkItemID)
	subject.RepositoryID = strings.TrimSpace(subject.RepositoryID)
	subject.AgentRunID = strings.TrimSpace(subject.AgentRunID)
	subject.BaseSHA = strings.TrimSpace(subject.BaseSHA)
	subject.Branch = strings.TrimSpace(subject.Branch)
	subject.DiffHash = strings.TrimSpace(subject.DiffHash)
	subject.GateMode = strings.ToUpper(strings.TrimSpace(subject.GateMode))
	if subject.GateMode == "" {
		subject.GateMode = GateReportOnly
	}
	if subject.GateMode != GateReportOnly && subject.GateMode != GateRequired {
		return Subject{}, apperr.New(apperr.CodeInvalidArgument, 422, "impact gate mode is invalid", nil)
	}
	if subject.OrganizationID == "" || subject.ProjectID == "" || subject.RepositoryID == "" || subject.AgentRunID == "" {
		return Subject{}, apperr.New(apperr.CodeInvalidArgument, 422, "organization_id, project_id, repository_id and agent_run_id are required", nil)
	}
	if len(subject.ChangedPaths) > maxPaths {
		return Subject{}, apperr.New(apperr.CodeInvalidArgument, 422, "changed_paths exceeds the limit", nil)
	}
	paths := make([]string, 0, len(subject.ChangedPaths))
	seen := make(map[string]struct{}, len(subject.ChangedPaths))
	for _, path := range subject.ChangedPaths {
		clean, err := normalizePath(path)
		if err != nil {
			return Subject{}, err
		}
		if clean != "" {
			if _, ok := seen[clean]; !ok {
				seen[clean] = struct{}{}
				paths = append(paths, clean)
			}
		}
	}
	sort.Strings(paths)
	subject.ChangedPaths = paths
	return subject, nil
}

func normalizeScopeInput(input ScopeInput) (string, string, []string, []string, string, error) {
	projectID, repositoryID := strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.RepositoryID)
	if projectID == "" || repositoryID == "" {
		return "", "", nil, nil, "", apperr.New(apperr.CodeInvalidArgument, 422, "project_id and repository_id are required", nil)
	}
	if scopeID := strings.TrimSpace(input.ID); scopeID != "" {
		if _, err := uuid.Parse(scopeID); err != nil {
			return "", "", nil, nil, "", apperr.New(apperr.CodeInvalidArgument, 422, "scope id must be a UUID", nil)
		}
	}
	paths := make([]string, 0, len(input.PathPrefixes))
	seenPaths := map[string]struct{}{}
	for _, path := range input.PathPrefixes {
		clean, err := normalizePath(path)
		if err != nil {
			return "", "", nil, nil, "", err
		}
		if clean != "" {
			if _, ok := seenPaths[clean]; !ok {
				seenPaths[clean] = struct{}{}
				paths = append(paths, clean)
			}
		}
	}
	modules, err := cleanModules(input.Modules)
	if err != nil {
		return "", "", nil, nil, "", err
	}
	summary := strings.TrimSpace(input.ImpactSummary)
	if summaryRunes := []rune(summary); len(summaryRunes) > maxSummaryBytes {
		summary = string(summaryRunes[:maxSummaryBytes])
	}
	sort.Strings(paths)
	sort.Strings(modules)
	return projectID, repositoryID, paths, modules, summary, nil
}

func normalizePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		return "", nil
	}
	if len([]byte(value)) > maxPathBytes {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, "path mapping is too long", nil)
	}
	if strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "../") || value == ".." || strings.Contains(value, "/../") || strings.HasPrefix(value, "./") {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, "path mapping contains an unsafe path", nil)
	}
	return value, nil
}

func cleanModules(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(result) >= maxScopeItems {
			continue
		}
		if len([]byte(value)) > maxModuleBytes || strings.ContainsAny(value, "\x00\r\n") {
			return nil, apperr.New(apperr.CodeInvalidArgument, 422, "module mapping is invalid or too long", nil)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func activeScopes(scopes []RepositoryProjectScope) []RepositoryProjectScope {
	result := make([]RepositoryProjectScope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.Status == ScopeActive && scope.Provenance != ProvenanceAIInferred {
			result = append(result, scope)
		}
	}
	return result
}

func scopesMatch(scopes []RepositoryProjectScope, paths []string) bool {
	for _, path := range paths {
		for _, scope := range scopes {
			for _, prefix := range scope.PathPrefixes {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					return true
				}
			}
			for _, module := range scope.Modules {
				if path == module || strings.HasPrefix(path, module+"/") || strings.Contains(path, "/"+module+"/") {
					return true
				}
			}
		}
	}
	return false
}

func scopeSummary(scopes []RepositoryProjectScope) ([]string, []string, string, string) {
	paths, modules := make([]string, 0), make([]string, 0)
	seenPaths, seenModules := map[string]struct{}{}, map[string]struct{}{}
	summary, provenance := "", ""
	for _, scope := range scopes {
		for _, path := range scope.PathPrefixes {
			if _, ok := seenPaths[path]; !ok {
				seenPaths[path] = struct{}{}
				paths = append(paths, path)
			}
		}
		for _, module := range scope.Modules {
			if _, ok := seenModules[module]; !ok {
				seenModules[module] = struct{}{}
				modules = append(modules, module)
			}
		}
		if summary == "" {
			summary = scope.ImpactSummary
		}
		if provenance == "" {
			provenance = scope.Provenance
		}
	}
	sort.Strings(paths)
	sort.Strings(modules)
	return paths, modules, summary, provenance
}

func anyUnknown(items []RelatedProject) bool {
	for _, item := range items {
		if item.PotentiallyAffected && (item.MappingStatus == "UNKNOWN" || item.Reason == "mapping_unbounded") {
			return true
		}
	}
	return false
}

func reportAffectsProject(report Report, projectID string) bool {
	for _, item := range report.AffectedProjects {
		if item.Project.ID == projectID {
			return true
		}
	}
	return false
}

func hasUnboundedScope(scopes []RepositoryProjectScope) bool {
	for _, scope := range scopes {
		if len(scope.PathPrefixes) == 0 && len(scope.Modules) == 0 {
			return true
		}
	}
	return false
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

func requireProject(actor identity.Actor, projectID, capability string) error {
	if strings.TrimSpace(projectID) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "project_id is required", nil)
	}
	if actor.ProjectCapabilities != nil {
		return require(actor.ForProject(projectID), capability)
	}
	return require(actor, capability)
}
