package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/settings/domain"
)

func TestUpsertSettingValidate(t *testing.T) {
	tests := []struct {
		name      string
		in        domain.UpsertSetting
		wantField string
	}{
		{"valid", domain.UpsertSetting{Key: "feature.flag", Value: json.RawMessage(`{"enabled":true}`), Reason: "rollout"}, ""},
		{"empty key", domain.UpsertSetting{Value: json.RawMessage(`{}`), Reason: "rollout"}, "settingKey"},
		{"invalid json", domain.UpsertSetting{Key: "k", Value: json.RawMessage(`{bad`), Reason: "rollout"}, "value"},
		{"null value", domain.UpsertSetting{Key: "k", Value: json.RawMessage(`null`), Reason: "rollout"}, "value"},
		{"empty value", domain.UpsertSetting{Key: "k", Reason: "rollout"}, "value"},
		{"short reason", domain.UpsertSetting{Key: "k", Value: json.RawMessage(`1`), Reason: "ab"}, "reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			var ve domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}
