// Package domain models the administrative-user bounded context. It must not
// depend on HTTP, SQL, or any framework.
package domain

import (
	"strings"
	"time"
)

// Role is the authorization role of an administrative user.
type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RoleSuperAdmin Role = "SUPER_ADMIN"
)

// Valid reports whether the role is a recognized value.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleSuperAdmin }

// Status is the lifecycle of an administrative user.
type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether the status is a recognized value.
func (s Status) Valid() bool { return s == StatusActive || s == StatusInactive }

// AdminUser is an administrative account.
type AdminUser struct {
	ID                 string
	InstitutionalEmail string
	FullName           string
	Role               Role
	Status             Status
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ValidationError signals a domain invariant violation tied to a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

// NewAdminUser holds the validated data to create an administrative user.
type NewAdminUser struct {
	InstitutionalEmail string
	FullName           string
	Role               Role
}

// Validate enforces the create invariants (OpenAPI CreateAdminUserRequest).
func (n NewAdminUser) Validate() error {
	if strings.TrimSpace(n.InstitutionalEmail) == "" || !strings.Contains(n.InstitutionalEmail, "@") {
		return ValidationError{Field: "institutionalEmail", Message: "a valid institutional email is required"}
	}
	if strings.TrimSpace(n.FullName) == "" {
		return ValidationError{Field: "fullName", Message: "full name is required"}
	}
	if !n.Role.Valid() {
		return ValidationError{Field: "role", Message: "role must be ADMIN or SUPER_ADMIN"}
	}
	return nil
}

// AdminUserPatch carries the optional fields of an update; nil means "leave as is".
type AdminUserPatch struct {
	FullName *string
	Role     *Role
	Status   *Status
}

// Validate enforces the update invariants for the fields that are present.
func (p AdminUserPatch) Validate() error {
	if p.FullName != nil && strings.TrimSpace(*p.FullName) == "" {
		return ValidationError{Field: "fullName", Message: "full name cannot be empty"}
	}
	if p.Role != nil && !p.Role.Valid() {
		return ValidationError{Field: "role", Message: "role must be ADMIN or SUPER_ADMIN"}
	}
	if p.Status != nil && !p.Status.Valid() {
		return ValidationError{Field: "status", Message: "status must be ACTIVE or INACTIVE"}
	}
	return nil
}
