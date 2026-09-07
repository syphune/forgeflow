package impact

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestHandlerScopesImpactReportAndApproval(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), testResolver{projects: []ProjectSummary{{ID: "project-1"}, {ID: "project-2"}}})
	handler := NewHandler(service, 1<<20)
	actor := testActor("owner", identity.CapabilityRepositoryManage, identity.CapabilityRepositoryRead, identity.CapabilityProjectRead, identity.CapabilityAgentApprove)

	request := func(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		var payload bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&payload).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, &payload)
		req = req.WithContext(identity.WithActor(req.Context(), actor))
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	scopeResponse := request(http.MethodPut, "/projects/project-2/repositories/repo-1/impact-scope", map[string]any{"path_prefixes": []string{"apps/web"}}, nil)
	if scopeResponse.Code != http.StatusOK {
		t.Fatalf("save scope status = %d, body = %s", scopeResponse.Code, scopeResponse.Body.String())
	}
	listResponse := request(http.MethodGet, "/projects/project-2/repositories/repo-1/impact-scope", nil, nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list scope status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}

	report, err := service.EnsureReport(ctx, Subject{OrganizationID: "org-1", ProjectID: "project-1", RepositoryID: "repo-1", AgentRunID: "run-1", ChangedPaths: []string{"apps/web/page.tsx"}, DiffHash: "diff-1", GateMode: GateRequired})
	if err != nil {
		t.Fatal(err)
	}
	reportResponse := request(http.MethodGet, "/agent-runs/run-1/impact-report", nil, map[string]string{"X-Project-ID": "project-1"})
	if reportResponse.Code != http.StatusOK {
		t.Fatalf("get report status = %d, body = %s", reportResponse.Code, reportResponse.Body.String())
	}
	approvalResponse := request(http.MethodPost, "/projects/project-1/agent-runs/run-1/impact-approval", map[string]any{"report_version": report.ReportVersion, "decision": DecisionApproved}, nil)
	if approvalResponse.Code != http.StatusOK {
		t.Fatalf("current-project approval status = %d, body = %s", approvalResponse.Code, approvalResponse.Body.String())
	}
	approvalResponse = request(http.MethodPost, "/projects/project-2/agent-runs/run-1/impact-approval", map[string]any{"report_version": report.ReportVersion, "decision": DecisionApproved}, nil)
	if approvalResponse.Code != http.StatusOK {
		t.Fatalf("related-project approval status = %d, body = %s", approvalResponse.Code, approvalResponse.Body.String())
	}
	var approved Report
	if err := json.Unmarshal(approvalResponse.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}
	if approved.Status != ReportApproved || len(approved.Approvals) != 2 {
		t.Fatalf("approved report = %#v", approved)
	}
}
