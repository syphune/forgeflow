package config

import "testing"

func TestLoadRejectsUnsafeProduction(t *testing.T) {
	t.Setenv("FORGEFLOW_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FORGEFLOW_DEV_AUTH", "true")
	t.Setenv("FORGEFLOW_SECURE_COOKIES", "false")
	t.Setenv("FORGEFLOW_WEB_BASE_URL", "https://app.example.com")

	if _, err := Load(); err == nil {
		t.Fatal("expected unsafe production configuration to be rejected")
	}
}

func TestLoadAcceptsProductionDefaultsWithExplicitSecurity(t *testing.T) {
	t.Setenv("FORGEFLOW_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FORGEFLOW_DEV_AUTH", "false")
	t.Setenv("FORGEFLOW_SECURE_COOKIES", "true")
	t.Setenv("FORGEFLOW_WEB_BASE_URL", "https://app.example.com")

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Environment != "production" || loaded.MaxBodyBytes <= 0 {
		t.Fatalf("unexpected config: %#v", loaded)
	}
}

func TestLoadRejectsInsecureProductionWebOrigin(t *testing.T) {
	t.Setenv("FORGEFLOW_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FORGEFLOW_DEV_AUTH", "false")
	t.Setenv("FORGEFLOW_SECURE_COOKIES", "true")
	t.Setenv("FORGEFLOW_WEB_BASE_URL", "http://app.example.com")

	if _, err := Load(); err == nil {
		t.Fatal("expected insecure production web origin to be rejected")
	}
}

func TestLoadIncludesConfiguredWebOrigins(t *testing.T) {
	t.Setenv("FORGEFLOW_WEB_BASE_URL", "http://localhost:13000")
	t.Setenv("FORGEFLOW_ALLOWED_WEB_ORIGINS", "https://forgeflow.example.com, http://localhost:13000")

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.WebAllowedOrigins) != 2 || loaded.WebAllowedOrigins[0] != "http://localhost:13000" || loaded.WebAllowedOrigins[1] != "https://forgeflow.example.com" {
		t.Fatalf("unexpected web origins: %#v", loaded.WebAllowedOrigins)
	}
}

func TestLoadRejectsInvalidConfiguredWebOrigin(t *testing.T) {
	t.Setenv("FORGEFLOW_ALLOWED_WEB_ORIGINS", "https://app.example.com/path")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid web origin to be rejected")
	}
}
