package notification

import "time"

type Notification struct {
	ID               string     `json:"id"`
	OrganizationID   string     `json:"organization_id"`
	UserID           string     `json:"user_id"`
	ProjectID        string     `json:"project_id,omitempty"`
	NotificationType string     `json:"notification_type"`
	Title            string     `json:"title"`
	Body             string     `json:"body"`
	ResourceType     string     `json:"resource_type,omitempty"`
	ResourceID       string     `json:"resource_id,omitempty"`
	ReadAt           *time.Time `json:"read_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}
