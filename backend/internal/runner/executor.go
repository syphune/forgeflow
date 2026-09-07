package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxOutputBytes = 2 << 20

const (
	codexAuthRequiredMessage  = "Codex CLI authentication is required or the session has expired; sign in from Project Settings, then retry the run."
	claudeAuthRequiredMessage = "Claude CLI authentication is required or the session has expired; sign in from Project Settings, then retry the run."
)

type CommandRunner interface {
	Run(context.Context, string, string, []string, io.Writer, io.Writer, []string) error
}

type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, dir, name string, args []string, stdout, stderr io.Writer, environment []string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

type Executor struct {
	Commands            CommandRunner
	Now                 func() time.Time
	UseDocker           bool
	DockerWorkspaceRoot string
	CodexAuthHostPath   string
	ClaudeAuthHostPath  string
}

func (e Executor) Execute(ctx context.Context, job Job, emit func(Event) error) (Result, error) {
	if err := job.Validate(); err != nil {
		return Result{}, err
	}
	operation := strings.ToLower(strings.TrimSpace(job.Operation))
	if operation == "" {
		operation = "execute"
	}
	if operation == "execute" && e.UseDocker && providerAuthPath(job.Provider, e.CodexAuthHostPath, e.ClaudeAuthHostPath) == "" {
		result := Result{ExitCode: 1, Error: authRequiredMessage(job.Provider), ErrorCode: ErrorCodeAuthRequired}
		if emit != nil {
			_ = emit(Event{JobID: job.ID, Type: EventFailed, ExitCode: result.ExitCode, Error: result.Error, ErrorCode: result.ErrorCode})
		}
		return result, errors.New(result.Error)
	}
	if err := os.MkdirAll(filepath.Clean(job.Workspace), 0o700); err != nil {
		return Result{}, fmt.Errorf("prepare runner workspace: %w", err)
	}
	if e.Commands == nil {
		e.Commands = OSCommandRunner{}
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := e.prepareWorkspace(ctx, job); err != nil {
		return Result{}, err
	}
	cleanupMCP := func() {}
	var err error
	if operation == "execute" {
		var preparedJob Job
		preparedJob, cleanupMCP, err = e.prepareMCP(job)
		if err != nil {
			return Result{}, err
		}
		job = preparedJob
	}
	defer cleanupMCP()
	if emit != nil {
		if err := emit(Event{JobID: job.ID, Type: EventStarted}); err != nil {
			return Result{}, err
		}
	}
	output := &limitedBuffer{limit: maxOutputBytes}
	environment := safeEnvironment(job.Environment, job.Provider)
	redactionEnvironment := append([]string(nil), environment...)
	if strings.TrimSpace(job.MCPToken) != "" {
		redactionEnvironment = append(redactionEnvironment, "FORGEFLOW_MCP_TOKEN="+job.MCPToken)
		environment = append(environment, "FORGEFLOW_MCP_TOKEN="+job.MCPToken)
	}
	if operation == "execute" {
		name, args, commandErr := providerCommand(job)
		if commandErr != nil {
			return Result{}, commandErr
		}
		if e.UseDocker {
			err = e.runInContainer(ctx, job, name, args, output, environment)
		} else {
			err = e.Commands.Run(ctx, job.Workspace, name, args, output, output, environment)
		}
	}
	result := Result{ExitCode: 0, Output: redactSecrets(output.String(), redactionEnvironment)}
	if err != nil {
		result.ExitCode = exitCode(err)
		result.Error = redactSecrets(boundedError(err), redactionEnvironment)
		if ctx.Err() != nil {
			result.Error = "runner timed out or was cancelled"
		} else if isProviderUsageLimit(job.Provider, result.Output, err) {
			result.ErrorCode = ErrorCodeUsageLimit
			result.Error = providerUsageLimitMessage(job.Provider)
		} else if isProviderAuthFailure(job.Provider, result.Output, err) {
			result.ErrorCode = ErrorCodeAuthRequired
			result.Error = authRequiredMessage(job.Provider)
		}
		if emit != nil {
			_ = emit(Event{JobID: job.ID, Type: EventFailed, Text: result.Output, ExitCode: result.ExitCode, Error: result.Error, ErrorCode: result.ErrorCode})
		}
		return result, err
	}
	changedPaths, diffHash, diffErr := e.collectChanges(ctx, job)
	if diffErr != nil {
		result.ExitCode = 1
		result.Error = redactSecrets(boundedError(diffErr), append(redactionEnvironment, "GIT_TOKEN="+job.GitToken))
		if emit != nil {
			_ = emit(Event{JobID: job.ID, Type: EventFailed, Text: result.Output, ExitCode: result.ExitCode, Error: result.Error})
		}
		return result, diffErr
	}
	result.ChangedPaths = changedPaths
	result.DiffHash = diffHash
	if job.PushBranch {
		if err := e.finalizeAndPush(ctx, job); err != nil {
			result.ExitCode = 1
			result.Error = redactSecrets(boundedError(err), append(redactionEnvironment, "GIT_TOKEN="+job.GitToken))
			if emit != nil {
				_ = emit(Event{JobID: job.ID, Type: EventFailed, Text: result.Output, ExitCode: result.ExitCode, Error: result.Error})
			}
			return result, err
		}
	}
	if emit != nil {
		if err := emit(Event{JobID: job.ID, Type: EventCompleted, Text: result.Output, ExitCode: result.ExitCode}); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func (e Executor) collectChanges(ctx context.Context, job Job) ([]string, string, error) {
	operation := strings.ToLower(strings.TrimSpace(job.Operation))
	if job.ReadOnly || (operation != "publish" && !job.CollectChanges && !job.PushBranch) || (operation != "publish" && strings.TrimSpace(job.RepositoryURL) == "" && !job.PushBranch) {
		return nil, "", nil
	}
	status, err := e.runGitOutputWithToken(ctx, "inspect agent changes", job.Workspace, []string{"-C", job.Workspace, "status", "--porcelain=v1", "--untracked-files=all", "-z"}, job.GitToken)
	if err != nil {
		return nil, "", err
	}
	paths := make([]string, 0)
	addPath := func(raw string) error {
		path := strings.ReplaceAll(raw, "\\", "/")
		if path == "" {
			return nil
		}
		if strings.ContainsAny(path, "\x00\r\n") || filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") {
			return fmt.Errorf("agent changed path is unsafe")
		}
		paths = append(paths, path)
		return nil
	}
	entries := strings.Split(status, "\x00")
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		statusCode, path := entry[:2], entry[3:]
		if err := addPath(path); err != nil {
			return nil, "", err
		}
		if strings.ContainsAny(statusCode, "RC") && index+1 < len(entries) && entries[index+1] != "" {
			index++
			if err := addPath(entries[index]); err != nil {
				return nil, "", err
			}
		}
	}
	sort.Strings(paths)
	unique := paths[:0]
	for _, path := range paths {
		if len(unique) == 0 || unique[len(unique)-1] != path {
			unique = append(unique, path)
		}
	}
	hash := sha256.New()
	var total int64
	for _, path := range unique {
		_, _ = io.WriteString(hash, path)
		_, _ = hash.Write([]byte{0})
		full := filepath.Join(job.Workspace, filepath.FromSlash(path))
		info, statErr := os.Lstat(full)
		if os.IsNotExist(statErr) {
			_, _ = io.WriteString(hash, "DELETED")
			_, _ = hash.Write([]byte{0})
			continue
		}
		if statErr != nil {
			return nil, "", fmt.Errorf("inspect changed path: %w", statErr)
		}
		if info.IsDir() {
			_, _ = io.WriteString(hash, "DIRECTORY")
			_, _ = hash.Write([]byte{0})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, readlinkErr := os.Readlink(full)
			if readlinkErr != nil {
				return nil, "", fmt.Errorf("read changed symlink: %w", readlinkErr)
			}
			_, _ = io.WriteString(hash, "SYMLINK:")
			_, _ = io.WriteString(hash, target)
			_, _ = hash.Write([]byte{0})
			continue
		}
		const maxFileBytes = 16 << 20
		if info.Size() > maxFileBytes {
			return nil, "", fmt.Errorf("changed file %q exceeds the impact hashing limit", path)
		}
		file, openErr := os.Open(full)
		if openErr != nil {
			return nil, "", fmt.Errorf("open changed path: %w", openErr)
		}
		read, copyErr := io.CopyN(hash, file, maxFileBytes+1)
		closeErr := file.Close()
		if copyErr != nil && copyErr != io.EOF {
			return nil, "", fmt.Errorf("hash changed path: %w", copyErr)
		}
		if closeErr != nil {
			return nil, "", fmt.Errorf("close changed path: %w", closeErr)
		}
		if read > maxFileBytes {
			return nil, "", fmt.Errorf("changed file %q exceeds the impact hashing limit", path)
		}
		total += read
		if total > 64<<20 {
			return nil, "", fmt.Errorf("changed files exceed the impact hashing limit")
		}
		_, _ = hash.Write([]byte{0})
	}
	return unique, hex.EncodeToString(hash.Sum(nil)), nil
}

func (e Executor) finalizeAndPush(ctx context.Context, job Job) error {
	status, err := e.runGitOutputWithToken(ctx, "inspect working tree", job.Workspace, []string{"-C", job.Workspace, "status", "--porcelain", "--untracked-files=all"}, job.GitToken)
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		if err := e.runGitWithToken(ctx, "stage agent changes", job.Workspace, []string{"-C", job.Workspace, "add", "--all"}, job.GitToken); err != nil {
			return err
		}
		if err := e.runGitWithToken(ctx, "commit agent changes", job.Workspace, []string{"-C", job.Workspace, "commit", "-m", strings.TrimSpace(job.CommitMessage)}, job.GitToken); err != nil {
			return err
		}
	}
	return e.runGitWithToken(ctx, "push agent branch", job.Workspace, []string{"-C", job.Workspace, "push", "--set-upstream", "origin", strings.TrimSpace(job.Branch)}, job.GitToken)
}

