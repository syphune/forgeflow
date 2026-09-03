package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/httpapi"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

const (
	AuthStatusAuthenticated       = "authenticated"
	AuthStatusLoginRequired       = "login_required"
	AuthStatusLoginPending        = "login_pending"
	AuthStatusUnavailable         = "unavailable"
	providerAuthLoginTTL          = 15 * time.Minute
	providerAuthMaxOutput         = 32 << 10
	providerAuthResponseMaxOutput = 2 << 20
	providerModelCatalogMaxOutput = 512 << 10
	providerModelCatalogTimeout   = 8 * time.Second
	providerModelPageLimit        = 200
	providerModelMaxPages         = 10
	providerModelMaxItems         = 200
)

var (
	ansiPattern        = regexp.MustCompile(`\x1b\[[0-9;]*[[:alpha:]]`)
	loginURLPattern    = regexp.MustCompile(`https://[^\s"'<>]+`)
	loginCodePattern   = regexp.MustCompile(`\b[A-Z0-9]{4,8}-[A-Z0-9]{4,8}\b`)
	loginExpiryPattern = regexp.MustCompile(`(?i)expires in\s+([0-9]+)\s+minutes?`)
	errAuthProfile     = errors.New("provider auth profile is unavailable")
)

type AuthStatus struct {
	Provider            string                    `json:"provider"`
	Status              string                    `json:"status"`
	Message             string                    `json:"message"`
	VerificationURI     string                    `json:"verification_uri,omitempty"`
	UserCode            string                    `json:"user_code,omitempty"`
	ExpiresAt           *time.Time                `json:"expires_at,omitempty"`
	Models              []ProviderModel           `json:"models,omitempty"`
	ReasoningEfforts    []ProviderReasoningEffort `json:"reasoning_efforts,omitempty"`
	ModelCatalogMessage string                    `json:"model_catalog_message,omitempty"`
}

type ProviderReasoningEffort struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type ProviderModel struct {
	ID                     string                    `json:"id"`
	DisplayName            string                    `json:"display_name"`
	Description            string                    `json:"description,omitempty"`
	IsDefault              bool                      `json:"is_default"`
	DefaultReasoningEffort string                    `json:"default_reasoning_effort,omitempty"`
	ReasoningEfforts       []ProviderReasoningEffort `json:"reasoning_efforts"`
}

type AuthList struct {
	Items []AuthStatus `json:"items"`
}

// ProviderAuthenticator is the boundary between HTTP/API code and provider CLI auth.
type ProviderAuthenticator interface {
	List(context.Context) ([]AuthStatus, error)
	Status(context.Context, string) (AuthStatus, error)
	Login(context.Context, string, bool) (AuthStatus, error)
	Logout(context.Context, string) (AuthStatus, error)
}

type authSession struct {
	provider  string
	command   *exec.Cmd
	output    *authOutput
	startedAt time.Time
	expiresAt time.Time
	done      chan struct{}
	finished  bool
}

// AuthManager owns the shared CLI profiles mounted into server-runner jobs.
type AuthManager struct {
	mu              sync.Mutex
	codexHome       string
	claudeConfigDir string
	now             func() time.Time
	sessions        map[string]*authSession
}

func NewAuthManager(codexHome, claudeConfigDir string) *AuthManager {
	return &AuthManager{
		codexHome:       strings.TrimSpace(codexHome),
		claudeConfigDir: strings.TrimSpace(claudeConfigDir),
		now:             func() time.Time { return time.Now().UTC() },
		sessions:        make(map[string]*authSession),
	}
}

func (m *AuthManager) List(ctx context.Context) ([]AuthStatus, error) {
	items := make([]AuthStatus, 0, 2)
	for _, provider := range []string{"codex", "claude"} {
		status, err := m.Status(ctx, provider)
		if err != nil {
			return nil, err
		}
		items = append(items, status)
	}
	return items, nil
}

