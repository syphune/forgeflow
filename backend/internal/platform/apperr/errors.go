package apperr

import (
	"errors"
	"fmt"
)

const (
	CodeInvalidArgument       = "INVALID_ARGUMENT"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeForbidden             = "FORBIDDEN"
	CodeNotFound              = "NOT_FOUND"
	CodeConflict              = "CONFLICT"
	CodeRateLimited           = "RATE_LIMITED"
	CodeSpecificationNotReady = "SPECIFICATION_NOT_READY"
	CodeStaleSpecification    = "STALE_SPECIFICATION"
	CodeAICannotVerify        = "AI_CANNOT_VERIFY"
	CodeInternal              = "INTERNAL"
)

type Error struct {
	Code    string
	Message string
	Details any
	Status  int
	Err     error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func New(code string, status int, message string, details any) *Error {
	return &Error{Code: code, Status: status, Message: message, Details: details}
}

func Wrap(code string, status int, message string, err error) *Error {
	return &Error{Code: code, Status: status, Message: message, Err: err}
}

func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Wrap(CodeInternal, 500, "internal server error", err)
}