func (e Executor) prepareWorkspace(ctx context.Context, job Job) error {
	if job.ReadOnly {
		return nil
	}
	if strings.TrimSpace(job.RepositoryURL) == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(job.Workspace, ".git")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect runner workspace: %w", err)
	}
	cloneArgs := []string{"clone", "--no-checkout", "--depth", "1"}
	cloneArgs = append(cloneArgs, strings.TrimSpace(job.RepositoryURL), job.Workspace)
	if err := e.runGitWithToken(ctx, "clone repository", "", cloneArgs, job.GitToken); err != nil {
		return err
	}
	base := strings.TrimSpace(job.BaseSHA)
	if base == "" {
		base = "HEAD"
	}
	if base != "HEAD" {
		if err := e.runGitWithToken(ctx, "fetch base commit", job.Workspace, []string{"-C", job.Workspace, "fetch", "--depth", "1", "origin", base}, job.GitToken); err != nil {
			return err
		}
	}
	checkoutArgs := []string{"-C", job.Workspace, "checkout"}
	if branch := strings.TrimSpace(job.Branch); branch != "" {
		checkoutArgs = append(checkoutArgs, "-B", branch)
	} else {
		checkoutArgs = append(checkoutArgs, "--detach")
	}
	checkoutArgs = append(checkoutArgs, base)
	return e.runGitWithToken(ctx, "checkout repository", job.Workspace, checkoutArgs, job.GitToken)
}

