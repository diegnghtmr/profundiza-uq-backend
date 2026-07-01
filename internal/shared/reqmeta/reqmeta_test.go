package reqmeta_test

import (
	"context"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/shared/reqmeta"
)

func TestWithRequestMeta_roundtrip(t *testing.T) {
	ctx := reqmeta.WithRequestMeta(context.Background(), "192.168.1.1", "TestAgent/1.0")
	m := reqmeta.RequestMetaFrom(ctx)
	if m.IPAddress != "192.168.1.1" {
		t.Fatalf("want IPAddress=192.168.1.1 got %q", m.IPAddress)
	}
	if m.UserAgent != "TestAgent/1.0" {
		t.Fatalf("want UserAgent=TestAgent/1.0 got %q", m.UserAgent)
	}
}

func TestRequestMetaFrom_emptyWhenNotSet(t *testing.T) {
	m := reqmeta.RequestMetaFrom(context.Background())
	if m.IPAddress != "" || m.UserAgent != "" {
		t.Fatalf("want zero Meta, got %+v", m)
	}
}
