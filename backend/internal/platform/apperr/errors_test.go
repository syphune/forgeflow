package apperr

import (
	"fmt"
	"testing"
)

type sqlStateTestError string

func (e sqlStateTestError) Error() string    { return string(e) }
func (e sqlStateTestError) SQLState() string { return string(e) }

func TestFromMapsInvalidTextRepresentationWithoutLeakingDatabaseError(t *testing.T) {
	err := fmt.Errorf("load tenant: %w", sqlStateTestError("22P02"))
	appErr := From(err)
	if appErr.Code != CodeInvalidArgument || appErr.Status != 422 {
		t.Fatalf("mapped error = %#v, want INVALID_ARGUMENT/422", appErr)
	}
	if appErr.Message != "request contains an invalid identifier" {
		t.Fatalf("message = %q", appErr.Message)
	}
}

func TestFromKeepsUnknownErrorsInternal(t *testing.T) {
	appErr := From(fmt.Errorf("database unavailable"))
	if appErr.Code != CodeInternal || appErr.Status != 500 {
		t.Fatalf("mapped error = %#v, want INTERNAL/500", appErr)
	}
}