func (e Executor) runGit(ctx context.Context, action, dir string, args []string) error {
	return e.runGitWithToken(ctx, action, dir, args, "")
}

func (e Executor) runGitWithToken(ctx context.Context, action, dir string, args []string, token string) error {
	output := &limitedBuffer{limit: maxOutputBytes}
	if err := e.Commands.Run(ctx, dir, "git", args, output, output, gitEnvironment(token)); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: runner timed out or was cancelled", action)
		}
		return gitCommandError(action, err, output.String(), token)
	}
	return nil
}

func (e Executor) runGitOutput(ctx context.Context, action, dir string, args []string) (string, error) {
	return e.runGitOutputWithToken(ctx, action, dir, args, "")
}

func (e Executor) runGitOutputWithToken(ctx context.Context, action, dir string, args []string, token string) (string, error) {
	output := &limitedBuffer{limit: maxOutputBytes}
	if err := e.Commands.Run(ctx, dir, "git", args, output, output, gitEnvironment(token)); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: runner timed out or was cancelled", action)
		}
		return "", gitCommandError(action, err, output.String(), token)
	}
	return output.String(), nil
}

func gitCommandError(action string, err error, output, token string) error {
	details := strings.TrimSpace(redactSecrets(output, []string{"GIT_TOKEN=" + strings.TrimSpace(token)}))
	if details != "" {
		if len(details) > 3500 {
			details = details[:3500]
		}
		return fmt.Errorf("%s: %s (%s)", action, details, boundedError(err))
	}
	return fmt.Errorf("%s: %s", action, redactSecrets(boundedError(err), []string{"GIT_TOKEN=" + strings.TrimSpace(token)}))
}

