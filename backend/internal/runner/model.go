package runner

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var resourceLimitPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?(?:[bkmg]i?|[ept])?$`)
var reasoningEffortPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Job struct {
	ID                string            `json:"id"`
	AutonomousRunID   string            `json:"autonomous_run_id"`
	AgentRunID        string            `json:"agent_run_id"`
	Provider          string            `json:"provider"`
	Model             string            `json:"model,omitempty"`
	ReasoningEffort   string            `json:"reasoning_effort,omitempty"`
	Prompt            string            `json:"prompt"`
	RepositoryURL     string            `json:"repository_url,omitempty"`
	BaseSHA           string            `json:"base_sha,omitempty"`
	Branch            string            `json:"branch,omitempty"`
	PushBranch        bool              `json:"push_branch,omitempty"`
	CommitMessage     string            `json:"commit_message,omitempty"`
	Workspace         string            `json:"workspace"`
	WorkspaceRoot     string            `json:"workspace_root"`
	Image             string            `json:"image,omitempty"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	CPULimit          string            `json:"cpu_limit,omitempty"`
	MemoryLimit       string            `json:"memory_limit,omitempty"`
	PidsLimit         int               `json:"pids_limit,omitempty"`
	NetworkMode       string            `json:"network_mode,omitempty"`
	AllowedHosts      []string          `json:"allowed_hosts,omitempty"`
	MCPPermissions    []string          `json:"mcp_permissions,omitempty"`
	MCPURL            string            `json:"mcp_url,omitempty"`
	ReadOnly          bool              `json:"read_only,omitempty"`
	Environment       map[string]string `json:"environment,omitempty"`
	GitToken          string            `json:"-"`
	MCPToken          string            `json:"-"`
	MCPConfigPath     string            `json:"-"`
	MCPConfigContents string            `json:"-"`
}

type EventType string

const (
	EventStarted   EventType = "started"
	EventOutput    EventType = "output"
	EventCompleted EventType = "completed"
	EventFailed    EventType = "failed"
	EventCancelled EventType = "cancelled"
)

