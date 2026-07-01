package domain_test

import (
	"errors"
	"testing"

	enroll "github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

func TestShiftOpposite(t *testing.T) {
	if got := shared.ShiftDay.Opposite(); got != shared.ShiftNight {
		t.Fatalf("Opposite(DAY) = %q, want NIGHT", got)
	}
	if got := shared.ShiftNight.Opposite(); got != shared.ShiftDay {
		t.Fatalf("Opposite(NIGHT) = %q, want DAY", got)
	}
	if !shared.ShiftDay.SameAs(shared.ShiftDay) || shared.ShiftDay.SameAs(shared.ShiftNight) {
		t.Fatal("SameAs comparison is wrong")
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name         string
		student      shared.AcademicShift
		group        shared.AcademicShift
		occ          enroll.GroupOccupancy
		wantPriority enroll.PriorityGroup
		wantStatus   enroll.RequestStatus
	}{
		{
			name:    "same shift with free seats becomes direct pending review",
			student: shared.ShiftDay, group: shared.ShiftDay,
			occ:          enroll.GroupOccupancy{Capacity: 30, AcceptedCount: 5, PendingDirectCount: 4},
			wantPriority: enroll.PriorityDirectSameShift,
			wantStatus:   enroll.StatusPendingReview,
		},
		{
			name:    "same shift exactly at capacity falls to same-shift waitlist",
			student: shared.ShiftDay, group: shared.ShiftDay,
			occ:          enroll.GroupOccupancy{Capacity: 30, AcceptedCount: 20, PendingDirectCount: 10},
			wantPriority: enroll.PriorityWaitlistSameShift,
			wantStatus:   enroll.StatusWaitlistSameShift,
		},
		{
			name:    "same shift on the last free seat is still direct",
			student: shared.ShiftNight, group: shared.ShiftNight,
			occ:          enroll.GroupOccupancy{Capacity: 1, AcceptedCount: 0, PendingDirectCount: 0},
			wantPriority: enroll.PriorityDirectSameShift,
			wantStatus:   enroll.StatusPendingReview,
		},
		{
			name:    "opposite shift is always last priority even with free seats",
			student: shared.ShiftNight, group: shared.ShiftDay,
			occ:          enroll.GroupOccupancy{Capacity: 30, AcceptedCount: 0, PendingDirectCount: 0},
			wantPriority: enroll.PriorityWaitlistOppositeShift,
			wantStatus:   enroll.StatusWaitlistOppositeShift,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPriority, gotStatus := enroll.Classify(tt.student, tt.group, tt.occ)
			if gotPriority != tt.wantPriority {
				t.Errorf("priority = %q, want %q", gotPriority, tt.wantPriority)
			}
			if gotStatus != tt.wantStatus {
				t.Errorf("status = %q, want %q", gotStatus, tt.wantStatus)
			}
		})
	}
}

func TestValidateSubmissionAllowed(t *testing.T) {
	for active := 0; active < enroll.MaxElectivesPerSemester; active++ {
		if err := enroll.ValidateSubmissionAllowed(active); err != nil {
			t.Errorf("active=%d should be allowed, got %v", active, err)
		}
	}
	if err := enroll.ValidateSubmissionAllowed(enroll.MaxElectivesPerSemester); !errors.Is(err, enroll.ErrMaxElectivesReached) {
		t.Errorf("active=4 should be blocked with ErrMaxElectivesReached, got %v", err)
	}
}

func TestApplyDecision_ReasonRequired(t *testing.T) {
	for _, reason := range []string{"", "  ", "ok"} { // "ok" is below minLength 3
		_, err := enroll.ApplyDecision(enroll.DecisionReject, enroll.DecisionContext{
			CurrentStatus: enroll.StatusPendingReview,
			Reason:        reason,
		})
		if !errors.Is(err, enroll.ErrReasonRequired) {
			t.Errorf("reason %q should be rejected, got %v", reason, err)
		}
	}
}

