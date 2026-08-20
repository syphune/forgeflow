package customfield

import "time"

type ValueType string

const (
	Text    ValueType = "TEXT"
	Number  ValueType = "NUMBER"
	Boolean ValueType = "BOOLEAN"
	Date    ValueType = "DATE"
	Select  ValueType = "SELECT"
)

type Definition struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	Key            string    `json:"key"`
	DisplayName    string    `json:"display_name"`
	ValueType      ValueType `json:"value_type"`
	Options        []string  `json:"options,omitempty"`
	Required       bool      `json:"required"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Value struct {
	DefinitionID string    `json:"definition_id"`
	WorkItemID   string    `json:"work_item_id"`
	Key          string    `json:"key"`
	DisplayName  string    `json:"display_name"`
	ValueType    ValueType `json:"value_type"`
	Value        string    `json:"value"`
	Options      []string  `json:"options,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateInput struct {
	ProjectID   string
	Key         string
	DisplayName string
	ValueType   ValueType
	Options     []string
	Required    bool
}

type UpdateInput struct {
	DisplayName *string
	Options     *[]string
	Required    *bool
}

type TypedValue struct {
	Text    *string
	Number  *float64
	Boolean *bool
	Date    *time.Time
	Option  *string
}
