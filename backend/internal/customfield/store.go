package customfield

import "context"

type Store interface {
	ListDefinitions(context.Context, string, string) ([]Definition, error)
	GetDefinition(context.Context, string, string, string) (Definition, error)
	CreateDefinition(context.Context, string, CreateInput) (Definition, error)
	UpdateDefinition(context.Context, string, string, string, UpdateInput) (Definition, error)
	DeleteDefinition(context.Context, string, string, string) error
	ListValues(context.Context, string, string, string) ([]Value, error)
	SetValue(context.Context, string, string, string, string, TypedValue) (Value, error)
	ClearValue(context.Context, string, string, string, string) error
}
