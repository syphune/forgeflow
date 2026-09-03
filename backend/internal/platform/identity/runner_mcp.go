package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const runnerMCPTokenPrefix = "ff_agent_v1."

type RunnerMCPClaims struct {
	OrganizationID string   `json:"organization_id"`
	ProjectID      string   `json:"project_id"`
	AgentRunID     string   `json:"agent_run_id"`
	Permissions    []string `json:"permissions"`
	ExpiresAt      int64    `json:"expires_at"`
}

func IssueRunnerMCPToken(secret string, claims RunnerMCPClaims, now time.Time) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", errors.New("runner MCP token secret is required")
	}
	if err := normalizeRunnerMCPClaims(&claims, now); err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode runner MCP claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return runnerMCPTokenPrefix + encoded + "." + runnerMCPSignature(secret, runnerMCPTokenPrefix+encoded), nil
}

func VerifyRunnerMCPToken(secret, token string, now time.Time) (RunnerMCPClaims, error) {
	secret = strings.TrimSpace(secret)
	token = strings.TrimSpace(token)
	if secret == "" || token == "" || len(token) > 16*1024 || !strings.HasPrefix(token, runnerMCPTokenPrefix) {
		return RunnerMCPClaims{}, errors.New("runner MCP token is invalid")
	}
	body := strings.TrimPrefix(token, runnerMCPTokenPrefix)
	encoded, signature, ok := strings.Cut(body, ".")
	if !ok || encoded == "" || signature == "" || strings.Contains(signature, ".") {
		return RunnerMCPClaims{}, errors.New("runner MCP token is invalid")
	}
	expected := runnerMCPSignature(secret, runnerMCPTokenPrefix+encoded)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return RunnerMCPClaims{}, errors.New("runner MCP token is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return RunnerMCPClaims{}, errors.New("runner MCP token is invalid")
	}
	var claims RunnerMCPClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return RunnerMCPClaims{}, errors.New("runner MCP token is invalid")
	}
	if err := normalizeRunnerMCPClaims(&claims, now); err != nil {
		return RunnerMCPClaims{}, errors.New("runner MCP token is invalid")
	}
	return claims, nil
}

func ActorForRunnerMCP(claims RunnerMCPClaims) Actor {
	capabilities := map[string]bool{
		CapabilityProjectRead:    true,
		CapabilityRepositoryRead: true,
		CapabilityAgentExecute:   true,
	}
	tools := make(map[string]bool, len(claims.Permissions))
	for _, tool := range claims.Permissions {
		tools[tool] = true
		switch {
		case tool == "project.list" || strings.HasPrefix(tool, "sprint.") || strings.HasPrefix(tool, "work_item.get") || strings.HasPrefix(tool, "specification.get") || tool == "agent_run.get" || tool == "autonomous.get":
			capabilities[CapabilityProjectRead] = true
		case strings.HasPrefix(tool, "repository."):
			capabilities[CapabilityRepositoryRead] = true
		case tool == "work_item.create":
			capabilities[CapabilityWorkItemCreate] = true
		case tool == "work_item.update":
			capabilities[CapabilityWorkItemEdit] = true
		case tool == "work_item.assign":
			capabilities[CapabilityWorkItemAssign] = true
		case tool == "work_item.transition":
			capabilities[CapabilityWorkItemTransition] = true
		case tool == "work_item.comment":
			capabilities[CapabilityCommentCreate] = true
		case strings.HasPrefix(tool, "specification."):
			capabilities[CapabilitySpecificationPropose] = true
			if tool == "specification.verify_field" {
				capabilities[CapabilitySpecificationVerify] = true
			}
		case strings.HasPrefix(tool, "agent_run."):
			capabilities[CapabilityAgentExecute] = true
		case tool == "autonomous.start":
			capabilities[CapabilityAutonomousStart] = true
		case tool == "autonomous.retry" || tool == "autonomous.resume":
			capabilities[CapabilityAutonomousRetry] = true
		case tool == "autonomous.cancel":
			capabilities[CapabilityAutonomousCancel] = true
		case tool == "autonomous.add_feedback":
			capabilities[CapabilityWorkItemEdit] = true
		case tool == "autonomous.record_test_results":
			capabilities[CapabilityAgentExecute] = true
		}
	}
	return Actor{Type: "agent", ID: claims.AgentRunID, OrganizationID: claims.OrganizationID, Source: "runner-mcp", Capabilities: capabilities, ToolPermissions: tools}
}

func normalizeRunnerMCPClaims(claims *RunnerMCPClaims, now time.Time) error {
	claims.OrganizationID = strings.TrimSpace(claims.OrganizationID)
	claims.ProjectID = strings.TrimSpace(claims.ProjectID)
	claims.AgentRunID = strings.TrimSpace(claims.AgentRunID)
	if claims.OrganizationID == "" || claims.ProjectID == "" || claims.AgentRunID == "" || strings.ContainsAny(claims.OrganizationID+claims.ProjectID+claims.AgentRunID, "\x00\r\n") {
		return errors.New("runner MCP claims are incomplete")
	}
	if claims.ExpiresAt <= now.UTC().Unix() || claims.ExpiresAt > now.UTC().Add(24*time.Hour).Unix() {
		return errors.New("runner MCP claims are expired or exceed the maximum lifetime")
	}
	if len(claims.Permissions) == 0 || len(claims.Permissions) > 100 {
		return errors.New("runner MCP permissions are invalid")
	}
	seen := make(map[string]struct{}, len(claims.Permissions))
	permissions := make([]string, 0, len(claims.Permissions))
	for _, permission := range claims.Permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" || len(permission) > 120 || strings.ContainsAny(permission, "\x00\r\n") {
			return errors.New("runner MCP permissions are invalid")
		}
		if _, ok := seen[permission]; ok {
			continue
		}
		seen[permission] = struct{}{}
		permissions = append(permissions, permission)
	}
	claims.Permissions = permissions
	return nil
}

func runnerMCPSignature(secret, value string) string {
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
