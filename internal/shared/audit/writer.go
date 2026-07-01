// Package audit provides an append-only audit writer usable inside a
// transaction, so a state change and its audit record commit atomically
// (BR-012). It is intentionally tiny and dependency-light.
package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniquindio/profundiza-uq/internal/shared/reqmeta"
)

// Execer is satisfied by *pgxpool.Pool and pgx.Tx, letting the writer enlist in
// an ongoing transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Actor types.
const (
	ActorStudent = "STUDENT"
	ActorAdmin   = "ADMIN"
	ActorSystem  = "SYSTEM"
)

// Event is a single audit record.
type Event struct {
	ActorType     string
	ActorID       string // empty => NULL
	Action        string
	EntityType    string
	EntityID      string
	PreviousValue any    // marshaled to JSONB; nil => NULL
	NewValue      any    // marshaled to JSONB; nil => NULL
	Reason        string // empty => NULL
	IPAddress     string // empty => NULL
	UserAgent     string // empty => NULL
}

// resolveEventMeta fills in the IPAddress and UserAgent fields of e from the
// reqmeta stored in ctx when the caller left them empty. Explicitly-set values
// always win over context values.
func resolveEventMeta(ctx context.Context, e Event) Event {
	if e.IPAddress == "" || e.UserAgent == "" {
		m := reqmeta.RequestMetaFrom(ctx)
		if e.IPAddress == "" {
			e.IPAddress = m.IPAddress
		}
		if e.UserAgent == "" {
			e.UserAgent = m.UserAgent
		}
	}
	return e
}

// Write appends an audit event. Pass the same tx used by the mutation so both
// commit together.
//
// When Event.IPAddress or Event.UserAgent are empty, Write falls back to the
// values stored in ctx by the authn middleware (via reqmeta.WithRequestMeta).
// Explicitly-set Event fields always win.
func Write(ctx context.Context, ex Execer, e Event) error {
	e = resolveEventMeta(ctx, e)
	prev, err := toJSON(e.PreviousValue)
	if err != nil {
		return err
	}
	next, err := toJSON(e.NewValue)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx,
		`INSERT INTO audit_events
		   (actor_type, actor_id, action, entity_type, entity_id,
		    previous_value_json, new_value_json, reason, ip_address, user_agent)
		 VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, NULLIF($8,''), NULLIF($9,''), NULLIF($10,''))`,
		e.ActorType, e.ActorID, e.Action, e.EntityType, e.EntityID,
		prev, next, e.Reason, e.IPAddress, e.UserAgent)
	return err
}

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
