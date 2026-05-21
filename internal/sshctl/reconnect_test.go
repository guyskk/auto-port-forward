package sshctl

import (
	"testing"
	"time"
)

func TestNextDelay_initialAttemptIsInitial(t *testing.T) {
	p := DefaultBackoff()
	if got := NextDelay(p, 0); got != p.Initial {
		t.Errorf("attempt 0 → %v, want %v", got, p.Initial)
	}
}

func TestNextDelay_doublesEachAttempt(t *testing.T) {
	p := BackoffParams{Initial: 500 * time.Millisecond, Max: 60 * time.Second}
	cases := []struct {
		n    int
		want time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
	}
	for _, c := range cases {
		if got := NextDelay(p, c.n); got != c.want {
			t.Errorf("attempt %d → %v, want %v", c.n, got, c.want)
		}
	}
}

func TestNextDelay_cappedAtMax(t *testing.T) {
	p := BackoffParams{Initial: 500 * time.Millisecond, Max: 60 * time.Second}
	if got := NextDelay(p, 30); got != p.Max {
		t.Errorf("large attempt → %v, want %v", got, p.Max)
	}
}

func TestNextDelay_negativeAttemptReturnsInitial(t *testing.T) {
	p := DefaultBackoff()
	if got := NextDelay(p, -3); got != p.Initial {
		t.Errorf("attempt -3 → %v, want %v", got, p.Initial)
	}
}

func TestIsDegraded(t *testing.T) {
	p := BackoffParams{Degraded: 15 * time.Minute}
	if IsDegraded(p, 5*time.Minute) {
		t.Errorf("5min should not be degraded")
	}
	if !IsDegraded(p, 16*time.Minute) {
		t.Errorf("16min should be degraded")
	}
	if !IsDegraded(p, 15*time.Minute+time.Second) {
		t.Errorf("just past 15min should be degraded")
	}
}
