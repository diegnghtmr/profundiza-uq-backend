package notification

import (
	"testing"
	"time"
)

// TestRetryBackoff proves the bounded-retry backoff schedule: it grows
// exponentially per failed attempt and is capped so a row can never be
// scheduled arbitrarily far in the future.
func TestRetryBackoff(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{"first failure", 1, 30 * time.Second},
		{"second failure", 2, 60 * time.Second},
		{"third failure", 3, 120 * time.Second},
		{"fourth failure", 4, 240 * time.Second},
		{"capped for large attempt counts", 20, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retryBackoff(tt.attempt)
			if got != tt.want {
				t.Fatalf("retryBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
			if got > 5*time.Minute {
				t.Fatalf("retryBackoff(%d) = %v exceeds the 5 minute cap", tt.attempt, got)
			}
		})
	}
}
