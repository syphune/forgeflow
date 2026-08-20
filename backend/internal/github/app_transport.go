package github

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const githubAPIBaseURL = "https://api.github.com"

type githubAppTransport struct {
	base           http.RoundTripper
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func parseGitHubAppPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA private key: %w", err)
	}
	return key, nil
}

func newGitHubAppTransport(base http.RoundTripper, appID, installationID int64, privateKey *rsa.PrivateKey) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &githubAppTransport{base: base, appID: appID, installationID: installationID, privateKey: privateKey}
}

func (t *githubAppTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	authorization, err := t.authorization(req.Context())
	if err != nil {
		return nil, err
	}
	request := req.Clone(req.Context())
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Accept", "application/vnd.github+json")
	return t.base.RoundTrip(request)
}

func (t *githubAppTransport) authorization(ctx context.Context) (string, error) {
	if t.installationID == 0 {
		jwtToken, err := t.appJWT()
		if err != nil {
			return "", err
		}
		return "Bearer " + jwtToken, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.accessToken != "" && time.Now().Before(t.expiresAt.Add(-time.Minute)) {
		return "Bearer " + t.accessToken, nil
	}

	jwtToken, err := t.appJWT()
	if err != nil {
		return "", err
	}
	token, expiresAt, err := t.fetchInstallationToken(ctx, jwtToken)
	if err != nil {
		return "", err
	}
	t.accessToken, t.expiresAt = token, expiresAt
	return "Bearer " + token, nil
}

func (t *githubAppTransport) appJWT() (string, error) {
	if t.privateKey == nil || t.appID <= 0 {
		return "", fmt.Errorf("GitHub App credentials are incomplete")
	}
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(t.appID, 10),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(t.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return signed, nil
}

func (t *githubAppTransport) fetchInstallationToken(ctx context.Context, jwtToken string) (string, time.Time, error) {
	endpoint := githubAPIBaseURL + "/app/installations/" + strconv.FormatInt(t.installationID, 10) + "/access_tokens"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create GitHub installation token request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+jwtToken)
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request GitHub installation token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", time.Time{}, fmt.Errorf("GitHub installation token returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&payload); err != nil {
		return "", time.Time{}, fmt.Errorf("decode GitHub installation token: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" || payload.ExpiresAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("GitHub installation token response is incomplete")
	}
	return payload.Token, payload.ExpiresAt, nil
}
