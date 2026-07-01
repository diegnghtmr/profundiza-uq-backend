// White-box tests for the IP/UA resolution helper. They live in package audit
// (not audit_test) so they can reach the unexported resolveEventMeta function.
package audit

import (
	"context"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/shared/reqmeta"
)

func TestResolveEventMeta_fillsIPAndUAFromContext(t *testing.T) {
	ctx := reqmeta.WithRequestMeta(context.Background(), "10.0.0.1", "Mozilla/5.0")
	e := resolveEventMeta(ctx, Event{ActorType: ActorStudent})
	if e.IPAddress != "10.0.0.1" {
		t.Fatalf("want IPAddress=10.0.0.1 got %q", e.IPAddress)
	}
	if e.UserAgent != "Mozilla/5.0" {
		t.Fatalf("want UserAgent=Mozilla/5.0 got %q", e.UserAgent)
	}
}

func TestResolveEventMeta_explicitValueWins(t *testing.T) {
	ctx := reqmeta.WithRequestMeta(context.Background(), "10.0.0.1", "Mozilla/5.0")
	e := resolveEventMeta(ctx, Event{IPAddress: "1.2.3.4", UserAgent: "CustomAgent/2"})
	if e.IPAddress != "1.2.3.4" {
		t.Fatalf("want IPAddress=1.2.3.4 got %q", e.IPAddress)
	}
	if e.UserAgent != "CustomAgent/2" {
		t.Fatalf("want UserAgent=CustomAgent/2 got %q", e.UserAgent)
	}
}

func TestResolveEventMeta_noContextMeta_fieldsStayEmpty(t *testing.T) {
	e := resolveEventMeta(context.Background(), Event{})
	if e.IPAddress != "" || e.UserAgent != "" {
		t.Fatalf("want empty fields, got ip=%q ua=%q", e.IPAddress, e.UserAgent)
	}
}

func TestResolveEventMeta_partialExplicit_onlyEmptyFilled(t *testing.T) {
	ctx := reqmeta.WithRequestMeta(context.Background(), "10.0.0.1", "Mozilla/5.0")
	// IPAddress set explicitly; UserAgent left empty — only UserAgent should be filled from ctx.
	e := resolveEventMeta(ctx, Event{IPAddress: "9.9.9.9"})
	if e.IPAddress != "9.9.9.9" {
		t.Fatalf("want IPAddress=9.9.9.9 got %q", e.IPAddress)
	}
	if e.UserAgent != "Mozilla/5.0" {
		t.Fatalf("want UserAgent=Mozilla/5.0 got %q", e.UserAgent)
	}
}
