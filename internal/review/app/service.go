// Package app holds the administrative review use cases: the prioritized review
// queue and the decision command (accept/reject/cancel/move).
package app

import (
	"context"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
)

// QueueFilter narrows the review queue. SemesterID is required.
type QueueFilter struct {
	SemesterID    string
	OfferingID    string
	GroupID       string
	Status        string
	PriorityGroup string
	Page          int
	PageSize      int
}

// Student is the queue's view of the requesting student.
type Student struct {
	ID             string
	Email          string
	DocumentNumber string
	FullName       string
	Shift          string
	Status         string
	CompletedCount int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Elective is the queue's view of the elective.
type Elective struct {
	ID, Name, Area, Description, Status string
}

// Group is the queue's view of the offering group with live counts.
type Group struct {
	ID                                                            string
	OfferingID, GroupCode, Shift, ScheduleText, Status            string
	TeacherName                                                   *string
	Capacity, AcceptedCount, PendingDirectCount                   int
	WaitlistSameShiftCount, WaitlistOppositeShiftCount            int
	CreatedAt, UpdatedAt                                          time.Time
}

// RequestRow is the queue's view of the enrollment request.
type RequestRow struct {
	ID                 string
	SemesterID         string
	StudentID          string
	OfferingID         string
	OfferingGroupID    string
	EnrollmentWindowID *string
	StudentShift       string
	OfferingShift      string
	PriorityGroup      string
	Status             string
	ArrivalSequence    int64
	SubmittedAt        time.Time
	CancelledAt        *time.Time
	LatestReason       *string
}

// QueueItem is one row of the review queue.
type QueueItem struct {
	Request  RequestRow
	Student  Student
	Elective Elective
	Group    Group
	Warnings []string
}

// DecisionInput is an administrative decision command.
type DecisionInput struct {
	RequestID    string
	AdminUserID  string
	DecisionType domain.DecisionType
	Reason       string
	// TargetGroupID is required only for CREATE_GROUP_ACCEPTANCE: the group the
	// waitlisted student is accepted INTO. It must belong to the same offering as
	// the original request. Ignored for every other decision type.
	TargetGroupID string
}

// Decision is the recorded decision.
type Decision struct {
	ID                  string
	EnrollmentRequestID string
	AdminUserID         string
	DecisionType        string
	PreviousStatus      string
	NewStatus           string
	Reason              string
	CreatedAt           time.Time
}

// DecisionResult bundles the updated request and the recorded decision.
type DecisionResult struct {
	Request  RequestRow
	Decision Decision
}

// Repository is the review persistence boundary.
type Repository interface {
	ListQueue(ctx context.Context, f QueueFilter) ([]QueueItem, int, error)
	Decide(ctx context.Context, in DecisionInput) (DecisionResult, error)
}

// Service exposes the review use cases.
type Service struct{ repo Repository }

// NewService wires the review service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Queue returns the prioritized review queue for a semester.
func (s *Service) Queue(ctx context.Context, f QueueFilter) ([]QueueItem, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}
	return s.repo.ListQueue(ctx, f)
}

// Decide applies an administrative decision (the reason and rule validation are
// enforced in the transaction via the enrollment domain).
func (s *Service) Decide(ctx context.Context, in DecisionInput) (DecisionResult, error) {
	return s.repo.Decide(ctx, in)
}
