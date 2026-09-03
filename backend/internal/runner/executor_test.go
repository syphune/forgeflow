package runner

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	name   string
	args   []string
	runErr error
	calls  []struct {
		name string
		args []string
		env  []string
	}
}

func (f *fakeCommandRunner) Run(_ context.Context, _ string, name string, args []string, stdout, _ io.Writer, env []string) error {
	f.name = name
	f.args = append([]string(nil), args...)
	f.calls = append(f.calls, struct {
		name string
		args []string
		env  []string
	}{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)})
	_, _ = io.WriteString(stdout, "ok")
	return f.runErr
}

func TestExecutorKeepsRunnerGitTokenOutOfAgentEnvironment(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix it", RepositoryURL: "https://github.com/acme/app.git", BaseSHA: "HEAD", Branch: "forgeflow/fix-1", PushBranch: true, CommitMessage: "chore: update", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 5, AllowedHosts: []string{"github.com"}, GitToken: "runner-token"}
	if _, err := (Executor{Commands: fake}).Execute(context.Background(), job, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, call := range fake.calls {
		joined := strings.Join(call.env, "\n")
		if call.name == "git" {
			if !strings.Contains(joined, "GIT_CONFIG_VALUE_0=Authorization: Bearer runner-token") {
				t.Fatalf("git command did not receive the short-lived credential: %q", joined)
			}
			continue
		}
		if strings.Contains(joined, "runner-token") {
			t.Fatalf("agent command received the Git credential: %q", joined)
		}
	}
}

func TestExecutorUsesFixedProviderArgumentsAndBoundedEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	fake := &fakeCommandRunner{}
	executor := Executor{Commands: fake}
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix it", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/forgeflow/job-1", TimeoutSeconds: 5, Environment: map[string]string{"API_KEY": "key-secret", "API_TOKEN": "token-secret", "SAFE_FLAG": "1"}}
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
	environment := safeEnvironment(map[string]string{"API_KEY": "key-secret", "API_TOKEN": "token-secret", "SAFE_FLAG": "1"}, "codex")
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "secret") || strings.Contains(joined, "OPENAI_API_KEY") || strings.Contains(joined, "ANTHROPIC_API_KEY") || strings.Contains(joined, "CLAUDE_CODE_OAUTH_TOKEN") || !strings.Contains(joined, "SAFE_FLAG=1") {
		t.Fatalf("unsafe environment leaked: %q", joined)
	}
}

func TestReadOnlyJobsUseNonMutatingProviderModes(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{provider: "codex", want: "exec --sandbox read-only --ephemeral --skip-git-repo-check draft"},
		{provider: "claude", want: "--print --output-format json --permission-mode plan draft"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			name, args, err := providerCommand(Job{Provider: tt.provider, Prompt: "draft", ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(append([]string{name}, args...), " ") != tt.provider+" "+tt.want {
				t.Fatalf("read-only command = %s %#v", name, args)
			}
		})
	}
}

func TestReadOnlyCodexCommandLimitsMCPTools(t *testing.T) {
	_, args, err := providerCommand(Job{Provider: "codex", Prompt: "inspect", ReadOnly: true, MCPPermissions: []string{"work_item.get", "repository.get_file"}})
	if err != nil {
		t.Fatal(err)
	}
	command := strings.Join(args, " ")
	if strings.Contains(command, "--json") || !strings.Contains(command, `mcp_servers.forgeflow.enabled_tools=["work_item.get","repository.get_file"]`) {
		t.Fatalf("read-only Codex command = %q", command)
	}
}

