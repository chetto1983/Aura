package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestConnectivityTimeout(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		deadline time.Time
		want     time.Duration
	}{
		{name: "no deadline", want: defaultConnectivityTimeout},
		{name: "caller deadline is shorter", deadline: now.Add(500 * time.Millisecond), want: 500 * time.Millisecond},
		{name: "caller deadline is longer", deadline: now.Add(5 * time.Second), want: defaultConnectivityTimeout},
		{name: "expired caller deadline", deadline: now.Add(-time.Second), want: minimumConnectivityTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if !tt.deadline.IsZero() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, tt.deadline)
				defer cancel()
			}
			if got := connectivityTimeout(ctx, now); got != tt.want {
				t.Fatalf("connectivityTimeout() = %s, want %s", got, tt.want)
			}
		})
	}
}