func (m *AuthManager) Status(ctx context.Context, provider string) (AuthStatus, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return AuthStatus{}, err
	}

	m.mu.Lock()
	if session := m.sessions[provider]; session != nil {
		if !session.finished {
			if !m.now().Before(session.expiresAt) {
				m.stopSessionLocked(provider, session)
				m.mu.Unlock()
				return loginRequiredStatus(provider, "The sign-in window expired. Start sign-in again."), nil
			}
			status := pendingStatus(provider, session.output.String(), session.startedAt, session.expiresAt)
			m.mu.Unlock()
			return status, nil
		}
		delete(m.sessions, provider)
	}
	m.mu.Unlock()

	if !m.configured(provider) {
		return unavailableStatus(provider), nil
	}
	output, commandErr := m.runProviderCommand(ctx, provider, statusArgs(provider)...)
	status := statusFromCommand(provider, output, commandErr)
	if status.Status == AuthStatusAuthenticated {
		m.addModelCatalog(ctx, provider, &status)
	}
	return status, nil
}

func (m *AuthManager) Login(ctx context.Context, provider string, switchAccount bool) (AuthStatus, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return AuthStatus{}, err
	}
	if !m.configured(provider) {
		return unavailableStatus(provider), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[provider]; session != nil && !session.finished {
		if !switchAccount {
			return pendingStatus(provider, session.output.String(), session.startedAt, session.expiresAt), nil
		}
		m.stopSessionLocked(provider, session)
	}
	// ponytail: one process-wide provider lock matches the shared profile; use per-account profiles if concurrent logins are needed.
	if switchAccount {
		if _, logoutErr := m.runProviderCommand(ctx, provider, logoutArgs(provider)...); logoutErr != nil && !isCommandUnavailable(logoutErr) {
			return unavailableStatus(provider), nil
		}
	}

	startedAt := m.now()
	commandName, args := loginCommand(provider)
	command := exec.CommandContext(context.Background(), commandName, args...)
	command.Dir = "/tmp"
	command.Env = m.authEnvironment(provider)
	output := &authOutput{limit: providerAuthMaxOutput}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		return unavailableStatus(provider), nil
	}
	session := &authSession{
		provider:  provider,
		command:   command,
		output:    output,
		startedAt: startedAt,
		expiresAt: startedAt.Add(providerAuthLoginTTL),
		done:      make(chan struct{}),
	}
	m.sessions[provider] = session
	go m.waitForSession(session)
	go m.expireSession(session)
	return pendingStatus(provider, output.String(), session.startedAt, session.expiresAt), nil
}

func (m *AuthManager) Logout(ctx context.Context, provider string) (AuthStatus, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return AuthStatus{}, err
	}
	if !m.configured(provider) {
		return unavailableStatus(provider), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[provider]; session != nil {
		m.stopSessionLocked(provider, session)
	}
	// ponytail: logout is serialized with login so a shared CLI profile cannot be half-switched.
	_, commandErr := m.runProviderCommand(ctx, provider, logoutArgs(provider)...)
	if isCommandUnavailable(commandErr) || errors.Is(commandErr, errAuthProfile) {
		return unavailableStatus(provider), nil
	}
	return loginRequiredStatus(provider, "The server CLI account is signed out."), nil
}

func (m *AuthManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for provider, session := range m.sessions {
		m.stopSessionLocked(provider, session)
	}
}

func (m *AuthManager) waitForSession(session *authSession) {
	_ = session.command.Wait()
	m.mu.Lock()
	if current := m.sessions[session.provider]; current == session {
		session.finished = true
	}
	close(session.done)
	m.mu.Unlock()
}

func (m *AuthManager) expireSession(session *authSession) {
	timer := time.NewTimer(time.Until(session.expiresAt))
	defer timer.Stop()
	select {
	case <-session.done:
		return
	case <-timer.C:
		m.mu.Lock()
		if current := m.sessions[session.provider]; current == session && !session.finished {
			_ = session.command.Process.Kill()
		}
		m.mu.Unlock()
	}
}

func (m *AuthManager) stopSessionLocked(provider string, session *authSession) {
	if !session.finished && session.command.Process != nil {
		_ = session.command.Process.Kill()
	}
	delete(m.sessions, provider)
}

