// Package config loads runtime configuration from environment variables.
// Secrets must never be committed; everything comes from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// knownEnvs are the recognized deployment environments. Anything else is treated
// as a misconfiguration and rejected by Validate (a typo like "dev" must not
// silently fall through to the strict, non-development path unexplained).
var knownEnvs = map[string]bool{
	"development": true,
	"staging":     true,
	"production":  true,
}

// Config holds the resolved runtime settings for the API process.
type Config struct {
	Env            string        // development | staging | production
	HTTPAddr       string        // e.g. ":8080"
	DatabaseURL    string        // pgx connection string
	CookieSecure   bool          // Secure flag on the session cookie
	SessionTTL     time.Duration // session lifetime
	OTPTTL         time.Duration // one-time-code lifetime
	AllowedDomains   []string    // institutional email domains (empty = any)
	SMTPAddr         string      // host:port of the SMTP server (Mailpit locally)
	MailFrom         string      // From address for transactional email
	ReportsOutputDir string      // directory where generated report files are written
}

// IsDevelopment reports whether the process runs in development mode.
func (c Config) IsDevelopment() bool { return c.Env == "development" }

// Validate returns an error when the configuration is unsafe. Call it at startup
// (after Load) and treat any error as fatal:
//
//   - APP_ENV must be a recognized environment; an unknown value is a
//     misconfiguration (and, because it is not "development", would otherwise
//     silently enable the strict path without explanation).
//   - In staging/production an empty AllowedDomains list would allow any email
//     to log in, which is an open-system configuration error.
//   - In staging/production CookieSecure=false would transmit session cookies
//     over plain HTTP, exposing them to interception.
//
// The domain/cookie checks are waived only in development, where the local
// docker-compose stack runs over plain HTTP and does not restrict email domains.
func (c Config) Validate() error {
	var errs []error
	if !knownEnvs[c.Env] {
		errs = append(errs, fmt.Errorf("config: APP_ENV must be one of development, staging, production (got %q)", c.Env))
	}
	if !c.IsDevelopment() {
		if len(c.AllowedDomains) == 0 {
			errs = append(errs, errors.New("config: ALLOWED_EMAIL_DOMAINS must not be empty in non-development environments"))
		}
		if !c.CookieSecure {
			errs = append(errs, errors.New("config: COOKIE_SECURE must be true in non-development environments"))
		}
	}
	return errors.Join(errs...)
}

// Load reads configuration from the environment. Security-sensitive settings
// fail closed: APP_ENV defaults to "production" so a deploy that forgets to set
// it runs the strict path (domain allow-list required, Secure cookies) instead
// of the permissive development one, and COOKIE_SECURE defaults to true. The
// local docker-compose stack opts into development explicitly via APP_ENV.
func Load() Config {
	return Config{
		Env:          getenv("APP_ENV", "production"),
		HTTPAddr:     getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:  getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/profundiza_uq?sslmode=disable"),
		CookieSecure: getbool("COOKIE_SECURE", true),
		SessionTTL:   getdur("SESSION_TTL", 12*time.Hour),
		OTPTTL:       getdur("OTP_TTL", 10*time.Minute),
		AllowedDomains: getcsv("ALLOWED_EMAIL_DOMAINS", nil),
		SMTPAddr:         getenv("SMTP_ADDR", "localhost:1025"),
		MailFrom:         getenv("MAIL_FROM", "no-reply@profundiza-uq.edu.co"),
		ReportsOutputDir: getenv("REPORTS_OUTPUT_DIR", "./reports-output"),
	}
}

func getcsv(key string, fallback []string) []string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	return fallback
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getbool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getdur(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