func TestProviderCommandPassesModelAndReasoningEffort(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     string
	}{
		{provider: "codex", model: "gpt-5.6-sol", want: `codex exec --json --full-auto --model gpt-5.6-sol --config model_reasoning_effort="high" prompt`},
		{provider: "claude", model: "sonnet", want: "claude --print --output-format json --model sonnet --effort max prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			name, args, err := providerCommand(Job{Provider: tt.provider, Model: tt.model, ReasoningEffort: map[string]string{"codex": "high", "claude": "max"}[tt.provider], Prompt: "prompt"})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(append([]string{name}, args...), " "); got != tt.want {
				t.Fatalf("provider command = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodexCommandRegistersForgeflowMCP(t *testing.T) {
	_, args, err := providerCommand(Job{Provider: "codex", Prompt: "inspect", MCPURL: "https://forgeflow.example/api/v1/mcp?project_id=project-1"})
	if err != nil {
		t.Fatal(err)
	}
	command := strings.Join(args, " ")
	for _, expected := range []string{
		`mcp_servers.forgeflow.url="https://forgeflow.example/api/v1/mcp?project_id=project-1"`,
		`mcp_servers.forgeflow.bearer_token_env_var="FORGEFLOW_MCP_TOKEN"`,
		`mcp_servers.forgeflow.default_tools_approval_mode="approve"`,
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("Codex MCP command = %q, missing %q", command, expected)
		}
	}
}

func TestExecutorKeepsMCPTokenOutOfDockerArguments(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	executor := Executor{Commands: fake, UseDocker: true, DockerWorkspaceRoot: root, CodexAuthHostPath: "/Users/test/.codex/auth.json"}
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "inspect", MCPURL: "https://forgeflow.example/api/v1/mcp", MCPToken: "mcp-secret", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 5}
	if _, err := executor.Execute(context.Background(), job, nil); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(fake.args, " ")
	if strings.Contains(args, "mcp-secret") || strings.Contains(args, "--env-file") {
		t.Fatal("MCP token must not be placed in Docker arguments")
	}
	if !strings.Contains(args, "--env FORGEFLOW_MCP_TOKEN") || !strings.Contains(strings.Join(fake.calls[0].env, "\n"), "FORGEFLOW_MCP_TOKEN=mcp-secret") {
		t.Fatalf("MCP token was not passed through the Docker process environment: args=%q env=%q", args, fake.calls[0].env)
	}
}

func TestExecutorCleansUpDockerContainerAfterFailure(t *testing.T) {
	fake := &fakeCommandRunner{runErr: errors.New("exit status 124")}
	root := t.TempDir()
	executor := Executor{Commands: fake, UseDocker: true, DockerWorkspaceRoot: root, CodexAuthHostPath: "/Users/test/.codex/auth.json"}
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "inspect", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 5}
	if _, err := executor.Execute(context.Background(), job, nil); err == nil {
		t.Fatal("failed Docker execution must return an error")
	}
	if len(fake.calls) != 2 || fake.calls[1].name != "docker" || strings.Join(fake.calls[1].args, " ") != "rm -f "+dockerContainerName(job.ID) {
		t.Fatalf("Docker cleanup calls = %#v", fake.calls)
	}
}

func TestExecutorMountsCodexCLIChatGPTAuthWithoutAPIKey(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	executor := Executor{Commands: fake, UseDocker: true, DockerWorkspaceRoot: root, CodexAuthHostPath: "/Users/test/.codex/auth.json"}
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix it", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 5}
	if _, err := executor.Execute(context.Background(), job, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	joined := strings.Join(fake.args, " ")
	if !strings.Contains(joined, "source=/Users/test/.codex/auth.json") || !strings.Contains(joined, "target=/tmp/forgeflow-codex-auth.json") || !strings.Contains(joined, "CODEX_HOME=/tmp/codex-home") || !strings.Contains(joined, "--entrypoint /bin/sh") {
		t.Fatalf("codex auth mount = %s", joined)
	}
}

func TestExecutorMountsClaudeCLIAuthWithoutAPIKey(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	executor := Executor{Commands: fake, UseDocker: true, DockerWorkspaceRoot: root, ClaudeAuthHostPath: "/Users/test/.claude"}
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "claude", Prompt: "fix it", MCPURL: "https://forgeflow.example/api/v1/mcp", MCPToken: "mcp-secret", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 5}
	if _, err := executor.Execute(context.Background(), job, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	joined := strings.Join(fake.args, " ")
	environment := strings.Join(fake.calls[0].env, "\n")
	if !strings.Contains(joined, "source=/Users/test/.claude") || !strings.Contains(joined, "target=/tmp/claude-home") || !strings.Contains(joined, "CLAUDE_CONFIG_DIR=/tmp/claude-home") || !strings.Contains(joined, "--entrypoint /bin/sh") || strings.Contains(joined, "mcp-secret") {
		t.Fatalf("claude auth mount = %s", joined)
	}
	if !strings.Contains(joined, "--env FORGEFLOW_CLAUDE_MCP_CONFIG") || !strings.Contains(environment, `FORGEFLOW_CLAUDE_MCP_CONFIG={"mcpServers":{"forgeflow":{"headers":{"Authorization":"Bearer mcp-secret"}`) {
		t.Fatalf("Claude MCP config was not passed through the Docker process environment: args=%q env=%q", joined, environment)
	}
}

func TestExecutorRequiresCodexCLIAuthForDocker(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix it", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 5}
	result, err := (Executor{Commands: fake, UseDocker: true}).Execute(context.Background(), job, nil)
	if err == nil || result.ErrorCode != ErrorCodeAuthRequired || result.Error == "" {
		t.Fatalf("missing Codex auth = %#v, err = %v", result, err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("Docker command should not run without auth: %#v", fake.calls)
	}
}

func TestCodexAuthFailureIsClassified(t *testing.T) {
	for _, output := range []string{"401 Unauthorized", "OpenAI API key not found", "token is expired"} {
		if !isCodexAuthFailure("codex", output, errors.New("exit status 1")) {
			t.Fatalf("expected Codex authentication failure for %q", output)
		}
	}
	if isCodexAuthFailure("claude", "401 Unauthorized", errors.New("exit status 1")) {
		t.Fatal("Claude failure must not be classified as Codex authentication")
	}
}

func TestProviderUsageLimitIsClassified(t *testing.T) {
	if !isProviderUsageLimit("codex", "ERROR: You've hit your usage limit", errors.New("exit status 1")) {
		t.Fatal("expected Codex usage limit to be classified")
	}
	if got := providerUsageLimitMessage("codex"); !strings.Contains(got, "Codex CLI account usage limit") {
		t.Fatalf("usage limit message = %q", got)
	}
	if isProviderUsageLimit("unknown", "usage limit", errors.New("exit status 1")) {
		t.Fatal("unknown providers must not be classified")
	}
}

func TestContainerArgumentsKeepExecutionBounded(t *testing.T) {
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/forgeflow/job-1", TimeoutSeconds: 30, Image: "forgeflow/agent-runner:1.0.0", CPULimit: "2", MemoryLimit: "2g", PidsLimit: 512}
	args, err := containerArguments(job)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, required := range []string{"--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--user 1000:1000", "--network none", "--tmpfs /tmp:rw,noexec,nosuid,size=256m", "--cpus 2", "--memory 2g", "--pids-limit 512"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("container args missing %q: %s", required, joined)
		}
	}
}

func TestReadOnlyJobsMountWorkspaceReadOnly(t *testing.T) {
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "draft", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/forgeflow/job-1", TimeoutSeconds: 30, ReadOnly: true}
	args, err := containerArguments(job)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "--volume /tmp/forgeflow/job-1:/workspace:ro") {
		t.Fatalf("read-only workspace mount = %#v", args)
	}
}

