// Package domain models enrollment windows: the periods during which students
// may submit requests.
package domain

import "time"

// Window is one enrollment submission period for a semester.
type Window struct {
	ID          string
	SemesterID  string
	Name        string
	StartsAt    time.Time
	EndsAt      time.Time
	TargetShift *string // nil => all shifts
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