func (m *AuthManager) configured(provider string) bool {
	path := m.profilePath(provider)
	return path != "" && filepath.IsAbs(path) && !strings.ContainsAny(path, "\x00\r\n,")
}

func (m *AuthManager) profilePath(provider string) string {
	if provider == "codex" {
		return m.codexHome
	}
	return m.claudeConfigDir
}

func (m *AuthManager) runProviderCommand(ctx context.Context, provider string, args ...string) (string, error) {
	profile := m.profilePath(provider)
	if !m.configured(provider) {
		return "", fmt.Errorf("%w for %s", errAuthProfile, provider)
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return "", fmt.Errorf("%w for %s: %v", errAuthProfile, provider, err)
	}
	commandName, _ := statusCommand(provider)
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, commandName, args...)
	command.Dir = "/tmp"
	command.Env = m.authEnvironment(provider)
	output := &authOutput{limit: providerAuthMaxOutput}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if commandCtx.Err() != nil {
		return output.String(), commandCtx.Err()
	}
	return output.String(), err
}

func (m *AuthManager) authEnvironment(provider string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/forgeflow"
	}
	environment := []string{"PATH=" + path, "HOME=" + home, "TERM=dumb", "NO_COLOR=1"}
	if provider == "codex" {
		return append(environment, "CODEX_HOME="+m.profilePath(provider))
	}
	return append(environment, "CLAUDE_CONFIG_DIR="+m.profilePath(provider))
}

func loginCommand(provider string) (string, []string) {
	if provider == "codex" {
		return "codex", []string{"login", "--device-auth"}
	}
	return "claude", []string{"auth", "login", "--claudeai"}
}

func statusCommand(provider string) (string, []string) {
	if provider == "codex" {
		return "codex", []string{"login", "status"}
	}
	return "claude", []string{"auth", "status", "--json"}
}

func statusArgs(provider string) []string {
	_, args := statusCommand(provider)
	return args
}

func logoutArgs(provider string) []string {
	if provider == "codex" {
		return []string{"logout"}
	}
	return []string{"auth", "logout"}
}

func normalizeProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "codex" && provider != "claude" {
		return "", apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "provider must be codex or claude", nil)
	}
	return provider, nil
}

func statusFromCommand(provider, output string, commandErr error) AuthStatus {
	if errors.Is(commandErr, errAuthProfile) || isCommandUnavailable(commandErr) {
		return unavailableStatus(provider)
	}
	clean := stripANSI(output)
	if providerLoggedIn(provider, clean) {
		return AuthStatus{Provider: provider, Status: AuthStatusAuthenticated, Message: providerName(provider) + " CLI is authenticated on the server."}
	}
	return loginRequiredStatus(provider, "Sign in to the server "+providerName(provider)+" CLI from this screen.")
}

func (m *AuthManager) addModelCatalog(ctx context.Context, provider string, status *AuthStatus) {
	if provider == "claude" {
		status.ReasoningEfforts = claudeReasoningEfforts()
		status.ModelCatalogMessage = "Claude CLI does not expose an account model catalog; enter a model alias or full model ID."
		return
	}
	models, err := m.codexModelCatalog(ctx)
	if err != nil {
		status.ModelCatalogMessage = "Codex CLI did not return its account model catalog; enter a model ID manually."
		return
	}
	status.Models = models
}

func claudeReasoningEfforts() []ProviderReasoningEffort {
	return []ProviderReasoningEffort{
		{Value: "low", Description: "Lighter reasoning for faster responses"},
		{Value: "medium", Description: "Balances speed and reasoning depth"},
		{Value: "high", Description: "Greater reasoning depth for complex tasks"},
		{Value: "xhigh", Description: "Extra high reasoning depth"},
		{Value: "max", Description: "Maximum reasoning depth"},
	}
}

type codexRPCEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type codexModelListResponse struct {
	Data       []codexModel `json:"data"`
	NextCursor *string      `json:"nextCursor"`
}

