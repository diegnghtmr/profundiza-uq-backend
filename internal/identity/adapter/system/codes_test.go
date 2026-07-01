package system_test

import (
	"errors"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/identity/adapter/system"
)

// failReader is an io.Reader that always returns an error, used to simulate
// crypto/rand failure and assert that the Codes methods propagate the error
// rather than returning predictable output.
type failReader struct{}

func (failReader) Read(_ []byte) (int, error) { return 0, errors.New("entropy exhausted") }

// --- RED tests: entropy failure must surface as an error ---

func TestGenerate_EntropyFailure_ReturnsError(t *testing.T) {
	c := system.NewCodes(failReader{})
	_, err := c.Generate()
	if err == nil {
		t.Error("want error from Generate when entropy fails, got nil")
	}
}

func TestNewSessionID_EntropyFailure_ReturnsError(t *testing.T) {
	c := system.NewCodes(failReader{})
	_, err := c.NewSessionID()
	if err == nil {
		t.Error("want error from NewSessionID when entropy fails, got nil")
	}
}

func TestNewCSRFToken_EntropyFailure_ReturnsError(t *testing.T) {
	c := system.NewCodes(failReader{})
	_, err := c.NewCSRFToken()
	if err == nil {
		t.Error("want error from NewCSRFToken when entropy fails, got nil")
	}
}

// --- GREEN tests: normal path must still produce valid output ---

func TestGenerate_NormalPath_Returns6DigitCode(t *testing.T) {
	c := system.NewCodes(nil)
	code, err := c.Generate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("want 6-digit code, got %q (len %d)", code, len(code))
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			t.Errorf("code contains non-digit character: %q", code)
			break
		}
	}
}

func TestNewSessionID_NormalPath_NonZeroHex(t *testing.T) {
	c := system.NewCodes(nil)
	id, err := c.NewSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("session ID must not be empty")
	}
	allZero := true
	for _, ch := range id {
		if ch != '0' {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("session ID must not be all-zero hex (entropy source produced no randomness)")
	}
}

func TestNewCSRFToken_NormalPath_NonZeroHex(t *testing.T) {
	c := system.NewCodes(nil)
	tok, err := c.NewCSRFToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == "" {
		t.Fatal("CSRF token must not be empty")
	}
}

func TestHash_Deterministic(t *testing.T) {
	c := system.NewCodes(nil)
	h1 := c.Hash("123456")
	h2 := c.Hash("123456")
	if h1 != h2 {
		t.Errorf("Hash must be deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("Hash must not be empty")
	}
}
