// Package httpx provides HTTP plumbing shared by all adapters: the canonical
// error envelope, JSON helpers, and middleware.
package httpx

import (
	"encoding/json"
	"net/http"
)

// APIError is the canonical error envelope (TRD §12.4):
//
//	{ "code": "...", "message": "...", "details": {}, "traceId": "..." }
type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	TraceID string         `json:"traceId"`
}

// Common error codes used across the API.
const (
	CodeBadRequest        = "BAD_REQUEST"
	CodeUnauthorized      = "UNAUTHORIZED"
	CodeForbidden         = "FORBIDDEN"
	CodeNotFound          = "NOT_FOUND"
	CodeConflict          = "CONFLICT"
	CodeValidation        = "VALIDATION_ERROR"
	CodeWindowClosed      = "ENROLLMENT_WINDOW_CLOSED"
	CodeMaxElectives      = "MAX_ELECTIVES_REACHED"
	CodeCapacityExceeded  = "CAPACITY_EXCEEDED"
	CodeReasonRequired    = "REASON_REQUIRED"
	CodeInternal          = "INTERNAL_ERROR"
	CodeTooManyRequests   = "TOO_MANY_REQUESTS"
)

// WriteJSON serializes v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError writes the canonical error envelope, stamping the request's trace
// id so clients and logs can be correlated.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, APIError{
		Code:    code,
		Message: message,
		Details: details,
		TraceID: TraceID(r.Context()),
	})
}
