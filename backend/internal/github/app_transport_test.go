package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

func TestGitHubAppJWTUsesShortLivedAppClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	parsedKey, err := parseGitHubAppPrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	transport := &githubAppTransport{appID: 42, privateKey: parsedKey}
	raw, err := transport.appJWT()
	if err != nil {
		t.Fatalf("sign app JWT: %v", err)
	}
	parsed, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		return &parsedKey.PublicKey, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("parse signed JWT: valid=%v err=%v", parsed.Valid, err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type = %T, want jwt.MapClaims", parsed.Claims)
	}
	if claims["iss"] != "42" {
		t.Fatalf("issuer = %v, want 42", claims["iss"])
	}
	issuedAt, ok := claims["iat"].(float64)
	if !ok {
		t.Fatalf("issued-at claim = %T, want number", claims["iat"])
	}
	expiresAt, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("expiry claim = %T, want number", claims["exp"])
	}
	if time.Duration(expiresAt-issuedAt)*time.Second > 10*time.Minute {
		t.Fatalf("JWT lifetime exceeds GitHub App limit: %v", time.Duration(expiresAt-issuedAt)*time.Second)
	}
}
