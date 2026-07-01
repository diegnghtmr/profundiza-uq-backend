package config_test

import (
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/platform/config"
)

// Fix #2 — ALLOWED_EMAIL_DOMAINS validation.

func TestValidate_EmptyDomains_Production_Errors(t *testing.T) {
	cfg := config.Config{Env: "production", CookieSecure: true, AllowedDomains: nil}
	if err := cfg.Validate(); err == nil {
		t.Error("want error: empty AllowedDomains must be rejected in production")
	}
}

func TestValidate_EmptyDomains_Staging_Errors(t *testing.T) {
	cfg := config.Config{Env: "staging", CookieSecure: true, AllowedDomains: nil}
	if err := cfg.Validate(); err == nil {
		t.Error("want error: empty AllowedDomains must be rejected in staging")
	}
}

func TestValidate_EmptyDomains_Development_OK(t *testing.T) {
	cfg := config.Config{Env: "development", CookieSecure: false, AllowedDomains: nil}
	if err := cfg.Validate(); err != nil {
		t.Errorf("want nil error for empty AllowedDomains in development, got %v", err)
	}
}

func TestValidate_NonEmptyDomains_Production_OK(t *testing.T) {
	cfg := config.Config{Env: "production", CookieSecure: true, AllowedDomains: []string{"uniquindio.edu.co"}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("want nil error for non-empty AllowedDomains in production, got %v", err)
	}
}

// Fix #3 — COOKIE_SECURE validation.

func TestValidate_CookieSecureFalse_Production_Errors(t *testing.T) {
	cfg := config.Config{Env: "production", CookieSecure: false, AllowedDomains: []string{"uniquindio.edu.co"}}
	if err := cfg.Validate(); err == nil {
		t.Error("want error: CookieSecure=false must be rejected in production")
	}
}

func TestValidate_CookieSecureFalse_Development_OK(t *testing.T) {
	cfg := config.Config{Env: "development", CookieSecure: false, AllowedDomains: nil}
	if err := cfg.Validate(); err != nil {
		t.Errorf("want nil error for CookieSecure=false in development, got %v", err)
	}
}

func TestValidate_CookieSecureTrue_Production_OK(t *testing.T) {
	cfg := config.Config{Env: "production", CookieSecure: true, AllowedDomains: []string{"uniquindio.edu.co"}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("want nil error for CookieSecure=true in production, got %v", err)
	}
}

// Fail-closed APP_ENV — an unknown or empty environment must be rejected so a
// typo ("dev") never silently runs the process in a partially-configured state.

func TestValidate_UnknownEnv_Errors(t *testing.T) {
	// Otherwise-safe prod config: only the unrecognized env should trip Validate.
	cfg := config.Config{Env: "dev", CookieSecure: true, AllowedDomains: []string{"uniquindio.edu.co"}}
	if err := cfg.Validate(); err == nil {
		t.Error("want error: unrecognized APP_ENV must be rejected")
	}
}

func TestValidate_EmptyEnv_Errors(t *testing.T) {
	cfg := config.Config{Env: "", CookieSecure: true, AllowedDomains: []string{"uniquindio.edu.co"}}
	if err := cfg.Validate(); err == nil {
		t.Error("want error: empty APP_ENV must be rejected")
	}
}
