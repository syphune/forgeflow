package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

var definitions = []ToolDefinition{
	{Name: "work_item.list", Description: "List work items in an authorized project scope.", InputSchema: objectSchema()},
	{Name: "work_item.search", Description: "Search authorized work items using bounded lexical filters.", InputSchema: objectSchema()},
	{Name: "work_item.get", Description: "Get one authorized work item.", InputSchema: objectSchema("id")},
	{Name: "work_item.get_context", Description: "Get bounded, provenance-labeled context for an authorized work item.", InputSchema: objectSchema("id")},
	{Name: "work_item.create", Description: "Create a work item through the application service.", InputSchema: objectSchema("project_id", "type", "title")},
	{Name: "work_item.update", Description: "Update editable work-item fields; status changes are not accepted here.", InputSchema: objectSchema("id")},
	{Name: "work_item.assign", Description: "Assign a work item through the application service.", InputSchema: objectSchema("id", "assignee_id")},
	{Name: "work_item.transition", Description: "Request a server-side workflow transition with an expected version.", InputSchema: objectSchema("id", "transition_key", "expected_version")},
	{Name: "work_item.comment", Description: "Add a comment to an authorized work item.", InputSchema: objectSchema("id", "body")},
	{Name: "specification.get", Description: "Read specification fields, provenance, verification, and readiness details.", InputSchema: objectSchema("work_item_id")},
	{Name: "specification.propose", Description: "Submit an AI-inferred or AI-hypothesis proposal; never verifies facts.", InputSchema: objectSchema("work_item_id", "field", "value", "provenance")},
	{Name: "specification.propose_analysis", Description: "Submit an untrusted root-cause analysis with an implementation and test plan; never creates a verified fact.", InputSchema: objectSchema("work_item_id", "root_cause_hypothesis", "implementation_plan", "test_plan", "confidence")},
	{Name: "specification.accept_proposal", Description: "Accept a proposal as unverified and preserve its provenance.", InputSchema: objectSchema("work_item_id", "proposal_id")},
	{Name: "specification.reject_proposal", Description: "Reject a pending AI proposal without changing verified specification content.", InputSchema: objectSchema("work_item_id", "proposal_id")},
	{Name: "specification.verify_field", Description: "Explicit human-only verification; agent actors are denied.", InputSchema: objectSchema("work_item_id", "field")},
	{Name: "specification.request_clarification", Description: "Request human clarification for an incomplete specification.", InputSchema: objectSchema("work_item_id", "question")},
	{Name: "repository.get", Description: "Read authorized repository metadata.", InputSchema: objectSchema("repository_id")},
	{Name: "repository.get_structure", Description: "Read a bounded repository tree, preferring the latest fixed-commit snapshot.", InputSchema: objectSchema("repository_id")},
	{Name: "repository.search_code", Description: "Search the latest fixed-commit repository snapshot with bounded lexical results.", InputSchema: objectSchema("repository_id", "query")},
	{Name: "repository.get_file", Description: "Read one bounded authorized file from the linked default branch.", InputSchema: objectSchema("repository_id", "path")},
	{Name: "repository.get_symbol", Description: "Find an extracted symbol in the latest fixed-commit snapshot.", InputSchema: objectSchema("repository_id", "symbol")},
	{Name: "repository.related_files", Description: "Read parser-proven related files only.", InputSchema: objectSchema("repository_id", "path")},
	{Name: "repository.related_commits", Description: "Read related commits from authorized repository history.", InputSchema: objectSchema("repository_id", "path")},
	{Name: "repository.related_pull_requests", Description: "Read related pull requests from authorized repository history.", InputSchema: objectSchema("repository_id", "path")},
	{Name: "agent_run.get", Description: "Read an authorized AgentRun and its bounded artifacts.", InputSchema: objectSchema("id")},
	{Name: "agent_run.create", Description: "Create an AgentRun subject to server approval and policy; rerun only the selected unresolved test cases when provided.", InputSchema: objectSchema("work_item_id", "repository_id")},
	{Name: "agent_run.cancel", Description: "Cancel an authorized AgentRun.", InputSchema: objectSchema("id")},
	{Name: "agent_run.attach_result", Description: "Attach redacted, bounded agent metadata; claims remain untrusted.", InputSchema: objectSchema("id", "result")},
	{Name: "agent_run.record_test_results", Description: "Record pass, fail, blocked, and not-run results with notes and evidence; passed cases remain available for incremental reruns.", InputSchema: objectSchema("id")},
	{Name: "autonomous.start", Description: "Start a policy-authorized autonomous workflow from a manager or leader objective; specification verification and deployment approval remain human-only.", InputSchema: objectSchema("objective")},
	{Name: "autonomous.get", Description: "Read an autonomous workflow, its current gate, attempts, feedback, and test progress.", InputSchema: objectSchema("id")},
	{Name: "autonomous.resume", Description: "Resume an autonomous workflow after a human specification review or clarification gate.", InputSchema: objectSchema("id")},
	{Name: "autonomous.retry", Description: "Retry an autonomous workflow with reviewer feedback and only the unresolved test cases.", InputSchema: objectSchema("id")},
	{Name: "autonomous.cancel", Description: "Cancel an autonomous workflow and its active AgentRun.", InputSchema: objectSchema("id")},
	{Name: "autonomous.add_feedback", Description: "Add bounded human, agent, or CI feedback to an autonomous workflow.", InputSchema: objectSchema("id", "note")},
	{Name: "autonomous.record_test_results", Description: "Record test results for the active autonomous attempt and preserve passing cases across retries.", InputSchema: objectSchema("id", "test_cases")},
}

