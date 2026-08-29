package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const githubOAuthTokenURL = "https://github.com/login/oauth/access_token"

func (c *goAppClient) ExchangeUserCode(ctx context.Context, code, verifier, redirectURI string) (UserAccessToken, error) {
	if strings.TrimSpace(c.clientID) == "" || strings.TrimSpace(c.clientSecret) == "" {
		return UserAccessToken{}, userAuthorizationNotConfigured()
	}
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {strings.TrimSpace(code)},
		"redirect_uri":  {strings.TrimSpace(redirectURI)},
		"code_verifier": {strings.TrimSpace(verifier)},
	}
	return c.requestUserToken(ctx, form)
}

func (c *goAppClient) RefreshUserToken(ctx context.Context, refreshToken string) (UserAccessToken, error) {
	if strings.TrimSpace(c.clientID) == "" || strings.TrimSpace(c.clientSecret) == "" {
		return UserAccessToken{}, userAuthorizationNotConfigured()
	}
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {strings.TrimSpace(c.clientSecret)},
		"grant_type":    {"refresh_token"},
		"refresh_token": {strings.TrimSpace(refreshToken)},
	}
	return c.requestUserToken(ctx, form)
}

func (c *goAppClient) requestUserToken(ctx context.Context, form url.Values) (UserAccessToken, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, githubOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return UserAccessToken{}, fmt.Errorf("create GitHub user token request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "forgeflow")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return UserAccessToken{}, fmt.Errorf("request GitHub user token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return UserAccessToken{}, fmt.Errorf("GitHub user token request returned status %d", response.StatusCode)
	}
	var payload struct {
		AccessToken           string `json:"access_token"`
		RefreshToken          string `json:"refresh_token"`
		ExpiresIn             int    `json:"expires_in"`
		RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
		Error                 string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&payload); err != nil {
		return UserAccessToken{}, fmt.Errorf("decode GitHub user token: %w", err)
	}
	if strings.TrimSpace(payload.Error) != "" || strings.TrimSpace(payload.AccessToken) == "" {
		return UserAccessToken{}, fmt.Errorf("GitHub user token was not returned")
	}
	now := time.Now().UTC()
	return UserAccessToken{
		AccessToken:           payload.AccessToken,
		RefreshToken:          payload.RefreshToken,
		AccessTokenExpiresAt:  expiresAt(now, payload.ExpiresIn),
		RefreshTokenExpiresAt: expiresAt(now, payload.RefreshTokenExpiresIn),
	}, nil
}

func expiresAt(now time.Time, seconds int) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(seconds) * time.Second)
}

func (c *goAppClient) GitHubUser(ctx context.Context, accessToken string) (GitHubUser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return GitHubUser{}, fmt.Errorf("create GitHub user request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	request.Header.Set("User-Agent", "forgeflow")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return GitHubUser{}, fmt.Errorf("request GitHub user: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return GitHubUser{}, fmt.Errorf("GitHub user request returned status %d", response.StatusCode)
	}
	var payload struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&payload); err != nil {
		return GitHubUser{}, fmt.Errorf("decode GitHub user: %w", err)
	}
	if payload.ID <= 0 || strings.TrimSpace(payload.Login) == "" {
		return GitHubUser{}, fmt.Errorf("GitHub user response is invalid")
	}
	return GitHubUser{ID: payload.ID, Login: payload.Login}, nil
}

var _ UserAuthClient = (*goAppClient)(nil)
