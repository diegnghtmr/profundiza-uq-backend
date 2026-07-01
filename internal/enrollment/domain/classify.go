package domain

import shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"

// GroupOccupancy is a point-in-time snapshot of a target offering group, taken
// inside the capacity transaction (after locking the group row). PendingDirect
// counts same-shift requests already holding a direct slot pending review.
type GroupOccupancy struct {
	Capacity          int
	AcceptedCount     int
	PendingDirectCount int
}

// directSlotsTaken returns how many of the group's seats are already committed
// to accepted students or to same-shift requests pending direct review.
func (o GroupOccupancy) directSlotsTaken() int {
	return o.AcceptedCount + o.PendingDirectCount
}

// HasDirectRoom reports whether a same-shift request can still take a direct
// slot without exceeding capacity.
func (o GroupOccupancy) HasDirectRoom() bool {
	return o.directSlotsTaken() < o.Capacity
}

// Classify decides the priority group and the initial status of a NEW request,
// implementing BR-006/007/008/009:
//
//   - Opposite-shift requests are always last priority and go to the
//     opposite-shift waitlist, regardless of available room.
//   - Same-shift requests take a direct review slot while capacity allows;
//     once the direct slots are full they fall to the same-shift waitlist.
//
// Classification never accepts a request — acceptance is a manual admin
// decision. It only positions the request fairly.
func Classify(studentShift, groupShift shared.AcademicShift, occ GroupOccupancy) (PriorityGroup, RequestStatus) {
	if !studentShift.SameAs(groupShift) {
		return PriorityWaitlistOppositeShift, StatusWaitlistOppositeShift
	}
	if occ.HasDirectRoom() {
		return PriorityDirectSameShift, StatusPendingReview
	}
	return PriorityWaitlistSameShift, StatusWaitlistSameShift
}
