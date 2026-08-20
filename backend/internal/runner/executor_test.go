package runner

import (
	"context"
	"io"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	name  string
	args  []string
	calls []struct {
		name string
		args []string
	}
}

func (f *fakeCommandRunner) Run(_ context.Context, _ string, name string, args []string, stdout, _ io.Writer, _ []string) error {
	f.name = name
	f.args = append([]string(nil), args...)
	f.calls = append(f.calls, struct {
		name string
		args []string
	}{name: name, args: append([]string(nil), args...)})
	_, _ = io.WriteString(stdout, "ok")
	return nil
}

func TestExecutorUsesFixedProviderArgumentsAndBoundedEnvironment(t *testing.T) {
	fake := &fakeCommandRunner{}
	executor := Executor{Commands: fake}
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix it", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/forgeflow/job-1", TimeoutSeconds: 5, Environment: map[string]string{"API_TOKEN": "secret", "SAFE_FLAG": "1"}}
	var events []Event
	result, err := executor.Execute(context.Background(), job, func(event Event) error { events = append(events, event); return nil })
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.name != "codex" || strings.Join(fake.args, " ") != "exec --json --full-auto fix it" {
		t.Fatalf("provider command = %s %#v", fake.name, fake.args)
	}
	if result.Output != "ok" || len(events) != 2 || events[0].Type != EventStarted || events[1].Type != EventCompleted {
		t.Fatalf("execution result/events = %#v %#v", result, events)
	}
	environment := safeEnvironment(map[string]string{"API_TOKEN": "secret", "SAFE_FLAG": "1"}, "codex")
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "secret") || !strings.Contains(joined, "SAFE_FLAG=1") {
		t.Fatalf("unsafe environment leaked: %q", joined)
	}
}

func TestJobRejectsWorkspaceEscape(t *testing.T) {
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "claude", Prompt: "fix", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/other", TimeoutSeconds: 1}
	if err := job.Validate(); err == nil {
		t.Fatal("workspace escape must be rejected")
	}
}

func TestExecutorHydratesLinkedRepositoryWithFixedGitCommands(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	workspace := root + "/job-1"
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix it", RepositoryURL: "https://github.com/acme/app.git", BaseSHA: "HEAD", Branch: "forgeflow/fix-1", WorkspaceRoot: root, Workspace: workspace, TimeoutSeconds: 5, AllowedHosts: []string{"github.com"}}
	if _, err := (Executor{Commands: fake}).Execute(context.Background(), job, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("command calls = %#v, want clone, checkout, provider", fake.calls)
	}
	if fake.calls[0].name != "git" || strings.Join(fake.calls[0].args, " ") != "clone --no-checkout --depth 1 https://github.com/acme/app.git "+workspace {
		t.Fatalf("clone command = %#v", fake.calls[0])
	}
	if fake.calls[1].name != "git" || strings.Join(fake.calls[1].args, " ") != "-C "+workspace+" checkout -B forgeflow/fix-1 HEAD" {
		t.Fatalf("checkout command = %#v", fake.calls[1])
	}
}

func TestJobRejectsUnsafeRepositoryInputs(t *testing.T) {
	base := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/forgeflow/job-1", TimeoutSeconds: 1}
	for name, job := range map[string]Job{
		"embedded credentials": {RepositoryURL: "https://token@github.com/acme/app.git"},
		"untrusted host":       {RepositoryURL: "https://evil.example/acme/app.git"},
		"unsafe branch":        {Branch: "--upload-pack=sh"},
		"unsafe sha":           {BaseSHA: "HEAD;id"},
	} {
		t.Run(name, func(t *testing.T) {
			job.ID, job.AutonomousRunID, job.AgentRunID, job.Provider, job.Prompt, job.WorkspaceRoot, job.Workspace, job.TimeoutSeconds = base.ID, base.AutonomousRunID, base.AgentRunID, base.Provider, base.Prompt, base.WorkspaceRoot, base.Workspace, base.TimeoutSeconds
			if err := job.Validate(); err == nil {
				t.Fatal("unsafe repository input must be rejected")
			}
		})
	}
}
