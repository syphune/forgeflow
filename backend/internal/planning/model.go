package planning

import "time"

type Status string

const (
	Planned   Status = "PLANNED"
	Active    Status = "ACTIVE"
	Completed Status = "COMPLETED"
)

type Sprint struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	ProjectID      string     `json:"project_id"`
	Name           string     `json:"name"`
	Goal           string     `json:"goal"`
	StartsAt       *time.Time `json:"starts_at,omitempty"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
	Status         Status     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
