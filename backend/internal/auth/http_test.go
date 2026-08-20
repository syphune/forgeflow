package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateScopesRejectsUnknownAndDeduplicates(t *testing.T) {
	scopes, err := validateScopes([]string{"project.read", "project.read", "work_item.edit", "autonomous.start", "autonomous.retry", "autonomous.cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 5 {
		t.Fatalf("got %d scopes", len(scopes))
	}
	if _, err := validateScopes([]string{"shell.execute"}); err == nil {
		t.Fatal("expected unsupported scope error")
	}
	if _, err := validateScopes(nil); err == nil {
		t.Fatal("expected explicit token scope requirement")
	}
}

func TestDecodeJSONRejectsTrailingValues(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"} nope`))
	w := httptest.NewRecorder()
	var target struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, 1024, &target); err == nil {
		t.Fatal("expected trailing input error")
	}
}