func TestApplyDecision_AcceptCapacityGuard(t *testing.T) {
	full := enroll.DecisionContext{
		CurrentStatus:      enroll.StatusPendingReview,
		Reason:             "meets all prerequisites",
		GroupCapacity:      10,
		GroupAcceptedCount: 10,
	}
	if _, err := enroll.ApplyDecision(enroll.DecisionAccept, full); !errors.Is(err, enroll.ErrCapacityExceeded) {
		t.Errorf("accept beyond capacity should fail, got %v", err)
	}

	// Fix #5: CAPACITY_ADJUSTMENT_ACCEPTANCE is no longer exempt. The capacity-
	// adjustment endpoint must raise GroupCapacity BEFORE this decision is
	// recorded. If the count is still at the old cap the adjustment was not done.
	if _, err := enroll.ApplyDecision(enroll.DecisionCapacityAdjustmentAcceptance, full); !errors.Is(err, enroll.ErrCapacityExceeded) {
		t.Errorf("CAPACITY_ADJUSTMENT_ACCEPTANCE at full capacity should fail with ErrCapacityExceeded, got %v", err)
	}
}

func TestApplyDecision_MaxAcceptedGuard(t *testing.T) {
	ctx := enroll.DecisionContext{
		CurrentStatus:             enroll.StatusPendingReview,
		Reason:                    "earliest valid request by arrival order",
		GroupCapacity:             10,
		GroupAcceptedCount:        2,
		StudentAcceptedInSemester: enroll.MaxElectivesPerSemester,
	}
	if _, err := enroll.ApplyDecision(enroll.DecisionAccept, ctx); !errors.Is(err, enroll.ErrMaxElectivesReached) {
		t.Errorf("accepting a 5th elective should fail, got %v", err)
	}
}

func TestApplyDecision_StatusTransitions(t *testing.T) {
	base := func() enroll.DecisionContext {
		return enroll.DecisionContext{
			CurrentStatus:      enroll.StatusPendingReview,
			Reason:             "documented administrative reason",
			GroupCapacity:      10,
			GroupAcceptedCount: 0,
		}
	}
	cases := map[enroll.DecisionType]enroll.RequestStatus{
		enroll.DecisionAccept:      enroll.StatusAccepted,
		enroll.DecisionReject:      enroll.StatusRejected,
		enroll.DecisionAdminCancel: enroll.StatusCancelledByAdmin,
		enroll.DecisionMoveToReview: enroll.StatusPendingReview,
	}
	for dt, want := range cases {
		got, err := enroll.ApplyDecision(dt, base())
		if err != nil {
			t.Fatalf("%s: unexpected error %v", dt, err)
		}
		if got != want {
			t.Errorf("%s -> %q, want %q", dt, got, want)
		}
	}
}

func TestApplyDecision_TerminalRequestRejected(t *testing.T) {
	_, err := enroll.ApplyDecision(enroll.DecisionAccept, enroll.DecisionContext{
		CurrentStatus:      enroll.StatusCancelledByStudent,
		Reason:             "trying to revive a cancelled request",
		GroupCapacity:      10,
		GroupAcceptedCount: 0,
	})
	if !errors.Is(err, enroll.ErrAlreadyTerminal) {
		t.Errorf("decisions on terminal requests should fail, got %v", err)
	}
}

// TestApplyDecision_AdminCancelOnAccepted verifies Fix #6: ADMIN_CANCEL must be
// permitted on an ACCEPTED request so admins can free a seat without manual DB
// edits. Before the fix this returned ErrAlreadyTerminal.
func TestApplyDecision_AdminCancelOnAccepted(t *testing.T) {
	got, err := enroll.ApplyDecision(enroll.DecisionAdminCancel, enroll.DecisionContext{
		CurrentStatus: enroll.StatusAccepted,
		Reason:        "student dropped the semester",
	})
	if err != nil {
		t.Fatalf("ADMIN_CANCEL on ACCEPTED should succeed, got %v", err)
	}
	if got != enroll.StatusCancelledByAdmin {
		t.Errorf("status = %q, want CANCELLED_BY_ADMIN", got)
	}
}

