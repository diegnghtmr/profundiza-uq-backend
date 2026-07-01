// Package domain holds identity rules: email normalization and the
// institutional-domain policy.
package domain

import "strings"

// NormalizeEmail lowercases and trims an email for consistent lookups.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// IsAllowedDomain reports whether the email belongs to one of the allowed
// institutional domains. An empty allow-list accepts any domain (useful in
// development); production should configure explicit domains.
func IsAllowedDomain(email string, allowedDomains []string) bool {
	if len(allowedDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	for _, d := range allowedDomains {
		if strings.EqualFold(domain, strings.TrimSpace(d)) {
			return true
		}
	}
	return false
}
