package workitem

import "context"

type Repository interface {
	Create(context.Context, Scope, CreateInput) (*WorkItem, error)
	Get(context.Context, Scope, string) (*WorkItem, error)
	List(context.Context, Scope, ListFilter) ([]*WorkItem, error)
	ListPage(context.Context, Scope, ListFilter) (ListResult, error)
	Update(context.Context, Scope, string, int64, func(*WorkItem) error) (*WorkItem, error)
	MoveRank(context.Context, Scope, string, string, int64) (*WorkItem, error)
	Move(context.Context, Scope, MoveInput) (MoveResult, error)
	ColumnOrderingVersions(context.Context, Scope, string) (map[string]int64, error)
	Archive(context.Context, Scope, string, int64, string) (*WorkItem, error)
	Restore(context.Context, Scope, string, int64) (*WorkItem, error)
	AddComment(context.Context, Scope, string, string, string) (Comment, error)
	ListComments(context.Context, Scope, string) ([]Comment, error)
	UpdateComment(context.Context, Scope, string, string, string) (Comment, error)
	DeleteComment(context.Context, Scope, string, string) (Comment, error)
	AddLink(context.Context, Scope, string, string, string) (Link, error)
	ListLinks(context.Context, Scope, string) ([]Link, error)
	RemoveLink(context.Context, Scope, string, string) error
	AddLabel(context.Context, Scope, string, string, string) (Label, error)
	RemoveLabel(context.Context, Scope, string, string) error
	ListLabels(context.Context, Scope, string) ([]Label, error)
}