func (e Executor) prepareMCP(job Job) (Job, func(), error) {
	if strings.TrimSpace(job.MCPURL) == "" {
		return job, func() {}, nil
	}
	token := strings.TrimSpace(job.MCPToken)
	if token == "" {
		return Job{}, nil, errors.New("MCP authentication token is required")
	}
	if strings.EqualFold(strings.TrimSpace(job.Provider), "codex") {
		return job, func() {}, nil
	}
	if !strings.EqualFold(strings.TrimSpace(job.Provider), "claude") {
		return job, func() {}, nil
	}
	config, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"forgeflow": map[string]any{
				"type": "http",
				"url":  strings.TrimSpace(job.MCPURL),
				"headers": map[string]string{
					"Authorization": "Bearer " + token,
				},
			},
		},
	})
	if err != nil {
		return Job{}, nil, fmt.Errorf("encode Claude MCP configuration: %w", err)
	}
	if e.UseDocker {
		job.MCPConfigPath = "/tmp/forgeflow-mcp.json"
		job.MCPConfigContents = string(config)
		return job, func() {}, nil
	}
	secretDir := filepath.Join(filepath.Clean(job.WorkspaceRoot), ".forgeflow-secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return Job{}, nil, fmt.Errorf("prepare MCP secret directory: %w", err)
	}
	file, err := os.CreateTemp(secretDir, "claude-mcp-*.json")
	if err != nil {
		return Job{}, nil, fmt.Errorf("prepare Claude MCP configuration: %w", err)
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return Job{}, nil, fmt.Errorf("prepare Claude MCP configuration: %w", err)
	}
	if _, err := file.Write(config); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return Job{}, nil, fmt.Errorf("prepare Claude MCP configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return Job{}, nil, fmt.Errorf("prepare Claude MCP configuration: %w", err)
	}
	job.MCPConfigPath = path
	return job, func() { _ = os.Remove(path); _ = os.Remove(secretDir) }, nil
}

func providerCommand(job Job) (string, []string, error) {
	prompt := strings.TrimSpace(job.Prompt)
	switch strings.ToLower(strings.TrimSpace(job.Provider)) {
	case "codex":
		args := []string{"exec"}
		if job.ReadOnly {
			args = append(args, "--sandbox", "read-only", "--ephemeral", "--skip-git-repo-check")
		} else {
			args = append(args, "--json", "--full-auto")
		}
		if strings.TrimSpace(job.Model) != "" {
			args = append(args, "--model", strings.TrimSpace(job.Model))
		}
		if reasoningEffort := strings.TrimSpace(job.ReasoningEffort); reasoningEffort != "" {
			args = append(args, "--config", `model_reasoning_effort="`+reasoningEffort+`"`)
		}
		if mcpURL := strings.TrimSpace(job.MCPURL); mcpURL != "" {
			args = append(args, "--config", "mcp_servers.forgeflow.url="+strconv.Quote(mcpURL), "--config", `mcp_servers.forgeflow.bearer_token_env_var="FORGEFLOW_MCP_TOKEN"`, "--config", `mcp_servers.forgeflow.default_tools_approval_mode="approve"`)
		}
		if job.ReadOnly && len(job.MCPPermissions) > 0 {
			permissions, _ := json.Marshal(job.MCPPermissions)
			args = append(args, "--config", "mcp_servers.forgeflow.enabled_tools="+string(permissions))
		}
		return "codex", append(args, prompt), nil
	case "claude":
		args := []string{"--print", "--output-format", "json"}
		if job.ReadOnly {
			args = append(args, "--permission-mode", "plan")
		}
		if strings.TrimSpace(job.Model) != "" {
			args = append(args, "--model", strings.TrimSpace(job.Model))
		}
		if reasoningEffort := strings.TrimSpace(job.ReasoningEffort); reasoningEffort != "" {
			args = append(args, "--effort", reasoningEffort)
		}
		if mcpConfig := strings.TrimSpace(job.MCPConfigPath); mcpConfig != "" {
			args = append(args, "--mcp-config", mcpConfig)
		}
		return "claude", append(args, prompt), nil
	default:
		return "", nil, fmt.Errorf("unsupported provider %q", job.Provider)
	}
}

func safeEnvironment(extra map[string]string, provider string) []string {
	allowed := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "TMPDIR": true, "HOME": true}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		allowed["OPENAI_BASE_URL"] = true
		allowed["CODEX_HOME"] = true
	case "claude":
		allowed["ANTHROPIC_BASE_URL"] = true
	}
	result := make([]string, 0, len(extra)+4)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			result = append(result, entry)
		}
	}
	for name, value := range extra {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "=\x00\n\r") || strings.ContainsAny(value, "\x00\n\r") {
			continue
		}
		upperName := strings.ToUpper(name)
		if strings.Contains(upperName, "KEY") || strings.Contains(upperName, "TOKEN") || strings.Contains(upperName, "SECRET") || strings.Contains(upperName, "PASSWORD") {
			continue
		}
		result = append(result, name+"="+value)
	}
	return result
}

