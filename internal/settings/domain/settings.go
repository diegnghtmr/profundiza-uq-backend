// Package domain models the global-settings bounded context. A setting is a
// key with an arbitrary JSON value (stored as JSONB). It must not depend on
// HTTP, SQL, or any framework.
package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// MinReasonLength is the minimum length of the mandatory change reason
// (OpenAPI UpdateGlobalSettingRequest.reason).
const MinReasonLength = 3

// GlobalSetting is a single configuration key with its JSON value.
type GlobalSetting struct {
	Key                  string
	Value                json.RawMessage
	Description          string
	UpdatedByAdminUserID *string
	UpdatedAt            time.Time
}

// ValidationError signals a domain invariant violation tied to a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

// UpsertSetting holds the validated data to create or update a setting.
type UpsertSetting struct {
	Key    string
	Value  json.RawMessage
	Reason string
}

// Validate enforces the upsert invariants. The value must be valid, non-null
// JSON and the reason must be present.
func (u UpsertSetting) Validate() error {
	if strings.TrimSpace(u.Key) == "" {
		return ValidationError{Field: "settingKey", Message: "setting key is required"}
	}
	if len(u.Value) == 0 || !json.Valid(u.Value) {
		return ValidationError{Field: "value", Message: "value must be a valid JSON document"}
	}
	if string(u.Value) == "null" {
		return ValidationError{Field: "value", Message: "value cannot be null"}
	}
	if len(strings.TrimSpace(u.Reason)) < MinReasonLength {
		return ValidationError{Field: "reason", Message: "a reason of at least 3 characters is required"}
	}
	return nil
}
