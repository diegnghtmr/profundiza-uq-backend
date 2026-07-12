// Package policy adapts the global-settings store to the identity module's
// DomainPolicy port: it reads the runtime-editable institutional email-domain
// allow-list from the "allowed_email_domains" global setting so a super-admin
// can change who may log in without a redeploy.
package policy

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	settingsdomain "github.com/uniquindio/profundiza-uq/internal/settings/domain"
)

// AllowedEmailDomainsKey is the global-settings key holding a JSON array of
// institutional email domains, e.g. ["uniquindio.edu.co"].
const AllowedEmailDomainsKey = "allowed_email_domains"

// SettingReader reads a single global setting by key. Satisfied by the settings
// app Service. ok is false when the key does not exist.
type SettingReader interface {
	Get(ctx context.Context, key string) (settingsdomain.GlobalSetting, bool, error)
}

// DomainPolicy implements identity/app.DomainPolicy over the global-settings
// store. It fails safe: any problem reading or parsing the setting yields
// ok=false so the identity service keeps its static Config default rather than
// falling back to an empty (accept-any) allow-list.
type DomainPolicy struct {
	reader SettingReader
	logger *slog.Logger
}

// NewDomainPolicy builds the adapter over a setting reader.
func NewDomainPolicy(reader SettingReader, logger *slog.Logger) *DomainPolicy {
	return &DomainPolicy{reader: reader, logger: logger}
}

// AllowedDomains returns the runtime allow-list parsed from the setting, or
// ok=false when the setting is absent, unreadable, malformed, or effectively
// empty.
func (p *DomainPolicy) AllowedDomains(ctx context.Context) ([]string, bool) {
	setting, ok, err := p.reader.Get(ctx, AllowedEmailDomainsKey)
	if err != nil {
		p.warn(ctx, "reading allowed_email_domains failed; using config default", err)
		return nil, false
	}
	if !ok {
		return nil, false
	}

	var raw []string
	if err := json.Unmarshal(setting.Value, &raw); err != nil {
		p.warn(ctx, "allowed_email_domains is not a JSON string array; using config default", err)
		return nil, false
	}

	domains := make([]string, 0, len(raw))
	for _, d := range raw {
		if t := strings.TrimSpace(d); t != "" {
			domains = append(domains, t)
		}
	}
	if len(domains) == 0 {
		return nil, false
	}
	return domains, true
}

func (p *DomainPolicy) warn(ctx context.Context, msg string, err error) {
	if p.logger != nil {
		p.logger.WarnContext(ctx, "domain policy: "+msg, "error", err)
	}
}
