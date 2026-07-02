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

// TestIsLeaseExpired proves the reaper's core decision: a row claimed
// (flipped to SENDING) more than leaseThreshold ago is eligible to be reset
// back to PENDING, while a row still within its lease — including one
// genuinely in flight in a live worker — is left alone. This is the pure
// piece of the stuck-SENDING reaper that is unit-testable without a
// database; reapStuckSending's SQL itself (UPDATE ... WHERE
// delivery_status='SENDING' AND claimed_at < $1) is integration-only and was
// not exercised here since no DB is available in this environment.
func TestIsLeaseExpired(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		claimedAt time.Time
		want      bool
	}{
		{"just claimed", now, false},
		{"well within the lease (10s, a live send in flight)", now.Add(-10 * time.Second), false},
		{"exactly at the lease threshold (not yet expired)", now.Add(-leaseThreshold), false},
		{"one second past the lease threshold", now.Add(-leaseThreshold - time.Second), true},
		{"long stuck (claimed an hour ago, worker likely crashed)", now.Add(-time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLeaseExpired(tt.claimedAt, now)
			if got != tt.want {
				t.Fatalf("isLeaseExpired(claimedAt=%v, now=%v) = %v, want %v", tt.claimedAt, now, got, tt.want)
			}
		})
	}
}
