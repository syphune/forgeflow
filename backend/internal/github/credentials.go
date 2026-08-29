package github

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
)

func encryptCredential(keyValue, value string) (string, error) {
	key, err := credentialKey(keyValue)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create GitHub credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GitHub credential seal: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate GitHub credential nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptCredential(keyValue, encoded string) (string, error) {
	key, err := credentialKey(keyValue)
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("decode GitHub credential: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create GitHub credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GitHub credential seal: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("GitHub credential is incomplete")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt GitHub credential: %w", err)
	}
	return string(plaintext), nil
}

func credentialKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, userAuthorizationNotConfigured()
	}
	for _, decode := range []func(string) ([]byte, error){
		func(input string) ([]byte, error) { return base64.RawStdEncoding.DecodeString(input) },
		func(input string) ([]byte, error) { return base64.StdEncoding.DecodeString(input) },
		func(input string) ([]byte, error) { return hex.DecodeString(input) },
	} {
		if decoded, err := decode(value); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if len([]byte(value)) == 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("GitHub credential encryption key must decode to 32 bytes")
}

func validateCloneURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() != "github.com" || parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, "GitHub repository clone URL is invalid", nil)
	}
	pathValue := strings.Trim(parsed.Path, "/")
	parts := strings.Split(pathValue, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(pathValue, "..") || strings.ContainsAny(pathValue, "\x00\r\n") {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, "GitHub repository clone URL is invalid", nil)
	}
	return parsed.String(), nil
}

func userAuthorizationNotConfigured() error {
	return apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "GitHub user code authorization is not configured", nil)
}

func userAuthorizationRequired() error {
	return apperr.New(apperr.CodeConflict, http.StatusConflict, "GitHub code authorization is required before cloning", map[string]any{"action": "authorize_github_code"})
}

func codeChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func isNotFound(err error) bool {
	var appErr *apperr.Error
	return errors.As(err, &appErr) && appErr.Code == apperr.CodeNotFound
}