func Definitions() []ToolDefinition {
	result := make([]ToolDefinition, len(definitions))
	for i, definition := range definitions {
		result[i] = definition
		result[i].InputSchema = cloneSchema(definition.InputSchema)
	}
	return result
}

func cloneSchema(schema map[string]any) map[string]any {
	raw, err := json.Marshal(schema)
	if err != nil {
		return map[string]any{}
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		return map[string]any{}
	}
	return clone
}

func SnapshotHash() string {
	b, _ := json.Marshal(definitions)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

type Adapter interface {
	Call(context.Context, string, map[string]any) (any, error)
}

func Register(server *sdk.Server, logger *slog.Logger, adapters ...Adapter) {
	for _, definition := range definitions {
		definition := definition
		inputSchema, _ := json.Marshal(definition.InputSchema)
		server.AddTool(&sdk.Tool{Name: definition.Name, Description: definition.Description, InputSchema: json.RawMessage(inputSchema)}, func(ctx context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			if len(adapters) == 0 {
				logger.Warn("MCP tool denied: application adapter is not configured", "tool", definition.Name)
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("tool %s requires an authenticated application adapter", definition.Name)}}, IsError: true}, nil
			}
			var args map[string]any
			if request.Params != nil && len(request.Params.Arguments) > 0 {
				if err := json.Unmarshal(request.Params.Arguments, &args); err != nil {
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "invalid tool arguments"}}, IsError: true}, nil
				}
			}
			result, err := adapters[0].Call(ctx, definition.Name, args)
			if err != nil {
				logger.Warn("MCP tool failed", "tool", definition.Name, "error", err)
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: apperr.From(err).Message}}, IsError: true}, nil
			}
			payload, err := json.Marshal(result)
			if err != nil {
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "tool result could not be encoded"}}, IsError: true}, nil
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(payload)}}}, nil
		})
	}
}

