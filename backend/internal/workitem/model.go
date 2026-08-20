package workitem

import "time"

type Type string

const (
	Epic    Type = "EPIC"
	Story   Type = "STORY"
	Task    Type = "TASK"
	Bug     Type = "BUG"
	SubTask Type = "SUB_TASK"
)

type Scope struct {
	OrganizationID string
	WorkspaceID    string
	ProjectID      string
	ProjectKey     string
}

type WorkItem struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	WorkspaceID    string     `json:"workspace_id"`
	ProjectID      string     `json:"project_id"`
	Number         int64      `json:"number"`
	Key            string     `json:"key"`
	Type           Type       `json:"type"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	ParentID       string     `json:"parent_id,omitempty"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority"`
	AssigneeID     string     `json:"assignee_id,omitempty"`
	ReporterID     string     `json:"reporter_id,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	EstimatePoints *int       `json:"estimate_points,omitempty"`
	BacklogRank    int64      `json:"backlog_rank"`
	RepositoryID   string     `json:"repository_id,omitempty"`
	SprintID       string     `json:"sprint_id,omitempty"`
	Labels         []Label    `json:"labels,omitempty"`
	Version        int64      `json:"version"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	ArchivedBy     string     `json:"archived_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type CreateInput struct {
	Type           Type
	Title          string
	Description    string
	ParentID       string
	RepositoryID   string
	AssigneeID     string
	ReporterID     string
	Priority       string
	DueAt          *time.Time
	EstimatePoints *int
	SprintID       string
}

type UpdateInput struct {
	Title             *string
	Description       *string
	ParentID          *string
	ParentIDSet       bool
	RepositoryID      *string
	RepositoryIDSet   bool
	Priority          *string
	DueAt             *time.Time
	DueAtSet          bool
	EstimatePoints    *int
	EstimatePointsSet bool
	SprintID          *string
	SprintIDSet       bool
	ExpectedVersion   int64
}

type ListFilter struct {
	Status          string
	Type            string
	Priority        string
	AssigneeID      string
	SprintID        string
	RepositoryID    string
	Query           string
	Sort            string
	Limit           int
	Cursor          string
	IncludeArchived bool
}

type TransitionInput struct {
	TransitionKey   string
	ExpectedVersion int64
}

type MoveInput struct {
	ItemID                             string
	TargetStatus                       string
	TransitionKey                      string
	BeforeID                           string
	AfterID                            string
	ExpectedVersion                    int64
	ExpectedSourceOrderingVersion      int64
	ExpectedDestinationOrderingVersion int64
}

type MoveResult struct {
	Item                       *WorkItem
	SourceOrderingVersion      int64
	DestinationOrderingVersion int64
	Reordered                  bool
}

type ColumnOrdering struct {
	Status   string `json:"status"`
	SprintID string `json:"sprint_id,omitempty"`
	Version  int64  `json:"ordering_version"`
}

type Comment struct {
	ID         string     `json:"id"`
	WorkItemID string     `json:"work_item_id"`
	AuthorID   string     `json:"author_id"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
	DeletedBy  string     `json:"deleted_by,omitempty"`
}

type Link struct {
	ID           string    `json:"id"`
	SourceID     string    `json:"source_id"`
	TargetID     string    `json:"target_id"`
	RelationType string    `json:"relation_type"`
	CreatedAt    time.Time `json:"created_at"`
}

type Label struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Color          string `json:"color"`
}