type codexModel struct {
	ID                        string                 `json:"id"`
	Model                     string                 `json:"model"`
	DisplayName               string                 `json:"displayName"`
	Description               string                 `json:"description"`
	Hidden                    bool                   `json:"hidden"`
	IsDefault                 bool                   `json:"isDefault"`
	DefaultReasoningEffort    string                 `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []codexReasoningEffort `json:"supportedReasoningEfforts"`
}

type codexReasoningEffort struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description"`
}

func (m *AuthManager) codexModelCatalog(ctx context.Context) ([]ProviderModel, error) {
	if !m.configured("codex") {
		return nil, errAuthProfile
	}
	commandCtx, cancel := context.WithTimeout(ctx, providerModelCatalogTimeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, "codex", "app-server", "--stdio")
	command.Dir = "/tmp"
	command.Env = m.authEnvironment("codex")
	command.Stderr = io.Discard
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	defer func() {
		cancel()
		_ = command.Process.Kill()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = command.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4<<10), providerModelCatalogMaxOutput)
	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{
		"method": "initialize",
		"id":     1,
		"params": map[string]any{
			"clientInfo":   map[string]string{"name": "forgeflow", "version": "0.1.0"},
			"capabilities": map[string]bool{"experimentalApi": true},
		},
	}); err != nil {
		return nil, err
	}
	if err := readCodexRPCResponse(scanner, 1, nil); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		return nil, err
	}

	models := make([]ProviderModel, 0)
	var cursor *string
	for page := 0; page < providerModelMaxPages; page++ {
		requestID := page + 2
		if err := encoder.Encode(map[string]any{
			"method": "model/list",
			"id":     requestID,
			"params": map[string]any{"cursor": cursor, "includeHidden": false, "limit": providerModelPageLimit},
		}); err != nil {
			return nil, err
		}
		var response codexModelListResponse
		if err := readCodexRPCResponse(scanner, requestID, &response); err != nil {
			return nil, err
		}
		models = append(models, providerModels(response.Data)...)
		if len(models) >= providerModelMaxItems {
			return models[:providerModelMaxItems], nil
		}
		if response.NextCursor == nil || strings.TrimSpace(*response.NextCursor) == "" {
			return models, nil
		}
		cursor = response.NextCursor
	}
	return models, nil
}

