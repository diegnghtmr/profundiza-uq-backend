// Package system provides the default Clock and Codes adapters backed by the
// standard library's crypto/rand.
package system

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"time"
)

// Clock is the real wall clock.
type Clock struct{}

// Now returns the current time.
func (Clock) Now() time.Time { return time.Now() }

// Codes generates secure one-time codes and opaque session/CSRF tokens.
// Use NewCodes(nil) to obtain an instance backed by crypto/rand (the default).
// Pass a custom io.Reader only in tests to exercise entropy-failure paths.
type Codes struct {
	src io.Reader
}

// NewCodes returns a Codes adapter. src is the entropy source; pass nil to use
// crypto/rand.Reader. This constructor exists so tests can inject a failing
// reader to verify that errors are propagated rather than silently degraded.
func NewCodes(src io.Reader) Codes {
	return Codes{src: src}
}

// reader returns the configured entropy source, defaulting to crypto/rand.Reader.
func (c Codes) reader() io.Reader {
	if c.src != nil {
		return c.src
	}
	return rand.Reader
}

// Generate returns a random 6-digit numeric code or an error if the entropy
// source fails. Callers must treat an error as an unrecoverable condition and
// surface it as an internal server error — never fall back to a fixed string.
func (c Codes) Generate() (string, error) {
	const digits = 6
	max := big.NewInt(1_000_000)
	n, err := rand.Int(c.reader(), max)
	if err != nil {
		return "", fmt.Errorf("codes: entropy failure generating OTP: %w", err)
	}
	s := n.String()
	for len(s) < digits {
		s = "0" + s
	}
	return s, nil
}

// Hash returns a hex-encoded SHA-256 of the code (codes are never stored raw).
// SHA-256 is deterministic and does not use the entropy source.
func (Codes) Hash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// NewSessionID returns a 256-bit opaque session id or an error if entropy fails.
func (c Codes) NewSessionID() (string, error) { return c.randomHex(32) }

// NewCSRFToken returns a 256-bit CSRF token or an error if entropy fails.
func (c Codes) NewCSRFToken() (string, error) { return c.randomHex(32) }

func (c Codes) randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(c.reader(), b); err != nil {
		return "", fmt.Errorf("codes: entropy failure generating random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