func (e Executor) runInContainer(ctx context.Context, job Job, name string, args []string, output io.Writer, environment []string) error {
	containerJob := job
	if root := strings.TrimSpace(e.DockerWorkspaceRoot); root != "" {
		relative, err := filepath.Rel(filepath.Clean(job.WorkspaceRoot), filepath.Clean(job.Workspace))
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("workspace cannot be mapped into the Docker runner")
		}
		containerJob.Workspace = filepath.Join(filepath.Clean(root), relative)
		containerJob.WorkspaceRoot = filepath.Clean(root)
	}
	containerArgs, err := containerArguments(containerJob)
	if err != nil {
		return err
	}
	imageIndex := len(containerArgs) - 1
	useCodexAuth := strings.EqualFold(strings.TrimSpace(job.Provider), "codex") && strings.TrimSpace(e.CodexAuthHostPath) != ""
	useClaudeAuth := strings.EqualFold(strings.TrimSpace(job.Provider), "claude") && strings.TrimSpace(e.ClaudeAuthHostPath) != ""
	if useCodexAuth {
		containerArgs, imageIndex, err = appendAuthMount(containerArgs, imageIndex, e.CodexAuthHostPath, "/tmp/forgeflow-codex-auth.json", true)
		if err != nil {
			return err
		}
	}
	if useClaudeAuth {
		containerArgs, imageIndex, err = appendAuthMount(containerArgs, imageIndex, e.ClaudeAuthHostPath, "/tmp/claude-home", false)
		if err != nil {
			return err
		}
	}
	containerEnvironment := append([]string(nil), environment...)
	entrypoint := name
	commandArgs := args
	if useCodexAuth {
		entrypoint = "/bin/sh"
		commandArgs = append([]string{"-c", `umask 077; mkdir -p /tmp/codex-home; cp /tmp/forgeflow-codex-auth.json /tmp/codex-home/auth.json; exec codex "$@"`, "--"}, args...)
	}
	if job.MCPConfigContents != "" {
		containerEnvironment = append(containerEnvironment, "FORGEFLOW_CLAUDE_MCP_CONFIG="+job.MCPConfigContents)
		entrypoint = "/bin/sh"
		commandArgs = append([]string{"-c", `umask 077; printf '%s' "$FORGEFLOW_CLAUDE_MCP_CONFIG" > /tmp/forgeflow-mcp.json; exec claude "$@"`, "--"}, args...)
	}
	argsWithEnvironment := append([]string(nil), containerArgs[:imageIndex]...)
	for _, entry := range containerEnvironment {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			argsWithEnvironment = append(argsWithEnvironment, "--env", key)
		}
	}
	if useCodexAuth {
		argsWithEnvironment = append(argsWithEnvironment, "--env", "CODEX_HOME=/tmp/codex-home")
	}
	if useClaudeAuth {
		argsWithEnvironment = append(argsWithEnvironment, "--env", "CLAUDE_CONFIG_DIR=/tmp/claude-home")
	}
	argsWithEnvironment = append(argsWithEnvironment, "--entrypoint", entrypoint, containerArgs[imageIndex])
	argsWithEnvironment = append(argsWithEnvironment, commandArgs...)
	if err := e.Commands.Run(ctx, "", "docker", argsWithEnvironment, output, output, containerEnvironment); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.Commands.Run(cleanupCtx, "", "docker", []string{"rm", "-f", dockerContainerName(job.ID)}, io.Discard, io.Discard, nil)
		return err
	}
	return nil
}

func isProviderAuthFailure(provider, output string, err error) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "codex" && provider != "claude" {
		return false
	}
	message := strings.ToLower(output)
	if err != nil {
		message += "\n" + strings.ToLower(err.Error())
	}
	for _, marker := range []string{
		"not logged in",
		"login required",
		"please log in",
		"codex login",
		"claude auth login",
		"authentication",
		"authentication required",
		"invalid authentication",
		"api key",
		"unauthorized",
		"401",
		"token expired",
		"token is expired",
		"expired token",
		"access token has expired",
		"refresh token",
		"bind source path does not exist",
		"mount source path",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return strings.Contains(message, "no such file or directory") && (strings.Contains(message, "auth.json") || strings.Contains(message, "codex-home"))
}

