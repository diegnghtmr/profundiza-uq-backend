package policy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/identity/adapter/policy"
	settingsdomain "github.com/uniquindio/profundiza-uq/internal/settings/domain"
)

type fakeReader struct {
	value json.RawMessage
	ok    bool
	err   error
}

func (f fakeReader) Get(context.Context, string) (settingsdomain.GlobalSetting, bool, error) {
	if f.err != nil {
		return settingsdomain.GlobalSetting{}, false, f.err
	}
	if !f.ok {
		return settingsdomain.GlobalSetting{}, false, nil
	}
	return settingsdomain.GlobalSetting{Key: policy.AllowedEmailDomainsKey, Value: f.value}, true, nil
}

func TestAllowedDomains(t *testing.T) {
	tests := []struct {
		name       string
		reader     fakeReader
		wantOK     bool
		wantDomain []string
	}{
		{"missing setting -> fall back", fakeReader{ok: false}, false, nil},
		{"read error -> fall back", fakeReader{err: errors.New("db down")}, false, nil},
		{"malformed json -> fall back", fakeReader{ok: true, value: json.RawMessage(`{bad`)}, false, nil},
		{"wrong shape (object) -> fall back", fakeReader{ok: true, value: json.RawMessage(`{"a":1}`)}, false, nil},
		{"empty array -> fall back (never accept-any)", fakeReader{ok: true, value: json.RawMessage(`[]`)}, false, nil},
		{"blanks only -> fall back", fakeReader{ok: true, value: json.RawMessage(`["  ",""]`)}, false, nil},
		{"valid -> override", fakeReader{ok: true, value: json.RawMessage(`["uniquindio.edu.co"," example.edu "]`)}, true, []string{"uniquindio.edu.co", "example.edu"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := policy.NewDomainPolicy(tt.reader, nil)
			domains, ok := p.AllowedDomains(context.Background())
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if len(domains) != len(tt.wantDomain) {
				t.Fatalf("domains = %v, want %v", domains, tt.wantDomain)
			}
			for i := range domains {
				if domains[i] != tt.wantDomain[i] {
					t.Fatalf("domains[%d] = %q, want %q", i, domains[i], tt.wantDomain[i])
				}
			}
		})
	}
}
