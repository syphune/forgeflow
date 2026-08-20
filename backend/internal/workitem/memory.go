package workitem

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/forgeflow/forgeflow/backend/internal/workflow"
)

type MemoryRepository struct {
	mu       sync.Mutex
	items    map[string]*WorkItem
	numbers  map[string]int64
	comments map[string][]Comment
	links    map[string][]Link
	labels   map[string][]Label
	ordering map[string]int64
	now      func() time.Time
}

func NewMemoryRepository(now func() time.Time) *MemoryRepository {
	return &MemoryRepository{items: make(map[string]*WorkItem), numbers: make(map[string]int64), comments: make(map[string][]Comment), links: make(map[string][]Link), labels: make(map[string][]Label), ordering: make(map[string]int64), now: now}
}

func (r *MemoryRepository) Create(_ context.Context, scope Scope, input CreateInput) (*WorkItem, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Title) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "title is required", nil)
	}
	if !validType(input.Type) {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported work item type", map[string]any{"type": input.Type})
	}
	id, err := ids.New()
	if err != nil {
		return nil, err
	}
	key := scope.OrganizationID + ":" + scope.ProjectID
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.ParentID != "" {
		parent := r.items[input.ParentID]
		if parent == nil || parent.OrganizationID != scope.OrganizationID || parent.ProjectID != scope.ProjectID {
			return nil, apperr.New(apperr.CodeNotFound, 404, "parent work item not found", nil)
		}
	}
	r.numbers[key]++
	now := r.now().UTC()
	backlogRank := int64(1000)
	for _, existing := range r.items {
		if existing.OrganizationID == scope.OrganizationID && existing.ProjectID == scope.ProjectID && existing.SprintID == strings.TrimSpace(input.SprintID) && existing.BacklogRank >= backlogRank {
			backlogRank = existing.BacklogRank + 1000
		}
	}
	item := &WorkItem{
		ID: id, OrganizationID: scope.OrganizationID, WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID,
		Number: r.numbers[key], Key: projectKey(scope) + "-" + itoa(r.numbers[key]), Type: input.Type,
		Title: strings.TrimSpace(input.Title), Description: input.Description, ParentID: strings.TrimSpace(input.ParentID), Status: workflow.Raw,
		Priority: normalizedPriority(input.Priority), AssigneeID: strings.TrimSpace(input.AssigneeID), ReporterID: strings.TrimSpace(input.ReporterID), DueAt: input.DueAt, EstimatePoints: input.EstimatePoints,
		BacklogRank: backlogRank, RepositoryID: strings.TrimSpace(input.RepositoryID), SprintID: strings.TrimSpace(input.SprintID), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	r.items[id] = item
	r.ensureOrdering(scope, item.Status, item.SprintID)
	return clone(item), nil
}

func (r *MemoryRepository) Get(_ context.Context, scope Scope, id string) (*WorkItem, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[id]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	return clone(item), nil
}

func (r *MemoryRepository) List(_ context.Context, scope Scope, filter ListFilter) ([]*WorkItem, error) {
	page, err := r.ListPage(context.Background(), scope, filter)
	return page.Items, err
}

