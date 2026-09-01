package arcadedb

import (
	"testing"
	"time"
)

// TestStringActiveFactKey tests the pure active fact key string conversion
func TestStringActiveFactKey(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		key     string
		validTo time.Time
		want    string
	}{
		{
			name:    "active fact key",
			key:     "key-123",
			validTo: time.Time{}, // zero means still active
			want:    "key-123",
		},
		{
			name:    "active fact key with far future expiry",
			key:     "key-456",
			validTo: now.AddDate(1, 0, 0), // far in the future
			want:    "key-456",
		},
		{
			name:    "expired fact key",
			key:     "key-789",
			validTo: now.AddDate(0, -1, 0), // in the past
			want:    "",
		},
		{
			name:    "empty key",
			key:     "",
			validTo: time.Time{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringActiveFactKey(tt.key, tt.validTo, now)
			if got != tt.want {
				t.Errorf("stringActiveFactKey(%q, %v, %v) = %q, want %q",
					tt.key, tt.validTo, now, got, tt.want)
			}
		})
	}
}
