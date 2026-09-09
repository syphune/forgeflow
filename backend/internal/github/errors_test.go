package github

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	gh "github.com/google/go-github/v68/github"
)

func TestMapGitHubRepositoryError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		code    string
	}{
		{name: "empty tree", status: http.StatusConflict, message: "Git Repository is empty.", code: apperr.CodeRepositoryEmpty},
		{name: "empty contents", status: http.StatusNotFound, message: "This repository is empty.", code: apperr.CodeRepositoryEmpty},
		{name: "other github error", status: http.StatusForbidden, message: "Resource not accessible by integration", code: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := fmt.Errorf("github request: %w", &gh.ErrorResponse{Response: &http.Response{StatusCode: test.status}, Message: test.message})
			mapped := mapGitHubRepositoryError(err)
			if test.code == "" {
				if mapped == nil || apperr.From(mapped).Code == apperr.CodeRepositoryEmpty {
					t.Fatalf("mapped = %v", mapped)
				}
				return
			}
			if mapped == nil || apperr.From(mapped).Code != test.code {
				t.Fatalf("mapped = %v, code = %s", mapped, apperr.From(mapped).Code)
			}
			if apperr.From(mapped).Details.(map[string]any)["action"] != "initialize_repository" {
				t.Fatalf("details = %#v", apperr.From(mapped).Details)
			}
		})
	}
}

func TestMapGitHubRepositoryErrorMapsMissingResources(t *testing.T) {
	err := fmt.Errorf("github request: %w", &gh.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}, Message: "Branch not found"})
	mapped := mapGitHubRepositoryError(err)
	if mapped == nil || apperr.From(mapped).Code != apperr.CodeNotFound {
		t.Fatalf("mapped = %v, code = %s", mapped, apperr.From(mapped).Code)
	}
}

func TestMapGitHubRepositoryResponseErrorMapsMissingBranch(t *testing.T) {
	response := &gh.Response{Response: &http.Response{StatusCode: http.StatusNotFound}}
	mapped := mapGitHubRepositoryResponseError(response, errors.New("unexpected status code: 404 Not Found"))
	if mapped == nil || apperr.From(mapped).Code != apperr.CodeNotFound {
		t.Fatalf("mapped = %v, code = %s", mapped, apperr.From(mapped).Code)
	}
}

func TestMapGitHubPullRequestResponseErrorMapsPermissionAndValidation(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
	}{
		{status: http.StatusForbidden, code: apperr.CodeForbidden},
		{status: http.StatusUnprocessableEntity, code: apperr.CodeInvalidArgument},
	} {
		mapped := mapGitHubPullRequestResponseError(&gh.Response{Response: &http.Response{StatusCode: test.status}}, errors.New("github request failed"))
		if mapped == nil || apperr.From(mapped).Code != test.code {
			t.Fatalf("status %d mapped = %v, code = %s", test.status, mapped, apperr.From(mapped).Code)
		}
	}
}
