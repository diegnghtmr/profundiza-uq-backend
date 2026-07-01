// Package reqmeta stores per-request HTTP metadata (client IP address and
// User-Agent string) in a context.Context so that downstream packages such as
// audit can read them without importing the HTTP or authn layers.
//
// It is intentionally tiny and dependency-free so that both
// internal/platform/authn (writer) and internal/shared/audit (reader) can
// import it without creating an import cycle.
package reqmeta

import "context"

type key struct{}

// Meta carries the HTTP-request metadata propagated via context.
type Meta struct {
	IPAddress string
	UserAgent string
}

// WithRequestMeta returns a derived context that carries ip and ua.
// ip should already have the port stripped.
func WithRequestMeta(ctx context.Context, ip, ua string) context.Context {
	return context.WithValue(ctx, key{}, Meta{IPAddress: ip, UserAgent: ua})
}

// RequestMetaFrom extracts the Meta from ctx. It returns a zero-value Meta
// when the context was not produced by WithRequestMeta.
func RequestMetaFrom(ctx context.Context) Meta {
	if m, ok := ctx.Value(key{}).(Meta); ok {
		return m
	}
	return Meta{}
}