func readCodexRPCResponse(scanner *bufio.Scanner, expectedID int, target any) error {
	wantedID := strconv.Itoa(expectedID)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope codexRPCEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			return fmt.Errorf("invalid Codex app-server response: %w", err)
		}
		if string(envelope.ID) != wantedID {
			continue
		}
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			return errors.New("Codex app-server returned an error")
		}
		if target == nil || len(envelope.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(envelope.Result, target); err != nil {
			return fmt.Errorf("decode Codex model catalog: %w", err)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("Codex app-server closed before returning a response")
}

func providerModels(values []codexModel) []ProviderModel {
	result := make([]ProviderModel, 0, len(values))
	for _, value := range values {
		if value.Hidden {
			continue
		}
		id := catalogText(value.ID, 120)
		if id == "" {
			id = catalogText(value.Model, 120)
		}
		if id == "" {
			continue
		}
		efforts := make([]ProviderReasoningEffort, 0, len(value.SupportedReasoningEfforts))
		for _, effort := range value.SupportedReasoningEfforts {
			reasoningEffort := catalogText(effort.ReasoningEffort, 64)
			if reasoningEffort == "" || strings.ContainsAny(reasoningEffort, "\x00\r\n") {
				continue
			}
			efforts = append(efforts, ProviderReasoningEffort{Value: reasoningEffort, Description: catalogText(effort.Description, 256)})
			if len(efforts) == 32 {
				break
			}
		}
		displayName := catalogText(value.DisplayName, 200)
		if displayName == "" {
			displayName = id
		}
		result = append(result, ProviderModel{
			ID: id, DisplayName: displayName, Description: catalogText(value.Description, 1000),
			IsDefault: value.IsDefault, DefaultReasoningEffort: catalogText(value.DefaultReasoningEffort, 64), ReasoningEfforts: efforts,
		})
		if len(result) == providerModelMaxItems {
			break
		}
	}
	return result
}

func catalogText(value string, maxRunes int) string {
	value = strings.TrimSpace(stripANSI(value))
	if len([]rune(value)) > maxRunes {
		value = string([]rune(value)[:maxRunes])
	}
	return value
}

func providerLoggedIn(provider, output string) bool {
	if provider == "claude" {
		var status struct {
			LoggedIn bool `json:"loggedIn"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(output)), &status) == nil && status.LoggedIn {
			return true
		}
		compact := strings.ReplaceAll(strings.ToLower(output), " ", "")
		return strings.Contains(compact, `"loggedin":true`)
	}
	return strings.Contains(strings.ToLower(output), "logged in using")
}

func pendingStatus(provider, output string, startedAt, fallbackExpiry time.Time) AuthStatus {
	clean := stripANSI(output)
	verificationURI, userCode := parseLoginDetails(clean)
	expiresAt := fallbackExpiry
	if matches := loginExpiryPattern.FindStringSubmatch(clean); len(matches) == 2 {
		if minutes, err := time.ParseDuration(matches[1] + "m"); err == nil {
			expiresAt = startedAt.Add(minutes)
		}
	}
	return AuthStatus{
		Provider:        provider,
		Status:          AuthStatusLoginPending,
		Message:         "Complete the browser sign-in to authenticate the server CLI.",
		VerificationURI: verificationURI,
		UserCode:        userCode,
		ExpiresAt:       timePointer(expiresAt),
	}
}

func parseLoginDetails(output string) (string, string) {
	verificationURI := ""
	if candidate := loginURLPattern.FindString(output); candidate != "" {
		candidate = strings.TrimRight(candidate, ".,)>]")
		if parsed, err := url.Parse(candidate); err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil {
			verificationURI = candidate
		}
	}
	userCode := ""
	if candidate := loginCodePattern.FindString(output); candidate != "" {
		userCode = candidate
	}
	return verificationURI, userCode
}

func loginRequiredStatus(provider, message string) AuthStatus {
	return AuthStatus{Provider: provider, Status: AuthStatusLoginRequired, Message: message}
}

func unavailableStatus(provider string) AuthStatus {
	return AuthStatus{Provider: provider, Status: AuthStatusUnavailable, Message: providerName(provider) + " CLI authentication is not configured on the server runner."}
}

func providerName(provider string) string {
	if provider == "codex" {
		return "Codex"
	}
	return "Claude"
}

func timePointer(value time.Time) *time.Time { return &value }

func stripANSI(value string) string { return ansiPattern.ReplaceAllString(value, "") }

func isCommandUnavailable(err error) bool {
	var commandErr *exec.Error
	return errors.As(err, &commandErr)
}

type authOutput struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (o *authOutput) Write(value []byte) (int, error) {
	originalLength := len(value)
	o.mu.Lock()
	defer o.mu.Unlock()
	remaining := o.limit - len(o.data)
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		o.data = append(o.data, value...)
	}
	return originalLength, nil
}

func (o *authOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.data)
}

type HTTPAuthClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (c HTTPAuthClient) List(ctx context.Context) ([]AuthStatus, error) {
	var response AuthList
	if err := c.request(ctx, http.MethodGet, "/auth", &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

func (c HTTPAuthClient) Status(ctx context.Context, provider string) (AuthStatus, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return AuthStatus{}, err
	}
	var response AuthStatus
	if err := c.request(ctx, http.MethodGet, "/auth/"+provider, &response); err != nil {
		return AuthStatus{}, err
	}
	return response, nil
}

func (c HTTPAuthClient) Login(ctx context.Context, provider string, switchAccount bool) (AuthStatus, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return AuthStatus{}, err
	}
	path := "/auth/" + provider + "/login"
	if switchAccount {
		path = "/auth/" + provider + "/switch"
	}
	var response AuthStatus
	if err := c.request(ctx, http.MethodPost, path, &response); err != nil {
		return AuthStatus{}, err
	}
	return response, nil
}

func (c HTTPAuthClient) Logout(ctx context.Context, provider string) (AuthStatus, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return AuthStatus{}, err
	}
	var response AuthStatus
	if err := c.request(ctx, http.MethodPost, "/auth/"+provider+"/logout", &response); err != nil {
		return AuthStatus{}, err
	}
	return response, nil
}

func (c HTTPAuthClient) request(ctx context.Context, method, path string, target any) error {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" || strings.TrimSpace(c.Token) == "" {
		return providerAuthUnavailableError()
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return providerAuthUnavailableError()
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return providerAuthUnavailableError()
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, providerAuthResponseMaxOutput))
	if err != nil {
		return providerAuthUnavailableError()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return providerAuthUnavailableError()
	}
	if err := json.Unmarshal(body, target); err != nil {
		return providerAuthUnavailableError()
	}
	return nil
}

func providerAuthUnavailableError() error {
	return apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "server runner authentication is unavailable", nil)
}

type ProviderAuthHandler struct {
	authenticator ProviderAuthenticator
}

func NewProviderAuthHandler(authenticator ProviderAuthenticator) http.Handler {
	h := &ProviderAuthHandler{authenticator: authenticator}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /projects/{id}/provider-auth", h.list)
	mux.HandleFunc("POST /projects/{id}/provider-auth/{provider}/login", h.login)
	mux.HandleFunc("POST /projects/{id}/provider-auth/{provider}/switch", h.switchAccount)
	mux.HandleFunc("POST /projects/{id}/provider-auth/{provider}/logout", h.logout)
	return mux
}

func (h *ProviderAuthHandler) list(w http.ResponseWriter, r *http.Request) {
	if err := h.authorizeList(r); err != nil {
		handleProviderAuthError(w, r, err)
		return
	}
	if h.authenticator == nil {
		handleProviderAuthError(w, r, providerAuthUnavailableError())
		return
	}
	items, err := h.authenticator.List(r.Context())
	if err != nil {
		handleProviderAuthError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, AuthList{Items: items})
}

func (h *ProviderAuthHandler) login(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, false)
}

func (h *ProviderAuthHandler) switchAccount(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, true)
}

func (h *ProviderAuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.authorize(r); err != nil {
		handleProviderAuthError(w, r, err)
		return
	}
	if h.authenticator == nil {
		handleProviderAuthError(w, r, providerAuthUnavailableError())
		return
	}
	status, err := h.authenticator.Logout(r.Context(), r.PathValue("provider"))
	if err != nil {
		handleProviderAuthError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, status)
}

func (h *ProviderAuthHandler) action(w http.ResponseWriter, r *http.Request, switchAccount bool) {
	if err := h.authorize(r); err != nil {
		handleProviderAuthError(w, r, err)
		return
	}
	if h.authenticator == nil {
		handleProviderAuthError(w, r, providerAuthUnavailableError())
		return
	}
	status, err := h.authenticator.Login(r.Context(), r.PathValue("provider"), switchAccount)
	if err != nil {
		handleProviderAuthError(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, status)
}

func (h *ProviderAuthHandler) authorize(r *http.Request) error {
	return h.authorizeCapabilities(r, identity.CapabilityProjectManage)
}

func (h *ProviderAuthHandler) authorizeList(r *http.Request) error {
	return h.authorizeCapabilities(r, identity.CapabilityProjectManage, identity.CapabilityAutonomousStart)
}

func (h *ProviderAuthHandler) authorizeCapabilities(r *http.Request, capabilities ...string) error {
	actor, ok := identity.ActorFromContext(r.Context())
	if !ok || strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authenticated actor is required", nil)
	}
	projectID := strings.TrimSpace(r.PathValue("id"))
	if projectID == "" {
		return apperr.New(apperr.CodeInvalidArgument, http.StatusUnprocessableEntity, "project id is required", nil)
	}
	for _, capability := range capabilities {
		if actor.ForProject(projectID).Has(capability) {
			return nil
		}
	}
	return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "permission denied", map[string]any{"capability": capabilities[0]})
}

func handleProviderAuthError(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.Error(w, r, err)
}
