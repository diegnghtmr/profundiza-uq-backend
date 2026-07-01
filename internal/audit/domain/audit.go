// Package domain models the read side of the append-only audit trail. Audit
// events are written by every state-changing operation (BR-012, TRD §11.3);
// this module only reads them. It must not depend on HTTP, SQL, or any framework.
package domain

import (
	"encoding/json"
	"time"
)

// Actor types recorded on an audit event.
const (
	ActorStudent = "STUDENT"
	ActorAdmin   = "ADMIN"
	ActorSystem  = "SYSTEM"
)

// AuditEvent is a single immutable audit record.
type AuditEvent struct {
	ID            int64
	ActorType     string
	ActorID       *string
	Action        string
	EntityType    string
	EntityID      *string
	PreviousValue json.RawMessage
	NewValue      json.RawMessage
	Reason        *string
	CreatedAt     time.Time
}