func isCodexAuthFailure(provider, output string, err error) bool {
	if !strings.EqualFold(strings.TrimSpace(provider), "codex") {
		return false
	}
	return isProviderAuthFailure(provider, output, err)
}

func isProviderUsageLimit(provider, output string, err error) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "codex" && provider != "claude" {
		return false
	}
	message := strings.ToLower(output)
	if err != nil {
		message += "\n" + strings.ToLower(err.Error())
	}
	for _, marker := range []string{"usage limit", "rate limit", "too many requests"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func providerUsageLimitMessage(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "claude") {
		return "Claude CLI account usage limit reached; retry after quota resets or switch to another authenticated account."
	}
	return "Codex CLI account usage limit reached; retry after quota resets or switch to another authenticated account."
}

func providerAuthPath(provider, codexPath, claudePath string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "codex") {
		return strings.TrimSpace(codexPath)
	}
	if strings.EqualFold(strings.TrimSpace(provider), "claude") {
		return strings.TrimSpace(claudePath)
	}
	return ""
}

func authRequiredMessage(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "claude") {
		return claudeAuthRequiredMessage
	}
	return codexAuthRequiredMessage
}

func appendAuthMount(containerArgs []string, imageIndex int, source, target string, file bool) ([]string, int, error) {
	source = filepath.Clean(strings.TrimSpace(source))
	if !filepath.IsAbs(source) || strings.ContainsAny(source, "\x00\r\n,") {
		kind := "directory"
		if file {
			kind = "file"
		}
		return nil, 0, fmt.Errorf("provider auth %s path must be an absolute host path without commas", kind)
	}
	withAuth := make([]string, 0, len(containerArgs)+3)
	withAuth = append(withAuth, containerArgs[:imageIndex]...)
	withAuth = append(withAuth, "--mount", "type=bind,source="+source+",target="+target+",readonly")
	withAuth = append(withAuth, containerArgs[imageIndex:]...)
	return withAuth, len(withAuth) - 1, nil
}

func gitEnvironment(explicitToken string) []string {
	result := safeEnvironment(nil, "")
	result = append(result, "GIT_TERMINAL_PROMPT=0")
	token := strings.TrimSpace(explicitToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("FORGEFLOW_GIT_TOKEN"))
	}
	if token != "" && !strings.ContainsAny(token, "\x00\n\r") {
		result = append(result, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Bearer "+token)
	}
	return result
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
		return exitErr.ExitCode()
	}
	return 1
}

func boundedError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 4000 {
		return message[:4000]
	}
	return message
}

type limitedBuffer struct {
	data  []byte
	limit int
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := b.limit - len(b.data)
	if remaining <= 0 {
		return originalLength, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
	}
	b.data = append(b.data, value...)
	return originalLength, nil
}

func (b *limitedBuffer) String() string { return string(b.data) }

func containerArguments(job Job) ([]string, error) {
	if err := job.Validate(); err != nil {
		return nil, err
	}
	image := strings.TrimSpace(job.Image)
	if image == "" {
		image = "forgeflow/agent-runner:1.0.0"
	}
	network := strings.TrimSpace(job.NetworkMode)
	if network == "" {
		network = "none"
	}
	workspaceMode := "rw"
	if job.ReadOnly {
		workspaceMode = "ro"
	}
	args := []string{"run", "--rm", "--name", dockerContainerName(job.ID), "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "1000:1000", "--network", network, "--workdir", "/workspace", "--volume", job.Workspace + ":/workspace:" + workspaceMode, "--tmpfs", "/tmp:rw,noexec,nosuid,size=256m"}
	if job.CPULimit != "" {
		args = append(args, "--cpus", job.CPULimit)
	}
	if job.MemoryLimit != "" {
		args = append(args, "--memory", job.MemoryLimit)
	}
	if job.PidsLimit > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(job.PidsLimit))
	}
	args = append(args, image)
	return args, nil
}

func dockerContainerName(jobID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(jobID)))
	return fmt.Sprintf("forgeflow-agent-%x", digest[:8])
}

func redactSecrets(value string, environment []string) string {
	for _, entry := range environment {
		name, secret, ok := strings.Cut(entry, "=")
		if !ok || secret == "" {
			continue
		}
		upper := strings.ToUpper(name)
		if !strings.Contains(upper, "KEY") && !strings.Contains(upper, "TOKEN") && !strings.Contains(upper, "SECRET") && !strings.Contains(upper, "PASSWORD") {
			continue
		}
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}
