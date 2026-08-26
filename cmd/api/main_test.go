package main

import (
	"testing"
	"time"
)

func TestLoadAuthenticationConfig(t *testing.T) {
	t.Setenv("AUTH_JWT_SECRET", "01234567890123456789012345678901")
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "30m")
	t.Setenv("AUTH_ADMIN_BOOTSTRAP_TOKEN", "bootstrap-token")

	configuration, err := loadAuthenticationConfig()
	if err != nil {
		t.Fatalf("loadAuthenticationConfig() error = %v", err)
	}
	if configuration.accessTokenTTL != 30*time.Minute {
		t.Fatalf("access token TTL = %s, want 30m", configuration.accessTokenTTL)
	}
	if configuration.adminBootstrapToken != "bootstrap-token" {
		t.Fatalf("bootstrap token = %q, want configured token", configuration.adminBootstrapToken)
	}
}

func TestLoadAuthenticationConfigRejectsShortJWTSecret(t *testing.T) {
	t.Setenv("AUTH_JWT_SECRET", "too-short")
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "")

	_, err := loadAuthenticationConfig()
	if err == nil {
		t.Fatal("loadAuthenticationConfig() error = nil, want short secret error")
	}
}
