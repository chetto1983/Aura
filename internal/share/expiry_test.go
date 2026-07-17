package share

import (
	"errors"
	"testing"
	"time"
)

// TestResolveExpiry is table-driven over every expiry row of the plan's
// <behavior> block (D-04): default, the three presets, custom within cap,
// custom clamped above cap, and the two rejected non-positive custom cases.
// now is fixed (never time.Now()) so assertions are exact.
func TestResolveExpiry(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	const cap = 90

	tests := []struct {
		name       string
		opt        ExpiryOption
		customDays int
		capDays    int
		want       time.Time
		wantErr    error
	}{
		{
			name:    "default option resolves to 7 days",
			opt:     ExpiryDefault,
			capDays: cap,
			want:    now.AddDate(0, 0, 7),
		},
		{
			name:    "empty option (zero value) resolves to 7 days",
			opt:     "",
			capDays: cap,
			want:    now.AddDate(0, 0, 7),
		},
		{
			name:    "1d resolves to 1 day",
			opt:     Expiry1Day,
			capDays: cap,
			want:    now.AddDate(0, 0, 1),
		},
		{
			name:    "7d resolves to 7 days",
			opt:     Expiry7Days,
			capDays: cap,
			want:    now.AddDate(0, 0, 7),
		},
		{
			name:    "30d resolves to 30 days",
			opt:     Expiry30Days,
			capDays: cap,
			want:    now.AddDate(0, 0, 30),
		},
		{
			name:       "custom within cap resolves exactly",
			opt:        ExpiryCustom,
			customDays: 45,
			capDays:    cap,
			want:       now.AddDate(0, 0, 45),
		},
		{
			name:       "custom at the cap boundary resolves exactly",
			opt:        ExpiryCustom,
			customDays: cap,
			capDays:    cap,
			want:       now.AddDate(0, 0, cap),
		},
		{
			name:       "custom above cap clamps to the cap",
			opt:        ExpiryCustom,
			customDays: cap + 30,
			capDays:    cap,
			want:       now.AddDate(0, 0, cap),
		},
		{
			name:       "custom zero is rejected",
			opt:        ExpiryCustom,
			customDays: 0,
			capDays:    cap,
			wantErr:    ErrNonPositiveCustomExpiry,
		},
		{
			name:       "custom negative is rejected",
			opt:        ExpiryCustom,
			customDays: -5,
			capDays:    cap,
			wantErr:    ErrNonPositiveCustomExpiry,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveExpiry(tc.opt, tc.customDays, now, tc.capDays)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ResolveExpiry() error = %v, want %v", err, tc.wantErr)
				}
				if !got.IsZero() {
					t.Fatalf("ResolveExpiry() on error returned non-zero time %v, want zero", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveExpiry() unexpected error = %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("ResolveExpiry() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveExpiryUnrecognizedOption proves an unrecognized ExpiryOption
// fails closed with an error rather than silently defaulting.
func TestResolveExpiryUnrecognizedOption(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := ResolveExpiry(ExpiryOption("bogus"), 0, now, 90); err == nil {
		t.Fatal("ResolveExpiry() with an unrecognized option succeeded, want error")
	}
}
