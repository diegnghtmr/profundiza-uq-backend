// Package domain models the catalog read-side: electives, offerings, groups
// (with live seat occupancy) and effective prerequisites.
package domain

import (
	"time"

	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Elective is an academic subject / profundización.
type Elective struct {
	ID          string
	Name        string
	Area        string
	Description string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Group is a concrete offering group with its live occupancy counts.
type Group struct {
	ID                         string
	OfferingID                 string
	GroupCode                  string
	Shift                      shared.AcademicShift
	TeacherName                *string
	ScheduleText               string
	Capacity                   int
	AcceptedCount              int
	PendingDirectCount         int
	WaitlistSameShiftCount     int
	WaitlistOppositeShiftCount int
	Status                     string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// HasOpenSeats reports whether a same-shift student could still take a direct
// slot in this group (used by the onlyOpen filter and student-facing badges).
func (g Group) HasOpenSeats() bool {
	return g.Status == "ACTIVE" && g.AcceptedCount+g.PendingDirectCount < g.Capacity
}

// Prerequisite is an effective requirement shown for an offering (either an
// elective default or an offering-specific one).
type Prerequisite struct {
	ID          string
	OfferingID  string
	Name        string
	Description string
	PlanType    *string
	Source      string
	Status      string
}

// OfferingSummary is the browse-list view of an offering.
type OfferingSummary struct {
	ID         string
	SemesterID string
	Elective   Elective
	Groups     []Group
}

// OfferingDetail is the full view of an offering, including effective
// prerequisites.
type OfferingDetail struct {
	ID            string
	SemesterID    string
	Elective      Elective
	Prerequisites []Prerequisite
	Groups        []Group
}
