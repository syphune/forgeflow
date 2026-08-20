package github

import (
	"context"
	"testing"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

func TestKnowledgeRejectsAIVerificationAndProtectsVerifiedRevision(t *testing.T) {
	service := NewKnowledgeService(nil, NewMemoryKnowledgeStore())
	human := identity.Actor{Type: "human", ID: "user-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true, identity.CapabilityRepositoryManage: true}}
	agent := identity.Actor{Type: "agent", ID: "agent-1", OrganizationID: "org-1", Capabilities: map[string]bool{identity.CapabilityRepositoryRead: true, identity.CapabilityRepositoryManage: true}}
	ctx := context.Background()
	document, err := service.Create(ctx, human, "project-1", KnowledgeInput{RepositoryID: "repo-1", Slug: "testing", Title: "Testing", Kind: "TESTING", Content: "Run go test.", Provenance: "MANUAL"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendRevision(ctx, agent, "project-1", "repo-1", document.ID, KnowledgeInput{Content: "AI says this is verified.", Provenance: "HUMAN_VERIFIED"}); err == nil || !hasKnowledgeCode(err, apperr.CodeAICannotVerify) {
		t.Fatalf("AI verification error = %v", err)
	}
	if _, err := service.AppendRevision(ctx, human, "project-1", "repo-1", document.ID, KnowledgeInput{Content: "Human confirmed.", Provenance: "HUMAN_VERIFIED"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendRevision(ctx, agent, "project-1", "repo-1", document.ID, KnowledgeInput{Content: "Unverified rewrite.", Provenance: "AI_PROPOSED"}); err == nil {
		t.Fatal("unverified knowledge must not overwrite verified knowledge")
	}
	revisions, err := service.Revisions(ctx, human, "project-1", "repo-1", document.ID, 10)
	if err != nil {
		t.Fatalf("list knowledge revisions: %v", err)
	}
	if len(revisions) != 2 || revisions[0].RevisionNumber != 2 || revisions[1].RevisionNumber != 1 {
		t.Fatalf("knowledge revisions = %#v", revisions)
	}
}

func hasKnowledgeCode(err error, code string) bool {
	appErr, ok := err.(*apperr.Error)
	return ok && appErr.Code == code
}
