package runner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type impactCommandRunner struct {
	workspace string
	status    string
	calls     []string
}

func (f *impactCommandRunner) Run(_ context.Context, _ string, name string, args []string, stdout, _ io.Writer, _ []string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name == "codex" {
		path := filepath.Join(f.workspace, "apps", "web", "page.tsx")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
			return err
		}
	}
	if name == "git" && strings.Contains(strings.Join(args, " "), "status") {
		status := f.status
		if status == "" {
			status = "?? apps/web/page.tsx\x00"
		}
		_, _ = io.WriteString(stdout, status)
	}
	return nil
}

func TestExecutorIncludesBothSidesOfRenameInImpactManifest(t *testing.T) {
	root := t.TempDir()
	fake := &impactCommandRunner{workspace: filepath.Join(root, "job-1"), status: "R  apps/web/new.tsx\x00apps/web/old.tsx\x00"}
	result, err := (Executor{Commands: fake}).Execute(context.Background(), Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix", RepositoryURL: "https://github.com/acme/app.git", Branch: "forgeflow/fix", WorkspaceRoot: root, Workspace: fake.workspace, TimeoutSeconds: 5, AllowedHosts: []string{"github.com"}, CollectChanges: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedPaths) != 2 || result.ChangedPaths[0] != "apps/web/new.tsx" || result.ChangedPaths[1] != "apps/web/old.tsx" {
		t.Fatalf("rename impact manifest = %#v", result.ChangedPaths)
	}
}

func TestExecutorCollectsBoundedChangedManifestWithoutPushing(t *testing.T) {
	root := t.TempDir()
	fake := &impactCommandRunner{workspace: filepath.Join(root, "job-1")}
	result, err := (Executor{Commands: fake}).Execute(context.Background(), Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix", RepositoryURL: "https://github.com/acme/app.git", Branch: "forgeflow/fix", WorkspaceRoot: root, Workspace: fake.workspace, TimeoutSeconds: 5, AllowedHosts: []string{"github.com"}, CollectChanges: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedPaths) != 1 || result.ChangedPaths[0] != "apps/web/page.tsx" || result.DiffHash == "" {
		t.Fatalf("impact manifest = %#v", result)
	}
	for _, call := range fake.calls {
		if strings.Contains(call, " push ") {
			t.Fatalf("execute phase pushed changes: %q", call)
		}
	}
}

func TestPublishOperationDoesNotInvokeProvider(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "job-1")
	if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &impactCommandRunner{workspace: workspace}
	_, err := (Executor{Commands: fake}).Execute(context.Background(), Job{ID: "job-1-publish", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Operation: "publish", Prompt: "publish", RepositoryURL: "https://github.com/acme/app.git", Branch: "forgeflow/fix", PushBranch: true, CommitMessage: "chore: publish", WorkspaceRoot: root, Workspace: workspace, TimeoutSeconds: 5, AllowedHosts: []string{"github.com"}, GitToken: "token"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "codex ") || strings.HasPrefix(call, "claude ") {
			t.Fatalf("publish phase invoked provider: %q", call)
		}
	}
	joined := strings.Join(fake.calls, "\n")
	if !strings.Contains(joined, " push --set-upstream origin forgeflow/fix") {
		t.Fatalf("publish phase did not push: %s", joined)
	}
}