func objectSchema(required ...string) map[string]any {
	properties := map[string]any{
		"id":                    map[string]any{"type": "string"},
		"project_id":            map[string]any{"type": "string"},
		"work_item_id":          map[string]any{"type": "string"},
		"repository_id":         map[string]any{"type": "string"},
		"proposal_id":           map[string]any{"type": "string"},
		"type":                  map[string]any{"type": "string"},
		"title":                 map[string]any{"type": "string"},
		"description":           map[string]any{"type": "string"},
		"priority":              map[string]any{"type": "string", "enum": []string{"LOWEST", "LOW", "MEDIUM", "HIGH", "HIGHEST"}},
		"due_at":                map[string]any{"type": "string", "format": "date-time"},
		"estimate_points":       map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
		"sprint_id":             map[string]any{"type": "string"},
		"parent_id":             map[string]any{"type": "string"},
		"status":                map[string]any{"type": "string"},
		"query":                 map[string]any{"type": "string", "maxLength": 256},
		"cursor":                map[string]any{"type": "string", "maxLength": 512},
		"include_archived":      map[string]any{"type": "boolean"},
		"assignee_id":           map[string]any{"type": "string"},
		"transition_key":        map[string]any{"type": "string"},
		"body":                  map[string]any{"type": "string"},
		"field":                 map[string]any{"type": "string"},
		"value":                 map[string]any{"type": "string"},
		"provenance":            map[string]any{"type": "string"},
		"root_cause_hypothesis": map[string]any{"type": "string", "maxLength": 10000},
		"blast_radius":          map[string]any{"type": "string", "maxLength": 10000},
		"implementation_plan":   map[string]any{"type": "string", "maxLength": 10000},
		"test_plan":             map[string]any{"type": "string", "maxLength": 10000},
		"evidence_refs":         map[string]any{"type": "array", "maxItems": 30, "items": map[string]any{"type": "string", "maxLength": 512}},
		"confidence":            map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"question":              map[string]any{"type": "string", "maxLength": 2000},
		"agent_provider":        map[string]any{"type": "string", "maxLength": 80},
		"agent_name":            map[string]any{"type": "string", "maxLength": 120},
		"model":                 map[string]any{"type": "string", "maxLength": 120},
		"base_sha":              map[string]any{"type": "string", "maxLength": 128},
		"branch":                map[string]any{"type": "string", "maxLength": 256},
		"path":                  map[string]any{"type": "string", "maxLength": 1024},
		"symbol":                map[string]any{"type": "string", "maxLength": 256},
		"limit":                 map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		"position":              map[string]any{"type": "integer", "minimum": 1},
		"expected_version":      map[string]any{"type": "integer", "minimum": 1},
		"result":                map[string]any{"type": "object", "additionalProperties": true},
		"commit_sha":            map[string]any{"type": "string", "maxLength": 128},
		"pull_request_id":       map[string]any{"type": "string", "maxLength": 128},
		"error":                 map[string]any{"type": "string", "maxLength": 4000},
		"metadata":              map[string]any{"type": "object", "additionalProperties": true},
		"prompt":                map[string]any{"type": "string", "maxLength": 131072},
		"test_case_positions":   map[string]any{"type": "array", "maxItems": 200, "items": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000}},
		"review_note":           map[string]any{"type": "string", "maxLength": 4000},
		"feedback":              map[string]any{"type": "string", "maxLength": 4000},
		"note":                  map[string]any{"type": "string", "maxLength": 4000},
		"severity":              map[string]any{"type": "string", "maxLength": 32},
		"source":                map[string]any{"type": "string", "enum": []string{"human", "agent", "ci", "system"}},
		"target_environment":    map[string]any{"type": "string", "maxLength": 120},
		"work_item_type":        map[string]any{"type": "string", "enum": []string{"TASK", "BUG", "STORY"}},
		"execution_profile":     map[string]any{"type": "string", "maxLength": 120},
		"policy":                map[string]any{"type": "object", "additionalProperties": true},
		"test_cases": map[string]any{
			"type": "array", "maxItems": 200,
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"position":      map[string]any{"type": "integer", "minimum": 1, "maximum": 10000},
					"status":        map[string]any{"type": "string", "enum": []string{"NOT_RUN", "PASS", "FAIL", "BLOCKED"}},
					"note":          map[string]any{"type": "string", "maxLength": 4000},
					"evidence_refs": map[string]any{"type": "array", "maxItems": 30, "items": map[string]any{"type": "string", "maxLength": 512}},
				},
			},
		},
	}
	result := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}
