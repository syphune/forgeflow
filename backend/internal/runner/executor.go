package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxOutputBytes = 2 << 20

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
	Commands CommandRunner
	Now      func() time.Time
}

func (e Executor) Execute(ctx context.Context, job Job, emit func(Event) error) (Result, error) {
	if err := job.Validate(); err != nil {
		return Result{}, err
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
	if emit != nil {
		if err := emit(Event{JobID: job.ID, Type: EventStarted}); err != nil {
			return Result{}, err
		}
	}
	name, args, err := providerCommand(job)
	if err != nil {
		return Result{}, err
	}
	output := &limitedBuffer{limit: maxOutputBytes}
	environment := safeEnvironment(job.Environment, job.Provider)
	err = e.Commands.Run(ctx, job.Workspace, name, args, output, output, environment)
	result := Result{ExitCode: 0, Output: output.String()}
	if err != nil {
		result.ExitCode = exitCode(err)
		result.Error = boundedError(err)
		if ctx.Err() != nil {
			result.Error = "runner timed out or was cancelled"
		}
		if emit != nil {
			_ = emit(Event{JobID: job.ID, Type: EventFailed, Text: result.Output, ExitCode: result.ExitCode, Error: result.Error})
		}
		return result, err
	}
	if emit != nil {
		if err := emit(Event{JobID: job.ID, Type: EventCompleted, Text: result.Output, ExitCode: result.ExitCode}); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func (e Executor) prepareWorkspace(ctx context.Context, job Job) error {
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
	if err := e.runGit(ctx, "clone repository", "", cloneArgs); err != nil {
		return err
	}
	base := strings.TrimSpace(job.BaseSHA)
	if base == "" {
		base = "HEAD"
	}
	if base != "HEAD" {
		if err := e.runGit(ctx, "fetch base commit", job.Workspace, []string{"-C", job.Workspace, "fetch", "--depth", "1", "origin", base}); err != nil {
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
	return e.runGit(ctx, "checkout repository", job.Workspace, checkoutArgs)
}

func (e Executor) runGit(ctx context.Context, action, dir string, args []string) error {
	output := &limitedBuffer{limit: maxOutputBytes}
	if err := e.Commands.Run(ctx, dir, "git", args, output, output, gitEnvironment()); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: runner timed out or was cancelled", action)
		}
		return fmt.Errorf("%s: %s", action, boundedError(err))
	}
	return nil
}

func providerCommand(job Job) (string, []string, error) {
	prompt := strings.TrimSpace(job.Prompt)
	switch strings.ToLower(strings.TrimSpace(job.Provider)) {
	case "codex":
		args := []string{"exec", "--json", "--full-auto"}
		if strings.TrimSpace(job.Model) != "" {
			args = append(args, "--model", strings.TrimSpace(job.Model))
		}
		return "codex", append(args, prompt), nil
	case "claude":
		args := []string{"--print", "--output-format", "json"}
		if strings.TrimSpace(job.Model) != "" {
			args = append(args, "--model", strings.TrimSpace(job.Model))
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
		allowed["OPENAI_API_KEY"] = true
		allowed["OPENAI_BASE_URL"] = true
		allowed["CODEX_HOME"] = true
	case "claude":
		allowed["ANTHROPIC_API_KEY"] = true
		allowed["ANTHROPIC_BASE_URL"] = true
		allowed["CLAUDE_CODE_OAUTH_TOKEN"] = true
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
		if strings.Contains(strings.ToUpper(name), "TOKEN") || strings.Contains(strings.ToUpper(name), "SECRET") || strings.Contains(strings.ToUpper(name), "PASSWORD") {
			continue
		}
		result = append(result, name+"="+value)
	}
	return result
}

func gitEnvironment() []string {
	result := safeEnvironment(nil, "")
	result = append(result, "GIT_TERMINAL_PROMPT=0")
	if token := strings.TrimSpace(os.Getenv("FORGEFLOW_GIT_TOKEN")); token != "" && !strings.ContainsAny(token, "\x00\n\r") {
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
		image = "forgeflow/agent-runner:latest"
	}
	network := strings.TrimSpace(job.NetworkMode)
	if network == "" {
		network = "none"
	}
	args := []string{"run", "--rm", "--read-only", "--network", network, "--workdir", "/workspace", "--volume", job.Workspace + ":/workspace:rw"}
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