// TestApplyDecision_AdminCancelOnSettledRequestsBlocked verifies Fix #6: already-
// cancelled or rejected requests must remain immutable (ErrAlreadyTerminal).
func TestApplyDecision_AdminCancelOnSettledRequestsBlocked(t *testing.T) {
	for _, s := range []enroll.RequestStatus{
		enroll.StatusCancelledByStudent,
		enroll.StatusRejected,
		enroll.StatusCancelledByAdmin,
	} {
		_, err := enroll.ApplyDecision(enroll.DecisionAdminCancel, enroll.DecisionContext{
			CurrentStatus: s,
			Reason:        "some valid reason",
		})
		if !errors.Is(err, enroll.ErrAlreadyTerminal) {
			t.Errorf("ADMIN_CANCEL on %q should return ErrAlreadyTerminal, got %v", s, err)
		}
	}
}

// TestApplyDecision_CapacityAdjustmentAcceptanceEnforcesCapacity verifies Fix #5:
// CAPACITY_ADJUSTMENT_ACCEPTANCE must check GroupAcceptedCount < GroupCapacity at
// decision time. The capacity-adjustment endpoint raises the group's capacity
// BEFORE this decision, so room must genuinely exist; if it doesn't the
// adjustment was not performed and the decision must fail. Before the fix this
// path skipped the capacity check entirely.
func TestApplyDecision_CapacityAdjustmentAcceptanceEnforcesCapacity(t *testing.T) {
	// At-capacity context (GroupAcceptedCount == GroupCapacity).
	full := enroll.DecisionContext{
		CurrentStatus:      enroll.StatusPendingReview,
		Reason:             "capacity was supposedly raised",
		GroupCapacity:      10,
		GroupAcceptedCount: 10,
	}
	if _, err := enroll.ApplyDecision(enroll.DecisionCapacityAdjustmentAcceptance, full); !errors.Is(err, enroll.ErrCapacityExceeded) {
		t.Errorf("CAPACITY_ADJUSTMENT_ACCEPTANCE at full capacity: want ErrCapacityExceeded, got %v", err)
	}

	// Same test for CREATE_GROUP_ACCEPTANCE: the newly created group's own
	// capacity must not be exceeded either.
	if _, err := enroll.ApplyDecision(enroll.DecisionCreateGroupAcceptance, full); !errors.Is(err, enroll.ErrCapacityExceeded) {
		t.Errorf("CREATE_GROUP_ACCEPTANCE at full capacity: want ErrCapacityExceeded, got %v", err)
	}

	// Success path: room exists after the capacity adjustment.
	room := enroll.DecisionContext{
		CurrentStatus:      enroll.StatusPendingReview,
		Reason:             "capacity raised from 10 to 15 via adjustment",
		GroupCapacity:      15,
		GroupAcceptedCount: 10,
	}
	status, err := enroll.ApplyDecision(enroll.DecisionCapacityAdjustmentAcceptance, room)
	if err != nil {
		t.Fatalf("CAPACITY_ADJUSTMENT_ACCEPTANCE with room should succeed, got %v", err)
	}
	if status != enroll.StatusAccepted {
		t.Errorf("status = %q, want ACCEPTED", status)
	}
}

func TestStatusActiveAndTerminal(t *testing.T) {
	active := []enroll.RequestStatus{
		enroll.StatusSubmitted, enroll.StatusPendingReview, enroll.StatusWaitlistSameShift,
		enroll.StatusWaitlistOppositeShift, enroll.StatusAccepted,
	}
	for _, s := range active {
		if !s.IsActive() {
			t.Errorf("%q should be active", s)
		}
	}
	terminal := []enroll.RequestStatus{
		enroll.StatusAccepted, enroll.StatusRejected,
		enroll.StatusCancelledByStudent, enroll.StatusCancelledByAdmin,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	if enroll.StatusCancelledByStudent.IsActive() {
		t.Error("cancelled request must not be active")
	}
}
