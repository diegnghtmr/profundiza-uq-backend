// Package domain models the student bounded context: students plus their manual
// academic records. It must not depend on HTTP, SQL, or any framework.
package domain

import (
	"strings"
	"time"

	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Status is the lifecycle of a student account.
type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether the status is a recognized value.
func (s Status) Valid() bool { return s == StatusActive || s == StatusInactive }

// MaxCompletedElectives is the administrative cap on the support counter
// (it does not perform automatic prerequisite validation).
const MaxCompletedElectives = 4

// Student is a student record managed by administrators.
type Student struct {
	ID                                  string
	InstitutionalEmail                  string
	DocumentNumber                      string
	FullName                            string
	AcademicShift                       shared.AcademicShift
	Status                              Status
	CompletedProfessionalElectivesCount int
	CreatedAt                           time.Time
	UpdatedAt                           time.Time
}

// AcademicRecord is a manual administrative note attached to a student.
type AcademicRecord struct {
	ID         string
	StudentID  string
	SemesterID *string
	Notes      string
	Source     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ValidationError signals a domain invariant violation tied to a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

// NewStudent holds the validated data needed to create a student.
type NewStudent struct {
	InstitutionalEmail                  string
	DocumentNumber                      string
	FullName                            string
	AcademicShift                       shared.AcademicShift
	CompletedProfessionalElectivesCount int
}

// Validate enforces the create invariants (OpenAPI CreateStudentRequest).
func (n NewStudent) Validate() error {
	if strings.TrimSpace(n.InstitutionalEmail) == "" || !strings.Contains(n.InstitutionalEmail, "@") {
		return ValidationError{Field: "institutionalEmail", Message: "a valid institutional email is required"}
	}
	if strings.TrimSpace(n.DocumentNumber) == "" {
		return ValidationError{Field: "documentNumber", Message: "document number is required"}
	}
	if strings.TrimSpace(n.FullName) == "" {
		return ValidationError{Field: "fullName", Message: "full name is required"}
	}
	if !n.AcademicShift.Valid() {
		return ValidationError{Field: "academicShift", Message: "academic shift must be DAY or NIGHT"}
	}
	if n.CompletedProfessionalElectivesCount < 0 || n.CompletedProfessionalElectivesCount > MaxCompletedElectives {
		return ValidationError{Field: "completedProfessionalElectivesCount", Message: "must be between 0 and 4"}
	}
	return nil
}

// StudentPatch carries the optional fields of an update; nil means "leave as is".
type StudentPatch struct {
	FullName                            *string
	AcademicShift                       *shared.AcademicShift
	Status                              *Status
	CompletedProfessionalElectivesCount *int
}

// Validate enforces the update invariants for the fields that are present.
func (p StudentPatch) Validate() error {
	if p.FullName != nil && strings.TrimSpace(*p.FullName) == "" {
		return ValidationError{Field: "fullName", Message: "full name cannot be empty"}
	}
	if p.AcademicShift != nil && !p.AcademicShift.Valid() {
		return ValidationError{Field: "academicShift", Message: "academic shift must be DAY or NIGHT"}
	}
	if p.Status != nil && !p.Status.Valid() {
		return ValidationError{Field: "status", Message: "status must be ACTIVE or INACTIVE"}
	}
	if p.CompletedProfessionalElectivesCount != nil &&
		(*p.CompletedProfessionalElectivesCount < 0 || *p.CompletedProfessionalElectivesCount > MaxCompletedElectives) {
		return ValidationError{Field: "completedProfessionalElectivesCount", Message: "must be between 0 and 4"}
	}
	return nil
}

// IsEmpty reports whether the patch carries no changes.
func (p StudentPatch) IsEmpty() bool {
	return p.FullName == nil && p.AcademicShift == nil && p.Status == nil &&
		p.CompletedProfessionalElectivesCount == nil
}

// NewAcademicRecord holds the validated data to create a manual academic record.
type NewAcademicRecord struct {
	SemesterID string
	Notes      string
	Source     string
}

// Validate enforces the create invariants (OpenAPI CreateStudentAcademicRecordRequest).
func (n NewAcademicRecord) Validate() error {
	if strings.TrimSpace(n.SemesterID) == "" {
		return ValidationError{Field: "semesterId", Message: "semester id is required"}
	}
	if strings.TrimSpace(n.Notes) == "" {
		return ValidationError{Field: "notes", Message: "notes are required"}
	}
	if strings.TrimSpace(n.Source) == "" {
		return ValidationError{Field: "source", Message: "source is required"}
	}
	return nil
}
