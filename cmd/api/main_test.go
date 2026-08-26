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

func TestLoadPaymentConfigAllowsFakeOnlyOutsideProduction(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PAYMENT_PROVIDER", "FAKE")
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "payment-webhook-secret-must-be-at-least-32")
	t.Setenv("PAYMENT_WEBHOOK_REPLAY_WINDOW", "10m")

	configuration, err := loadPaymentConfig()
	if err != nil {
		t.Fatalf("loadPaymentConfig() error = %v", err)
	}
	if configuration.provider != "FAKE" || configuration.replayWindow != 10*time.Minute {
		t.Fatalf("payment config = %#v, want fake provider with 10 minute replay window", configuration)
	}

	t.Setenv("APP_ENV", "production")
	if _, err := loadPaymentConfig(); err == nil {
		t.Fatal("loadPaymentConfig() error = nil, want fake provider rejected in production")
	}
}
