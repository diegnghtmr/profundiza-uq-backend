// Package domain models notifications: delivery state is kept separate from the
// in-app read state (TRD §16.3).
package domain

import "time"

// Notification types (TRD §16.1).
const (
	TypeRequestSubmitted          = "REQUEST_SUBMITTED"
	TypeRequestCancelledByStudent = "REQUEST_CANCELLED_BY_STUDENT"
	TypeRequestAccepted           = "REQUEST_ACCEPTED"
	TypeRequestRejected           = "REQUEST_REJECTED"
	TypeRequestCancelledByAdmin   = "REQUEST_CANCELLED_BY_ADMIN"
	TypeReportReady               = "REPORT_READY"
	TypeReportFailed              = "REPORT_FAILED"
	TypeWindowOpen                = "WINDOW_OPEN"
	TypeWindowClosingSoon         = "WINDOW_CLOSING_SOON"
)

// Channels.
const (
	ChannelEmail = "EMAIL"
	ChannelInApp = "IN_APP"
)

// Delivery statuses.
const (
	DeliveryPending   = "PENDING"
	DeliverySent      = "SENT"
	DeliveryFailed    = "FAILED"
	DeliveryCancelled = "CANCELLED"
)

// Notification is a single message in a channel with its own delivery state.
type Notification struct {
	ID                string
	RecipientUserID   *string
	RecipientEmail    string
	Type              string
	Channel           string
	Subject           string
	Body              string
	DeliveryStatus    string
	RelatedEntityType *string
	RelatedEntityID   *string
	ReadAt            *time.Time
	CreatedAt         time.Time
	SentAt            *time.Time
	FailedAt          *time.Time
	FailureReason     *string
}