func (r *MemoryRepository) ListPage(_ context.Context, scope Scope, filter ListFilter) (ListResult, error) {
	if err := validateScope(scope); err != nil {
		return ListResult{}, err
	}
	sortOrder := strings.ToLower(strings.TrimSpace(filter.Sort))
	if sortOrder == "" {
		sortOrder = "updated"
	}
	if sortOrder != "updated" && sortOrder != "backlog" {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "unsupported work item sort", nil)
	}
	cursor, err := decodeWorkItemCursor(filter.Cursor)
	if err != nil {
		return ListResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "cursor is invalid", nil)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*WorkItem, 0, limit)
	for _, item := range r.items {
		if item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID || (filter.Status != "" && item.Status != filter.Status) || (filter.Type != "" && item.Type != Type(strings.ToUpper(filter.Type))) || (filter.Priority != "" && item.Priority != strings.ToUpper(filter.Priority)) || (filter.AssigneeID != "" && item.AssigneeID != filter.AssigneeID) || (filter.SprintID != "" && item.SprintID != filter.SprintID) || (filter.RepositoryID != "" && item.RepositoryID != filter.RepositoryID) || (filter.Query != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Description), strings.ToLower(filter.Query))) {
			continue
		}
		if item.ArchivedAt != nil && !filter.IncludeArchived {
			continue
		}
		if cursor.Sort == "backlog" {
			if sortOrder != "backlog" || item.BacklogRank < cursor.BacklogRank || (item.BacklogRank == cursor.BacklogRank && item.ID <= cursor.ID) {
				continue
			}
		} else if !cursor.UpdatedAt.IsZero() && (item.UpdatedAt.After(cursor.UpdatedAt) || (item.UpdatedAt.Equal(cursor.UpdatedAt) && item.ID >= cursor.ID)) {
			continue
		}
		result = append(result, clone(item))
	}
	sort.Slice(result, func(i, j int) bool {
		if sortOrder == "backlog" {
			if result[i].BacklogRank == result[j].BacklogRank {
				return result[i].ID < result[j].ID
			}
			return result[i].BacklogRank < result[j].BacklogRank
		}
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	var nextCursor string
	if len(result) > limit {
		last := result[limit-1]
		result = result[:limit]
		nextCursor, err = encodeWorkItemCursor(last, sortOrder)
		if err != nil {
			return ListResult{}, err
		}
	}
	return ListResult{Items: result, NextCursor: nextCursor}, nil
}

func (r *MemoryRepository) AddComment(_ context.Context, scope Scope, workItemID, authorID, body string) (Comment, error) {
	if err := validateScope(scope); err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, apperr.New(apperr.CodeInvalidArgument, 422, "comment body is required", nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return Comment{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	id, err := ids.New()
	if err != nil {
		return Comment{}, err
	}
	now := r.now().UTC()
	comment := Comment{ID: id, WorkItemID: workItemID, AuthorID: authorID, Body: body, CreatedAt: now, UpdatedAt: now}
	r.comments[workItemID] = append(r.comments[workItemID], comment)
	return comment, nil
}

func (r *MemoryRepository) ListComments(_ context.Context, scope Scope, workItemID string) ([]Comment, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	return append([]Comment(nil), r.comments[workItemID]...), nil
}

func (r *MemoryRepository) UpdateComment(_ context.Context, scope Scope, commentID, authorID, body string) (Comment, error) {
	if err := validateScope(scope); err != nil {
		return Comment{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, apperr.New(apperr.CodeInvalidArgument, 422, "comment body is required", nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, comment := range r.comments[commentIDWorkItem(r.comments, commentID)] {
		if comment.ID != commentID || comment.AuthorID != authorID || comment.DeletedAt != nil {
			continue
		}
		item := r.items[comment.WorkItemID]
		if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
			break
		}
		comment.Body = strings.TrimSpace(body)
		comment.UpdatedAt = r.now().UTC()
		r.comments[comment.WorkItemID][index] = comment
		return comment, nil
	}
	return Comment{}, apperr.New(apperr.CodeNotFound, 404, "comment not found", nil)
}

func (r *MemoryRepository) DeleteComment(_ context.Context, scope Scope, commentID, authorID string) (Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for workItemID, comments := range r.comments {
		for index, comment := range comments {
			if comment.ID != commentID || comment.AuthorID != authorID || comment.DeletedAt != nil {
				continue
			}
			item := r.items[workItemID]
			if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
				break
			}
			now := r.now().UTC()
			comment.Body = "[deleted]"
			comment.DeletedAt = &now
			comment.DeletedBy = authorID
			comment.UpdatedAt = now
			r.comments[workItemID][index] = comment
			return comment, nil
		}
	}
	return Comment{}, apperr.New(apperr.CodeNotFound, 404, "comment not found", nil)
}

func commentIDWorkItem(comments map[string][]Comment, commentID string) string {
	for workItemID, items := range comments {
		for _, item := range items {
			if item.ID == commentID {
				return workItemID
			}
		}
	}
	return ""
}

func (r *MemoryRepository) AddLink(_ context.Context, scope Scope, sourceID, targetID, relationType string) (Link, error) {
	if err := validateScope(scope); err != nil {
		return Link{}, err
	}
	if sourceID == targetID || strings.TrimSpace(relationType) == "" {
		return Link{}, apperr.New(apperr.CodeInvalidArgument, 422, "distinct work items and relation_type are required", nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	source, target := r.items[sourceID], r.items[targetID]
	if source == nil || target == nil || source.OrganizationID != scope.OrganizationID || target.OrganizationID != scope.OrganizationID || source.ProjectID != scope.ProjectID || target.ProjectID != scope.ProjectID {
		return Link{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	for _, link := range r.links[sourceID] {
		if link.TargetID == targetID && link.RelationType == relationType {
			return Link{}, apperr.New(apperr.CodeConflict, 409, "work item link already exists", nil)
		}
	}
	id, err := ids.New()
	if err != nil {
		return Link{}, err
	}
	link := Link{ID: id, SourceID: sourceID, TargetID: targetID, RelationType: strings.TrimSpace(relationType), CreatedAt: r.now().UTC()}
	r.links[sourceID] = append(r.links[sourceID], link)
	return link, nil
}

func (r *MemoryRepository) ListLinks(_ context.Context, scope Scope, workItemID string) ([]Link, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	return append([]Link(nil), r.links[workItemID]...), nil
}

func (r *MemoryRepository) RemoveLink(_ context.Context, scope Scope, workItemID, linkID string) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	links := r.links[workItemID]
	for index, link := range links {
		if link.ID == linkID {
			r.links[workItemID] = append(links[:index], links[index+1:]...)
			return nil
		}
	}
	return apperr.New(apperr.CodeNotFound, 404, "work item link not found", nil)
}

func (r *MemoryRepository) AddLabel(_ context.Context, scope Scope, workItemID, name, color string) (Label, error) {
	if err := validateScope(scope); err != nil {
		return Label{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return Label{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	for _, label := range r.labels[workItemID] {
		if strings.EqualFold(label.Name, name) {
			return label, nil
		}
	}
	id, err := ids.New()
	if err != nil {
		return Label{}, err
	}
	label := Label{ID: id, OrganizationID: scope.OrganizationID, Name: strings.TrimSpace(name), Color: strings.ToUpper(strings.TrimSpace(color))}
	r.labels[workItemID] = append(r.labels[workItemID], label)
	return label, nil
}

func (r *MemoryRepository) RemoveLabel(_ context.Context, scope Scope, workItemID, labelID string) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	labels := r.labels[workItemID]
	for i, label := range labels {
		if label.ID == labelID {
			r.labels[workItemID] = append(labels[:i], labels[i+1:]...)
			return nil
		}
	}
	return apperr.New(apperr.CodeNotFound, 404, "label is not attached to work item", nil)
}

func (r *MemoryRepository) ListLabels(_ context.Context, scope Scope, workItemID string) ([]Label, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[workItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	return append([]Label(nil), r.labels[workItemID]...), nil
}

func (r *MemoryRepository) Update(_ context.Context, scope Scope, id string, expectedVersion int64, mutate func(*WorkItem) error) (*WorkItem, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[id]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if expectedVersion != item.Version {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": item.Version})
	}
	updated := clone(item)
	if err := mutate(updated); err != nil {
		return nil, err
	}
	updated.Version++
	updated.UpdatedAt = r.now().UTC()
	r.items[id] = updated
	return clone(updated), nil
}

func (r *MemoryRepository) MoveRank(_ context.Context, scope Scope, id, direction string, expectedVersion int64) (*WorkItem, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	if direction != "up" && direction != "down" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "rank direction must be up or down", nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[id]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if item.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": item.Version})
	}
	candidates := make([]*WorkItem, 0, len(r.items))
	for _, candidate := range r.items {
		if candidate.OrganizationID == scope.OrganizationID && candidate.ProjectID == scope.ProjectID && candidate.SprintID == item.SprintID && candidate.ArchivedAt == nil {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].BacklogRank == candidates[j].BacklogRank {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].BacklogRank < candidates[j].BacklogRank
	})
	index := -1
	for i, candidate := range candidates {
		if candidate.ID == id {
			index = i
			break
		}
	}
	neighborIndex := index - 1
	if direction == "down" {
		neighborIndex = index + 1
	}
	if index < 0 || neighborIndex < 0 || neighborIndex >= len(candidates) {
		return clone(item), nil
	}
	neighbor := candidates[neighborIndex]
	itemRank := item.BacklogRank
	item.BacklogRank = neighbor.BacklogRank
	neighbor.BacklogRank = itemRank
	now := r.now().UTC()
	item.Version++
	item.UpdatedAt = now
	neighbor.Version++
	neighbor.UpdatedAt = now
	return clone(item), nil
}

func (r *MemoryRepository) Move(_ context.Context, scope Scope, input MoveInput) (MoveResult, error) {
	if err := validateScope(scope); err != nil {
		return MoveResult{}, err
	}
	if input.ExpectedVersion < 1 || input.ExpectedSourceOrderingVersion < 1 || input.ExpectedDestinationOrderingVersion < 1 {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "item and column ordering versions are required", nil)
	}
	targetStatus := strings.ToUpper(strings.TrimSpace(input.TargetStatus))
	if targetStatus == "" {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "target_status is required", nil)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.moveLocked(scope, input, targetStatus)
}

func (r *MemoryRepository) moveLocked(scope Scope, input MoveInput, targetStatus string) (MoveResult, error) {
	item := r.items[input.ItemID]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return MoveResult{}, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if item.Version != input.ExpectedVersion {
		return MoveResult{}, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": input.ExpectedVersion, "current_version": item.Version})
	}
	sourceKey := orderingKey(scope, item.Status, item.SprintID)
	destinationKey := orderingKey(scope, targetStatus, item.SprintID)
	sourceVersion := r.ensureOrdering(scope, item.Status, item.SprintID)
	destinationVersion := r.ensureOrdering(scope, targetStatus, item.SprintID)
	if input.ExpectedSourceOrderingVersion != sourceVersion || input.ExpectedDestinationOrderingVersion != destinationVersion {
		return MoveResult{}, staleOrderingError(input.ExpectedSourceOrderingVersion, sourceVersion, input.ExpectedDestinationOrderingVersion, destinationVersion)
	}
	if input.BeforeID != "" && input.BeforeID == input.AfterID {
		return MoveResult{}, apperr.New(apperr.CodeInvalidArgument, 422, "before_id and after_id must be different", nil)
	}
	destination := r.columnItems(scope, targetStatus, item.SprintID, item.ID)
	var beforeRank, afterRank int64
	if input.BeforeID != "" {
		neighbor, err := r.columnNeighbor(scope, targetStatus, item.SprintID, input.BeforeID)
		if err != nil {
			return MoveResult{}, err
		}
		beforeRank = neighbor.BacklogRank
	}
	if input.AfterID != "" {
		neighbor, err := r.columnNeighbor(scope, targetStatus, item.SprintID, input.AfterID)
		if err != nil {
			return MoveResult{}, err
		}
		afterRank = neighbor.BacklogRank
	}
	rank := insertionRank(destination, input.BeforeID, input.AfterID, beforeRank, afterRank)
	if !validInsertionRank(destination, input.BeforeID, input.AfterID, rank) {
		r.rebalanceColumn(destination)
		beforeRank, afterRank = 0, 0
		if input.BeforeID != "" {
			beforeRank = r.items[input.BeforeID].BacklogRank
		}
		if input.AfterID != "" {
			afterRank = r.items[input.AfterID].BacklogRank
		}
		rank = insertionRank(destination, input.BeforeID, input.AfterID, beforeRank, afterRank)
	}
	statusChanged := item.Status != targetStatus
	rankChanged := item.BacklogRank != rank
	if !statusChanged && !rankChanged {
		return MoveResult{Item: clone(item), SourceOrderingVersion: sourceVersion, DestinationOrderingVersion: destinationVersion}, nil
	}
	now := r.now().UTC()
	item.Status = targetStatus
	item.BacklogRank = rank
	item.Version++
	item.UpdatedAt = now
	if sourceKey == destinationKey {
		sourceVersion++
		r.ordering[sourceKey] = sourceVersion
		destinationVersion = sourceVersion
	} else {
		sourceVersion++
		destinationVersion++
		r.ordering[sourceKey] = sourceVersion
		r.ordering[destinationKey] = destinationVersion
	}
	return MoveResult{Item: clone(item), SourceOrderingVersion: sourceVersion, DestinationOrderingVersion: destinationVersion, Reordered: rankChanged || statusChanged}, nil
}

func (r *MemoryRepository) ColumnOrderingVersions(_ context.Context, scope Scope, sprintID string) (map[string]int64, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]int64)
	for _, item := range r.items {
		if item.OrganizationID == scope.OrganizationID && item.ProjectID == scope.ProjectID && item.SprintID == sprintID {
			result[item.Status] = r.ensureOrdering(scope, item.Status, sprintID)
		}
	}
	for key, version := range r.ordering {
		parts := strings.Split(key, "\x00")
		if len(parts) == 4 && parts[0] == scope.OrganizationID && parts[1] == scope.ProjectID && parts[3] == sprintID {
			result[parts[2]] = version
		}
	}
	return result, nil
}

func (r *MemoryRepository) ensureOrdering(scope Scope, status, sprintID string) int64 {
	key := orderingKey(scope, status, sprintID)
	if r.ordering[key] < 1 {
		r.ordering[key] = 1
	}
	return r.ordering[key]
}

func orderingKey(scope Scope, status, sprintID string) string {
	return scope.OrganizationID + "\x00" + scope.ProjectID + "\x00" + status + "\x00" + sprintID
}

func (r *MemoryRepository) columnItems(scope Scope, status, sprintID, excludeID string) []*WorkItem {
	items := make([]*WorkItem, 0)
	for _, item := range r.items {
		if item.ID != excludeID && item.OrganizationID == scope.OrganizationID && item.ProjectID == scope.ProjectID && item.Status == status && item.SprintID == sprintID && item.ArchivedAt == nil {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].BacklogRank == items[j].BacklogRank {
			return items[i].ID < items[j].ID
		}
		return items[i].BacklogRank < items[j].BacklogRank
	})
	return items
}

func (r *MemoryRepository) columnNeighbor(scope Scope, status, sprintID, id string) (*WorkItem, error) {
	item := r.items[id]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID || item.Status != status || item.SprintID != sprintID || item.ArchivedAt != nil {
		return nil, apperr.New(apperr.CodeNotFound, 404, "move neighbor not found in destination column", map[string]any{"id": id})
	}
	return item, nil
}

func insertionRank(items []*WorkItem, beforeID, afterID string, beforeRank, afterRank int64) int64 {
	switch {
	case beforeID != "" && afterID != "":
		return beforeRank + (afterRank-beforeRank)/2
	case beforeID != "":
		for index, item := range items {
			if item.ID == beforeID && index+1 < len(items) {
				return beforeRank + (items[index+1].BacklogRank-beforeRank)/2
			}
		}
		return beforeRank + 1000
	case afterID != "":
		for index, item := range items {
			if item.ID == afterID {
				if index == 0 {
					return afterRank / 2
				}
				return items[index-1].BacklogRank + (afterRank-items[index-1].BacklogRank)/2
			}
		}
		return afterRank - 1000
	case len(items) == 0:
		return 1000
	default:
		return items[len(items)-1].BacklogRank + 1000
	}
}

func validInsertionRank(items []*WorkItem, beforeID, afterID string, rank int64) bool {
	if rank <= 0 {
		return false
	}
	for _, item := range items {
		if item.ID == beforeID && rank <= item.BacklogRank {
			return false
		}
		if item.ID == afterID && rank >= item.BacklogRank {
			return false
		}
		if item.ID != beforeID && item.ID != afterID && item.BacklogRank == rank {
			return false
		}
	}
	return true
}

func (r *MemoryRepository) rebalanceColumn(items []*WorkItem) {
	for index, item := range items {
		item.BacklogRank = int64(index+1) * 1000
		item.Version++
		item.UpdatedAt = r.now().UTC()
	}
}

func staleOrderingError(expectedSource, source, expectedDestination, destination int64) error {
	return apperr.New(apperr.CodeConflict, 409, "column ordering version is stale", map[string]any{
		"expected_source_ordering_version":      expectedSource,
		"source_ordering_version":               source,
		"expected_destination_ordering_version": expectedDestination,
		"destination_ordering_version":          destination,
	})
}

func (r *MemoryRepository) Archive(_ context.Context, scope Scope, id string, expectedVersion int64, actorID string) (*WorkItem, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[id]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if item.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": item.Version})
	}
	if item.ArchivedAt != nil {
		return clone(item), nil
	}
	now := r.now().UTC()
	updated := clone(item)
	updated.ArchivedAt = &now
	updated.ArchivedBy = strings.TrimSpace(actorID)
	updated.Version++
	updated.UpdatedAt = now
	r.items[id] = updated
	return clone(updated), nil
}

func (r *MemoryRepository) Restore(_ context.Context, scope Scope, id string, expectedVersion int64) (*WorkItem, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.items[id]
	if item == nil || item.OrganizationID != scope.OrganizationID || item.ProjectID != scope.ProjectID {
		return nil, apperr.New(apperr.CodeNotFound, 404, "work item not found", nil)
	}
	if item.Version != expectedVersion {
		return nil, apperr.New(apperr.CodeConflict, 409, "work item version is stale", map[string]any{"expected_version": expectedVersion, "current_version": item.Version})
	}
	if item.ArchivedAt == nil {
		return clone(item), nil
	}
	updated := clone(item)
	updated.ArchivedAt = nil
	updated.ArchivedBy = ""
	updated.Version++
	updated.UpdatedAt = r.now().UTC()
	r.items[id] = updated
	return clone(updated), nil
}

func validateScope(scope Scope) error {
	if strings.TrimSpace(scope.OrganizationID) == "" || strings.TrimSpace(scope.ProjectID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "organization and project scope are required", nil)
	}
	return nil
}

func projectKey(scope Scope) string {
	if strings.TrimSpace(scope.ProjectKey) == "" {
		return "PROJ"
	}
	return strings.ToUpper(strings.TrimSpace(scope.ProjectKey))
}

func validType(itemType Type) bool {
	switch itemType {
	case Epic, Story, Task, Bug, SubTask:
		return true
	default:
		return false
	}
}

func clone(item *WorkItem) *WorkItem {
	if item == nil {
		return nil
	}
	result := *item
	result.Labels = append([]Label(nil), item.Labels...)
	return &result
}

func normalizedPriority(priority string) string {
	priority = strings.ToUpper(strings.TrimSpace(priority))
	if priority == "" {
		return PriorityMedium
	}
	return priority
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var result [20]byte
	i := len(result)
	for n > 0 {
		i--
		result[i] = byte('0' + n%10)
		n /= 10
	}
	return string(result[i:])
}
