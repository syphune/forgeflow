package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookAutomationEventMapping(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "push", want: "github.push"},
		{name: "pull_request", want: "github.pull_request.updated"},
		{name: "workflow_run", want: "github.ci.updated"},
		{name: "check_run", want: "github.ci.updated"},
		{name: "issues", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := webhookAutomationEvent(test.name); got != test.want {
				t.Fatalf("event mapping = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWebhookValidatesSignatureAndDeduplicates(t *testing.T) {
	secret, body := "secret", `{"action":"opened"}`
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	request := httptest.NewRequest("POST", "/webhooks", strings.NewReader(body))
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-GitHub-Event", "issues")
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	store := NewMemoryWebhookStore()
	handler := NewWebhookHandler(store, secret, 1024)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 202 {
		t.Fatalf("status = %d", response.Code)
	}
	second := httptest.NewRequest("POST", "/webhooks", strings.NewReader(body))
	second.Header = request.Header.Clone()
	duplicate := httptest.NewRecorder()
	handler.ServeHTTP(duplicate, second)
	if !strings.Contains(duplicate.Body.String(), `"duplicate":true`) {
		t.Fatalf("expected duplicate response: %s", duplicate.Body.String())
	}
}
