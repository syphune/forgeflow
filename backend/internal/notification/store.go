package notification

import "context"

type Store interface {
	List(context.Context, string, string, int) ([]Notification, error)
	CountUnread(context.Context, string, string) (int, error)
	MarkRead(context.Context, string, string, string) error
	MarkAllRead(context.Context, string, string) error
	CreateForProject(context.Context, string, string, string, string, string, string, string) error
}
