// Package domain models the semester bounded context.
package domain

import "time"

// Status is the lifecycle of a semester.
type Status string

const (
	StatusDraft  Status = "DRAFT"
	StatusActive Status = "ACTIVE"
	StatusClosed Status = "CLOSED"
)

// Semester is a reusable academic period.
type Semester struct {
	ID        string
	Code      string
	Name      string
	StartsAt  time.Time
	EndsAt    time.Time
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}
