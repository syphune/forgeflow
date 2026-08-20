package attachment

import "context"

type Store interface {
	List(context.Context, string, string, string) ([]Attachment, error)
	Create(context.Context, Attachment) (Attachment, error)
	Get(context.Context, string, string, string, string) (Attachment, error)
	Delete(context.Context, string, string, string, string) error
}