func TestJobRejectsWorkspaceEscape(t *testing.T) {
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "claude", Prompt: "fix", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/other", TimeoutSeconds: 1}
	if err := job.Validate(); err == nil {
		t.Fatal("workspace escape must be rejected")
	}
}

func TestReadOnlyPrivateCloneMayUseRunnerGitToken(t *testing.T) {
	root := t.TempDir()
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "inspect", RepositoryURL: "https://github.com/acme/private.git", WorkspaceRoot: root, Workspace: root + "/job-1", TimeoutSeconds: 1, ReadOnly: true, GitToken: "runner-token"}
	if err := job.Validate(); err != nil {
		t.Fatalf("read-only private clone job rejected: %v", err)
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

func TestReadOnlyExecutorDoesNotCloneRepository(t *testing.T) {
	fake := &fakeCommandRunner{}
	root := t.TempDir()
	workspace := root + "/job-1"
	job := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "inspect", RepositoryURL: "https://github.com/acme/app.git", WorkspaceRoot: root, Workspace: workspace, TimeoutSeconds: 5, ReadOnly: true, AllowedHosts: []string{"github.com"}}
	if _, err := (Executor{Commands: fake}).Execute(context.Background(), job, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0].name != "codex" {
		t.Fatalf("read-only execution commands = %#v", fake.calls)
	}
}

func TestJobRejectsUnsafeRepositoryInputs(t *testing.T) {
	base := Job{ID: "job-1", AutonomousRunID: "workflow-1", AgentRunID: "run-1", Provider: "codex", Prompt: "fix", WorkspaceRoot: "/tmp/forgeflow", Workspace: "/tmp/forgeflow/job-1", TimeoutSeconds: 1}
	for name, job := range map[string]Job{
		"embedded credentials": {RepositoryURL: "https://token@github.com/acme/app.git"},
		"untrusted host":       {RepositoryURL: "https://evil.example/acme/app.git"},
		"unsafe branch":        {Branch: "--upload-pack=sh"},
		"symbolic head branch": {Branch: "HEAD"},
		"trailing slash":       {Branch: "feature/"},
		"trailing dot":         {Branch: "feature."},
		"double slash":         {Branch: "feature//one"},
		"reflog selector":      {Branch: "feature@{1}"},
		"dot component":        {Branch: "feature/."},
		"lock component":       {Branch: "feature.lock"},
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
