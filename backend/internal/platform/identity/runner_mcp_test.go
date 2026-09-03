package identity

import (
	"testing"
	"time"
)

func TestRunnerMCPTokenIsScopedAndTamperResistant(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	token, err := IssueRunnerMCPToken("runner-secret", RunnerMCPClaims{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		AgentRunID:     "run-1",
		Permissions:    []string{"work_item.get_context", "agent_run.get"},
		ExpiresAt:      now.Add(time.Hour).Unix(),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := VerifyRunnerMCPToken("runner-secret", token, now)
	if err != nil || claims.ProjectID != "project-1" || len(claims.Permissions) != 2 {
		t.Fatalf("claims = %#v, err = %v", claims, err)
	}
	if _, err := VerifyRunnerMCPToken("wrong-secret", token, now); err == nil {
		t.Fatal("token signed with another secret must be rejected")
	}
	if _, err := VerifyRunnerMCPToken("runner-secret", token+"x", now); err == nil {
		t.Fatal("tampered token must be rejected")
	}
	if _, err := VerifyRunnerMCPToken("runner-secret", token, now.Add(time.Hour)); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestRunnerMCPActorMapsToolPermissions(t *testing.T) {
	actor := ActorForRunnerMCP(RunnerMCPClaims{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		AgentRunID:     "run-1",
		Permissions:    []string{"work_item.get_context", "agent_run.get"},
	})
	if actor.Type != "agent" || actor.ID != "run-1" || !actor.Has(CapabilityProjectRead) || !actor.Has(CapabilityAgentExecute) {
		t.Fatalf("actor = %#v", actor)
	}
	if !actor.AllowsTool("work_item.get_context") || actor.AllowsTool("work_item.update") {
		t.Fatalf("tool permissions = %#v", actor.ToolPermissions)
	}
}
