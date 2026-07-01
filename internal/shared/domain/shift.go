// Package domain holds value objects shared across bounded contexts.
// It must not depend on HTTP, SQL, or any framework.
package domain

import "fmt"

// AcademicShift is the schedule shift a student belongs to or an offering group runs in.
type AcademicShift string

const (
	ShiftDay   AcademicShift = "DAY"
	ShiftNight AcademicShift = "NIGHT"
)

// Valid reports whether the shift is a recognized value.
func (s AcademicShift) Valid() bool {
	return s == ShiftDay || s == ShiftNight
}

// Opposite returns the other shift. It panics only on an invalid receiver,
// which the caller is expected to guard with Valid first.
func (s AcademicShift) Opposite() AcademicShift {
	switch s {
	case ShiftDay:
		return ShiftNight
	case ShiftNight:
		return ShiftDay
	default:
		panic(fmt.Sprintf("domain: Opposite called on invalid shift %q", s))
	}
}

// SameAs reports whether two shifts match.
func (s AcademicShift) SameAs(other AcademicShift) bool {
	return s == other
}
