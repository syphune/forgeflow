package mcp

import "testing"

func TestResourceTopicsAreAllowlisted(t *testing.T) {
	for _, topic := range []string{"architecture", "conventions", "testing", "known-issues", "domain-rules"} {
		if !allowedResourceTopic(topic) {
			t.Fatalf("resource topic %q was rejected", topic)
		}
	}
	if allowedResourceTopic("instructions") {
		t.Fatal("dynamic resource topics must not be accepted")
	}
}

func TestKnowledgeKindForTopic(t *testing.T) {
	for topic, want := range map[string]string{
		"architecture": "ARCHITECTURE",
		"conventions":  "CONVENTIONS",
		"testing":      "TESTING",
		"known-issues": "KNOWN_ISSUES",
		"domain-rules": "DOMAIN_RULES",
	} {
		if got := knowledgeKindForTopic(topic); got != want {
			t.Fatalf("knowledge kind for %q = %q, want %q", topic, got, want)
		}
	}
	if knowledgeKindForTopic("module") != "" {
		t.Fatal("module resources must not select a knowledge kind")
	}
}
