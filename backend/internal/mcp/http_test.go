package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPProjectIDPrefersHeaderAndSupportsHTTPQuerySetup(t *testing.T) {
	queryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mcp?project_id=query-project", nil)
	if got := mcpProjectID(queryRequest); got != "query-project" {
		t.Fatalf("query project id = %q, want query-project", got)
	}

	headerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mcp?project_id=query-project", nil)
	headerRequest.Header.Set("X-Project-ID", "header-project")
	if got := mcpProjectID(headerRequest); got != "header-project" {
		t.Fatalf("header project id = %q, want header-project", got)
	}
}