type Event struct {
	JobID     string    `json:"job_id"`
	Type      EventType `json:"type"`
	Stream    string    `json:"stream,omitempty"`
	Text      string    `json:"text,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	Error     string    `json:"error,omitempty"`
	ErrorCode string    `json:"error_code,omitempty"`
}

type Result struct {
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

const (
	ErrorCodeAuthRequired = "AUTH_REQUIRED"
	ErrorCodeUsageLimit   = "USAGE_LIMIT"
)

func (j Job) Validate() error {
	j.Provider = strings.ToLower(strings.TrimSpace(j.Provider))
	if j.ID == "" || j.AgentRunID == "" || j.AutonomousRunID == "" {
		return fmt.Errorf("job id, autonomous_run_id and agent_run_id are required")
	}
	if j.Provider != "codex" && j.Provider != "claude" {
		return fmt.Errorf("unsupported provider %q", j.Provider)
	}
	model := strings.TrimSpace(j.Model)
	if len([]rune(model)) > 120 || strings.ContainsAny(model, "\x00\r\n") {
		return fmt.Errorf("model is invalid or too long")
	}
	reasoningEffort := strings.TrimSpace(j.ReasoningEffort)
	if reasoningEffort != "" && (len([]rune(reasoningEffort)) > 64 || !reasoningEffortPattern.MatchString(reasoningEffort)) {
		return fmt.Errorf("reasoning_effort is invalid or too long")
	}
	if strings.TrimSpace(j.Prompt) == "" || len(j.Prompt) > 131072 {
		return fmt.Errorf("prompt is required and must be bounded")
	}
	if strings.TrimSpace(j.RepositoryURL) != "" {
		if err := validateRepositoryURL(j.RepositoryURL, j.AllowedHosts); err != nil {
			return err
		}
	}
	if strings.TrimSpace(j.MCPURL) != "" {
		if err := validateMCPURL(j.MCPURL); err != nil {
			return err
		}
	}
	if err := validateBaseSHA(j.BaseSHA); err != nil {
		return err
	}
	if err := validateBranch(j.Branch); err != nil {
		return err
	}
	if j.PushBranch {
		if strings.TrimSpace(j.Branch) == "" || strings.TrimSpace(j.RepositoryURL) == "" {
			return fmt.Errorf("push_branch requires repository_url and branch")
		}
		message := strings.TrimSpace(j.CommitMessage)
		if message == "" || len([]rune(message)) > 200 || strings.ContainsAny(message, "\x00\r\n") {
			return fmt.Errorf("commit_message is required and must be a single line of at most 200 characters")
		}
	} else if strings.TrimSpace(j.GitToken) != "" && strings.TrimSpace(j.RepositoryURL) == "" {
		return fmt.Errorf("git token requires repository_url")
	}
	if len([]byte(j.GitToken)) > 4096 || strings.ContainsAny(j.GitToken, "\x00\r\n") {
		return fmt.Errorf("git token is invalid")
	}
	if len([]byte(j.MCPToken)) > 4096 || strings.ContainsAny(j.MCPToken, "\x00\r\n") {
		return fmt.Errorf("MCP token is invalid")
	}
	if !safeWorkspace(j.WorkspaceRoot, j.Workspace) {
		return fmt.Errorf("workspace must be an existing child of workspace_root")
	}
	if j.TimeoutSeconds <= 0 || j.TimeoutSeconds > 24*60*60 {
		return fmt.Errorf("timeout_seconds must be between 1 and 86400")
	}
	if j.PidsLimit < 0 || j.PidsLimit > 4096 {
		return fmt.Errorf("pids_limit is invalid")
	}
	if j.CPULimit != "" && !resourceLimitPattern.MatchString(strings.ToLower(strings.TrimSpace(j.CPULimit))) {
		return fmt.Errorf("cpu_limit is invalid")
	}
	if j.MemoryLimit != "" && !resourceLimitPattern.MatchString(strings.ToLower(strings.TrimSpace(j.MemoryLimit))) {
		return fmt.Errorf("memory_limit is invalid")
	}
	if network := strings.ToLower(strings.TrimSpace(j.NetworkMode)); network != "" && network != "none" && network != "bridge" {
		return fmt.Errorf("network_mode must be none or bridge")
	}
	if image := strings.TrimSpace(j.Image); image != "" && (strings.ContainsAny(image, " \t\r\n") || strings.HasSuffix(strings.ToLower(image), ":latest")) {
		return fmt.Errorf("image must be a pinned image without whitespace or latest tag")
	}
	return nil
}

func safeWorkspace(root, workspace string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	workspace = filepath.Clean(strings.TrimSpace(workspace))
	if root == "." || workspace == "." || !filepath.IsAbs(root) || !filepath.IsAbs(workspace) {
		return false
	}
	rel, err := filepath.Rel(root, workspace)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateRepositoryURL(raw string, allowedHosts []string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.Path == "" {
		return fmt.Errorf("repository_url must be an HTTPS URL without embedded credentials")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if len(allowedHosts) == 0 {
		allowedHosts = []string{"github.com"}
	}
	for _, allowed := range allowedHosts {
		if host == strings.ToLower(strings.TrimSuffix(strings.TrimSpace(allowed), ".")) {
			return nil
		}
	}
	return fmt.Errorf("repository_url host is not allowed")
}

func validateMCPURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" || strings.ContainsAny(raw, "\x00\r\n") {
		return fmt.Errorf("mcp_url must be an HTTP(S) URL without embedded credentials")
	}
	return nil
}

func validateBaseSHA(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "HEAD" {
		return nil
	}
	if len(value) < 7 || len(value) > 64 {
		return fmt.Errorf("base_sha must be HEAD or a hexadecimal commit SHA")
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return fmt.Errorf("base_sha must be HEAD or a hexadecimal commit SHA")
		}
	}
	return nil
}

func validateBranch(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if value == "HEAD" || len(value) > 200 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.Contains(value, "//") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.ContainsAny(value, "\x00\n\r ~^:?*[\\") {
		return fmt.Errorf("branch is invalid")
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("branch is invalid")
		}
	}
	return nil
}
